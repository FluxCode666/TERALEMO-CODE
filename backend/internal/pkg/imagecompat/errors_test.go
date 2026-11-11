//go:build unit

package imagecompat

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertUpstreamError(t *testing.T) {
	cases := []struct {
		name          string
		p             Protocol
		status        int
		body          string
		wantStatus    int
		wantType      string
		wantCode      string
		wantRetryable bool
		wantContains  []string
		wantAbsent    []string
	}{
		{
			name:       "gemini invalid argument",
			p:          ProtocolGemini,
			status:     400,
			body:       `{"error":{"code":400,"message":"Unable to process input image. Please retry or report in https://developers.generativeai.google/guide/troubleshooting","status":"INVALID_ARGUMENT"}}`,
			wantStatus: 400, wantType: ErrorTypeInvalidRequest, wantCode: "upstream_invalid_request",
			wantContains: []string{"Unable to process input image", "[url]"},
			wantAbsent:   []string{"https://", "generativeai.google"},
		},
		{
			name:       "gemini invalid api key is a 400 with API_KEY_INVALID",
			p:          ProtocolGemini,
			status:     400,
			body:       `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID","domain":"googleapis.com","metadata":{"service":"generativelanguage.googleapis.com"}}]}}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_auth_failed", wantRetryable: true,
			wantAbsent: []string{"API key not valid", "googleapis"},
		},
		{
			name:       "gemini 403 permission denied",
			p:          ProtocolGemini,
			status:     403,
			body:       `{"error":{"code":403,"message":"Generative Language API has not been used in project 123456 before or it is disabled. Enable it by visiting https://console.developers.google.com/apis/api/generativelanguage.googleapis.com/overview?project=123456","status":"PERMISSION_DENIED"}}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_auth_failed", wantRetryable: true,
			wantAbsent: []string{"123456", "console.developers"},
		},
		{
			name:       "401 never echoes upstream text",
			p:          ProtocolGrok,
			status:     401,
			body:       `{"error":{"message":"Invalid bearer token Bearer xai-SECRETSECRET","type":"invalid_request_error"}}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_auth_failed", wantRetryable: true,
			wantAbsent: []string{"SECRET", "Bearer"},
		},
		{
			name:       "xai 400 incorrect api key",
			p:          ProtocolGrok,
			status:     400,
			body:       `{"code":"Client specified an invalid argument","error":"Incorrect API key provided: xa***yz. You can obtain an API key from https://console.x.ai."}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_auth_failed", wantRetryable: true,
			wantAbsent: []string{"xa***", "console.x.ai"},
		},
		{
			name:       "xai 403 no credits",
			p:          ProtocolGrok,
			status:     403,
			body:       `{"code":"The caller does not have permission to execute the specified operation","error":"Your team abc-123 does not have any credits or licenses."}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_auth_failed", wantRetryable: true,
			wantAbsent: []string{"abc-123"},
		},
		{
			name:       "xai 400 invalid argument with detail",
			p:          ProtocolGrok,
			status:     400,
			body:       `{"code":"Client specified an invalid argument","error":"Invalid aspect ratio: 7:4"}`,
			wantStatus: 400, wantType: ErrorTypeInvalidRequest, wantCode: "upstream_invalid_request",
			wantContains: []string{"Invalid aspect ratio: 7:4"},
		},
		{
			name:       "xai 400 moderation",
			p:          ProtocolGrok,
			status:     400,
			body:       `{"code":"Client specified an invalid argument","error":"Generated image rejected by content moderation."}`,
			wantStatus: 400, wantType: ErrorTypeContentPolicy, wantCode: "content_policy_violation",
		},
		{
			name:       "gemini location not supported",
			p:          ProtocolGemini,
			status:     400,
			body:       `{"error":{"code":400,"message":"User location is not supported for the API use.","status":"FAILED_PRECONDITION"}}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_account_unavailable", wantRetryable: true,
		},
		{
			name:       "402 payment required",
			p:          ProtocolGrok,
			status:     402,
			body:       `{"error":"insufficient balance"}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_quota_exhausted", wantRetryable: true,
		},
		{
			name:       "404 model not found",
			p:          ProtocolGemini,
			status:     404,
			body:       `{"error":{"code":404,"message":"models/gemini-9-image is not found for API version v1beta, or is not supported for generateContent.","status":"NOT_FOUND"}}`,
			wantStatus: 404, wantType: ErrorTypeInvalidRequest, wantCode: "model_not_found", wantRetryable: true,
		},
		{
			name:       "429 rate limited hides quota internals",
			p:          ProtocolGemini,
			status:     429,
			body:       `{"error":{"code":429,"message":"You exceeded your current quota. * Quota exceeded for metric: generativelanguage.googleapis.com/generate_content_free_tier_requests, limit: 0","status":"RESOURCE_EXHAUSTED"}}`,
			wantStatus: 429, wantType: ErrorTypeRateLimit, wantCode: "rate_limit_exceeded", wantRetryable: true,
			wantAbsent: []string{"free_tier", "googleapis"},
		},
		{
			name:       "503 overloaded",
			p:          ProtocolGemini,
			status:     503,
			body:       `{"error":{"code":503,"message":"The model is overloaded. Please try again later.","status":"UNAVAILABLE"}}`,
			wantStatus: 503, wantType: ErrorTypeUpstream, wantCode: "upstream_overloaded", wantRetryable: true,
		},
		{
			name:       "529 overloaded",
			p:          ProtocolGrok,
			status:     529,
			body:       ``,
			wantStatus: 503, wantType: ErrorTypeUpstream, wantCode: "upstream_overloaded", wantRetryable: true,
		},
		{
			name:       "500 internal",
			p:          ProtocolGemini,
			status:     500,
			body:       `{"error":{"code":500,"message":"An internal error has occurred.","status":"INTERNAL"}}`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_error", wantRetryable: true,
		},
		{
			name:       "502 html page",
			p:          ProtocolGrok,
			status:     502,
			body:       `<html><head><title>502 Bad Gateway</title></head><body>nginx at 10.0.0.12</body></html>`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_error", wantRetryable: true,
			wantAbsent: []string{"nginx", "10.0.0.12"},
		},
		{
			name:       "504 timeout",
			p:          ProtocolGemini,
			status:     504,
			body:       `{"error":{"code":504,"message":"Deadline expired","status":"DEADLINE_EXCEEDED"}}`,
			wantStatus: 504, wantType: ErrorTypeUpstream, wantCode: "upstream_timeout", wantRetryable: true,
		},
		{
			name:       "408 timeout",
			p:          ProtocolGrok,
			status:     408,
			wantStatus: 504, wantType: ErrorTypeUpstream, wantCode: "upstream_timeout", wantRetryable: true,
		},
		{
			name:       "405 failover eligible",
			p:          ProtocolGrok,
			status:     405,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_error", wantRetryable: true,
		},
		{
			name:       "413 too large",
			p:          ProtocolGemini,
			status:     413,
			body:       `Request Entity Too Large`,
			wantStatus: 400, wantType: ErrorTypeInvalidRequest, wantCode: "request_too_large",
			wantContains: []string{"too large"},
		},
		{
			name:       "plain text 400",
			p:          ProtocolGrok,
			status:     400,
			body:       "bad request: prompt too long",
			wantStatus: 400, wantType: ErrorTypeInvalidRequest, wantCode: "upstream_invalid_request",
			wantContains: []string{"prompt too long"},
		},
		{
			name:       "empty 400",
			p:          ProtocolGemini,
			status:     400,
			wantStatus: 400, wantType: ErrorTypeInvalidRequest, wantCode: "upstream_invalid_request",
		},
		{
			name:       "gemini array wrapped error",
			p:          ProtocolGemini,
			status:     400,
			body:       `[{"error":{"code":400,"message":"Invalid value at 'generation_config.image_config.aspect_ratio'","status":"INVALID_ARGUMENT"}}]`,
			wantStatus: 400, wantType: ErrorTypeInvalidRequest, wantCode: "upstream_invalid_request",
			wantContains: []string{"aspect_ratio"},
		},
		{
			name:       "openai style 422",
			p:          ProtocolGrok,
			status:     422,
			body:       `{"detail":"Failed to deserialize the JSON body into the target type: n: invalid type"}`,
			wantStatus: 400, wantType: ErrorTypeInvalidRequest, wantCode: "upstream_invalid_request",
			wantContains: []string{"Failed to deserialize"},
		},
		{
			name:       "status 0 transport failure",
			p:          ProtocolGemini,
			status:     0,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_error", wantRetryable: true,
		},
		{
			name:       "unexpected 3xx",
			p:          ProtocolGrok,
			status:     302,
			body:       `redirect to https://evil.example.com`,
			wantStatus: 502, wantType: ErrorTypeUpstream, wantCode: "upstream_error",
			wantAbsent: []string{"evil"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := ConvertUpstreamError(tc.p, tc.status, []byte(tc.body))
			require.NotNil(t, e)
			require.Equal(t, tc.wantStatus, e.HTTPStatus, e.Message)
			require.Equal(t, tc.wantType, e.Type, e.Message)
			require.Equal(t, tc.wantCode, e.Code, e.Message)
			require.Equal(t, tc.wantRetryable, e.Retryable, e.Message)
			require.Equal(t, tc.status, e.UpstreamStatus)
			require.Equal(t, -1, e.CallIndex, "standalone ConvertUpstreamError is not tied to a call")
			require.NotEmpty(t, e.Message)
			require.LessOrEqual(t, utf8.RuneCountInString(e.Message), maxErrorMessageRunes)
			for _, s := range tc.wantContains {
				require.Contains(t, e.Message, s)
			}
			for _, s := range tc.wantAbsent {
				require.NotContains(t, e.Message, s)
			}
			// The rendered body never leaks the upstream status / raw body.
			body := gjson.ParseBytes(e.OpenAIErrorBody())
			require.Equal(t, e.Message, body.Get("error.message").String())
			require.Equal(t, e.Type, body.Get("error.type").String())
		})
	}
}

