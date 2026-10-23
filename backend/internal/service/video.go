package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	VideoProtocolSeedance                                        = "seedance"
	VideoProtocolGemini                                          = "gemini"
	VideoProtocolMiniMax                                         = "minimax"
	GeminiVideoEndpointCreate           GrokMediaEndpoint        = "gemini_video_create"
	GeminiVideoEndpointStatus           GrokMediaEndpoint        = "gemini_video_status"
	OpenAIEndpointCapabilityGeminiVideo OpenAIEndpointCapability = "gemini_video"
)

func (s *OpenAIGatewayService) HasVideoTaskPricing(ctx context.Context, key *APIKey, model, resolution string) bool {
	return s.VideoTaskBillingMode(ctx, key, model, resolution) != ""
}

func (e GrokMediaEndpoint) IsGeminiVideo() bool {
	return e == GeminiVideoEndpointCreate || e == GeminiVideoEndpointStatus
}

func (a *Account) VideoProtocol() string {
	if value := strings.TrimSpace(a.GetCredential("video_protocol")); value != "" {
		return value
	}
	return VideoProtocolSeedance
}

func (a *Account) VideoBaseURL() string {
	if base := strings.TrimSpace(a.GetCredential("base_url")); base != "" {
		return base
	}
	if a.VideoProtocol() == VideoProtocolGemini {
		return "https://generativelanguage.googleapis.com"
	}
	if a.VideoProtocol() == VideoProtocolMiniMax {
		return "https://api.minimax.io"
	}
	return "https://ark.cn-beijing.volces.com/api/v3"
}

// ValidateVideoAccount reports invalid admin input as a 400 so account
// create/update/bulk endpoints do not surface validation failures as 500s.
func ValidateVideoAccount(account *Account) error {
	if err := validateVideoAccount(account); err != nil {
		return infraerrors.BadRequest("INVALID_VIDEO_ACCOUNT", err.Error())
	}
	return nil
}

func validateVideoAccount(account *Account) error {
	if account.Platform != PlatformVideo {
		return nil
	}
	if account.Type != AccountTypeAPIKey {
		return fmt.Errorf("video accounts only support apikey credentials")
	}
	if protocol := account.VideoProtocol(); protocol != VideoProtocolSeedance && protocol != VideoProtocolGemini && protocol != VideoProtocolMiniMax {
		return fmt.Errorf("video_protocol must be seedance, gemini or minimax")
	}
	if strings.TrimSpace(account.GetCredential("api_key")) == "" {
		return fmt.Errorf("video account requires api_key")
	}
	if _, err := NormalizeVideoUpstreamOrigin(account.VideoBaseURL()); err != nil {
		return err
	}
	if value, exists := account.Credentials["hide_video_upstream_url"]; exists {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("hide_video_upstream_url must be a boolean")
		}
	}
	return normalizeVideoAccountCapabilities(account)
}

// Gemini operation names contain path segments; validate every segment and the
// resource shape before attaching it to the configured, validated upstream.
func ValidateGeminiVideoOperation(name string) error {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "models" || parts[2] != "operations" {
		return fmt.Errorf("invalid Gemini video operation name")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("invalid Gemini video operation name")
		}
		if err := validateUpstreamPathSegment("operation", part); err != nil {
			return err
		}
	}
	return nil
}

func ParseGeminiVideoRequest(body []byte) (GrokMediaRequestInfo, error) {
	return parseGeminiVideoSchedulingRequest(body)
}

