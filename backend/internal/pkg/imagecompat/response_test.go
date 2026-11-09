//go:build unit

package imagecompat

import (
	"encoding/base64"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var testNow = time.Unix(1762675200, 0)

func geminiPlan(t *testing.T, model string, n int) *Plan {
	t.Helper()
	plan, err := BuildPlan(ProtocolGemini, Request{Operation: OperationGenerations, Model: model, Prompt: "cat", N: n})
	require.NoError(t, err)
	return plan
}

func grokPlan(t *testing.T, n int, format string) *Plan {
	t.Helper()
	plan, err := BuildPlan(ProtocolGrok, Request{Operation: OperationGenerations, Model: "grok-imagine-image", Prompt: "cat", N: n, ResponseFormat: format})
	require.NoError(t, err)
	return plan
}

const (
	geminiImageOK = `{"candidates":[{"content":{"role":"model","parts":[{"text":"Here is your cat"},{"inlineData":{"mimeType":"image/png","data":"SU1BR0Ux"}}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}`
	geminiImageOK2 = `{"candidates":[{"content":{"parts":[{"inline_data":{"mime_type":"image/jpeg","data":"SU1BR0Uy"}}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"thoughtsTokenCount":100,"totalTokenCount":1400}}`
)

func TestBuildResponseGemini(t *testing.T) {
	cases := []struct {
		name        string
		n           int
		results     []UpstreamResult
		wantImages  []string
		wantUsage   *Usage
		wantErrType string
		wantStatus  int
		wantRetry   bool
	}{
		{
			name:       "single success",
			n:          1,
			results:    []UpstreamResult{{200, []byte(geminiImageOK)}},
			wantImages: []string{"SU1BR0Ux"},
			wantUsage:  &Usage{InputTokens: 10, OutputTokens: 1290, TotalTokens: 1300},
		},
		{
			name:       "n=2 both succeed, snake_case blob and thoughts tokens",
			n:          2,
			results:    []UpstreamResult{{200, []byte(geminiImageOK)}, {200, []byte(geminiImageOK2)}},
			wantImages: []string{"SU1BR0Ux", "SU1BR0Uy"},
			wantUsage:  &Usage{InputTokens: 20, OutputTokens: 2680, TotalTokens: 2700},
		},
		{
			name: "partial success returns only produced images",
			n:    3,
			results: []UpstreamResult{
				{429, []byte(`{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`)},
				{200, []byte(geminiImageOK)},
				{200, []byte(`{"candidates":[{"finishReason":"IMAGE_SAFETY"}]}`)},
			},
			wantImages: []string{"SU1BR0Ux"},
			wantUsage:  &Usage{InputTokens: 10, OutputTokens: 1290, TotalTokens: 1300},
		},
		{
			name:        "I4: image and IMAGE_SAFETY in the same candidate is blocked",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"VU5TQUZF"}}]},"finishReason":"IMAGE_SAFETY"}]}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name:        "I4: image with a blocked safety rating is blocked",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"VU5TQUZF"}}]},"finishReason":"STOP","safetyRatings":[{"category":"HARM_CATEGORY_DANGEROUS_CONTENT","probability":"HIGH","blocked":true}]}]}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name:        "I4: promptFeedback block with image data present",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"promptFeedback":{"blockReason":"OTHER"},"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"VU5TQUZF"}}]},"finishReason":"STOP"}]}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name: "I4: blocked candidate excluded, safe candidate kept",
			n:    1,
			results: []UpstreamResult{{200, []byte(`{"candidates":[
				{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"VU5TQUZF"}}]},"finishReason":"IMAGE_SAFETY"},
				{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"U0FGRQ=="}}]},"finishReason":"STOP"}]}`)}},
			wantImages: []string{"U0FGRQ=="},
		},
		{
			name: "I4: safe rating not blocked keeps image",
			n:    1,
			results: []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"U0FGRQ=="}}]},"finishReason":"STOP",
				"safetyRatings":[{"category":"HARM_CATEGORY_HARASSMENT","probability":"NEGLIGIBLE"}]}]}`)}},
			wantImages: []string{"U0FGRQ=="},
		},
		{
			name: "I4: cross-call partial success keeps the safe call",
			n:    2,
			results: []UpstreamResult{
				{200, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"VU5TQUZF"}}]},"finishReason":"IMAGE_SAFETY"}]}`)},
				{200, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"U0FGRQ=="}}]},"finishReason":"STOP"}]}`)},
			},
			wantImages: []string{"U0FGRQ=="},
		},
		{
			name: "thought images are skipped",
			n:    1,
			results: []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[
				{"thought":true,"inlineData":{"mimeType":"image/png","data":"VEhPVUdIVA=="}},
				{"inlineData":{"mimeType":"image/png","data":"RklOQUw="}}]}}]}`)}},
			wantImages: []string{"RklOQUw="},
		},
		{
			name: "extra images beyond ExpectImages are dropped",
			n:    1,
			results: []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[
				{"inlineData":{"mimeType":"image/png","data":"QQ=="}},
				{"inlineData":{"mimeType":"image/png","data":"Qg=="}}]}}]}`)}},
			wantImages: []string{"QQ=="},
		},
		{
			name:        "prompt blocked",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"},"usageMetadata":{"promptTokenCount":8,"totalTokenCount":8}}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name:        "finishReason IMAGE_SAFETY",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[{"text":"I can't draw that"}]},"finishReason":"IMAGE_SAFETY"}]}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name:        "finishReason SAFETY without content",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"finishReason":"SAFETY"}]}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name:        "finishReason RECITATION",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"finishReason":"RECITATION"}]}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name:        "blocked safety rating",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"finishReason":"OTHER","safetyRatings":[{"category":"HARM_CATEGORY_SEXUALLY_EXPLICIT","blocked":true}]}]}`)}},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
		{
			name:        "text only, no block reason",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[{"text":"Sure! What kind of cat?"}]},"finishReason":"STOP"}]}`)}},
			wantErrType: ErrorTypeUpstream,
			wantStatus:  502,
		},
		{
			name:        "NO_IMAGE finish reason is not a policy block",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"finishReason":"NO_IMAGE"}]}`)}},
			wantErrType: ErrorTypeUpstream,
			wantStatus:  502,
		},
		{
			name:        "non-image inline data ignored",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"text/plain","data":"aGk="}}]}}]}`)}},
			wantErrType: ErrorTypeUpstream,
			wantStatus:  502,
		},
		{
			name:        "malformed JSON",
			n:           1,
			results:     []UpstreamResult{{200, []byte(`{"candidates":[`)}},
			wantErrType: ErrorTypeUpstream,
			wantStatus:  502,
		},
		{
			name:        "empty 200 body",
			n:           1,
			results:     []UpstreamResult{{200, nil}},
			wantErrType: ErrorTypeUpstream,
			wantStatus:  502,
		},
		{
			name: "all calls rate limited stays retryable",
			n:    2,
			results: []UpstreamResult{
				{429, []byte(`{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`)},
				{503, []byte(`{"error":{"code":503,"message":"The model is overloaded.","status":"UNAVAILABLE"}}`)},
			},
			wantErrType: ErrorTypeRateLimit,
			wantStatus:  429,
			wantRetry:   true,
		},
		{
			name: "non-retryable failure wins over retryable",
			n:    2,
			results: []UpstreamResult{
				{429, []byte(`{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`)},
				{200, []byte(`{"candidates":[{"finishReason":"IMAGE_SAFETY"}]}`)},
			},
			wantErrType: ErrorTypeContentPolicy,
			wantStatus:  400,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := geminiPlan(t, "gemini-2.5-flash-image", tc.n)
			require.Len(t, plan.Calls, len(tc.results))
			resp, err := BuildResponse(plan, tc.results, testNow)
			if tc.wantErrType != "" {
				require.Nil(t, resp)
				apiErr := requireAPIError(t, err, tc.wantStatus, tc.wantErrType)
				require.Equal(t, tc.wantRetry, apiErr.Retryable)
				return
			}
			require.NoError(t, err)
			requireImageResponse(t, resp, tc.wantImages, "b64_json")
			require.Equal(t, tc.wantUsage, resp.Usage)
			if tc.wantUsage != nil {
				body := gjson.ParseBytes(resp.Body)
				require.Equal(t, int64(tc.wantUsage.InputTokens), body.Get("usage.input_tokens").Int())
				require.Equal(t, int64(tc.wantUsage.OutputTokens), body.Get("usage.output_tokens").Int())
				require.Equal(t, int64(tc.wantUsage.TotalTokens), body.Get("usage.total_tokens").Int())
			} else {
				require.False(t, gjson.GetBytes(resp.Body, "usage").Exists())
			}
		})
	}
}

func TestBuildResponseGeminiContentPolicyMessage(t *testing.T) {
	plan := geminiPlan(t, "gemini-2.5-flash-image", 1)
	_, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"promptFeedback":{"blockReason":"SAFETY"}}`)}}, testNow)
	apiErr := requireAPIError(t, err, 400, ErrorTypeContentPolicy)
	require.Equal(t, "content_policy_violation", apiErr.Code)
	require.Contains(t, apiErr.Message, "SAFETY")
	require.False(t, apiErr.Retryable)
	require.Equal(t, 200, apiErr.UpstreamStatus)
}

