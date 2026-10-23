//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func requireGeminiVideoError(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	require.Equal(t, int64(status), gjson.Get(w.Body.String(), "error.code").Int(), w.Body.String())
	require.NotEmpty(t, gjson.Get(w.Body.String(), "error.status").String())
	require.NotEmpty(t, gjson.Get(w.Body.String(), "error.message").String())
	require.False(t, gjson.Get(w.Body.String(), "error.type").Exists())
}

func TestGeminiVideoSharedLocalErrorsUseGoogleFormat(t *testing.T) {
	for _, scenario := range []string{"missing-user", "missing-dependencies", "invalid-instances", "invalid-duration", "missing-pricing", "no-account", "ownership", "account-concurrency", "image-concurrency"} {
		t.Run(scenario, func(t *testing.T) {
			protocol := service.VideoProtocolGemini
			if scenario == "no-account" {
				protocol = service.VideoProtocolSeedance
			}
			h, slots, _, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, protocol)
			c, w := grokMediaSlotContext(context.Background(), true)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = service.PlatformVideo
			body := `{"instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}`
			wantStatus := http.StatusBadRequest
			switch scenario {
			case "missing-user":
				delete(c.Keys, string(middleware.ContextKeyUser))
				wantStatus = http.StatusInternalServerError
			case "missing-dependencies":
				h.apiKeyService = nil
				wantStatus = http.StatusServiceUnavailable
			case "invalid-instances":
				body = `{"instances":[]}`
			case "invalid-duration":
				body = `{"instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":16}}`
			case "missing-pricing":
				key.Group.VideoPrice720P = nil
			case "no-account":
				wantStatus = http.StatusServiceUnavailable
			case "ownership", "account-concurrency":
				wantStatus = http.StatusNotFound
				if scenario == "account-concurrency" {
					require.NoError(t, h.gatewayService.BindGrokMediaVideoRequestAccount(context.Background(), key.GroupID, "gemini:models/veo/operations/task-1", 10, 20, 1))
					slots.full, slots.queueFull = true, true
					wantStatus = http.StatusTooManyRequests
				}
			case "image-concurrency":
				h.cfg.Gateway.ImageConcurrency.Enabled = true
				h.cfg.Gateway.ImageConcurrency.MaxConcurrentRequests = 1
				h.imageLimiter = &imageConcurrencyLimiter{}
				release, acquired := h.imageLimiter.TryAcquire(true, 1)
				require.True(t, acquired)
				defer release()
				wantStatus = http.StatusTooManyRequests
			}
			if scenario == "ownership" || scenario == "account-concurrency" {
				c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/veo/operations/task-1", nil)
				c.Params = gin.Params{{Key: "model", Value: "veo"}, {Key: "operation", Value: "task-1"}}
			} else {
				c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/veo:predictLongRunning", strings.NewReader(body))
				c.Params = gin.Params{{Key: "modelAction", Value: "/veo:predictLongRunning"}}
			}
			h.GeminiVideoTasks(c)
			requireGeminiVideoError(t, w, wantStatus)
			require.Zero(t, upstream.calls, "local errors must never reach the upstream")
			slots.assertReleased(t)
		})
	}
}

func TestGeminiVideoSharedConcurrencyErrorKeepsOpsCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"queue-full", &WaitQueueFullError{SlotType: "user"}, 429, gatewayQueueFullCode},
		{"timeout", &ConcurrencyError{SlotType: "account", IsTimeout: true}, 429, gatewayConcurrencyLimitCode},
		{"backend", errors.New("backend unavailable"), 503, ""},
		{"cancelled", context.Canceled, 499, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := grokMediaSlotContext(context.Background(), true)
			c.Set(geminiVideoErrorFormatKey, true)
			(&OpenAIGatewayHandler{}).handleConcurrencyError(c, tc.err, "user", false)
			requireGeminiVideoError(t, w, tc.status)
			errType, code := service.OpsLocalErrorType(c)
			require.NotEmpty(t, errType)
			require.Equal(t, tc.code, code)
		})
	}
}

