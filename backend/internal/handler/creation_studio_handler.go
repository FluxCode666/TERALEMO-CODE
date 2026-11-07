package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	creationSubmitBodyLimit   int64 = 1 << 20
	creationUploadBodyLimit   int64 = 11 << 20
	creationDispatchTimeout         = 90 * time.Second
	creationDispatchBodyLimit       = 1 << 20
)

// StudioDispatcher replays a request through the real gateway router.
type StudioDispatcher interface {
	Dispatch(w http.ResponseWriter, r *http.Request)
}

// ginStudioDispatcher adapts *gin.Engine to StudioDispatcher.
type ginStudioDispatcher struct {
	engine *gin.Engine
}

func (d ginStudioDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	d.engine.ServeHTTP(w, r)
}

// NewStudioDispatcher wraps the gin engine injected after the gateway routes are
// registered.
func NewStudioDispatcher(engine *gin.Engine) StudioDispatcher {
	return ginStudioDispatcher{engine: engine}
}

// CreationStudioHandler serves the four panel endpoints of the creation studio.
type CreationStudioHandler struct {
	apiKeys    *service.APIKeyService
	gateway    *service.OpenAIGatewayService
	imageTasks *service.ImageTaskService
	dispatcher StudioDispatcher
}

// NewCreationStudioHandler builds the handler from dependencies that already
// exist in the handler wire set (no new provider).
func NewCreationStudioHandler(apiKeys *service.APIKeyService, gateway *service.OpenAIGatewayService, imageTasks *service.ImageTaskService) *CreationStudioHandler {
	return &CreationStudioHandler{apiKeys: apiKeys, gateway: gateway, imageTasks: imageTasks}
}

// SetDispatcher injects the in-process replay target once routes are registered.
func (h *CreationStudioHandler) SetDispatcher(dispatcher StudioDispatcher) {
	if h == nil {
		return
	}
	h.dispatcher = dispatcher
}

// CreationSubmitResponse is the submit response (contract §5.6).
type CreationSubmitResponse struct {
	TaskID        string  `json:"task_id"`
	CatalogID     string  `json:"catalog_id"`
	Mode          string  `json:"mode"`
	Protocol      string  `json:"protocol"`
	Model         string  `json:"model"`
	Status        string  `json:"status"`
	EstimatedCost float64 `json:"estimated_cost"`
	CreatedAt     int64   `json:"created_at"`
	ExpiresAt     int64   `json:"expires_at"`
}

// creationStudioSubject returns the authenticated user or writes a 401.
func creationStudioSubject(c *gin.Context) (middleware2.AuthSubject, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return middleware2.AuthSubject{}, false
	}
	return subject, true
}

// creationStudioRejectReplay blocks recursive entry through the panel endpoints.
func creationStudioRejectReplay(c *gin.Context) bool {
	if !service.IsCreationStudioReplay(c.Request.Context()) {
		return false
	}
	response.ErrorFrom(c, service.ErrCreationInternal)
	return true
}

// Options returns the available modes, models and descriptors of one key.
func (h *CreationStudioHandler) Options(c *gin.Context) {
	if creationStudioRejectReplay(c) {
		return
	}
	subject, ok := creationStudioSubject(c)
	if !ok {
		return
	}
	keyID, err := strconv.ParseInt(strings.TrimSpace(c.Query("api_key_id")), 10, 64)
	if err != nil || keyID <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "api_key_id is required"))
		return
	}
	key, err := h.resolveKey(c.Request.Context(), subject.UserID, keyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	options, err := h.creationOptions(c.Request.Context(), key)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, options)
}

