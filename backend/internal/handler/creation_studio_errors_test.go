//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func creationStudioInnerError(status int, body string, header map[string]string) creationStudioNormalized {
	headers := http.Header{}
	for key, value := range header {
		headers.Set(key, value)
	}
	return creationStudioNormalizeError(status, headers, []byte(body))
}

func TestCreationStudioNormalizeErrorRules(t *testing.T) {
	t.Run("rule 1 balance below the reserve", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusForbidden,
			`{"code":"billing_error","message":"insufficient balance"}`, nil)
		require.Equal(t, http.StatusForbidden, normalized.Status)
		require.Equal(t, "INSUFFICIENT_BALANCE", normalized.Reason)
	})

	t.Run("rule 2 image generation forbidden", func(t *testing.T) {
		body := `{"error":{"type":"permission_error","code":"permission_error","message":"` +
			service.ImageGenerationPermissionMessage() + `"}}`
		normalized := creationStudioInnerError(http.StatusForbidden, body, nil)
		require.Equal(t, "IMAGE_GENERATION_FORBIDDEN", normalized.Reason)
	})

	t.Run("rule 3 async image disabled", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusNotFound,
			`{"error":{"type":"not_found_error","code":"not_found_error","message":"async image tasks are not enabled"}}`, nil)
		require.Equal(t, http.StatusNotFound, normalized.Status)
		require.Equal(t, "ASYNC_IMAGE_DISABLED", normalized.Reason)
	})

	t.Run("rule 4 platform does not serve the api", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusNotFound,
			`{"error":{"type":"not_found_error","message":"Images API is not supported for this platform"}}`, nil)
		require.Equal(t, http.StatusBadRequest, normalized.Status)
		require.Equal(t, "CREATION_MODEL_NOT_AVAILABLE", normalized.Reason)

		normalized = creationStudioInnerError(http.StatusNotFound,
			`{"error":{"type":"not_found_error","message":"Videos API is not supported for this platform"}}`, nil)
		require.Equal(t, http.StatusBadRequest, normalized.Status)
		require.Equal(t, "CREATION_MODEL_NOT_AVAILABLE", normalized.Reason)
	})

	t.Run("rule 5 openai insufficient quota", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusTooManyRequests,
			`{"error":{"type":"insufficient_quota","message":"You exceeded your quota"}}`, nil)
		require.Equal(t, "API_KEY_QUOTA_EXHAUSTED", normalized.Reason)
	})

	t.Run("rule 6 invalid request", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusBadRequest,
			`{"error":{"type":"invalid_request_error","message":"size is invalid"}}`, nil)
		require.Equal(t, "INVALID_CREATION_REQUEST", normalized.Reason)
		require.Equal(t, "size is invalid", normalized.Message)

		// Google 400: numeric error.code marks the Google envelope.
		normalized = creationStudioInnerError(http.StatusBadRequest,
			`{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`, nil)
		require.Equal(t, "INVALID_CREATION_REQUEST", normalized.Reason)
		require.Equal(t, "bad request", normalized.Message)
	})

	t.Run("rule 7 structured code wins", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusTooManyRequests,
			`{"code":"USAGE_LIMIT_EXCEEDED","message":"daily limit"}`, nil)
		require.Equal(t, http.StatusTooManyRequests, normalized.Status)
		require.Equal(t, "USAGE_LIMIT_EXCEEDED", normalized.Reason)

		normalized = creationStudioInnerError(http.StatusForbidden,
			`{"error":{"type":"permission_error","code":"GROUP_DISABLED","message":"group disabled"}}`, nil)
		require.Equal(t, http.StatusForbidden, normalized.Status)
		require.Equal(t, "GROUP_DISABLED", normalized.Reason)
	})

	t.Run("rule 8 minimax business error", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusOK,
			`{"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`, nil)
		require.Equal(t, http.StatusBadGateway, normalized.Status)
		require.Equal(t, "upstream_error", normalized.Reason)
		require.Equal(t, "invalid api key", normalized.Message)
	})

	t.Run("rule 9 other client error", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusConflict, `{"message":"conflict"}`, nil)
		require.Equal(t, http.StatusConflict, normalized.Status)
		require.Equal(t, "INVALID_CREATION_REQUEST", normalized.Reason)
	})

	t.Run("rule 10 other server error", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusInternalServerError, `{"message":"boom"}`, nil)
		require.Equal(t, http.StatusBadGateway, normalized.Status)
		require.Equal(t, "upstream_error", normalized.Reason)

		normalized = creationStudioInnerError(http.StatusServiceUnavailable, `{"message":"maintenance"}`, nil)
		require.Equal(t, http.StatusServiceUnavailable, normalized.Status)
		require.Equal(t, "upstream_error", normalized.Reason)
	})

	t.Run("retry after becomes metadata", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusTooManyRequests,
			`{"code":"rate_limit_exceeded","message":"slow down"}`, map[string]string{"Retry-After": "30"})
		require.Equal(t, "rate_limit_exceeded", normalized.Reason)
		require.Equal(t, "30", normalized.Metadata["retry_after"])
	})

	t.Run("enterprise metadata is copied", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusTooManyRequests,
			`{"code":"DEPARTMENT_QUOTA_EXCEEDED","message":"department quota","metadata":{"window_resets_at":"2026-11-08T00:00:00Z"}}`, nil)
		require.Equal(t, "DEPARTMENT_QUOTA_EXCEEDED", normalized.Reason)
		require.Equal(t, "2026-11-08T00:00:00Z", normalized.Metadata["window_resets_at"])
	})

	t.Run("bare body", func(t *testing.T) {
		normalized := creationStudioInnerError(http.StatusBadGateway, "", nil)
		require.Equal(t, "upstream_error", normalized.Reason)
		require.NotEmpty(t, normalized.Message)
	})
}

