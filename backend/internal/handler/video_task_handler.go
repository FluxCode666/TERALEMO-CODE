package handler

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Shared media admission and lookup errors must retain the entry protocol.
const geminiVideoErrorFormatKey = "gemini_video_error_format"

// NativeVideoContent serves an owned task's bytes, never an upstream redirect.
func (h *OpenAIGatewayHandler) NativeVideoContent(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	subject, subjectOK := middleware.GetAuthSubjectFromContext(c)
	if !ok || !subjectOK || key.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	id, err := service.ParseVideoContentResource(c.Param("resource"))
	index, indexErr := strconv.Atoi(c.DefaultQuery("index", "0"))
	if err != nil || indexErr != nil || index < 0 || index > 7 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid video resource or output index")
		return
	}
	protocol, _, _ := strings.Cut(id, ":")
	endpoint := service.MiniMaxVideoEndpointStatus
	switch protocol {
	case "minimax-file":
		protocol, endpoint = service.VideoProtocolMiniMax, service.MiniMaxVideoEndpointFile
	case service.VideoProtocolGemini:
		endpoint = service.GeminiVideoEndpointStatus
	case service.VideoProtocolSeedance:
		endpoint = service.SeedanceEndpointStatus
	}
	// Reuse the normal lookup path for ownership, account policy, concurrency
	// and the completion billing claim. No create request is ever issued here.
	poll, recorder, release := newAsyncImageContext(c, nil, 30*time.Second)
	defer release()
	poll.Request.Method = http.MethodGet
	h.handleGrokMedia(poll, endpoint, id)
	if recorder.Code >= 300 {
		h.errorResponse(c, recorder.Code, "video_unavailable", "Video is unavailable")
		return
	}
	task := &service.AsyncTaskRecord{AsyncTask: service.AsyncTask{APIKeyID: key.ID, GroupID: key.GroupID, Protocol: protocol}, UserID: subject.UserID, RequestID: id}
	if err := h.gatewayService.ForwardNativeVideoContent(c.Request.Context(), c, task, index); err != nil && !c.Writer.Written() {
		h.errorResponse(c, http.StatusBadGateway, "video_download_failed", "Failed to download video")
	}
}

// MiniMaxVideoTasks preserves MiniMax's native create/query/file resources.
func (h *OpenAIGatewayHandler) MiniMaxVideoTasks(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key.Group == nil || key.Group.Platform != service.PlatformVideo {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "MiniMax video requires a video group")
		return
	}
	if c.Request.Method == http.MethodPost {
		h.handleGrokMedia(c, service.MiniMaxVideoEndpointCreate, "")
		return
	}
	field, prefix, endpoint := "task_id", "minimax:", service.MiniMaxVideoEndpointStatus
	if strings.HasSuffix(c.Request.URL.Path, "/files/retrieve") {
		field, prefix, endpoint = "file_id", "minimax-file:", service.MiniMaxVideoEndpointFile
	}
	id := c.Query(field)
	if param := c.Param("task_id"); param != "" && endpoint == service.MiniMaxVideoEndpointStatus {
		id = param
	}
	if err := service.ValidateMiniMaxVideoID(id); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", field+" is required and must be a valid MiniMax ID")
		return
	}
	h.handleGrokMedia(c, endpoint, prefix+id)
}

// GeminiVideoTasks preserves Google's predictLongRunning/Operation protocol.
func (h *OpenAIGatewayHandler) GeminiVideoTasks(c *gin.Context) {
	c.Set(geminiVideoErrorFormatKey, true)
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key.Group == nil || key.Group.Platform != service.PlatformVideo {
		googleError(c, http.StatusForbidden, "Gemini video requires a video group")
		return
	}
	if c.Request.Method == http.MethodGet {
		name := "models/" + c.Param("model") + "/operations/" + c.Param("operation")
		if err := service.ValidateGeminiVideoOperation(name); err != nil {
			googleError(c, http.StatusBadRequest, err.Error())
			return
		}
		h.handleGrokMedia(c, service.GeminiVideoEndpointStatus, "gemini:"+name)
		return
	}
	action := strings.TrimPrefix(c.Param("modelAction"), "/")
	model := strings.TrimSuffix(action, ":predictLongRunning")
	if model == action || strings.TrimSpace(model) == "" || strings.Contains(model, "/") {
		googleError(c, http.StatusBadRequest, "expected models/{model}:predictLongRunning")
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			googleError(c, http.StatusRequestEntityTooLarge, buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		googleError(c, http.StatusBadRequest, "Failed to read request body")
		return
	}
	// Validate before setting the URL model so malformed JSON cannot be repaired
	// into an accepted request. The resource path is authoritative for model.
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		googleError(c, http.StatusBadRequest, "request body must be a JSON object")
		return
	}
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		googleError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	h.handleGrokMedia(c, service.GeminiVideoEndpointCreate, "")
}