func TestBuildResponseImagen(t *testing.T) {
	cases := []struct {
		name        string
		n           int
		body        string
		status      int
		wantImages  []string
		wantErrType string
		wantStatus  int
	}{
		{"two images", 2, `{"predictions":[{"bytesBase64Encoded":"SU1HMQ==","mimeType":"image/png"},{"bytesBase64Encoded":"SU1HMg==","mimeType":"image/png"}]}`, 200, []string{"SU1HMQ==", "SU1HMg=="}, "", 0},
		{"partially filtered", 3, `{"predictions":[{"bytesBase64Encoded":"SU1HMQ=="},{"raiFilteredReason":"filtered"}]}`, 200, []string{"SU1HMQ=="}, "", 0},
		{"I4: filtered prediction with bytes not counted", 2, `{"predictions":[{"bytesBase64Encoded":"VU5TQUZF","raiFilteredReason":"filtered"},{"bytesBase64Encoded":"U0FGRQ=="}]}`, 200, []string{"U0FGRQ=="}, "", 0},
		{"I4: only filtered prediction with bytes", 1, `{"predictions":[{"bytesBase64Encoded":"VU5TQUZF","raiFilteredReason":"filtered"}]}`, 200, nil, ErrorTypeContentPolicy, 400},
		{"rai filtered", 1, `{"predictions":[{"raiFilteredReason":"Your current safety filter threshold filtered out 1 generated images. Support codes: 56562880"}]}`, 200, nil, ErrorTypeContentPolicy, 400},
		{"no predictions", 1, `{}`, 200, nil, ErrorTypeContentPolicy, 400},
		{"empty predictions", 2, `{"predictions":[]}`, 200, nil, ErrorTypeContentPolicy, 400},
		{"prediction without bytes or reason", 1, `{"predictions":[{"mimeType":"image/png"}]}`, 200, nil, ErrorTypeUpstream, 502},
		{"malformed", 1, `not json`, 200, nil, ErrorTypeUpstream, 502},
		{"upstream 400", 1, `{"error":{"code":400,"message":"Imagen API is only accessible to billed users at this time.","status":"INVALID_ARGUMENT"}}`, 400, nil, ErrorTypeUpstream, 502},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := geminiPlan(t, "imagen-4.0-generate-001", tc.n)
			resp, err := BuildResponse(plan, []UpstreamResult{{tc.status, []byte(tc.body)}}, testNow)
			if tc.wantErrType != "" {
				require.Nil(t, resp)
				_ = requireAPIError(t, err, tc.wantStatus, tc.wantErrType)
				return
			}
			require.NoError(t, err)
			requireImageResponse(t, resp, tc.wantImages, "b64_json")
			require.Nil(t, resp.Usage)
		})
	}
}

