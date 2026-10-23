package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// VideoTestModelIDs exposes public aliases, keeping known models from other
// video protocols out of the picker (including the legacy mixed whitelist).
func (a *Account) VideoTestModelIDs() []string {
	defaults := map[string][]string{
		VideoProtocolSeedance: {"doubao-seedance-1-0-pro"},
		VideoProtocolGemini:   {"veo-3.0-fast-generate-001", "veo-3.0-generate-001"},
		VideoProtocolMiniMax:  {"MiniMax-Hailuo-2.3", "MiniMax-Hailuo-2.3-Fast", "MiniMax-Hailuo-02"},
	}[a.VideoProtocol()]
	if a.VideoProtocol() == VideoProtocolMiniMax && miniMaxVideoV2(a.VideoBaseURL()) {
		defaults = []string{"MiniMax-H3", "MiniMax-H3-Max"}
	}
	mapping := a.GetModelMapping()
	if len(mapping) == 0 {
		return defaults
	}
	models := make([]string, 0, len(mapping))
	for public, target := range mapping {
		if strings.Contains(public, "*") || strings.TrimSpace(public) == "" {
			continue
		}
		if videoTestModelMatchesProtocol(target, a.VideoProtocol()) {
			models = append(models, public)
		}
	}
	for _, model := range defaults {
		if _, explicit := mapping[model]; !explicit && a.IsModelSupported(model) && videoTestModelMatchesProtocol(a.GetMappedModel(model), a.VideoProtocol()) {
			models = append(models, model)
		}
	}
	rank := func(id string) int {
		for i, model := range defaults {
			if id == model {
				return i
			}
		}
		return len(defaults)
	}
	sort.SliceStable(models, func(i, j int) bool {
		if rank(models[i]) != rank(models[j]) {
			return rank(models[i]) < rank(models[j])
		}
		return models[i] < models[j]
	})
	return models
}

func videoTestModelMatchesProtocol(model, protocol string) bool {
	model = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	switch {
	case strings.HasPrefix(model, "veo-"):
		return protocol == VideoProtocolGemini
	case strings.HasPrefix(model, "minimax-"), strings.HasPrefix(model, "hailuo-"), strings.HasPrefix(model, "t2v-"), strings.HasPrefix(model, "i2v-"):
		return protocol == VideoProtocolMiniMax
	case strings.Contains(model, "seedance"), strings.HasPrefix(model, "ep-"):
		return protocol == VideoProtocolSeedance
	default:
		return true // custom upstream model / endpoint ID
	}
}

