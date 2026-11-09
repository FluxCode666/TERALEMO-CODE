//go:build unit

package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type imageRegressionHTTP struct {
	service.HTTPUpstream
	status int
	body   string
	calls  atomic.Int32
}

func (u *imageRegressionHTTP) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls.Add(1)
	return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

type imageRegressionError struct {
	Type    string  `json:"type"`
	Code    *string `json:"code"`
	Param   *string `json:"param"`
	Message string  `json:"message"`
}

func imageRegressionRouter(t *testing.T, protocol, secret string, upstream service.HTTPUpstream) (*gin.Engine, *imagePlatformLifecycleUsage, *observer.ObservedLogs) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	// Keep JSON keepalive at its default (disabled), matching production's
	// terminal JSON error path rather than the streaming fallback.
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	usage := &imagePlatformLifecycleUsage{}
	account := service.Account{ID: 1, Platform: service.PlatformImage, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": secret, "image_protocol": protocol}}
	gateway := service.NewOpenAIGatewayService(openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	images := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	tasks := service.NewImageTaskServiceWithUploader(&asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}, nil, time.Hour, time.Minute)
	async := NewAsyncImageHandler(tasks, images)
	core, logs := observer.New(zap.DebugLevel)
	requestLogger := zap.New(core)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		gid, price := int64(17), .01
		c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), requestLogger))
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, GroupID: &gid, Status: service.StatusActive,
			Group: &service.Group{ID: gid, Platform: service.PlatformImage, AllowImageGeneration: true, RateMultiplier: 1, ImagePrice1K: &price, ImagePrice2K: &price}, User: &service.User{ID: 7}})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		c.Next()
	})
	router.POST("/v1/images/generations", images.ImagePlatformImages)
	router.POST("/v1/images/edits", images.ImagePlatformImages)
	router.POST("/v1/images/generations/async", async.Submit)
	router.GET("/v1/images/tasks/:task_id", async.Get)
	return router, usage, logs
}

func imageRegressionRequest(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func imageRegressionAsyncError(t *testing.T, router *gin.Engine, body string) imageRegressionError {
	t.Helper()
	rec := imageRegressionRequest(router, "/v1/images/generations/async", body)
	require.Equal(t, 202, rec.Code, rec.Body.String())
	pollURL := gjson.GetBytes(rec.Body.Bytes(), "poll_url").String()
	var result struct {
		Status string               `json:"status"`
		Error  imageRegressionError `json:"error"`
	}
	require.Eventually(t, func() bool {
		poll := httptest.NewRecorder()
		router.ServeHTTP(poll, httptest.NewRequest(http.MethodGet, pollURL, nil))
		if json.Unmarshal(poll.Body.Bytes(), &result) != nil {
			return false
		}
		return result.Status == service.ImageTaskStatusFailed
	}, 3*time.Second, 10*time.Millisecond)
	return result.Error
}

func TestImagePlatformTerminalJSONErrors(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, path, body, upstreamBody, typ, code, param string
		status, calls                                              int
	}{
		{"safety", "gemini", "generations", `{"model":"gemini-2.5-flash-image","prompt":"draw"}`, `{"promptFeedback":{"blockReason":"SAFETY"}}`, "content_policy_violation", "content_policy_violation", "", 400, 1},
		{"quality", "gemini", "generations", `{"model":"gemini-2.5-flash-image","prompt":"draw","quality":"high"}`, "", "invalid_request_error", "unsupported_parameter", "quality", 400, 0},
		{"format", "gemini", "generations", `{"model":"gemini-2.5-flash-image","prompt":"draw","response_format":"url"}`, "", "invalid_request_error", "unsupported_parameter", "response_format", 400, 0},
		{"unknown", "gemini", "generations", `{"model":"gemini-2.5-flash-image","prompt":"draw","unknown":true}`, "", "invalid_request_error", "unsupported_parameter", "unknown", 400, 0},
		{"explicit4K", "gemini", "generations", `{"model":"gemini-2.5-flash-image","prompt":"draw","size":"4K"}`, "", "invalid_request_error", "unsupported_parameter", "size", 400, 0},
		{"imagenEdits", "gemini", "edits", `{"model":"imagen-4.0-generate-001","prompt":"draw","images":[{"image_url":"data:image/png;base64,aW1hZ2U="}]}`, "", "invalid_request_error", "unsupported_parameter", "model", 400, 0},
		{"backfill", "grok", "generations", `{"model":"grok-imagine-image","prompt":"draw","response_format":"b64_json"}`, `{"data":[{"url":"http://127.0.0.1/image.png"}]}`, "upstream_error", "", "", 502, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &imageRegressionHTTP{status: 200, body: tc.upstreamBody}
			router, usage, _ := imageRegressionRouter(t, tc.protocol, "relay-secret-42", upstream)
			rec := imageRegressionRequest(router, "/v1/images/"+tc.path, tc.body)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			var envelope struct {
				Error imageRegressionError `json:"error"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), rec.Body.String())
			require.Equal(t, tc.typ, envelope.Error.Type)
			if tc.code == "" {
				require.Nil(t, envelope.Error.Code)
			} else {
				require.NotNil(t, envelope.Error.Code)
				require.Equal(t, tc.code, *envelope.Error.Code)
			}
			if tc.param == "" {
				require.Nil(t, envelope.Error.Param)
			} else {
				require.NotNil(t, envelope.Error.Param)
				require.Equal(t, tc.param, *envelope.Error.Param)
			}
			require.EqualValues(t, tc.calls, upstream.calls.Load())
			require.Empty(t, usage.logs)
			if tc.name == "safety" || tc.name == "quality" {
				asyncErr := imageRegressionAsyncError(t, router, tc.body)
				require.Equal(t, envelope.Error, asyncErr)
			}
		})
	}
}

func TestImagePlatformEarlyValidationKeepsErrorFields(t *testing.T) {
	for _, tc := range []struct {
		param, field string
	}{
		{"n", `"n":1.5`},
		{"partial_images", `"partial_images":0.5`},
		{"images", `"images":[{"image_url":"https://public.example/a"}]`},
	} {
		for _, suffix := range []string{"", "/async"} {
			t.Run(tc.param+suffix, func(t *testing.T) {
				upstream := &imageRegressionHTTP{status: 200, body: `{}`}
				router, usage, _ := imageRegressionRouter(t, "gemini", "relay-secret-42", upstream)
				body := `{"model":"gemini-2.5-flash-image","prompt":"draw",` + tc.field + `}`
				rec := imageRegressionRequest(router, "/v1/images/generations"+suffix, body)
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				require.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json"))
				var envelope struct {
					Error map[string]json.RawMessage `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), rec.Body.String())
				require.JSONEq(t, `"invalid_request_error"`, string(envelope.Error["type"]))
				require.Equal(t, "null", string(envelope.Error["code"]), "nullable code must be present")
				require.JSONEq(t, fmt.Sprintf("%q", tc.param), string(envelope.Error["param"]))
				var message string
				require.NoError(t, json.Unmarshal(envelope.Error["message"], &message))
				require.NotEmpty(t, message)
				require.NotContains(t, message, "imagecompat:")
				require.Zero(t, upstream.calls.Load())
				require.Empty(t, usage.logs)
				require.Empty(t, rec.Header().Get("Location"), "invalid async requests must not create a task")
			})
		}
	}
}

