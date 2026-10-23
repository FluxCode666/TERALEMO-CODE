package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	MiniMaxVideoEndpointCreate           GrokMediaEndpoint        = "minimax_video_create"
	MiniMaxVideoEndpointStatus           GrokMediaEndpoint        = "minimax_video_status"
	MiniMaxVideoEndpointFile             GrokMediaEndpoint        = "minimax_video_file"
	OpenAIEndpointCapabilityMiniMaxVideo OpenAIEndpointCapability = "minimax_video"
)

func (e GrokMediaEndpoint) IsMiniMaxVideo() bool {
	return e == MiniMaxVideoEndpointCreate || e == MiniMaxVideoEndpointStatus || e == MiniMaxVideoEndpointFile
}

func ValidateMiniMaxVideoID(id string) error {
	if len(id) == 0 || len(id) > 128 {
		return fmt.Errorf("invalid MiniMax task or file ID")
	}
	for _, ch := range id {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_' || ch == '-') {
			return fmt.Errorf("invalid MiniMax task or file ID")
		}
	}
	return nil
}

func ParseMiniMaxVideoRequest(body []byte) (GrokMediaRequestInfo, error) {
	return parseMiniMaxVideoRequestWithLimits(body, nil)
}

func parseMiniMaxVideoRequestWithLimits(body []byte, limits *videoModelLimits) (GrokMediaRequestInfo, error) {
	var info GrokMediaRequestInfo
	if err := ValidateVideoSubmissionJSON(body); err != nil {
		return info, err
	}
	if err := RejectUnsupportedVideoBillingFields(body, VideoProtocolMiniMax); err != nil {
		return info, err
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if info.Model == "" {
		return info, fmt.Errorf("model is required")
	}
	info.N = 1 // Native MiniMax create responses represent one video task.
	if gjson.GetBytes(body, "content").Exists() || miniMaxH3Model(info.Model) {
		prepared, err := prepareMiniMaxVideoV2(body)
		if err != nil {
			return info, err
		}
		return parseMiniMaxVideoV2WithLimits(prepared, info, limits)
	}
	info.Prompt = gjson.GetBytes(body, "prompt").String()
	for _, field := range []string{"first_frame_image", "last_frame_image"} {
		if image := gjson.GetBytes(body, field).String(); image != "" {
			info.InputImageURLs = append(info.InputImageURLs, image)
		}
	}
	for _, subject := range gjson.GetBytes(body, "subject_reference").Array() {
		for _, image := range subject.Get("image").Array() {
			info.InputImageURLs = append(info.InputImageURLs, image.String())
		}
	}
	if strings.TrimSpace(info.Prompt) == "" && len(info.InputImageURLs) == 0 {
		return info, fmt.Errorf("prompt or an input image is required")
	}
	duration := gjson.GetBytes(body, "duration")
	info.DurationSeconds = 6
	if limits != nil {
		seconds, err := geminiVideoInteger(duration, "MiniMax duration", 6, 1, 60)
		if err != nil {
			return info, err
		}
		resolution := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "resolution").String()))
		if resolution != "" && resolution != "512p" && !slices.Contains(videoCapabilityResolutionOrder, resolution) {
			return info, fmt.Errorf("MiniMax resolution must be 480P, 512P, 720P, 768P, 1080P or 2K")
		}
		if resolution == "512p" {
			resolution = VideoBillingResolution480P
		}
		explicit := resolution != ""
		if !explicit {
			resolution = VideoBillingResolution768P
		}
		info.Resolution, info.DurationSeconds, err = limits.normalize(resolution, seconds, explicit, duration.Exists())
		info.DurationSecondsExact = float64(info.DurationSeconds)
		return info, err
	}
	if duration.Exists() {
		if duration.Type != gjson.Number || duration.Float() != float64(duration.Int()) || duration.Int() != 6 && duration.Int() != 10 {
			return info, fmt.Errorf("MiniMax duration must be 6 or 10 seconds")
		}
		info.DurationSeconds = int(duration.Int())
	}
	// Pin the default in the forwarded body too: aliases may map to models with
	// different native defaults, so billing must not infer the generated size.
	resolution := strings.ToUpper(strings.TrimSpace(gjson.GetBytes(body, "resolution").String()))
	if resolution == "" {
		resolution = "768P"
	}
	switch resolution {
	case "512P":
		info.Resolution = VideoBillingResolution480P
	case "720P":
		info.Resolution = VideoBillingResolution720P
	case "768P":
		info.Resolution = VideoBillingResolution768P
	case "1080P":
		info.Resolution = VideoBillingResolution1080P
	case "2K":
		info.Resolution = VideoBillingResolution2K
	default:
		return info, fmt.Errorf("MiniMax resolution must be 512P, 720P, 768P, 1080P or 2K")
	}
	return info, nil
}