func TestCreationStudioTableMappings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	status, code, _, _ := creationStudioTableAError(c, service.ErrInsufficientBalance)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "INSUFFICIENT_BALANCE", code)

	status, code, _, _ = creationStudioTableAError(c, service.ErrDailyLimitExceeded)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "USAGE_LIMIT_EXCEEDED", code)

	status, code, _, _ = creationStudioTableBError(c, service.ErrInsufficientBalance)
	require.Equal(t, http.StatusPaymentRequired, status)
	require.Equal(t, "insufficient_balance", code)

	status, code, _, _ = creationStudioTableBError(c, service.ErrEnterpriseBillingBalanceInsufficient)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "ENTERPRISE_BALANCE_INSUFFICIENT", code)

	status, code, _, _ = creationStudioTableBError(c, service.ErrAPIKeyQuotaExhausted)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "API_KEY_QUOTA_EXHAUSTED", code)

	status, code, _, _ = creationStudioTableBError(c, service.ErrMonthlyLimitExceeded)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "USAGE_LIMIT_EXCEEDED", code)
}

// newCreationPanelContext builds a gin context carrying an authenticated subject.
func newCreationPanelContext(t *testing.T, method, target, body string, replay bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	ctx := request.Context()
	if replay {
		ctx = service.WithCreationStudioReplay(ctx)
	}
	c.Request = request.WithContext(ctx)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	return c, recorder
}

// TestCreationStudioEnvelopeForBindingFailure covers the panel-wide promise that
// every failure uses the {code,message,reason,metadata} envelope.
func TestCreationStudioEnvelopeForBindingFailure(t *testing.T) {
	handler := NewCreationStudioHandler(nil, nil, nil)
	c, recorder := newCreationPanelContext(t, http.MethodPost, "/api/v1/user/creation/estimate", "{not json", false)
	handler.Estimate(c)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	body := recorder.Body.Bytes()
	require.Equal(t, float64(http.StatusBadRequest), gjson.GetBytes(body, "code").Float())
	require.Equal(t, "INVALID_CREATION_REQUEST", gjson.GetBytes(body, "reason").String())
	require.NotEmpty(t, gjson.GetBytes(body, "message").String())

	unknown := pkgerrors.FromError(service.ErrCreationInternal)
	require.Equal(t, "CREATION_INTERNAL_ERROR", unknown.Reason)
}

// TestCreationStudioRejectsReplay keeps the recursion guard observable and
// enveloped.
func TestCreationStudioRejectsReplay(t *testing.T) {
	handler := NewCreationStudioHandler(nil, nil, nil)
	c, recorder := newCreationPanelContext(t, http.MethodGet, "/api/v1/user/creation/options?api_key_id=9", "", true)
	handler.Options(c)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	body := recorder.Body.Bytes()
	require.Equal(t, float64(http.StatusInternalServerError), gjson.GetBytes(body, "code").Float())
	require.Equal(t, "CREATION_INTERNAL_ERROR", gjson.GetBytes(body, "reason").String())
}

// TestCreationStudioOptionsIgnoresUnknownQueryParams mirrors the frontend client,
// which appends a timezone parameter to every GET.
func TestCreationStudioOptionsIgnoresUnknownQueryParams(t *testing.T) {
	handler := NewCreationStudioHandler(nil, nil, nil)
	c, recorder := newCreationPanelContext(t, http.MethodGet,
		"/api/v1/user/creation/options?api_key_id=9&timezone=Asia%2FShanghai&_t=1", "", false)
	handler.Options(c)
	// apiKeys is nil, so the request stops at the key lookup (404), proving the
	// extra query parameters were accepted rather than rejected as a bad request.
	require.Equal(t, http.StatusNotFound, recorder.Code)
	body := recorder.Body.Bytes()
	require.Equal(t, "CREATION_KEY_NOT_FOUND", gjson.GetBytes(body, "reason").String())
}