func TestBuildResponseGrok(t *testing.T) {
	t.Run("b64 with revised prompt and usage", func(t *testing.T) {
		plan := grokPlan(t, 2, "")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"created":1,"data":[
			{"b64_json":"R1JPSzE=","revised_prompt":"a cute cat","mime_type":"image/jpeg"},
			{"b64_json":"R1JPSzI="}],"usage":{"input_tokens":12,"output_tokens":34}}`)}}, testNow)
		require.NoError(t, err)
		requireImageResponse(t, resp, []string{"R1JPSzE=", "R1JPSzI="}, "b64_json")
		require.Equal(t, "a cute cat", gjson.GetBytes(resp.Body, "data.0.revised_prompt").String())
		require.False(t, gjson.GetBytes(resp.Body, "data.1.revised_prompt").Exists())
		require.Equal(t, &Usage{InputTokens: 12, OutputTokens: 34, TotalTokens: 46}, resp.Usage)
		require.Equal(t, int64(46), gjson.GetBytes(resp.Body, "usage.total_tokens").Int())
	})
	t.Run("url format keeps url", func(t *testing.T) {
		plan := grokPlan(t, 1, "url")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"url":"https://imgen.x.ai/xai-imgen/abc.jpg?a=1&b=2"}]}`)}}, testNow)
		require.NoError(t, err)
		requireImageResponse(t, resp, []string{"https://imgen.x.ai/xai-imgen/abc.jpg?a=1&b=2"}, "url")
		require.Nil(t, resp.Usage)
		require.NotContains(t, string(resp.Body), "\\"+"u0026")
	})
	// S2: imagecompat does not convert between url and b64_json. When xAI
	// ignores the requested format, the entry keeps the field it actually
	// returned (the platform backfills url → b64_json) and Images still
	// counts it.
	t.Run("S2: b64_json requested but upstream returns only url", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		require.Equal(t, "b64_json", plan.ResponseFormat)
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"url":"https://imgen.x.ai/a.jpg"}]}`)}}, testNow)
		require.NoError(t, err)
		requireImageResponse(t, resp, []string{"https://imgen.x.ai/a.jpg"}, "url")
		require.Equal(t, 1, resp.Images)
	})
	t.Run("R2-4: url requested but upstream returns only b64_json → data URL", func(t *testing.T) {
		plan := grokPlan(t, 1, "url")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"b64_json":"QQ=="}]}`)}}, testNow)
		require.NoError(t, err)
		requireImageResponse(t, resp, []string{"data:image/png;base64,QQ=="}, "url")
		require.Equal(t, 1, resp.Images)
	})
	t.Run("S2: mixed entries keep their own field and are all counted", func(t *testing.T) {
		plan := grokPlan(t, 2, "")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"b64_json":"QQ=="},{"url":"https://imgen.x.ai/b.jpg"}]}`)}}, testNow)
		require.NoError(t, err)
		require.Equal(t, 2, resp.Images)
		data := gjson.GetBytes(resp.Body, "data").Array()
		require.Len(t, data, 2)
		require.Equal(t, "QQ==", data[0].Get("b64_json").String())
		require.False(t, data[0].Get("url").Exists())
		require.Equal(t, "https://imgen.x.ai/b.jpg", data[1].Get("url").String())
		require.False(t, data[1].Get("b64_json").Exists())
	})
	t.Run("prompt_tokens style usage", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"b64_json":"QQ=="}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":9}}`)}}, testNow)
		require.NoError(t, err)
		require.Equal(t, &Usage{InputTokens: 3, OutputTokens: 4, TotalTokens: 9}, resp.Usage)
	})
	t.Run("zero usage is nil", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"b64_json":"QQ=="}],"usage":{"cost_in_usd_ticks":1000}}`)}}, testNow)
		require.NoError(t, err)
		require.Nil(t, resp.Usage)
	})
	t.Run("more images than requested are capped", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"b64_json":"QQ=="},{"b64_json":"Qg=="}]}`)}}, testNow)
		require.NoError(t, err)
		require.Equal(t, 1, resp.Images)
	})
	t.Run("empty items skipped", func(t *testing.T) {
		plan := grokPlan(t, 2, "")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"b64_json":""},{"b64_json":"Qg=="}]}`)}}, testNow)
		require.NoError(t, err)
		requireImageResponse(t, resp, []string{"Qg=="}, "b64_json")
	})
	t.Run("no data", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		_, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[]}`)}}, testNow)
		_ = requireAPIError(t, err, 502, ErrorTypeUpstream)
	})
	t.Run("2xx moderation error", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		_, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"error":"Generated image rejected by content moderation."}`)}}, testNow)
		_ = requireAPIError(t, err, 400, ErrorTypeContentPolicy)
	})
	t.Run("malformed", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		_, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`<html>oops</html>`)}}, testNow)
		_ = requireAPIError(t, err, 502, ErrorTypeUpstream)
	})
	t.Run("upstream error converted", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		_, err := BuildResponse(plan, []UpstreamResult{{400, []byte(`{"code":"Client specified an invalid argument","error":"Incorrect API key provided: xai-abc***xyz. You can obtain an API key from https://console.x.ai."}`)}}, testNow)
		apiErr := requireAPIError(t, err, 502, ErrorTypeUpstream)
		require.True(t, apiErr.Retryable)
		require.NotContains(t, apiErr.Message, "xai-")
		require.NotContains(t, apiErr.Message, "console.x.ai")
	})
}

func TestBuildResponseCallerErrors(t *testing.T) {
	plan := geminiPlan(t, "gemini-2.5-flash-image", 2)

	_, err := BuildResponse(nil, nil, testNow)
	require.Error(t, err)

	_, err = BuildResponse(plan, []UpstreamResult{{200, []byte(geminiImageOK)}}, testNow)
	require.Error(t, err)
	var apiErr *Error
	require.False(t, errors.As(err, &apiErr))

	bad := *plan
	bad.Protocol = ProtocolOpenAI
	_, err = BuildResponse(&bad, []UpstreamResult{{200, nil}, {200, nil}}, testNow)
	require.Error(t, err)
	require.False(t, errors.As(err, &apiErr))
}

func TestBuildResponseNoResultsMeansNoImage(t *testing.T) {
	plan := &Plan{Protocol: ProtocolGemini, Model: "gemini-2.5-flash-image"}
	_, err := BuildResponse(plan, nil, testNow)
	apiErr := requireAPIError(t, err, http.StatusBadGateway, ErrorTypeUpstream)
	require.Equal(t, -1, apiErr.CallIndex, "no call to point at")
}

func requireImageResponse(t *testing.T, resp *Response, want []string, format string) {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, len(want), resp.Images)
	body := gjson.ParseBytes(resp.Body)
	require.True(t, body.IsObject(), string(resp.Body))
	require.Equal(t, testNow.Unix(), body.Get("created").Int())
	data := body.Get("data").Array()
	require.Len(t, data, len(want))
	other := "url"
	if format == "url" {
		other = "b64_json"
	}
	for i, item := range data {
		require.Equal(t, want[i], item.Get(format).String())
		require.False(t, item.Get(other).Exists())
	}
}

var webpBytes = []byte("RIFF\x24\x00\x00\x00WEBPVP8 fakewebp")

// TestBuildResponseURLFormatConvertsBase64 covers reviewer finding R2-4: a
// plan that must answer with response_format "url" turns base64-only entries
// into data: URLs (sniffed MIME), and drops entries that are not valid base64.
func TestBuildResponseURLFormatConvertsBase64(t *testing.T) {
	std := base64.StdEncoding.EncodeToString
	cases := []struct {
		name       string
		n          int
		body       string
		wantURLs   []string
		wantErr    bool
		wantPrompt string
	}{
		{"png", 1, `{"data":[{"b64_json":"` + std(pngBytes) + `","revised_prompt":"a cat"}]}`, []string{"data:image/png;base64," + std(pngBytes)}, false, "a cat"},
		{"jpeg", 1, `{"data":[{"b64_json":"` + std(jpegBytes) + `"}]}`, []string{"data:image/jpeg;base64," + std(jpegBytes)}, false, ""},
		{"gif", 1, `{"data":[{"b64_json":"` + std(gifBytes) + `"}]}`, []string{"data:image/gif;base64," + std(gifBytes)}, false, ""},
		{"webp", 1, `{"data":[{"b64_json":"` + std(webpBytes) + `"}]}`, []string{"data:image/webp;base64," + std(webpBytes)}, false, ""},
		{"unknown bytes default to png", 1, `{"data":[{"b64_json":"QQ=="}]}`, []string{"data:image/png;base64,QQ=="}, false, ""},
		{"unpadded base64 re-encoded", 1, `{"data":[{"b64_json":"` + base64.RawStdEncoding.EncodeToString(pngBytes) + `"}]}`, []string{"data:image/png;base64," + std(pngBytes)}, false, ""},
		{"url-safe base64 re-encoded", 1, `{"data":[{"b64_json":"` + base64.URLEncoding.EncodeToString(jpegBytes) + `"}]}`, []string{"data:image/jpeg;base64," + std(jpegBytes)}, false, ""},
		{"wrapped base64 whitespace removed", 1, `{"data":[{"b64_json":"` + std(pngBytes)[:8] + `\n` + std(pngBytes)[8:] + `"}]}`, []string{"data:image/png;base64," + std(pngBytes)}, false, ""},
		{"data URL in b64_json re-sniffed", 1, `{"data":[{"b64_json":"data:image/jpeg;base64,` + std(pngBytes) + `"}]}`, []string{"data:image/png;base64," + std(pngBytes)}, false, ""},
		{"url entry kept", 2, `{"data":[{"url":"https://imgen.x.ai/a.jpg"},{"b64_json":"QQ=="}]}`, []string{"https://imgen.x.ai/a.jpg", "data:image/png;base64,QQ=="}, false, ""},
		{"invalid base64 dropped before ExpectImages cap", 1, `{"data":[{"b64_json":"not base64!!"},{"b64_json":"` + std(pngBytes) + `"}]}`, []string{"data:image/png;base64," + std(pngBytes)}, false, ""},
		{"only invalid base64 → no image", 1, `{"data":[{"b64_json":"not base64!!"}]}`, nil, true, ""},
		{"data URL without base64 marker → no image", 1, `{"data":[{"b64_json":"data:image/png,raw"}]}`, nil, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := grokPlan(t, tc.n, "url")
			resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(tc.body)}}, testNow)
			if tc.wantErr {
				require.Nil(t, resp)
				apiErr := requireAPIError(t, err, http.StatusBadGateway, ErrorTypeUpstream)
				require.Equal(t, "upstream_no_image", apiErr.Code)
				require.False(t, apiErr.Retryable)
				require.Equal(t, 0, apiErr.CallIndex)
				return
			}
			require.NoError(t, err)
			requireImageResponse(t, resp, tc.wantURLs, "url")
			require.Equal(t, len(tc.wantURLs), resp.Images)
			if tc.wantPrompt != "" {
				require.Equal(t, tc.wantPrompt, gjson.GetBytes(resp.Body, "data.0.revised_prompt").String())
			}
		})
	}

	t.Run("applies to any protocol with a url plan", func(t *testing.T) {
		plan := geminiPlan(t, "gemini-2.5-flash-image", 1)
		plan.ResponseFormat = "url" // BuildPlan rejects url for Gemini; hand-built plans still convert
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/jpeg","data":"` + std(jpegBytes) + `"}}]}}]}`)}}, testNow)
		require.NoError(t, err)
		requireImageResponse(t, resp, []string{"data:image/jpeg;base64," + std(jpegBytes)}, "url")
	})
	t.Run("b64_json plans are not converted", func(t *testing.T) {
		plan := grokPlan(t, 1, "")
		resp, err := BuildResponse(plan, []UpstreamResult{{200, []byte(`{"data":[{"b64_json":"QQ=="}]}`)}}, testNow)
		require.NoError(t, err)
		requireImageResponse(t, resp, []string{"QQ=="}, "b64_json")
	})
}