func TestSanitizeMessage(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		absent []string
	}{
		{"empty", "", "", nil},
		{"url", "see https://generativelanguage.googleapis.com/v1beta/models?key=AIzaSyA-123456789012345678901234567 now", "see [url] now", []string{"AIza", "googleapis"}},
		{"google key", "key AIzaSyA1234567890abcdefghijklmnopqrstu leaked", "key [redacted] leaked", nil},
		{"key param", "failed for key=abc123&alt=json", "", []string{"abc123"}},
		{"api_key param", "api_key: sk-proj-abcdefghijklmnop", "", []string{"abcdefghijklmnop"}},
		{"xai key", "token xai-AbCdEf0123456789 rejected", "token [redacted] rejected", nil},
		{"masked xai key", "Incorrect API key provided: xa***yz.", "", []string{"xa***"}},
		{"bearer", "Authorization: Bearer eyJhbGciOi.payload.sig", "", []string{"eyJhbGciOi"}},
		{"openai key", "sk-abcdefghijklmnopqrstuvwx is bad", "[redacted] is bad", nil},
		{"oauth token", "token ya29.a0AfH6SMBx-abc is expired", "", []string{"ya29."}},
		{"ip and port", "dial tcp 192.168.1.20:8443: refused", "dial tcp [host]: refused", nil},
		{"bare host", "proxy my-relay.example.com:8080/v1 failed", "proxy [host] failed", nil},
		{"data url", "bad image data:image/png;base64,iVBORw0KGgo=", "bad image [data]", nil},
		{"only first line kept", "line1\n\tline2\r\n\x00line3", "line1", nil},
		{"control chars within a line", "a\tb\x00c  d", "a b c d", nil},
		{"long opaque token", "id " + strings.Repeat("A", 60) + " end", "id [redacted] end", nil},
		{"model names survive", "models/gemini-2.5-flash-image is not found", "models/gemini-2.5-flash-image is not found", nil},
		{"invalid utf8", "bad \xff\xfe bytes", "bad bytes", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeMessage(tc.in)
			if tc.want != "" || tc.in == "" {
				require.Equal(t, tc.want, got)
			}
			for _, s := range tc.absent {
				require.NotContains(t, got, s)
			}
		})
	}

	long := strings.Repeat("错误 ", 400)
	got := sanitizeMessage(long)
	require.LessOrEqual(t, utf8.RuneCountInString(got), maxErrorMessageRunes)
	require.True(t, strings.HasSuffix(got, "…"))
	require.True(t, utf8.ValidString(got))
}