// creationStudioProtocolErrorBody renders the body the given video protocol's
// replay actually produces for one error class, so the matrix below is driven by
// the real gateway shapes rather than by idealized ones:
//   - gemini: Google envelope {error:{code:<int>,message,status}} written by
//     errorResponse/googleErrorWithType and by the Google apiKeyAuth path;
//   - seedance/minimax/grok: OpenAI envelope {error:{type,message}} written by
//     errorResponse, or the middleware envelope {code,message}.
func creationStudioProtocolErrorBody(protocol, class string) (int, string) {
	google := protocol == service.CreationProtocolGemini
	switch class {
	case "balance_low":
		if google {
			return http.StatusForbidden, `{"error":{"code":403,"message":"Insufficient account balance","status":"PERMISSION_DENIED"}}`
		}
		return http.StatusForbidden, `{"code":"INSUFFICIENT_BALANCE","message":"Insufficient account balance"}`
	case "balance_below_reserve":
		// handleGrokMedia eligibility: the code travels in the OpenAI type field
		// for Seedance / MiniMax / Grok and in the message for Gemini.
		if google {
			return http.StatusForbidden, `{"error":{"code":403,"message":"insufficient balance","status":"PERMISSION_DENIED"}}`
		}
		return http.StatusForbidden, `{"error":{"type":"billing_error","message":"insufficient balance"}}`
	case "coverage_short":
		if google {
			return http.StatusPaymentRequired, `{"error":{"code":402,"message":"Available balance does not cover the video reservation","status":"UNKNOWN"}}`
		}
		return http.StatusPaymentRequired, `{"error":{"type":"insufficient_balance","message":"Available balance does not cover the video reservation"}}`
	case "key_quota":
		if google {
			return http.StatusTooManyRequests, `{"error":{"code":429,"message":"API key 额度已用完","status":"RESOURCE_EXHAUSTED"}}`
		}
		return http.StatusTooManyRequests, `{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"API key 额度已用完"}}`
	case "platform_quota":
		if google {
			return http.StatusTooManyRequests, `{"error":{"code":429,"message":"Daily usage quota exhausted for this platform.","status":"RESOURCE_EXHAUSTED"}}`
		}
		return http.StatusTooManyRequests, `{"code":"rate_limit_exceeded","message":"Daily usage quota exhausted for this platform."}`
	case "reservation_quota":
		if google {
			return http.StatusTooManyRequests, `{"error":{"code":429,"message":"Video cost exceeds available quota after outstanding reservations.","status":"RESOURCE_EXHAUSTED"}}`
		}
		return http.StatusTooManyRequests, `{"error":{"type":"VIDEO_RESERVATION_QUOTA_EXCEEDED","message":"Video cost exceeds available quota after outstanding reservations."}}`
	case "rate_limit":
		if google {
			return http.StatusTooManyRequests, `{"error":{"code":429,"message":"group requests-per-minute limit exceeded","status":"RESOURCE_EXHAUSTED"}}`
		}
		return http.StatusTooManyRequests, `{"error":{"type":"rate_limit_exceeded","message":"group requests-per-minute limit exceeded"}}`
	case "enterprise":
		if google {
			return http.StatusForbidden, `{"error":{"code":403,"message":"Enterprise balance is insufficient.","status":"PERMISSION_DENIED",
				"reason":"ENTERPRISE_BALANCE_INSUFFICIENT","metadata":{"window_resets_at":"2026-11-08T00:00:00Z"}}}`
		}
		return http.StatusForbidden, `{"error":{"type":"billing_error","message":"Enterprise balance is insufficient.",
			"reason":"ENTERPRISE_BALANCE_INSUFFICIENT","metadata":{"window_resets_at":"2026-11-08T00:00:00Z"}}}`
	default:
		return http.StatusInternalServerError, `{}`
	}
}