// TestBuildResponseErrorCallIndex covers reviewer finding R2-6: the error
// BuildResponse picks names the call it came from, so callers can read that
// call's headers (Retry-After).
func TestBuildResponseErrorCallIndex(t *testing.T) {
	const (
		unavailable = `{"error":{"code":503,"message":"The model is overloaded.","status":"UNAVAILABLE"}}`
		limited     = `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`
		internal    = `{"error":{"code":500,"message":"internal","status":"INTERNAL"}}`
		badRequest  = `{"error":{"code":400,"message":"Invalid prompt","status":"INVALID_ARGUMENT"}}`
		safety      = `{"candidates":[{"finishReason":"IMAGE_SAFETY"}]}`
		textOnly    = `{"candidates":[{"content":{"parts":[{"text":"no"}]},"finishReason":"STOP"}]}`
	)
	cases := []struct {
		name          string
		results       []UpstreamResult
		wantIndex     int
		wantType      string
		wantRetryable bool
	}{
		{"single retryable", []UpstreamResult{{429, []byte(limited)}}, 0, ErrorTypeRateLimit, true},
		{"all retryable → first call", []UpstreamResult{{503, []byte(unavailable)}, {429, []byte(limited)}, {500, []byte(internal)}}, 0, ErrorTypeUpstream, true},
		{"all retryable, 429 first", []UpstreamResult{{429, []byte(limited)}, {503, []byte(unavailable)}}, 0, ErrorTypeRateLimit, true},
		{"non-retryable 400 in the middle wins", []UpstreamResult{{429, []byte(limited)}, {400, []byte(badRequest)}, {503, []byte(unavailable)}}, 1, ErrorTypeInvalidRequest, false},
		{"2xx safety block after retryable", []UpstreamResult{{503, []byte(unavailable)}, {200, []byte(safety)}}, 1, ErrorTypeContentPolicy, false},
		{"2xx no image at the end", []UpstreamResult{{503, []byte(unavailable)}, {503, []byte(unavailable)}, {200, []byte(textOnly)}}, 2, ErrorTypeUpstream, false},
		{"malformed 2xx after retryable", []UpstreamResult{{429, []byte(limited)}, {200, []byte(`{`)}}, 1, ErrorTypeUpstream, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := geminiPlan(t, "gemini-2.5-flash-image", len(tc.results))
			_, err := BuildResponse(plan, tc.results, testNow)
			require.Error(t, err)
			var apiErr *Error
			require.True(t, errors.As(err, &apiErr))
			require.Equal(t, tc.wantIndex, apiErr.CallIndex)
			require.Equal(t, tc.wantType, apiErr.Type)
			require.Equal(t, tc.wantRetryable, apiErr.Retryable)
			// The chosen error describes exactly the call it points at.
			require.Equal(t, tc.results[apiErr.CallIndex].StatusCode, apiErr.UpstreamStatus)
		})
	}
}