// Estimate prices one form without any persistent side effect.
func (h *CreationStudioHandler) Estimate(c *gin.Context) {
	if creationStudioRejectReplay(c) {
		return
	}
	subject, ok := creationStudioSubject(c)
	if !ok {
		return
	}
	body, err := readCreationBody(c, creationSubmitBodyLimit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var request service.CreationEstimateRequest
	if err := json.Unmarshal(body, &request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "request body is not valid JSON"))
		return
	}
	if !service.ValidCreationMode(request.Mode) {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "mode must be image or video"))
		return
	}
	key, err := h.resolveKey(c.Request.Context(), subject.UserID, request.APIKeyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// Quota and balance class errors outrank model availability: the model is
	// fine, the key simply cannot pay for it.
	if key.IsQuotaExhausted() {
		response.ErrorFrom(c, service.ErrAPIKeyQuotaExhausted)
		return
	}
	options, err := h.creationOptions(c.Request.Context(), key)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	option, ok := creationStudioFindOption(options, request.Mode, request.Protocol, request.Model)
	if !ok {
		response.ErrorFrom(c, service.ErrCreationModelNotAvailable)
		return
	}
	estimate, err := h.gateway.EstimateCreation(c.Request.Context(), key, request, option, 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, estimate)
}

// Upload stores one reference image and returns its id and link.
func (h *CreationStudioHandler) Upload(c *gin.Context) {
	if creationStudioRejectReplay(c) {
		return
	}
	subject, ok := creationStudioSubject(c)
	if !ok {
		return
	}
	if h.imageTasks == nil || !h.imageTasks.Enabled() {
		response.ErrorFrom(c, service.ErrCreationStorageDisabled)
		return
	}
	if err := c.Request.ParseMultipartForm(creationUploadBodyLimit); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.ErrorFrom(c, service.ErrCreationUploadTooLarge.WithMetadata(map[string]string{
				"max_bytes": strconv.FormatInt(service.CreationUploadMaxBytes, 10),
			}))
			return
		}
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "request body must be multipart/form-data"))
		return
	}
	headers := c.Request.MultipartForm.File["file"]
	if len(headers) != 1 {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "exactly one file field is required"))
		return
	}
	file, err := headers[0].Open()
	if err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "uploaded file cannot be read"))
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, service.CreationUploadMaxBytes+1))
	if err != nil {
		response.ErrorFrom(c, service.ErrCreationUploadStorageFailed.WithCause(err))
		return
	}
	if int64(len(data)) > service.CreationUploadMaxBytes {
		response.ErrorFrom(c, service.ErrCreationUploadTooLarge.WithMetadata(map[string]string{
			"max_bytes": strconv.FormatInt(service.CreationUploadMaxBytes, 10),
		}))
		return
	}
	record, err := h.imageTasks.CreateCreationUpload(c.Request.Context(), subject.UserID, data)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, record.Public())
}

