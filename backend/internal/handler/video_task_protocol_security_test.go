//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Correct-behavior replacement for W1's cross-origin credential recovery probe.
// Exercise the production HTTP client; both servers are local mocks.
func TestVideoForwardRejectsRedirectWithoutLeakingKey(t *testing.T) {
	for _, protocol := range []string{service.VideoProtocolGemini, service.VideoProtocolSeedance} {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
			for _, operation := range []string{"create", "status", "delete"} {
				if protocol == service.VideoProtocolGemini && operation == "delete" {
					continue
				}
				t.Run(fmt.Sprintf("%s/%d/%s", protocol, status, operation), func(t *testing.T) {
					var sinkCalls, sourceCalls atomic.Int32
					sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						sinkCalls.Add(1)
						t.Errorf("redirect target received request: method=%s", r.Method)
						_, _ = io.WriteString(w, `{"name":"models/veo/operations/mock-1","id":"ark-1"}`)
					}))
					defer sink.Close()
					target := strings.Replace(sink.URL, "127.0.0.1", "localhost", 1) + "/third-party"
					source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						sourceCalls.Add(1)
						if protocol == service.VideoProtocolGemini {
							require.Equal(t, "mock-upstream-secret", r.Header.Get("x-goog-api-key"))
						} else {
							require.Equal(t, "Bearer mock-upstream-secret", r.Header.Get("Authorization"))
						}
						http.Redirect(w, r, target, status)
					}))
					defer source.Close()
					cfg := &config.Config{}
					cfg.Security.URLAllowlist.AllowInsecureHTTP = true
					upstream := repository.NewHTTPUpstream(cfg)
					svc := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
					account := &service.Account{ID: 1, Platform: service.PlatformVideo, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "mock-upstream-secret", "video_protocol": protocol, "base_url": source.URL, "hide_video_upstream_url": true}}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/mock", nil)
					var result *service.OpenAIForwardResult
					var err error
					if protocol == service.VideoProtocolGemini {
						endpoint, id, body := service.GeminiVideoEndpointCreate, "", []byte(`{"model":"veo","instances":[{"prompt":"waves"}]}`)
						if operation == "status" {
							endpoint, id, body = service.GeminiVideoEndpointStatus, "gemini:models/veo/operations/mock-1", nil
						}
						result, err = svc.ForwardGeminiVideo(context.Background(), c, account, endpoint, id, body)
					} else {
						endpoint, id, body := service.SeedanceEndpointCreate, "", []byte(`{"model":"ark","content":[{"type":"text","text":"waves"}]}`)
						if operation == "status" {
							endpoint, id, body = service.SeedanceEndpointStatus, "seedance:ark-1", nil
						}
						if operation == "delete" {
							endpoint, id, body = service.SeedanceEndpointDelete, "seedance:ark-1", nil
						}
						result, err = svc.ForwardSeedance(context.Background(), c, account, endpoint, id, body)
					}
					require.Error(t, err)
					require.Nil(t, result)
					require.Equal(t, int32(1), sourceCalls.Load(), "async submissions must not retry")
					require.Zero(t, sinkCalls.Load(), "never follow the redirect or disclose credentials")
				})
			}
		}
	}
}

type videoProtocolReadError struct{}

func (videoProtocolReadError) Read([]byte) (int, error) { return 0, fmt.Errorf("read failed") }
func (videoProtocolReadError) Close() error             { return nil }

func TestGeminiVideoEntryLocalErrorsUseGoogleFormat(t *testing.T) {
	for _, tc := range []struct {
		name, method, body, modelAction, operation       string
		status                                           int
		readFailure, maxBytes, wrongPlatform, missingKey bool
	}{
		{name: "missing-key", method: http.MethodPost, status: 403, missingKey: true},
		{name: "wrong-platform", method: http.MethodPost, status: 403, wrongPlatform: true},
		{name: "invalid-operation", method: http.MethodGet, operation: "..", status: 400},
		{name: "invalid-action", method: http.MethodPost, modelAction: "/veo:generateContent", status: 400},
		{name: "invalid-json", method: http.MethodPost, modelAction: "/veo:predictLongRunning", body: `{`, status: 400},
		{name: "array", method: http.MethodPost, modelAction: "/veo:predictLongRunning", body: `[]`, status: 400},
		{name: "read-error", method: http.MethodPost, modelAction: "/veo:predictLongRunning", status: 400, readFailure: true},
		{name: "oversized", method: http.MethodPost, modelAction: "/veo:predictLongRunning", body: `{"instances":[{"prompt":"long"}]}`, status: 413, maxBytes: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &OpenAIGatewayHandler{}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tc.method, "/v1beta/models/veo:predictLongRunning", strings.NewReader(tc.body))
			if tc.readFailure {
				c.Request.Body = videoProtocolReadError{}
			}
			if tc.maxBytes {
				c.Request.Body = http.MaxBytesReader(w, c.Request.Body, 8)
			}
			c.Params = gin.Params{{Key: "modelAction", Value: tc.modelAction}, {Key: "model", Value: "veo"}, {Key: "operation", Value: tc.operation}}
			if !tc.missingKey {
				platform := service.PlatformVideo
				if tc.wrongPlatform {
					platform = service.PlatformGemini
				}
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: platform}})
			}
			h.GeminiVideoTasks(c)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, int64(tc.status), gjson.Get(w.Body.String(), "error.code").Int())
			require.NotEmpty(t, gjson.Get(w.Body.String(), "error.status").String())
			require.NotEmpty(t, gjson.Get(w.Body.String(), "error.message").String())
			require.False(t, gjson.Get(w.Body.String(), "error.type").Exists())
		})
	}
}

func TestMiniMaxVideoGatewayErrorsPreserveStatusAndReleaseSlots(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(fmt.Sprintf("%s/%d", version, status), func(t *testing.T) {
				h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolMiniMax, "https://relay.example/"+version, "true")
				upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}, "Retry-After": {"17"}, "Location": {"https://diagnostic.example/path?signature=header-secret"}}, Body: io.NopCloser(strings.NewReader(`<html>https://diagnostic.example/path?signature=body-secret</html>`))}, nil
				}
				c, w := grokMediaSlotContext(context.Background(), true)
				key, _ := middleware.GetAPIKeyFromContext(c)
				key.Group.Platform = service.PlatformVideo
				price := 0.1
				key.Group.VideoPrice768P = &price
				model := "MiniMax-Hailuo-2.3"
				if version == "v2" {
					model = "MiniMax-H3"
				}
				c.Request = httptest.NewRequest(http.MethodPost, "/"+version+"/video_generation", strings.NewReader(`{"model":"`+model+`","prompt":"waves","duration":6}`))
				c.Request.Header.Set("Content-Type", "application/json")
				h.MiniMaxVideoTasks(c)
				require.Equal(t, status, w.Code, w.Body.String())
				require.Equal(t, "17", w.Header().Get("Retry-After"))
				require.NotContains(t, w.Body.String(), "diagnostic.example")
				require.NotContains(t, w.Body.String(), "body-secret")
				require.Empty(t, w.Header().Get("Location"))
				require.True(t, gjson.Valid(w.Body.String()))
				require.Equal(t, 1, upstream.calls, "no retry after any attempted async submission")
				require.Empty(t, bindings.pending)
				require.Empty(t, bindings.billed)
				slots.assertReleased(t)
			})
		}
	}
}
