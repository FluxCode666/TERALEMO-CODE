//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type imageModelSyncHTTP struct {
	HTTPUpstream
	client *http.Client
	mu     sync.Mutex
	paths  []string
	proxy  string
	id     int64
	limit  int
}

func (u *imageModelSyncHTTP) DoWithTLS(req *http.Request, proxy string, id int64, limit int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.mu.Lock()
	u.paths = append(u.paths, req.URL.RequestURI())
	u.proxy, u.id, u.limit = proxy, id, limit
	u.mu.Unlock()
	return u.client.Do(req)
}

func newImageModelSyncService(t *testing.T, serve http.HandlerFunc) (*AccountTestService, *imageModelSyncHTTP, string) {
	t.Helper()
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	cfg := upstreamModelSyncTestConfig()
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	upstream := &imageModelSyncHTTP{client: server.Client()}
	return &AccountTestService{cfg: cfg, httpUpstream: upstream}, upstream, server.URL
}

func TestImageUpstreamModelSyncProtocolsAndFiltering(t *testing.T) {
	for _, tc := range []struct {
		protocol, path, body string
		want                 []string
	}{
		{"openai", "/v1/models", `{"data":[{"id":"gpt-5"},{"id":"gpt-image-2"},{"id":"chatgpt-image-latest"},{"id":"dall-e-3"},{"id":"custom-image"}]}`, []string{"chatgpt-image-latest", "custom-image", "dall-e-3", "gpt-image-2"}},
		{"", "/v1/models", `{"data":[{"id":"gpt-image-2"},{"id":"gpt-5"}]}`, []string{"gpt-image-2"}},
		{"gemini", "/v1beta/models", `{"models":[{"name":"models/gemini-2.5-pro"},{"name":"models/gemini-2.5-flash-image"},{"name":"models/imagen-4.0-generate-001"},{"name":"models/gemini-3-pro-image-preview"},{"name":"models/gemini-2.0-flash-preview-image-generation"}]}`, []string{"gemini-2.0-flash-preview-image-generation", "gemini-2.5-flash-image", "gemini-3-pro-image-preview", "imagen-4.0-generate-001"}},
		{"grok", "/v1/image-generation-models", `{"models":[{"id":"image-label","model":"grok-imagine-image"},{"id":"custom-generator"}]}`, []string{"custom-generator", "grok-imagine-image"}},
	} {
		t.Run(tc.protocol+tc.path, func(t *testing.T) {
			var auth, googleKey string
			svc, upstream, base := newImageModelSyncService(t, func(w http.ResponseWriter, r *http.Request) {
				auth, googleKey = r.Header.Get("Authorization"), r.Header.Get("x-goog-api-key")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})
			account := &Account{ID: 31, Platform: PlatformImage, Type: AccountTypeAPIKey, Concurrency: 7,
				Credentials: map[string]any{"api_key": "image-secret", "base_url": base, "image_protocol": tc.protocol}}
			catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, tc.want, catalog.Models)
			require.Empty(t, catalog.Metadata)
			require.Empty(t, catalog.Warnings)
			require.Equal(t, []string{tc.path}, upstream.paths, "image sync must not contact the capability registry")
			if tc.protocol == "gemini" {
				require.Equal(t, "image-secret", googleKey)
				require.Empty(t, auth)
			} else {
				require.Equal(t, "Bearer image-secret", auth)
				require.Empty(t, googleKey)
			}
			require.Equal(t, PlatformImage, account.Platform)
			require.Equal(t, base, account.GetCredential("base_url"), "request builder must not mutate credentials")
			require.Equal(t, int64(31), upstream.id)
			require.Equal(t, 7, upstream.limit)
		})
	}
}

func TestImageUpstreamModelSyncGeminiPagination(t *testing.T) {
	svc, upstream, base := newImageModelSyncService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "next + /?" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/imagen-4.0-generate-001"},{"name":"models/gemini-2.5-flash-image"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-text"},{"name":"models/gemini-2.5-flash-image"}],"nextPageToken":"next + /?"}`))
	})
	account := &Account{Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": base + "/v1beta", "image_protocol": "gemini"}}
	models, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"gemini-2.5-flash-image", "imagen-4.0-generate-001"}, models)
	require.Equal(t, []string{"/v1beta/models", "/v1beta/models?pageToken=next+%2B+%2F%3F"}, upstream.paths)
}

func TestImageUpstreamModelSyncGeminiPaginationLimit(t *testing.T) {
	svc, upstream, base := newImageModelSyncService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-image"}],"nextPageToken":"again"}`))
	})
	_, err := svc.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: PlatformImage, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test", "base_url": base, "image_protocol": "gemini"}})
	var typed *UpstreamModelSyncError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "Upstream model list pagination did not finish", typed.SafeMessage())
	require.Len(t, upstream.paths, imageUpstreamModelsMaxPages)
}