func TestImagePlatformShortSecretsSyncAsyncAndLogs(t *testing.T) {
	for _, key := range []string{"Z", "Z7", "Z7Q", "Z7Q8", "Z7Q8X"} {
		for _, status := range []int{400, 422} {
			t.Run(fmt.Sprintf("%d/%d", len(key), status), func(t *testing.T) {
				upstream := &imageRegressionHTTP{status: status, body: fmt.Sprintf(`{"error":{"message":%q}}`, "RenderError "+key+"\n at render (/srv/image.js:42)")}
				router, usage, logs := imageRegressionRouter(t, "gemini", key, upstream)
				body := `{"model":"gemini-2.5-flash-image","prompt":"draw"}`
				rec := imageRegressionRequest(router, "/v1/images/generations", body)
				require.Equal(t, 400, rec.Code)
				var envelope struct {
					Error imageRegressionError `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
				require.Equal(t, "Upstream rejected the request", envelope.Error.Message)
				require.Equal(t, "invalid_request_error", envelope.Error.Type)
				require.NotNil(t, envelope.Error.Code)
				require.Equal(t, "upstream_invalid_request", *envelope.Error.Code)
				require.Nil(t, envelope.Error.Param)
				require.Equal(t, envelope.Error, imageRegressionAsyncError(t, router, body))
				entries := logs.FilterMessage("openai.images.forward_failed").All()
				require.Len(t, entries, 2, "both sync and async must exercise the real logging exit")
				for _, entry := range entries {
					fields, err := json.Marshal(entry.ContextMap())
					require.NoError(t, err)
					require.NotContains(t, string(fields), key)
					require.NotContains(t, string(fields), "RenderError")
					require.NotContains(t, string(fields), "/srv/image")
				}
				require.Empty(t, usage.logs)
			})
		}
	}
}

func TestImagePlatformGrokURLFromBase64HandlerBilling(t *testing.T) {
	upstream := &imageRegressionHTTP{status: 200, body: `{"data":[{"b64_json":"aW1hZ2U="}]}`}
	router, usage, _ := imageRegressionRouter(t, "grok", "relay-secret-42", upstream)
	rec := imageRegressionRequest(router, "/v1/images/generations", `{"model":"grok-imagine-image","prompt":"draw","size":"1024x1024","response_format":"url"}`)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	var response struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	require.Equal(t, "data:image/png;base64,aW1hZ2U=", response.Data[0].URL)
	require.Empty(t, response.Data[0].B64JSON)
	require.Len(t, usage.logs, 1)
	require.Equal(t, 1, usage.logs[0].ImageCount)
	require.InDelta(t, .01, usage.logs[0].ActualCost, 1e-9)
}

func TestImagePlatformSSEExhaustionReturnsJSON(t *testing.T) {
	u := &imageErrorHTTP{sse: true, code: "invalid_api_key"}
	rec, usage := imageErrorHandlerRequest(t, u, "openai")
	require.Equal(t, 502, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var envelope struct {
		Error imageRegressionError `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Error.Code)
	require.Equal(t, "upstream_auth_failed", *envelope.Error.Code)
	require.Equal(t, []int64{1, 2}, u.ids)
	require.Empty(t, usage.logs)
}
