//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type allowlistSettingRepo struct {
	service.SettingRepository
	values map[string]string
}

func TestTrustedProxyContextMetadataCompatibilityAndSecurity(t *testing.T) {
	for _, test := range []struct {
		name               string
		enabled            bool
		trustForwarded     bool
		proxies            []string
		metadata, security string
	}{
		{"legacy metadata", false, true, nil, "203.0.113.42", "172.18.0.1"},
		{"legacy forwarding disabled", false, false, nil, "172.18.0.1", "172.18.0.1"},
		{"allowlist enabled", true, true, nil, "172.18.0.1", "172.18.0.1"},
		{"configured proxy", false, true, []string{"172.18.0.1"}, "198.51.100.2", "198.51.100.2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.Config{Server: config.ServerConfig{TrustedProxies: test.proxies}}
			cfg.SetTrustForwardedIPForAPIKeyACL(test.trustForwarded)
			settings := service.NewSettingService(&allowlistSettingRepo{values: map[string]string{
				service.SettingKeyModelAllowlistEnabled: strconv.FormatBool(test.enabled),
			}}, cfg)
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(test.proxies))
			router.Use(SessionBindingContext(cfg), TrustedProxyContext(settings, cfg))
			router.GET("/", func(c *gin.Context) {
				require.Equal(t, test.metadata, ip.GetClientIP(c))
				require.Equal(t, test.metadata, SecurityClientIP(c))
				require.Equal(t, test.security, ip.GetSecurityClientIP(c, true))
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = "172.18.0.1:1234"
			request.Header.Set("X-Real-IP", "172.18.0.1")
			request.Header.Set("CF-Connecting-IP", "203.0.113.42")
			request.Header.Set("X-Forwarded-For", "198.51.100.2")
			router.ServeHTTP(httptest.NewRecorder(), request)
		})
	}
}

func (repo *allowlistSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return repo.values, nil
}

func TestModelAllowlistAndPerKeyRulesBothApply(t *testing.T) {
	for _, google := range []bool{false, true} {
		for _, test := range []struct {
			name        string
			enabled     bool
			global, key []string
			peer, xff   string
			status      int
		}{
			{"disabled allows", false, nil, nil, "198.65.0.92:1234", "192.0.2.1", 200},
			{"disabled retains key ACL", false, nil, []string{"192.0.2.1"}, "198.65.0.92:1234", "192.0.2.1", 403},
			{"both allow", true, []string{"192.0.2.1"}, []string{"192.0.2.1"}, "192.0.2.1:1234", "192.0.2.1", 200},
			{"global denies", true, []string{"192.0.2.1"}, nil, "198.65.0.92:1234", "192.0.2.1", 403},
			{"key denies", true, []string{"192.0.2.1"}, []string{"203.0.113.1"}, "192.0.2.1:1234", "192.0.2.1", 403},
			{"nginx untrusted client spoof", true, []string{"192.0.2.1"}, nil, "127.0.0.1:1234", "192.0.2.1, 198.65.0.92", 403},
			{"cloudflare real client", true, []string{"192.0.2.1"}, []string{"192.0.2.1"}, "127.0.0.1:1234", "192.0.2.1, 172.69.1.2", 200},
		} {
			t.Run(test.name+map[bool]string{true: " google", false: " openai"}[google], func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple, Server: config.ServerConfig{TrustedProxies: []string{"127.0.0.1"}}}
				cfg.SetTrustForwardedIPForAPIKeyACL(true)
				encoded, err := json.Marshal(service.ModelAllowlistSettings{Enabled: test.enabled, Entries: test.global, Cloudflare: true})
				require.NoError(t, err)
				settings := service.NewSettingService(&allowlistSettingRepo{values: map[string]string{
					service.SettingKeyModelAllowlist: string(encoded), service.SettingKeyModelAllowlistEnabled: strconv.FormatBool(test.enabled),
				}}, cfg)
				key := &service.APIKey{ID: 1, Key: "test-key", Status: service.StatusActive, IPWhitelist: test.key, User: &service.User{ID: 2, Status: service.StatusActive}}
				repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { copyKey := *key; return &copyKey, nil }}
				keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
				keys.SetModelAllowlistSettings(settings)
				router := gin.New()
				require.NoError(t, router.SetTrustedProxies(nil))
				router.Use(SessionBindingContext(cfg), TrustedProxyContext(settings, cfg))
				if google {
					router.Use(APIKeyAuthWithSubscriptionGoogle(keys, nil, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)))
				}
				router.GET("/v1/responses", func(ctx *gin.Context) { ctx.Status(200) })
				request := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
				request.RemoteAddr = test.peer
				request.Header.Set("Authorization", "Bearer test-key")
				request.Header.Set("X-Forwarded-For", test.xff)
				request.Header.Set("CF-Connecting-IP", "192.0.2.1")
				request.Header.Set("X-Real-IP", "192.0.2.1")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(t, test.status, recorder.Code, recorder.Body.String())
				if test.enabled && test.status == 403 && test.name != "key denies" {
					require.Contains(t, recorder.Body.String(), "ip_not_allowed")
				}
			})
		}
	}
}