func TestVideoSharedErrorFormatsRemainRequestScoped(t *testing.T) {
	for _, protocol := range []string{"openai", "grok", service.VideoProtocolSeedance, service.VideoProtocolMiniMax, service.VideoProtocolGemini} {
		for _, response := range []string{"plain", "coded", "dependencies"} {
			t.Run(protocol+"/"+response, func(t *testing.T) {
				c, w := grokMediaSlotContext(context.Background(), true)
				if protocol == service.VideoProtocolGemini {
					c.Set(geminiVideoErrorFormatKey, true)
				}
				h := &OpenAIGatewayHandler{}
				switch response {
				case "plain":
					h.errorResponse(c, 503, "api_error", "Service temporarily unavailable")
				case "coded":
					h.handleStreamingAwareErrorWithCode(c, 503, "api_error", "local_error", "Service temporarily unavailable", false, false)
				case "dependencies":
					require.False(t, h.ensureResponsesDependencies(c, nil))
				}
				if protocol == service.VideoProtocolGemini {
					requireGeminiVideoError(t, w, 503)
					errType, code := service.OpsLocalErrorType(c)
					require.Equal(t, "api_error", errType)
					if response == "coded" {
						require.Equal(t, "local_error", code)
					} else {
						require.Empty(t, code)
					}
				} else {
					require.Equal(t, 503, w.Code)
					require.Equal(t, "api_error", gjson.Get(w.Body.String(), "error.type").String())
					require.False(t, gjson.Get(w.Body.String(), "error.status").Exists())
					if response == "coded" {
						require.Equal(t, "local_error", gjson.Get(w.Body.String(), "error.code").String())
						require.Equal(t, `{"error":{"code":"local_error","message":"Service temporarily unavailable","type":"api_error"}}`, w.Body.String())
					} else {
						require.False(t, gjson.Get(w.Body.String(), "error.code").Exists())
						require.Equal(t, `{"error":{"message":"Service temporarily unavailable","type":"api_error"}}`, w.Body.String())
					}
				}
			})
		}
	}
}

func TestVideoSecurityAuditErrorsPreserveEntryProtocol(t *testing.T) {
	for _, protocol := range []string{"openai", "grok", service.VideoProtocolSeedance, service.VideoProtocolMiniMax, service.VideoProtocolGemini} {
		for _, kind := range []securityaudit.DecisionKind{securityaudit.DecisionBlock, securityaudit.DecisionUnavailable, securityaudit.DecisionInvalid} {
			t.Run(protocol+"/"+string(kind), func(t *testing.T) {
				decision := promptGuardDecision(kind)
				c, w := securityAuditErrorTestContext(t)
				if protocol == service.VideoProtocolGemini {
					c.Set(geminiVideoErrorFormatKey, true)
				}
				(&OpenAIGatewayHandler{}).openAISecurityAuditError(c, decision)
				require.Equal(t, decision.HTTPStatus, w.Code)
				require.Equal(t, decision.ClientMessage, gjson.Get(w.Body.String(), "error.message").String())
				if protocol == service.VideoProtocolGemini {
					requireGeminiVideoError(t, w, decision.HTTPStatus)
					require.Equal(t, decision.ErrorCode, gjson.Get(w.Body.String(), "error.details.0.reason").String())
					errType, code := service.OpsLocalErrorType(c)
					wantType := "api_error"
					if kind == securityaudit.DecisionBlock {
						wantType = "permission_error"
					}
					require.Equal(t, wantType, errType)
					require.Equal(t, decision.ErrorCode, code)
				} else {
					require.Equal(t, decision.ErrorCode, gjson.Get(w.Body.String(), "error.code").String())
					require.False(t, gjson.Get(w.Body.String(), "error.status").Exists())
					errType := "api_error"
					if kind == securityaudit.DecisionBlock {
						errType = "permission_error"
					}
					require.Equal(t, errType, gjson.Get(w.Body.String(), "error.type").String())
				}
			})
		}
	}
}

func TestGeminiVideoSharedErrorsKeepOpsClassificationAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name, errType, code string
		status              int
	}{
		{"plain", "video_pricing_not_configured", "", http.StatusBadRequest},
		{"dependencies", "api_error", "", http.StatusServiceUnavailable},
		{"coded", "rate_limit_error", gatewayQueueFullCode, http.StatusTooManyRequests},
		{"security-audit", "permission_error", promptGuardDecision(securityaudit.DecisionBlock).ErrorCode, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := recordGoogleFormatOpsError(t, func(c *gin.Context) {
				c.Set(geminiVideoErrorFormatKey, true)
				h := &OpenAIGatewayHandler{}
				switch tc.name {
				case "plain":
					h.errorResponse(c, tc.status, tc.errType, "Video pricing must be configured")
				case "dependencies":
					require.False(t, h.ensureResponsesDependencies(c, nil))
				case "coded":
					h.handleStreamingAwareErrorWithCode(c, tc.status, tc.errType, tc.code, "The user queue is full", false, false)
				case "security-audit":
					h.openAISecurityAuditError(c, promptGuardDecision(securityaudit.DecisionBlock))
				}
				errType, code := service.OpsLocalErrorType(c)
				require.Equal(t, tc.errType, errType)
				require.Equal(t, tc.code, code)
				require.Equal(t, tc.status, c.Writer.Status())
			})
			loggedType := tc.errType
			if tc.name == "plain" {
				// The existing Ops logger normalizes custom types such as this
				// pricing reason to api_error for both OpenAI and Google bodies.
				baseline := recordGoogleFormatOpsError(t, func(c *gin.Context) {
					(&OpenAIGatewayHandler{}).errorResponse(c, tc.status, tc.errType, "Video pricing must be configured")
				})
				require.Equal(t, "api_error", baseline.ErrorType)
				loggedType = baseline.ErrorType
			}
			require.Equal(t, loggedType, entry.ErrorType)
			require.Equal(t, tc.status, entry.StatusCode)
		})
	}
}