func buildMiniMaxVideoURL(base string, endpoint GrokMediaEndpoint, id string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	explicitEndpoint := false
	for _, suffix := range []string{"/query/video_generation", "/files/retrieve", "/video_generation"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			explicitEndpoint = true
			break
		}
	}
	// Compatible proxies may expose /api/minimax/v2. Preserve the configured
	// version or full endpoint; origins and unversioned bases default to /v1.
	if !explicitEndpoint && !openAIBaseURLHasVersionSuffix(base) {
		base += "/v1"
	}
	switch endpoint {
	case MiniMaxVideoEndpointCreate:
		return base + "/video_generation", nil
	case MiniMaxVideoEndpointStatus, MiniMaxVideoEndpointFile:
		if err := ValidateMiniMaxVideoID(id); err != nil {
			return "", err
		}
		if endpoint == MiniMaxVideoEndpointFile {
			if miniMaxVideoV2(base) {
				return "", fmt.Errorf("MiniMax v2 returns the video URL in the task; file retrieval is a v1 endpoint")
			}
			return base + "/files/retrieve?file_id=" + url.QueryEscape(id), nil
		}
		if miniMaxVideoV2(base) {
			return base + "/query/video_generation/" + url.PathEscape(id), nil
		}
		return base + "/query/video_generation?task_id=" + url.QueryEscape(id), nil
	default:
		return "", fmt.Errorf("unsupported MiniMax video endpoint")
	}
}

// miniMaxVideoRequest performs exactly one upstream request. Creates are never
// retried, including HTTP 200 responses carrying a nonzero business status.
func (s *OpenAIGatewayService) miniMaxVideoRequest(ctx context.Context, account *Account, endpoint GrokMediaEndpoint, id string, body []byte) (*http.Response, []byte, error) {
	base, err := s.validateUpstreamBaseURL(account.VideoBaseURL())
	if err != nil {
		return nil, nil, err
	}
	target, err := buildMiniMaxVideoURL(base, endpoint, id)
	if err != nil {
		return nil, nil, err
	}
	method := http.MethodGet
	if endpoint == MiniMaxVideoEndpointCreate {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), method, target, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.doVideoUpstreamRequest(req, proxy, account.ID, account.Concurrency)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// This adapter is also used by admin SSE tests without a gateway context.
	// Let callers report read failures rather than invoking a JSON writer on nil.
	responseBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, nil, nil)
	if err != nil {
		return resp, nil, err
	}
	if resp.StatusCode >= 300 {
		return resp, responseBody, fmt.Errorf("MiniMax upstream status %d", resp.StatusCode)
	}
	if !gjson.ValidBytes(responseBody) || !gjson.ParseBytes(responseBody).IsObject() {
		return resp, responseBody, fmt.Errorf("invalid MiniMax response")
	}
	if miniMaxVideoV2(base) {
		if gjson.GetBytes(responseBody, "type").String() == "error" || gjson.GetBytes(responseBody, "error").Exists() {
			return resp, responseBody, fmt.Errorf("MiniMax upstream error")
		}
		// Endpoint-specific validation below rejects missing IDs, task lists and
		// incomplete results. V2 has no base_resp on successful responses.
		return resp, responseBody, nil
	}
	code := gjson.GetBytes(responseBody, "base_resp.status_code")
	if !code.Exists() || code.Type != gjson.Number || code.Float() != float64(code.Int()) {
		return resp, responseBody, fmt.Errorf("invalid MiniMax response")
	}
	if code.Int() != 0 && !(endpoint == MiniMaxVideoEndpointStatus && strings.EqualFold(gjson.GetBytes(responseBody, "status").String(), "Fail")) {
		return resp, responseBody, fmt.Errorf("MiniMax upstream business status %d", code.Int())
	}
	return resp, responseBody, nil
}