// TestCreationStudioBillingReasonsAcrossProtocols locks one reason per error
// class for every video protocol, using the body each path really writes.
func TestCreationStudioBillingReasonsAcrossProtocols(t *testing.T) {
	protocols := []string{
		service.CreationProtocolSeedance,
		service.CreationProtocolGemini,
		service.CreationProtocolMiniMax,
		service.CreationProtocolGrok,
	}
	classes := []struct {
		name       string
		wantReason string
		wantStatus int
	}{
		{"balance_low", "INSUFFICIENT_BALANCE", http.StatusForbidden},
		{"balance_below_reserve", "INSUFFICIENT_BALANCE", http.StatusForbidden},
		{"coverage_short", "insufficient_balance", http.StatusPaymentRequired},
		{"key_quota", "API_KEY_QUOTA_EXHAUSTED", http.StatusTooManyRequests},
		{"platform_quota", "rate_limit_exceeded", http.StatusTooManyRequests},
		{"reservation_quota", "VIDEO_RESERVATION_QUOTA_EXCEEDED", http.StatusTooManyRequests},
		{"rate_limit", "rate_limit_exceeded", http.StatusTooManyRequests},
		{"enterprise", "ENTERPRISE_BALANCE_INSUFFICIENT", http.StatusForbidden},
	}
	for _, protocol := range protocols {
		for _, class := range classes {
			t.Run(protocol+"/"+class.name, func(t *testing.T) {
				status, body := creationStudioProtocolErrorBody(protocol, class.name)
				normalized := creationStudioInnerError(status, body, map[string]string{"Retry-After": "30"})
				require.Equal(t, class.wantStatus, normalized.Status)
				require.Equal(t, class.wantReason, normalized.Reason, "body: %s", body)
				if class.name == "rate_limit" || class.name == "platform_quota" {
					require.Equal(t, "30", normalized.Metadata["retry_after"])
				}
				if class.name == "enterprise" {
					require.Equal(t, "2026-11-08T00:00:00Z", normalized.Metadata["window_resets_at"])
				}
			})
		}
	}
}

// TestCreationStudioGoogleEnvelopeFallbacks covers the remaining Google shapes
// the auth middleware writes.
func TestCreationStudioGoogleEnvelopeFallbacks(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantReason string
	}{
		{
			name: "enterprise details reason", status: http.StatusForbidden,
			body:       `{"error":{"code":403,"message":"enterprise disabled","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"ENTERPRISE_DISABLED","domain":"sub2api"}]}}`,
			wantReason: "ENTERPRISE_DISABLED",
		},
		{
			name: "key disabled", status: http.StatusUnauthorized,
			body:       `{"error":{"code":401,"message":"API key is disabled","status":"UNAUTHENTICATED"}}`,
			wantReason: "API_KEY_DISABLED",
		},
		{
			name: "ip restriction", status: http.StatusForbidden,
			body:       `{"error":{"code":403,"message":"Access denied. Your IP is 203.0.113.9","status":"PERMISSION_DENIED"}}`,
			wantReason: "ACCESS_DENIED",
		},
		{
			// The subscription middleware writes the whole ApplicationError text; the
			// internal window code it contains is not in the §6.1 table and must be
			// reported as USAGE_LIMIT_EXCEEDED, with the window metadata kept.
			name: "subscription window via application error text", status: http.StatusTooManyRequests,
			body: `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","metadata":{"window_resets_at":"2026-11-08T00:00:00Z"},
				"message":"error: code=429 reason=\"DAILY_LIMIT_EXCEEDED\" message=\"daily usage limit exceeded\""}}`,
			wantReason: "USAGE_LIMIT_EXCEEDED",
		},
		{
			name: "weekly window code", status: http.StatusTooManyRequests,
			body:       `{"error":{"code":429,"message":"error: code=429 reason=\"WEEKLY_LIMIT_EXCEEDED\" message=\"weekly usage limit exceeded\"","status":"RESOURCE_EXHAUSTED"}}`,
			wantReason: "USAGE_LIMIT_EXCEEDED",
		},
		{
			name: "monthly window code", status: http.StatusTooManyRequests,
			body:       `{"error":{"code":429,"message":"error: code=429 reason=\"MONTHLY_LIMIT_EXCEEDED\" message=\"monthly usage limit exceeded\"","status":"RESOURCE_EXHAUSTED"}}`,
			wantReason: "USAGE_LIMIT_EXCEEDED",
		},
		{
			// A middleware body may carry the internal code explicitly.
			name: "subscription window as explicit code", status: http.StatusTooManyRequests,
			body:       `{"code":"DAILY_LIMIT_EXCEEDED","message":"daily usage limit exceeded"}`,
			wantReason: "USAGE_LIMIT_EXCEEDED",
		},
		{
			name: "google 400 stays a rejected form", status: http.StatusBadRequest,
			body:       `{"error":{"code":400,"message":"parameters.aspectRatio must be 16:9 or 9:16","status":"INVALID_ARGUMENT"}}`,
			wantReason: "INVALID_CREATION_REQUEST",
		},
		{
			name: "google upstream failure stays upstream", status: http.StatusBadGateway,
			body:       `{"error":{"code":502,"message":"Upstream request failed","status":"INTERNAL"}}`,
			wantReason: "upstream_error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normalized := creationStudioInnerError(tc.status, tc.body, nil)
			require.Equal(t, tc.wantReason, normalized.Reason)
			if tc.name == "subscription window via application error text" {
				require.Equal(t, "2026-11-08T00:00:00Z", normalized.Metadata["window_resets_at"])
			}
		})
	}
}