// Use the gateway's native video adapters so model mapping, auth, task IDs and
// MiniMax business errors behave exactly as real video requests. Capture their
// native responses separately; the admin connection test always speaks SSE.
func (s *AccountTestService) testVideoAccountConnection(c *gin.Context, account *Account, modelID, prompt string, opts AccountTestOptions) error {
	s.prepareGrokTestSSE(c)
	// Sanitize before sendErrorAndEnd: it writes both SSE and the failure log.
	fail := func(message string) error {
		return s.sendErrorAndEnd(c, sanitizeNativeVideoTestError(message, account))
	}
	if err := ValidateVideoAccount(account); err != nil {
		return fail(err.Error())
	}
	if c.Request.Context().Err() != nil {
		return fail("Video test canceled before submission")
	}
	if modelID = strings.TrimSpace(modelID); modelID == "" {
		models := account.VideoTestModelIDs()
		if len(models) == 0 {
			return fail("No video test model is configured for this protocol")
		}
		modelID = models[0]
	}
	if !account.IsModelSupported(modelID) || !videoTestModelMatchesProtocol(account.GetMappedModel(modelID), account.VideoProtocol()) {
		return fail("The selected model is not available for this video protocol")
	}
	if prompt = strings.TrimSpace(prompt); prompt == "" {
		prompt = defaultGrokVideoTestPrompt
	}
	image := strings.TrimSpace(opts.ImageDataURL)
	if image != "" {
		var err error
		if account.VideoProtocol() == VideoProtocolMiniMax && miniMaxVideoV2(account.VideoBaseURL()) {
			image, err = normalizeMiniMaxV2TestImage(image)
		} else {
			image, err = normalizeAccountTestImageDataURL(image)
		}
		if err != nil {
			return fail(err.Error())
		}
	}
	createEndpoint, statusEndpoint := SeedanceEndpointCreate, SeedanceEndpointStatus
	payload := map[string]any{"model": modelID}
	switch account.VideoProtocol() {
	case VideoProtocolSeedance:
		content := []map[string]any{{"type": "text", "text": prompt}}
		if image != "" {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": image}, "role": "first_frame"})
		}
		payload["content"] = content
		payload["duration"], payload["resolution"] = 5, "720p"
	case VideoProtocolGemini:
		createEndpoint, statusEndpoint = GeminiVideoEndpointCreate, GeminiVideoEndpointStatus
		instance := map[string]any{"prompt": prompt}
		if image != "" {
			header, data, _ := strings.Cut(image, ",")
			instance["image"] = map[string]string{"bytesBase64Encoded": data, "mimeType": strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")}
		}
		payload["instances"] = []map[string]any{instance}
		payload["parameters"] = map[string]any{"durationSeconds": 8, "resolution": "720p", "sampleCount": 1}
	case VideoProtocolMiniMax:
		createEndpoint, statusEndpoint = MiniMaxVideoEndpointCreate, MiniMaxVideoEndpointStatus
		payload["prompt"], payload["duration"], payload["resolution"] = prompt, 6, "768P"
		if image != "" {
			payload["first_frame_image"] = image
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fail("Failed to encode video test request")
	}
	if account.VideoProtocol() == VideoProtocolMiniMax && miniMaxVideoV2(account.VideoBaseURL()) {
		body, err = prepareMiniMaxVideoV2(body)
		if err != nil {
			return fail(err.Error())
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	gateway := &OpenAIGatewayService{cfg: s.cfg, httpUpstream: s.httpUpstream}
	forward := func(endpoint GrokMediaEndpoint, id string, body []byte) (*OpenAIForwardResult, error) {
		recorder := httptest.NewRecorder()
		upstreamContext, _ := gin.CreateTestContext(recorder)
		upstreamContext.Request = c.Request.WithContext(ctx)
		var result *OpenAIForwardResult
		var forwardErr error
		switch account.VideoProtocol() {
		case VideoProtocolSeedance:
			result, forwardErr = gateway.ForwardSeedance(ctx, upstreamContext, account, endpoint, id, body)
		case VideoProtocolGemini:
			result, forwardErr = gateway.ForwardGeminiVideo(ctx, upstreamContext, account, endpoint, id, body)
		case VideoProtocolMiniMax:
			result, forwardErr = gateway.ForwardMiniMaxVideo(ctx, upstreamContext, account, endpoint, id, body)
		}
		if forwardErr != nil {
			message := firstNonEmpty(gjson.GetBytes(recorder.Body.Bytes(), "error.message").String(), gjson.GetBytes(recorder.Body.Bytes(), "base_resp.status_msg").String())
			message = fmt.Sprintf("%v %s", forwardErr, message)
			return nil, fmt.Errorf("%s", sanitizeNativeVideoTestError(message, account))
		}
		return result, nil
	}
	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	s.sendEvent(c, TestEvent{Type: "status", Text: "Submitting native video request (" + account.VideoProtocol() + ")..."})
	result, err := forward(createEndpoint, "", body)
	if err != nil {
		return fail(err.Error())
	}
	id := result.ResponseID
	s.sendEvent(c, TestEvent{Type: "content", Text: "Video task accepted: " + id + "\n"})
	s.sendEvent(c, TestEvent{Type: "status", Text: "Waiting for video generation (up to 10 minutes). Closing stops polling; the upstream task may continue."})
	for {
		if ctx.Err() != nil {
			return fail("Video test polling stopped; task_id=" + id)
		}
		result, err = forward(statusEndpoint, id, nil)
		if err != nil {
			return fail(err.Error() + "; task_id=" + id)
		}
		s.sendEvent(c, TestEvent{Type: "status", Text: "Video task status: " + result.AsyncTaskStatus})
		switch result.AsyncTaskStatus {
		case "completed":
			if strings.TrimSpace(result.AsyncTaskURL) == "" {
				return fail("Video generation returned no output; task_id=" + id)
			}
			if err := s.emitNativeVideoTestResult(c, ctx, account, result.AsyncTaskURL); err != nil {
				return fail(err.Error() + "; task_id=" + id)
			}
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		case "failed", "cancelled":
			return fail("Video generation " + result.AsyncTaskStatus + ": " + result.AsyncTaskError + "; task_id=" + id)
		}
		select {
		case <-ctx.Done():
			return fail("Video test polling stopped; task_id=" + id)
		case <-time.After(5 * time.Second):
		}
	}
}

var nativeVideoTestQueryPattern = regexp.MustCompile(`\?[^\s"'<>\\]+`)

// Upstream diagnostics can contain signed URLs even when no result is returned.
// Apply the same policy regardless of the account's result-link privacy switch.
func sanitizeNativeVideoTestError(message string, account *Account) string {
	if account != nil {
		if key := account.GetCredential("api_key"); key != "" {
			for _, secret := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
	}
	// Some transports include JSON-escaped URLs in their error text.
	message = strings.ReplaceAll(message, `\/`, "/")
	message = videoUpstreamURLPattern.ReplaceAllString(message, "[hidden]")
	message = nativeVideoTestQueryPattern.ReplaceAllString(message, "[hidden]")
	message = logredact.RedactText(message, "api_key", "authorization", "key", "sig", "signature",
		"x-tos-signature", "x-amz-signature", "x-goog-signature",
		"x-tos-credential", "x-amz-credential", "x-goog-credential")
	// Redact before truncating so partial secrets never bypass the redactor.
	return truncateString(message, 700)
}

func normalizeMiniMaxV2TestImage(raw string) (string, error) {
	if err := validateAccountTestDataURL(raw, "image/"); err != nil {
		return "", err
	}
	data, mime, err := decodeAccountTestDataURL(raw)
	if err != nil {
		return "", err
	}
	if len(data) > 30<<20 {
		return "", fmt.Errorf("MiniMax v2 first-frame images must be no larger than 30 MB")
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		ratio := float64(cfg.Width) / float64(cfg.Height)
		if cfg.Width < 256 || cfg.Height < 256 || cfg.Width > 5760 || cfg.Height > 5760 || ratio < 0.4 || ratio > 2.5 {
			return "", fmt.Errorf("MiniMax v2 first-frame images require dimensions 256–5760 pixels and aspect ratio 0.4–2.5")
		}
	}
	return "data:" + strings.ToLower(mime) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (s *AccountTestService) emitNativeVideoTestResult(c *gin.Context, ctx context.Context, account *Account, link string) error {
	target, err := url.Parse(link)
	if err != nil || target.User != nil || target.Fragment != "" || target.Scheme != "https" || target.Host == "" {
		return fmt.Errorf("invalid video result URL")
	}
	// Gemini authenticates on the configured origin. Seedance and MiniMax's
	// signed CDN URLs are fetched server-side without the upstream key.
	downloadCtx := WithHTTPUpstreamRedirectsDisabled(ctx)
	if account.VideoProtocol() == VideoProtocolGemini {
		base, err := s.validateUpstreamBaseURL(account.VideoBaseURL())
		if err != nil {
			return err
		}
		origin, err := url.Parse(base)
		if err != nil || target.Scheme != origin.Scheme || !strings.EqualFold(target.Host, origin.Host) {
			return fmt.Errorf("video result URL must use the configured upstream origin")
		}
		query := target.Query()
		query.Del("key")
		query.Del("api_key")
		target.RawQuery = query.Encode()
	} else {
		if _, err := urlvalidator.ValidateHTTPSURL(link, urlvalidator.ValidationOptions{}); err != nil {
			return fmt.Errorf("invalid video preview URL")
		}
		if _, err := (&OpenAIGatewayService{cfg: s.cfg}).validateOutboundURL(link); err != nil {
			return fmt.Errorf("video preview URL is not allowed")
		}
		downloadCtx = WithHTTPUpstreamPublicHostsOnly(downloadCtx)
	}
	downloadCtx, cancel := context.WithTimeout(downloadCtx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	if account.VideoProtocol() == VideoProtocolGemini {
		req.Header.Set("x-goog-api-key", account.GetCredential("api_key"))
	}
	s.sendEvent(c, TestEvent{Type: "status", Text: "Downloading generated video for preview..."})
	resp, err := s.httpUpstream.Do(req, s.grokTestProxyURL(account), account.ID, account.Concurrency)
	if err != nil {
		return fmt.Errorf("video preview download failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("video preview upstream status %d", resp.StatusCode)
	}
	const maxPreview = 32 << 20
	if resp.ContentLength > maxPreview {
		return fmt.Errorf("video preview is empty, too large, or could not be read")
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if contentType == "application/octet-stream" {
		contentType = "video/mp4"
	}
	if !strings.HasPrefix(contentType, "video/") {
		return fmt.Errorf("upstream returned a non-video preview")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPreview+1))
	if err != nil || len(data) == 0 || len(data) > maxPreview {
		return fmt.Errorf("video preview is empty, too large, or could not be read")
	}
	s.sendEvent(c, TestEvent{Type: "video", VideoURL: "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), MimeType: contentType})
	return nil
}