// Submit validates the unified form, builds the native request and replays it.
func (h *CreationStudioHandler) Submit(c *gin.Context) {
	if creationStudioRejectReplay(c) {
		return
	}
	subject, ok := creationStudioSubject(c)
	if !ok {
		return
	}
	if h == nil {
		response.ErrorFrom(c, service.ErrCreationInternal)
		return
	}
	// V1 first: the body limit is part of the request contract, while the
	// dispatcher is an assembly concern.
	body, err := readCreationBody(c, creationSubmitBodyLimit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.dispatcher == nil {
		response.ErrorFrom(c, service.ErrCreationDispatcherUnavailable)
		return
	}
	var request service.CreationSubmitRequest
	if err := json.Unmarshal(body, &request); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "request body is not valid JSON"))
		return
	}
	if !service.ValidCreationMode(request.Mode) {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "mode must be image or video"))
		return
	}
	key, err := h.resolveKey(c.Request.Context(), subject.UserID, request.APIKeyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// Quota outranks model availability: the model is available, the key simply
	// cannot pay for it, so answer 429 instead of CREATION_MODEL_NOT_AVAILABLE.
	if key.IsQuotaExhausted() {
		response.ErrorFrom(c, service.ErrAPIKeyQuotaExhausted)
		return
	}
	options, err := h.creationOptions(c.Request.Context(), key)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	option, ok := creationStudioFindOption(options, request.Mode, request.Protocol, request.Model)
	if !ok {
		if request.Mode == service.CreationModeImage && options.Image.Reason == service.CreationUnavailableStorageDisabled {
			response.ErrorFrom(c, service.ErrCreationAsyncImageDisabled)
			return
		}
		response.ErrorFrom(c, service.ErrCreationModelNotAvailable)
		return
	}
	if err := service.ValidateCreationSubmit(&request, option); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	storageEnabled := h.imageTasks != nil && h.imageTasks.Enabled()
	if request.Mode == service.CreationModeImage && !storageEnabled {
		response.ErrorFrom(c, service.ErrCreationAsyncImageDisabled)
		return
	}
	if len(request.Images) > 0 && !storageEnabled {
		response.ErrorFrom(c, service.ErrCreationStorageDisabled)
		return
	}
	params, err := service.ResolveCreationParams(option, request.Params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	assets, err := h.resolveCreationImages(c.Request.Context(), subject.UserID, &request, storageEnabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	native, err := service.BuildCreationNativeRequest(service.CreationNativeRequestInput{
		Mode: request.Mode, Protocol: request.Protocol, Model: request.Model,
		Prompt: strings.TrimSpace(request.Prompt), Params: params, Images: assets,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := h.dispatch(c, key, native)
	if result.err != nil {
		response.ErrorFrom(c, result.err)
		return
	}
	if result.status < http.StatusOK || result.status >= http.StatusMultipleChoices {
		normalized := creationStudioNormalizeError(result.status, result.header, result.body)
		response.ErrorWithDetails(c, normalized.Status, normalized.Message, normalized.Reason, normalized.Metadata)
		return
	}
	taskID, requestID, ok := creationStudioParseTaskIdentity(request.Protocol, result.body)
	if !ok {
		response.ErrorWithDetails(c, http.StatusBadGateway, "upstream returned no task identifier", "upstream_error", nil)
		return
	}
	createdAt := time.Now().Unix()
	catalogID := taskID
	if request.Mode == service.CreationModeVideo {
		catalogID = service.AsyncVideoTaskCatalogID(subject.UserID, key.ID, requestID)
	}
	expiresAt := createdAt + int64((24 * time.Hour).Seconds())
	if h.imageTasks != nil {
		if record, recordErr := h.imageTasks.GetUserTask(c.Request.Context(), subject.UserID, catalogID); recordErr == nil {
			expiresAt = record.ExpiresAt
			if record.TaskID != "" && request.Mode == service.CreationModeVideo {
				taskID = record.TaskID
			}
		} else if request.Mode == service.CreationModeVideo {
			// The catalog record is written asynchronously; keep the response usable
			// by falling back to the search-based lookup on the panel.
			catalogID = ""
		}
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, CreationSubmitResponse{
		TaskID:        taskID,
		CatalogID:     catalogID,
		Mode:          request.Mode,
		Protocol:      request.Protocol,
		Model:         request.Model,
		Status:        "processing",
		EstimatedCost: h.creationEstimatedCost(c.Request.Context(), key, request, option, params, native, requestID),
		CreatedAt:     createdAt,
		ExpiresAt:     expiresAt,
	})
}

// creationOptions loads the option catalog of one key.
func (h *CreationStudioHandler) creationOptions(ctx context.Context, key *service.APIKey) (*service.CreationOptionsResponse, error) {
	if h == nil || h.gateway == nil {
		return nil, service.ErrCreationEstimateUnavailable
	}
	return h.gateway.CreationOptions(ctx, key, h.imageTasks)
}

// resolveKey enforces key ownership: a key of another user is reported as missing.
func (h *CreationStudioHandler) resolveKey(ctx context.Context, userID, keyID int64) (*service.APIKey, error) {
	if h == nil || h.apiKeys == nil || keyID <= 0 {
		return nil, service.ErrCreationKeyNotFound
	}
	key, err := h.apiKeys.GetByID(ctx, keyID)
	if err != nil || key == nil || key.UserID != userID {
		return nil, service.ErrCreationKeyNotFound
	}
	hydrated, err := h.apiKeys.GetByKey(ctx, key.Key)
	if err != nil || hydrated == nil || hydrated.ID != key.ID || hydrated.UserID != userID {
		return nil, service.ErrCreationKeyNotFound
	}
	return hydrated, nil
}

// resolveCreationImages loads every referenced upload and, for the protocols that
// need bytes inline, reads them back from object storage (never over HTTP).
func (h *CreationStudioHandler) resolveCreationImages(ctx context.Context, userID int64, request *service.CreationSubmitRequest, storageEnabled bool) (map[string][]service.CreationImageAsset, error) {
	assets := make(map[string][]service.CreationImageAsset, len(request.Images))
	if len(request.Images) == 0 {
		return assets, nil
	}
	if !storageEnabled || h.imageTasks == nil {
		return nil, service.ErrCreationStorageDisabled
	}
	needsInlineBytes := request.Protocol == service.CreationProtocolGemini || request.Mode == service.CreationModeImage
	uploader, _ := h.imageTasks.CurrentUploader()
	now := time.Now()
	for _, image := range request.Images {
		record, err := h.imageTasks.ResolveCreationUpload(ctx, userID, image.UploadID, now)
		if err != nil {
			return nil, err
		}
		asset := service.CreationImageAsset{URL: record.URL, ContentType: record.ContentType}
		if needsInlineBytes {
			if uploader == nil {
				return nil, service.ErrCreationUploadStorageFailed
			}
			data, contentType, readErr := uploader.ReadObject(ctx, record.ObjectKey, service.CreationUploadMaxBytes)
			if readErr != nil {
				return nil, service.ErrCreationUploadStorageFailed.WithCause(readErr)
			}
			asset.Data = data
			if strings.TrimSpace(contentType) != "" {
				asset.ContentType = contentType
			}
		}
		role := strings.TrimSpace(image.Role)
		assets[role] = append(assets[role], asset)
	}
	return assets, nil
}

// creationStudioDispatchResult is one replayed response.
type creationStudioDispatchResult struct {
	status int
	header http.Header
	body   []byte
	err    error
}

// dispatch replays the native request in-process as the selected key.
func (h *CreationStudioHandler) dispatch(c *gin.Context, key *service.APIKey, native *service.NativeCreationRequest) creationStudioDispatchResult {
	outer := c.Request
	base := service.WithCreationStudioReplay(context.WithoutCancel(outer.Context()))
	ctx, cancel := context.WithTimeout(base, creationDispatchTimeout)
	defer cancel()

	inner := httptest.NewRequest(http.MethodPost, native.Path, bytes.NewReader(native.Body))
	inner = inner.WithContext(ctx)
	inner.RemoteAddr = outer.RemoteAddr
	inner.Header.Set("Authorization", "Bearer "+key.Key)
	inner.Header.Set("Content-Type", "application/json")
	for _, header := range []string{"User-Agent", "X-Forwarded-For", "X-Real-IP", "X-Request-ID"} {
		if value := strings.TrimSpace(outer.Header.Get(header)); value != "" {
			inner.Header.Set(header, value)
		}
	}
	recorder := httptest.NewRecorder()
	h.dispatcher.Dispatch(recorder, inner)

	result := creationStudioDispatchResult{status: recorder.Code, header: recorder.Header()}
	if result.status == 0 {
		result.status = http.StatusOK
	}
	body := recorder.Body.Bytes()
	if len(body) > creationDispatchBodyLimit {
		body = body[:creationDispatchBodyLimit]
	}
	result.body = body
	return result
}

// creationEstimatedCost returns the same amount the admission gate used: the
// image estimate for image submissions, the frozen hold for video.
func (h *CreationStudioHandler) creationEstimatedCost(ctx context.Context, key *service.APIKey, request service.CreationSubmitRequest, option service.CreationModelOption, params map[string]any, native *service.NativeCreationRequest, requestID string) float64 {
	if h == nil || h.gateway == nil {
		return 0
	}
	if request.Mode == service.CreationModeImage {
		sizeKey := "size"
		if request.Protocol == service.CreationProtocolGrok {
			sizeKey = "resolution"
		}
		size, _ := params[sizeKey].(string)
		count, ok := params["n"].(int)
		if !ok {
			count = 1
		}
		estimate := h.gateway.EstimateCreationImageCost(ctx, key, key.UserID, option.Model, size, count, len(native.Body))
		return estimate.Amount
	}
	protocol := request.Protocol
	hold := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(requestID, "seedance:"), "gemini:"), "minimax:")
	if reservation, err := h.gateway.FindVideoTaskReservation(ctx, protocol, hold); err == nil && reservation != nil {
		return reservation.HoldAmount
	}
	// Fall back to the estimate's own contract when the reservation cannot be read.
	request.Params = params
	if estimate, err := h.gateway.EstimateCreation(ctx, key, service.CreationEstimateRequest{
		APIKeyID: key.ID, Mode: request.Mode, Model: request.Model, Protocol: request.Protocol, Params: params,
	}, option, 0); err == nil {
		return estimate.EstimatedCost
	}
	return 0
}

// creationStudioFindOption locates the (mode, protocol, model) row.
func creationStudioFindOption(options *service.CreationOptionsResponse, mode, protocol, model string) (service.CreationModelOption, bool) {
	if options == nil {
		return service.CreationModelOption{}, false
	}
	for _, option := range options.Models {
		if option.Mode == mode && option.Protocol == protocol && option.Model == model {
			return option, true
		}
	}
	return service.CreationModelOption{}, false
}

// creationStudioParseTaskIdentity extracts the native task id and the request id
// used for the catalog hash (contract §5.5).
func creationStudioParseTaskIdentity(protocol string, body []byte) (taskID, requestID string, ok bool) {
	root := gjson.ParseBytes(body)
	switch protocol {
	case service.CreationProtocolSeedance:
		taskID = strings.TrimSpace(root.Get("id").String())
		if taskID == "" {
			return "", "", false
		}
		return taskID, "seedance:" + taskID, true
	case service.CreationProtocolGemini:
		name := strings.TrimSpace(root.Get("name").String())
		if name == "" {
			return "", "", false
		}
		return name, "gemini:" + name, true
	case service.CreationProtocolMiniMax:
		taskID = strings.TrimSpace(root.Get("task_id").String())
		if taskID == "" {
			taskID = strings.TrimSpace(root.Get("task.id").String())
		}
		if taskID == "" {
			return "", "", false
		}
		return taskID, "minimax:" + taskID, true
	case service.CreationProtocolGrok:
		taskID = strings.TrimSpace(root.Get("request_id").String())
		if taskID == "" {
			taskID = strings.TrimSpace(root.Get("id").String())
		}
		if taskID == "" {
			return "", "", false
		}
		return taskID, taskID, true
	default:
		taskID = strings.TrimSpace(root.Get("id").String())
		if taskID == "" {
			return "", "", false
		}
		return taskID, taskID, true
	}
}

// readCreationBody reads at most limit bytes, mapping an overflow to the
// contract's 413 CREATION_BODY_TOO_LARGE.
func readCreationBody(c *gin.Context, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, service.ErrCreationBodyTooLarge.WithMetadata(map[string]string{
				"max_bytes": strconv.FormatInt(limit, 10),
			})
		}
		return nil, infraerrors.BadRequest("INVALID_CREATION_REQUEST", "request body cannot be read")
	}
	if int64(len(body)) > limit {
		return nil, service.ErrCreationBodyTooLarge.WithMetadata(map[string]string{
			"max_bytes": strconv.FormatInt(limit, 10),
		})
	}
	return body, nil
}
