package service

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	SeedanceEndpointCreate           GrokMediaEndpoint        = "seedance_create"
	SeedanceEndpointStatus           GrokMediaEndpoint        = "seedance_status"
	SeedanceEndpointDelete           GrokMediaEndpoint        = "seedance_delete"
	OpenAIEndpointCapabilitySeedance OpenAIEndpointCapability = "seedance"
)

func (e GrokMediaEndpoint) IsSeedance() bool {
	return e == SeedanceEndpointCreate || e == SeedanceEndpointStatus || e == SeedanceEndpointDelete
}

// SeedanceTaskKey isolates ownership and billing keys from other video providers.
func SeedanceTaskKey(id string) string { return "seedance:" + strings.TrimSpace(id) }

func ParseSeedanceRequest(body []byte) (GrokMediaRequestInfo, error) {
	var info GrokMediaRequestInfo
	if err := ValidateVideoSubmissionJSON(body); err != nil {
		return info, err
	}
	if err := RejectUnsupportedVideoBillingFields(body, VideoProtocolSeedance); err != nil {
		return info, err
	}
	draftID, err := seedanceDraftTaskID(body)
	if err != nil {
		return info, err
	}
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return info, fmt.Errorf("model is required")
	}
	content := gjson.GetBytes(body, "content")
	if !content.IsArray() || len(content.Array()) == 0 {
		return info, fmt.Errorf("content must be a non-empty array")
	}
	info.Model = strings.TrimSpace(model.String())
	info.N = 1 // Ark creates one video per task; output-count aliases are rejected.
	if draftID != "" {
		info.Resolution = VideoBillingResolution1080P
		return info, nil // Only the authenticated source receipt supplies timing.
	}
	var texts []string
	for _, item := range content.Array() {
		switch item.Get("type").String() {
		case "text":
			texts = append(texts, item.Get("text").String())
		case "image_url":
			info.InputImageURLs = append(info.InputImageURLs, item.Get("image_url.url").String())
		}
	}
	info.Prompt = strings.Join(texts, "\n")
	resolution, err := seedanceResolution(body, info.Prompt)
	if err != nil {
		return info, err
	}
	info.Resolution = resolution
	// Account defaults are selected after scheduling. Draft's mandatory 480p
	// tier is checked then; the pre-scheduling parser only checks its hard type.
	if draft := gjson.GetBytes(body, "draft"); draft.Exists() && draft.Type != gjson.True && draft.Type != gjson.False {
		return info, fmt.Errorf("Seedance draft must be a boolean")
	}
	info.DurationSeconds = SeedanceDefaultDurationSeconds
	durationValue, framesValue, _, timingErr := seedanceTiming(body, info.Prompt)
	if timingErr != nil {
		return info, timingErr
	}
	if durationValue != 0 {
		info.DurationSeconds = durationValue
	}
	info.Frames = framesValue
	if framesValue > 0 {
		info.DurationSecondsExact = float64(framesValue) / 24
		info.DurationSeconds = int(math.Ceil(info.DurationSecondsExact))
	} else {
		info.DurationSecondsExact = float64(info.DurationSeconds)
	}
	return info, nil
}

func buildSeedanceURL(base string, endpoint GrokMediaEndpoint, taskID string) (string, error) {
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/contents/generations/tasks")
	base = strings.TrimSuffix(base, "/v1")
	// Accept an origin, a proxy prefix, or the full Ark API base.
	if !strings.HasSuffix(base, "/api/v3") && !strings.HasSuffix(base, "/v3") {
		base += "/api/v3"
	}
	base += "/contents/generations/tasks"
	if endpoint != SeedanceEndpointCreate {
		if err := validateUpstreamPathSegment("Seedance task ID", taskID); err != nil || strings.TrimSpace(taskID) == "" {
			return "", fmt.Errorf("invalid Seedance task ID")
		}
		base += "/" + taskID
	}
	return base, nil
}