func TestErrorOpenAIBody(t *testing.T) {
	e := &Error{HTTPStatus: 400, Type: ErrorTypeInvalidRequest, Code: "unsupported_parameter", Param: "quality", Message: `bad "quality" <x> & y`}
	body := e.OpenAIErrorBody()
	parsed := gjson.ParseBytes(body)
	require.Equal(t, `bad "quality" <x> & y`, parsed.Get("error.message").String())
	require.Equal(t, "invalid_request_error", parsed.Get("error.type").String())
	require.Equal(t, "unsupported_parameter", parsed.Get("error.code").String())
	require.Equal(t, "quality", parsed.Get("error.param").String())
	require.NotContains(t, string(body), "HTTPStatus")
	require.NotContains(t, string(body), "Retryable")

	bare := &Error{HTTPStatus: 502, Type: ErrorTypeUpstream, Message: "boom", UpstreamStatus: 500}
	parsed = gjson.ParseBytes(bare.OpenAIErrorBody())
	require.Equal(t, gjson.Null, parsed.Get("error.code").Type)
	require.Equal(t, gjson.Null, parsed.Get("error.param").Type)
	require.False(t, parsed.Get("error.upstream_status").Exists())

	var nilErr *Error
	require.True(t, gjson.ValidBytes(nilErr.OpenAIErrorBody()))
	require.Contains(t, nilErr.Error(), "nil")
	require.Equal(t, "imagecompat: 502 upstream_error: boom", bare.Error())
}

