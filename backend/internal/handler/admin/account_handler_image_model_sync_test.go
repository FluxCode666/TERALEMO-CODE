//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageSyncAdminHTTP struct {
	service.HTTPUpstream
	client *http.Client
	calls  atomic.Int32
}

func (u *imageSyncAdminHTTP) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls.Add(1)
	return u.client.Do(req)
}

func imageSyncAdminRouter(t *testing.T, protocol string, serve http.HandlerFunc) (*gin.Engine, *imageSyncAdminHTTP, string) {
	t.Helper()
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	upstream := &imageSyncAdminHTTP{client: server.Client()}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	testService := service.NewAccountTestService(nil, nil, nil, nil, nil, upstream, cfg, nil)
	admin := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
		ID: 42, Platform: service.PlatformImage, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "image-sync-secret", "base_url": server.URL, "image_protocol": protocol}}}
	handler := NewAccountHandler(admin, nil, nil, nil, nil, nil, nil, nil, testService, nil, nil, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/accounts/:id/models/sync-upstream", handler.SyncUpstreamModels)
	router.POST("/accounts/models/sync-upstream-preview", handler.SyncUpstreamModelsPreview)
	return router, upstream, server.URL
}

func imageSyncAdminRequest(router *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	return rec
}

func TestAccountHandlerImageUpstreamModelSyncSavedAndPreview(t *testing.T) {
	for _, tc := range []struct{ protocol, path, body, want string }{
		{"openai", "/v1/models", `{"data":[{"id":"gpt-image-2"},{"id":"gpt-5"}]}`, "gpt-image-2"},
		{"", "/v1/models", `{"data":[{"id":"gpt-image-2"},{"id":"gpt-5"}]}`, "gpt-image-2"},
		{"gemini", "/v1beta/models", `{"models":[{"name":"models/gemini-2.5-flash-image"},{"name":"models/gemini-2.5-pro"}]}`, "gemini-2.5-flash-image"},
		{"grok", "/v1/image-generation-models", `{"data":[{"id":"grok-imagine-image"}]}`, "grok-imagine-image"},
	} {
		for _, saved := range []bool{false, true} {
			name := tc.protocol + "/preview"
			if saved {
				name = tc.protocol + "/saved"
			}
			t.Run(name, func(t *testing.T) {
				var path, bearer, googleKey string
				router, upstream, base := imageSyncAdminRouter(t, tc.protocol, func(w http.ResponseWriter, r *http.Request) {
					path, bearer, googleKey = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-goog-api-key")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.body))
				})
				requestPath := "/accounts/models/sync-upstream-preview"
				if saved {
					requestPath = "/accounts/42/models/sync-upstream"
				}
				rec := imageSyncAdminRequest(router, requestPath, map[string]any{
					"platform": "image", "type": "apikey", "api_key": "image-sync-secret", "base_url": base, "image_protocol": tc.protocol,
				})
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var result struct {
					Data service.UpstreamModelCatalog `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				require.Equal(t, []string{tc.want}, result.Data.Models)
				require.Empty(t, result.Data.Warnings)
				require.Empty(t, result.Data.Metadata)
				require.Equal(t, tc.path, path)
				if tc.protocol == "gemini" {
					require.Equal(t, "image-sync-secret", googleKey)
					require.Empty(t, bearer)
				} else {
					require.Equal(t, "Bearer image-sync-secret", bearer)
					require.Empty(t, googleKey)
				}
				require.EqualValues(t, 1, upstream.calls.Load())
			})
		}
	}
}

func TestAccountHandlerImageUpstreamModelSyncPreviewValidation(t *testing.T) {
	for _, protocol := range []any{"invalid", 123} {
		router, upstream, base := imageSyncAdminRouter(t, "openai", func(http.ResponseWriter, *http.Request) {
			t.Error("invalid preview must not send an upstream request")
		})
		rec := imageSyncAdminRequest(router, "/accounts/models/sync-upstream-preview", map[string]any{
			"platform": "image", "type": "apikey", "api_key": "image-sync-secret", "base_url": base, "image_protocol": protocol,
		})
		require.Equal(t, 400, rec.Code, rec.Body.String())
		require.Zero(t, upstream.calls.Load())
		require.NotContains(t, rec.Body.String(), "image-sync-secret")
	}
	// Other platforms ignore this field entirely, including its JSON type.
	for _, protocol := range []any{"invalid", 123, map[string]any{"unused": true}} {
		router, upstream, base := imageSyncAdminRouter(t, "openai", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":"text-model","reasoning":false,"input_modalities":["text"],"context_window":32000}]}`))
		})
		rec := imageSyncAdminRequest(router, "/accounts/models/sync-upstream-preview", map[string]any{
			"platform": "openai", "type": "apikey", "api_key": "test", "base_url": base, "image_protocol": protocol,
		})
		require.Equal(t, 200, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "text-model")
		require.EqualValues(t, 1, upstream.calls.Load())
	}
}

func TestAccountHandlerImageUpstreamModelSyncWarningsAndSafeErrors(t *testing.T) {
	for _, status := range []int{200, 401} {
		for _, saved := range []bool{false, true} {
			router, upstream, base := imageSyncAdminRouter(t, "gemini", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				if status == 401 {
					_, _ = w.Write([]byte(`{"error":"image-sync-secret private raw body https://relay.example /srv/render"}`))
				} else {
					_, _ = w.Write([]byte(`{"models":[{"name":"models/custom-render"}]}`))
				}
			})
			path := "/accounts/models/sync-upstream-preview"
			if saved {
				path = "/accounts/42/models/sync-upstream"
			}
			rec := imageSyncAdminRequest(router, path, map[string]any{
				"platform": "image", "type": "apikey", "api_key": "image-sync-secret", "base_url": base, "image_protocol": "gemini",
			})
			if status == 200 {
				require.Equal(t, 200, rec.Code, rec.Body.String())
				var result struct {
					Data service.UpstreamModelCatalog `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				require.Equal(t, []string{"custom-render"}, result.Data.Models)
				require.Len(t, result.Data.Warnings, 1)
				require.Equal(t, service.ImageModelsNotIdentifiedCode, result.Data.Warnings[0].Code)
			} else {
				require.Equal(t, 502, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "HTTP 401")
				for _, sensitive := range []string{"image-sync-secret", "private raw body", "relay.example", "/srv/render"} {
					require.NotContains(t, rec.Body.String(), sensitive)
				}
			}
			require.EqualValues(t, 1, upstream.calls.Load())
		}
	}
}