// File retrieval is nested inside status polling and is also called directly by
// content downloads. Keep response metadata without coupling it to an HTTP writer.
type miniMaxVideoResponseError struct {
	response *http.Response
	body     []byte
	err      error
}

func (e *miniMaxVideoResponseError) Error() string { return e.err.Error() }
func (e *miniMaxVideoResponseError) Unwrap() error { return e.err }

func validMiniMaxVideoRetryAfter(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			_, err := http.ParseTime(value)
			return err == nil
		}
	}
	return true
}

func (s *OpenAIGatewayService) miniMaxVideoFile(ctx context.Context, account *Account, id string) (string, error) {
	resp, body, err := s.miniMaxVideoRequest(ctx, account, MiniMaxVideoEndpointFile, id, nil)
	if err != nil {
		return "", &miniMaxVideoResponseError{response: resp, body: body, err: err}
	}
	if gjson.GetBytes(body, "file.file_id").String() != id {
		return "", fmt.Errorf("MiniMax returned a different file ID")
	}
	link := gjson.GetBytes(body, "file.download_url").String()
	if strings.TrimSpace(link) == "" {
		return "", fmt.Errorf("MiniMax file response missing download_url")
	}
	return link, nil
}

func (s *OpenAIGatewayService) writeMiniMaxVideoError(c *gin.Context, account *Account, resp *http.Response, body []byte, taskID string) {
	if c == nil || resp == nil {
		return
	}
	copy := *resp
	resp = &copy
	if resp.StatusCode >= 400 && resp.StatusCode <= 599 && !gjson.ValidBytes(body) {
		// Proxies can return HTML/plain text diagnostics containing signed URLs.
		// Preserve the HTTP error and retry boundary, but never relay that body.
		safeHeaders := make(http.Header)
		writeOpenAIPassthroughErrorHeaders(safeHeaders, resp.Header)
		if retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After")); validMiniMaxVideoRetryAfter(retryAfter) {
			// HTTP-date remains meaningful when it is already in the past: the
			// upstream explicitly permits an immediate retry.
			safeHeaders.Set("Retry-After", retryAfter)
		}
		resp.Header = safeHeaders
		body = []byte(`{"error":{"type":"upstream_error","message":"MiniMax upstream request failed"}}`)
		// The normal response header filter need not allow Retry-After.
		writeOpenAIPassthroughErrorHeaders(c.Writer.Header(), resp.Header)
		c.Header("Retry-After", resp.Header.Get("Retry-After"))
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return
	}
	if resp.StatusCode < 300 {
		resp.StatusCode = http.StatusBadGateway
		if code := gjson.GetBytes(body, "base_resp.status_code").Int(); code == 1002 || code == 1039 {
			resp.StatusCode = http.StatusTooManyRequests
		}
	}
	s.writeNativeVideoResponse(c, account, resp, body, taskID)
}