func TestValidationErrorsAreBounded(t *testing.T) {
	_, err := BuildPlan(ProtocolGemini, Request{
		Operation: OperationGenerations, Model: "gemini-2.5-flash-image", Prompt: "x",
		Quality: strings.Repeat("q", 1000),
	})
	apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
	require.LessOrEqual(t, utf8.RuneCountInString(apiErr.Message), maxErrorMessageRunes)

	_, err = BuildPlan(ProtocolGemini, Request{
		Operation: OperationGenerations, Model: "https://evil.example.com/" + strings.Repeat("m", 500), Prompt: "x",
	})
	apiErr = requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
	require.NotContains(t, apiErr.Message, "evil")
	require.LessOrEqual(t, utf8.RuneCountInString(apiErr.Message), maxErrorMessageRunes)
}

// TestSanitizeMessageStackAndLines covers reviewer finding B1: only the first
// meaningful line survives and stack-frame fragments / paths are removed.
func TestSanitizeMessageStackAndLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"reviewer sample", "RenderError for credential relay-secret-42\n at render (/srv/image/renderer.js:87)", "RenderError for credential relay-secret-42"},
		{"leading blank lines skipped", "\n\n  \r\n  real error\nsecond line", "real error"},
		{"unicode line separator", "first\u2028second", "first"},
		{"inline js frame", "boom at render (/srv/image/renderer.js:87:12) next", "boom next"},
		{"inline js frame with async", "boom at async Object.render (/srv/x.js:1:2)", "boom"},
		{"java frame", "NullPointerException at com.acme.Render.run(Render.java:42)", "NullPointerException"},
		{"anonymous frame", "failed at /srv/app/index.js:10:5", "failed"},
		{"go source location", "panic in /srv/app/main.go:123 handler", "panic in handler"},
		{"bare source location", "error in renderer.js:87", "error in"},
		{"goroutine dump", "panic: boom goroutine 1 [running]:", "panic: boom"},
		{"traceback header only", "Traceback (most recent call last):\n  File \"/srv/x.py\", line 3, in <module>\nValueError: bad", ""},
		{"stack frame as first line", "    at render (/srv/image/renderer.js:87)\nreal error", ""},
		{"python frame inline", `error File "/srv/app/x.py", line 3, in render`, "error"},
		{"unix path", "cannot open /var/lib/image/cfg.yaml for reading", "cannot open [path] for reading"},
		{"windows path", `cannot open C:\srv\image\cfg.yaml now`, "cannot open [path] now"},
		{"file url", "load file:///srv/x.png failed", "load [url] failed"},
		{"gemini field path survives", "Invalid value at 'generation_config.image_config.aspect_ratio' (TYPE_ENUM)", "Invalid value at 'generation_config.image_config.aspect_ratio' (TYPE_ENUM)"},
		{"ratio survives", "Invalid aspect ratio: 7:4", "Invalid aspect ratio: 7:4"},
		{"relative model path survives", "models/gemini-2.5-flash-image is not found", "models/gemini-2.5-flash-image is not found"},
		{"fraction survives", "only 3/4 of the quota left", "only 3/4 of the quota left"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sanitizeMessage(tc.in))
		})
	}
}