func videoTaskObservation(protocol string, body []byte) (status, resultURL, taskError string) {
	root := gjson.ParseBytes(body)
	status = "processing"
	if protocol == VideoProtocolGemini {
		if root.Get("done").Bool() {
			status = "completed"
			if root.Get("error").Exists() {
				status = "failed"
			}
		}
		resultURL = root.Get("response.generateVideoResponse.generatedSamples.0.video.uri").String()
		if resultURL == "" {
			resultURL = root.Get("response.generatedVideos.0.video.uri").String()
		}
		if status == "completed" && resultURL == "" {
			status = "failed"
			taskError = "Video generation returned no output"
		}
	} else {
		switch root.Get("status").String() {
		case "succeeded", "done", "completed":
			status = "completed"
		case "failed", "expired":
			status = "failed"
		case "cancelled", "canceled":
			status = "cancelled"
		}
		resultURL = root.Get("content.video_url").String()
		if resultURL == "" {
			resultURL = root.Get("video.url").String()
		}
	}
	if message := root.Get("error.message").String(); message != "" {
		taskError = message
	}
	return
}

func (s *OpenAIGatewayService) ForwardGeminiVideo(ctx context.Context, c *gin.Context, account *Account, endpoint GrokMediaEndpoint, taskID string, body []byte) (*OpenAIForwardResult, error) {
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGeminiVideo) || !endpoint.IsGeminiVideo() {
		return nil, fmt.Errorf("Gemini video requires a video account using the Gemini protocol")
	}
	base, err := s.validateUpstreamBaseURL(account.VideoBaseURL())
	if err != nil {
		return nil, err
	}
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(strings.TrimSuffix(base, "/v1beta"), "/v1") + "/v1beta/"
	model, upstreamModel, resource := "", "", strings.TrimPrefix(taskID, "gemini:")
	method := http.MethodGet
	var requestInfo GrokMediaRequestInfo
	if endpoint == GeminiVideoEndpointCreate {
		if err := rejectPrivateVideoCallback(c, account, body); err != nil {
			return nil, err
		}
		body, requestInfo, upstreamModel, err = normalizeGeminiVideoSubmission(body, account)
		if err != nil {
			return nil, err
		}
		model = requestInfo.Model
		resource = "models/" + upstreamModel + ":predictLongRunning"
		body, err = sjson.DeleteBytes(body, "model")
		if err != nil {
			return nil, err
		}
		method = http.MethodPost
	} else if err := ValidateGeminiVideoOperation(resource); err != nil {
		return nil, err
	}
	started := time.Now()
	// A bounded timeout covers both submission and polling; never retry creates.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ctx = WithHTTPUpstreamRedirectsDisabled(ctx)
	req, err := http.NewRequestWithContext(ctx, method, base+resource, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-goog-api-key", account.GetCredential("api_key"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
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
	if resp.StatusCode >= 300 {
		s.writeNativeVideoResponse(c, account, resp, responseBody, taskID)
		return nil, fmt.Errorf("Gemini video upstream status %d", resp.StatusCode)
	}
	result := &OpenAIForwardResult{Model: model, BillingModel: model, UpstreamModel: upstreamModel, ResponseID: taskID, Duration: time.Since(started), ResponseHeaders: resp.Header.Clone(), VideoResolution: requestInfo.Resolution, VideoDurationSeconds: requestInfo.DurationSeconds}
	result.AsyncTaskStatus, result.AsyncTaskURL, result.AsyncTaskError = videoTaskObservation(VideoProtocolGemini, responseBody)
	if endpoint == GeminiVideoEndpointCreate {
		name := gjson.GetBytes(responseBody, "name").String()
		if err := ValidateGeminiVideoOperation(name); err != nil {
			return nil, fmt.Errorf("Gemini video create response missing valid operation name: %w", err)
		}
		result.ResponseID = "gemini:" + name
	} else if name := gjson.GetBytes(responseBody, "name").String(); name != "" && name != resource {
		return nil, fmt.Errorf("Gemini returned a different operation name")
	} else if result.AsyncTaskStatus == "completed" && result.AsyncTaskURL != "" {
		result.VideoCount = max(1, max(len(gjson.GetBytes(responseBody, "response.generateVideoResponse.generatedSamples").Array()), len(gjson.GetBytes(responseBody, "response.generatedVideos").Array())))
	}
	s.writeNativeVideoResponse(c, account, resp, responseBody, result.ResponseID)
	return result, nil
}