func (s *OpenAIGatewayService) ForwardMiniMaxVideo(ctx context.Context, c *gin.Context, account *Account, endpoint GrokMediaEndpoint, taskID string, body []byte) (*OpenAIForwardResult, error) {
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityMiniMaxVideo) || !endpoint.IsMiniMaxVideo() {
		return nil, fmt.Errorf("MiniMax video requires a video account using the MiniMax protocol")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result := &OpenAIForwardResult{ResponseID: taskID, AsyncTaskStatus: "processing"}
	id := strings.TrimPrefix(strings.TrimPrefix(taskID, "minimax:"), "minimax-file:")
	if endpoint == MiniMaxVideoEndpointCreate {
		if err := rejectPrivateVideoCallback(c, account, body); err != nil {
			return nil, err
		}
		prepared, info, err := NormalizeMiniMaxSubmission(body, account, false)
		if err != nil {
			return nil, err
		}
		result.Model, result.BillingModel, result.UpstreamModel = info.Model, info.Model, account.GetMappedModel(info.Model)
		result.VideoResolution, result.VideoDurationSeconds = info.Resolution, info.DurationSeconds
		body, err = sjson.SetBytes(prepared, "model", result.UpstreamModel)
		if err != nil {
			return nil, err
		}
	}
	resp, responseBody, err := s.miniMaxVideoRequest(ctx, account, endpoint, id, body)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		s.writeMiniMaxVideoError(c, account, resp, responseBody, taskID)
		return nil, err
	}
	result.ResponseHeaders = resp.Header.Clone()
	if endpoint == MiniMaxVideoEndpointCreate {
		id = gjson.GetBytes(responseBody, "task_id").String()
		if err := ValidateMiniMaxVideoID(id); err != nil {
			return nil, fmt.Errorf("MiniMax create response missing valid task_id")
		}
		result.ResponseID = "minimax:" + id
	} else if endpoint == MiniMaxVideoEndpointStatus && miniMaxVideoV2(account.VideoBaseURL()) {
		observed, err := observeMiniMaxVideoV2(responseBody, id)
		if err != nil {
			return nil, err
		}
		result.AsyncTaskStatus, result.AsyncTaskURL, result.AsyncTaskError = observed.AsyncTaskStatus, observed.AsyncTaskURL, observed.AsyncTaskError
		result.VideoCount = observed.VideoCount
	} else if endpoint == MiniMaxVideoEndpointStatus {
		if returned := gjson.GetBytes(responseBody, "task_id").String(); returned != id {
			return nil, fmt.Errorf("MiniMax returned a different task ID")
		}
		switch strings.ToLower(gjson.GetBytes(responseBody, "status").String()) {
		case "success":
			result.VideoFileID = gjson.GetBytes(responseBody, "file_id").String()
			result.AsyncTaskStatus, result.VideoCount = "completed", 1
			// Success is authoritative even when the separate file lookup fails.
			// Capture the durable obligation before requesting download metadata;
			// later native/background observations settle the same task once.
			if _, err := s.ObserveVideoTaskBilling(ctx, VideoProtocolMiniMax, taskID, result, false); err != nil {
				return nil, fmt.Errorf("settle completed MiniMax task: %w", err)
			}
			result.AsyncTaskURL, err = s.miniMaxVideoFile(ctx, account, result.VideoFileID)
			if err != nil {
				var upstreamErr *miniMaxVideoResponseError
				if errors.As(err, &upstreamErr) {
					s.writeMiniMaxVideoError(c, account, upstreamErr.response, upstreamErr.body, taskID)
				}
				return nil, err
			}
		case "fail", "failed":
			result.AsyncTaskStatus = "failed"
			result.AsyncTaskError = gjson.GetBytes(responseBody, "base_resp.status_msg").String()
		case "preparing", "queueing", "processing":
		default:
			return nil, fmt.Errorf("MiniMax returned an unknown task status")
		}
	} else if gjson.GetBytes(responseBody, "file.file_id").String() != id {
		return nil, fmt.Errorf("MiniMax returned a different file ID")
	}
	result.Duration = time.Since(started)
	s.writeNativeVideoResponse(c, account, resp, responseBody, result.ResponseID)
	return result, nil
}