// TestConvertUpstreamErrorStripsStacks pins B1 end to end: ordinary 4xx
// messages (JSON and plain text) reach the client without stacks or paths.
func TestConvertUpstreamErrorStripsStacks(t *testing.T) {
	cases := []struct {
		name         string
		p            Protocol
		status       int
		body         string
		wantType     string
		wantContains string
	}{
		{"json 400 reviewer sample", ProtocolGemini, 400,
			`{"error":{"code":400,"message":"RenderError for credential relay-secret-42\n at render (/srv/image/renderer.js:87)","status":"INVALID_ARGUMENT"}}`,
			ErrorTypeInvalidRequest, "RenderError for credential"},
		{"xai string error with stack", ProtocolGrok, 400,
			`{"code":"Client specified an invalid argument","error":"bad prompt\n    at validate (/srv/xai/api.js:12:3)"}`,
			ErrorTypeInvalidRequest, "bad prompt"},
		{"422 detail starting with traceback", ProtocolGrok, 422,
			`{"detail":"Traceback (most recent call last):\n  File \"/srv/app/main.py\", line 9, in handler\nKeyError: 'n'"}`,
			ErrorTypeInvalidRequest, "rejected the request"},
		{"plain text with node stack", ProtocolGrok, 400,
			"invalid prompt\n    at Object.<anonymous> (/srv/app/server.js:12:3)\n    at Module._compile (node:internal/modules/cjs/loader:1105:14)",
			ErrorTypeInvalidRequest, "invalid prompt"},
		{"plain text go panic", ProtocolGemini, 400,
			"panic: nil map\n\ngoroutine 1 [running]:\nmain.main()\n\t/srv/app/main.go:12 +0x1d",
			ErrorTypeInvalidRequest, "panic: nil map"},
		{"content policy with stack", ProtocolGrok, 400,
			`{"error":"Generated image rejected by content moderation.\n at moderate (/srv/mod.js:1)"}`,
			ErrorTypeContentPolicy, "content moderation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := ConvertUpstreamError(tc.p, tc.status, []byte(tc.body))
			require.Equal(t, 400, e.HTTPStatus, e.Message)
			require.Equal(t, tc.wantType, e.Type, e.Message)
			require.Contains(t, e.Message, tc.wantContains)
			for _, leak := range []string{"/srv", ".js", ".go", ".py", " at ", "Traceback", "goroutine", "node:internal", "\n", "renderer", "KeyError"} {
				require.NotContains(t, e.Message, leak, e.Message)
			}
			body := string(e.OpenAIErrorBody())
			require.NotContains(t, body, "/srv")
			require.NotContains(t, body, `\n`)
		})
	}
}

// TestErrorConstructorsSetNoCallIndex pins R2-6: every *Error constructor sets
// CallIndex explicitly, so the zero value is never mistaken for "call 0".
func TestErrorConstructorsSetNoCallIndex(t *testing.T) {
	errs := map[string]*Error{
		"invalidParam":            invalidParam("n", errorCodeInvalidValue, "bad"),
		"unsupportedParam":        unsupportedParam("label", "style", "vivid"),
		"contentPolicyError":      contentPolicyError(200, "SAFETY"),
		"upstreamError":           upstreamError(502, "upstream_error", "boom", true, 500),
		"invalidUpstreamRequest":  invalidUpstreamRequest(400, "upstream_invalid_request", "bad"),
		"invalidUpstreamResponse": invalidUpstreamResponse(ProtocolGrok, 200),
		"noImageError":            noImageError(ProtocolGemini, 200),
		"convert 429":             ConvertUpstreamError(ProtocolGemini, 429, nil),
		"convert 404":             ConvertUpstreamError(ProtocolGrok, 404, nil),
		"convert 0":               ConvertUpstreamError(ProtocolGrok, 0, nil),
	}
	for name, e := range errs {
		require.Equal(t, -1, e.CallIndex, name)
	}
}