func TestImageUpstreamModelSyncGrokFallback(t *testing.T) {
	for _, status := range []int{404, 405, 401, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			svc, upstream, base := newImageModelSyncService(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/image-generation-models") {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":"image-secret raw body /srv/stack"}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"model":"grok-text"},{"id":"grok-imagine-image"},{"id":"custom-imagine"},{"id":"custom-image"}]}`))
			})
			catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: PlatformImage, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "image-secret", "base_url": base + "/v1", "image_protocol": "grok",
					"model_mapping": map[string]any{"public": "configured-image"}}})
			if status == 404 || status == 405 {
				require.NoError(t, err)
				require.Equal(t, []string{"custom-image", "custom-imagine", "grok-imagine-image"}, catalog.Models)
				require.Empty(t, catalog.Warnings)
				require.Equal(t, []string{"/v1/image-generation-models", "/v1/models"}, upstream.paths)
			} else {
				var typed *UpstreamModelSyncError
				require.ErrorAs(t, err, &typed)
				require.NotContains(t, typed.SafeMessage(), "image-secret")
				require.NotContains(t, typed.SafeMessage(), "raw body")
				require.NotContains(t, typed.SafeMessage(), "/srv/stack")
				require.Len(t, upstream.paths, 1, "only 404/405 may fall back")
			}
		})
	}
}

func TestImageUpstreamModelSyncUnidentifiedAndEmpty(t *testing.T) {
	for _, protocol := range []string{"openai", "gemini", "grok"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", protocol, empty), func(t *testing.T) {
				svc, _, base := newImageModelSyncService(t, func(w http.ResponseWriter, r *http.Request) {
					if protocol == "grok" && strings.HasSuffix(r.URL.Path, "/image-generation-models") {
						w.WriteHeader(404)
						return
					}
					if empty {
						_, _ = w.Write([]byte(`{"data":[]}`))
					} else {
						_, _ = w.Write([]byte(`{"data":[{"id":"render-b"},{"id":"render-a"},{"id":"render-a"}]}`))
					}
				})
				catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: PlatformImage, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "test", "base_url": base, "image_protocol": protocol}})
				if empty {
					var typed *UpstreamModelSyncError
					require.ErrorAs(t, err, &typed)
					require.Equal(t, "Upstream returned no supported models", typed.SafeMessage())
				} else {
					require.NoError(t, err)
					require.Equal(t, []string{"render-a", "render-b"}, catalog.Models)
					require.Len(t, catalog.Warnings, 1)
					require.Equal(t, ImageModelsNotIdentifiedCode, catalog.Warnings[0].Code)
					require.NotEmpty(t, catalog.Warnings[0].Message)
				}
			})
		}
	}
}

func TestImageUpstreamModelSyncDefaultsAndProxy(t *testing.T) {
	svc := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	for _, tc := range []struct{ protocol, want string }{
		{"", "https://api.openai.com/v1/models"},
		{"openai", "https://api.openai.com/v1/models"},
		{" GEMINI ", "https://generativelanguage.googleapis.com/v1beta/models"},
		{"grok", "https://api.x.ai/v1/image-generation-models"},
	} {
		req, err := svc.buildUpstreamModelsRequest(context.Background(), &Account{Platform: PlatformImage, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "test", "image_protocol": tc.protocol}})
		require.NoError(t, err)
		require.Equal(t, tc.want, req.URL.String())
	}
	svc, upstream, base := newImageModelSyncService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-image-2"}]}`))
	})
	proxyID := int64(4)
	proxy := &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
	_, err := svc.SyncUpstreamModelCatalog(context.Background(), &Account{ID: 19, Platform: PlatformImage, Type: AccountTypeAPIKey,
		ProxyID: &proxyID, Proxy: proxy, Credentials: map[string]any{"api_key": "test", "base_url": base}})
	require.NoError(t, err)
	require.Equal(t, proxy.URL(), upstream.proxy)

	svc.cfg.Security.URLAllowlist.Enabled = true
	svc.cfg.Security.URLAllowlist.UpstreamHosts = []string{"allowed.example"}
	_, err = svc.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: PlatformImage, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test", "base_url": "https://blocked.example"}})
	var typed *UpstreamModelSyncError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, UpstreamModelSyncErrorConfiguration, typed.Kind)
	require.Len(t, upstream.paths, 1, "disallowed host must not reach transport")
}

func TestImageUpstreamModelSyncDoesNotChangeLegacyLists(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGemini, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			svc, upstream, base := newImageModelSyncService(t, func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "text-model"}, {"id": "custom-image"}}})
			})
			models, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{Platform: platform, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "base_url": base, "image_protocol": "invalid-ignored"}})
			require.NoError(t, err)
			require.Equal(t, []string{"custom-image", "text-model"}, models)
			path := "/v1/models"
			if platform == PlatformGemini {
				path = "/v1beta/models"
			}
			require.Equal(t, []string{path}, upstream.paths)
		})
	}
}