// ForwardSeedance preserves the Ark protocol, including multimodal content and
// future fields. Only model is rewritten using the account's configured mapping.
func (s *OpenAIGatewayService) ForwardSeedance(ctx context.Context, c *gin.Context, account *Account, endpoint GrokMediaEndpoint, taskID string, body []byte) (*OpenAIForwardResult, error) {
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance) || !endpoint.IsSeedance() {
		return nil, fmt.Errorf("seedance requires a Seedance video account or an enabled OpenAI API key account")
	}
	base, err := s.validateUpstreamBaseURL(account.VideoBaseURL())
	if err != nil {
		return nil, err
	}
	target, err := buildSeedanceURL(base, endpoint, strings.TrimPrefix(taskID, "seedance:"))
	if err != nil {
		return nil, err
	}
	model, upstreamModel := "", ""
	var requestInfo GrokMediaRequestInfo
	method := http.MethodGet
	switch endpoint {
	case SeedanceEndpointCreate:
		if err := rejectPrivateVideoCallback(c, account, body); err != nil {
			return nil, err
		}
		info, parseErr := ParseSeedanceRequest(body)
		if parseErr != nil {
			return nil, parseErr
		}
		model = info.Model
		upstreamModel = account.GetMappedModel(model)
		// Defend service callers as well as the HTTP handler: normalize timing
		// after account model mapping, before any upstream request is created.
		body, requestInfo, err = s.NormalizeSeedanceSubmissionForAccount(ctx, body, account)
		if err != nil {
			return nil, err
		}
		body, err = sjson.SetBytes(body, "model", upstreamModel)
		if err != nil {
			return nil, err
		}
		method = http.MethodPost
	case SeedanceEndpointDelete:
		method = http.MethodDelete
	}
	token := strings.TrimSpace(account.GetCredential("api_key"))
	if token == "" {
		return nil, fmt.Errorf("seedance account missing api_key")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ctx = WithHTTPUpstreamRedirectsDisabled(ctx)
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.doVideoUpstreamRequest(req, proxy, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	// Do not retry ambiguous asynchronous creates: the upstream may already have
	// accepted a billable job. Preserve native error codes and response bodies.
	if resp.StatusCode >= 300 {
		s.writeNativeVideoResponse(c, account, resp, responseBody, taskID)
		return nil, fmt.Errorf("seedance upstream status %d", resp.StatusCode)
	}
	result := &OpenAIForwardResult{Model: model, BillingModel: model, UpstreamModel: upstreamModel, Duration: time.Since(started), ResponseHeaders: resp.Header.Clone()}
	result.VideoResolution = requestInfo.Resolution
	result.VideoDurationSeconds = requestInfo.DurationSeconds
	result.VideoDurationSecondsExact = requestInfo.DurationSecondsExact
	result.AsyncTaskStatus, result.AsyncTaskURL, result.AsyncTaskError = videoTaskObservation(VideoProtocolSeedance, responseBody)
	if endpoint == SeedanceEndpointCreate {
		id := strings.TrimSpace(gjson.GetBytes(responseBody, "id").String())
		if id == "" {
			return nil, fmt.Errorf("seedance create response missing task ID")
		}
		result.ResponseID = SeedanceTaskKey(id)
	}
	if endpoint == SeedanceEndpointStatus {
		if id := gjson.GetBytes(responseBody, "id").String(); id != "" && id != strings.TrimPrefix(taskID, "seedance:") {
			return nil, fmt.Errorf("Seedance returned a different task ID")
		}
		result.ResponseID = taskID
		result.UpstreamModel = gjson.GetBytes(responseBody, "model").String()
		if gjson.GetBytes(responseBody, "status").String() == "succeeded" {
			result.Usage.OutputTokens = max(0, int(gjson.GetBytes(responseBody, "usage.completion_tokens").Int()))
			result.VideoResolution = gjson.GetBytes(responseBody, "resolution").String()
			result.VideoDurationSecondsExact = seedanceResponseDurationSeconds(responseBody)
			result.VideoDurationSeconds = int(math.Ceil(result.VideoDurationSecondsExact))
		}
	}
	s.writeNativeVideoResponse(c, account, resp, responseBody, result.ResponseID)
	return result, nil
}

func seedanceResponseDurationSeconds(body []byte) float64 {
	// Ark's frames field is exact at 24 fps. Its integer duration can be a
	// truncated display value, so frames take precedence when both exist.
	if frames := gjson.GetBytes(body, "frames"); frames.Type == gjson.Number && frames.Float() > 0 && frames.Float() == math.Trunc(frames.Float()) && !math.IsInf(frames.Float(), 0) {
		return frames.Float() / 24
	}
	if duration := gjson.GetBytes(body, "duration"); duration.Type == gjson.Number && validVideoContractNumber(duration.Float()) && duration.Float() > 0 {
		return duration.Float()
	}
	return 0
}
