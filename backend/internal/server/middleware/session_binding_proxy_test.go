//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bindingUserRepo struct {
	service.UserRepository
}

func (bindingUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 1, Status: service.StatusActive, TokenVersionResolved: true}, nil
}

type bindingRefreshCache struct {
	service.RefreshTokenCache
	data    *service.RefreshTokenData
	revoked int
}

func (cache *bindingRefreshCache) GetRefreshToken(context.Context, string) (*service.RefreshTokenData, error) {
	return cache.data, nil
}

func (cache *bindingRefreshCache) DeleteTokenFamily(context.Context, string) error {
	cache.revoked++
	return nil
}

func TestSessionBindingUsesTrustedAddressAcrossTokenPaths(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetTrustForwardedIPForAPIKeyACL(true)
	settings := service.NewSettingService(&panelRateLimitStubRepo{values: map[string]string{
		service.SettingKeyModelAllowlistEnabled: "false",
		service.SettingKeySessionBindingEnabled: "true",
	}}, cfg)
	bound := &service.SessionBinding{IP: "192.0.2.10", UserAgent: "test-agent"}
	for _, path := range []string{"access", "refresh"} {
		t.Run(path, func(t *testing.T) {
			cache := &bindingRefreshCache{data: &service.RefreshTokenData{
				UserID: 1, FamilyID: "test-family", BindingHash: bound.Hash(), SessionVersion: "", TokenVersion: 0, ExpiresAt: time.Now().Add(time.Hour),
			}}
			auth := service.NewAuthService(nil, bindingUserRepo{}, nil, cache, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(nil))
			router.Use(SessionBindingContext(cfg), TrustedProxyContext(settings, cfg))
			router.GET("/", func(c *gin.Context) {
				if path == "refresh" {
					_, err := auth.RefreshTokenPair(c.Request.Context(), "rt_test-token")
					require.ErrorIs(t, err, service.ErrSessionBindingMismatch)
					c.Status(http.StatusUnauthorized)
				} else if enforceSessionBinding(c, auth, settings, nil, &service.JWTClaims{BindingHash: bound.Hash(), SessionID: "test-family"}) {
					c.Status(http.StatusNoContent)
				}
			})
			for _, header := range []string{"", bound.IP, "198.51.100.20", "127.0.0.1"} {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.RemoteAddr = "192.0.2.30:1234"
				req.Header.Set("User-Agent", bound.UserAgent)
				req.Header.Set("X-Forwarded-For", header)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				require.Equal(t, http.StatusUnauthorized, response.Code)
			}
			require.Equal(t, 4, cache.revoked)
		})
	}
}

func TestPublicRateLimitUsesTrustedAddress(t *testing.T) {
	for _, peer := range []string{"192.0.2.30", "10.20.0.1"} {
		t.Run(peer, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetTrustForwardedIPForAPIKeyACL(true)
			settings := service.NewSettingService(&panelRateLimitStubRepo{values: map[string]string{
				service.SettingKeyModelAllowlistEnabled:  "false",
				service.SettingKeyPanelRateLimitSettings: `{"enabled":true,"public_ip_rpm":1}`,
			}}, cfg)
			allower := &fakePanelAllower{}
			limiter := &PanelRateLimiter{limiter: allower, settingService: settings}
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(nil))
			router.Use(SessionBindingContext(cfg), TrustedProxyContext(settings, cfg), limiter.PublicIP())
			router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			for i, header := range []string{"192.0.2.10", "198.51.100.20", "127.0.0.1"} {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.RemoteAddr = peer + ":1234"
				req.Header.Set("X-Forwarded-For", header)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				want := http.StatusTooManyRequests
				if i == 0 {
					want = http.StatusNoContent
				}
				require.Equal(t, want, response.Code)
			}
			require.Equal(t, map[string]int64{"panel:public:ip:" + peer: 3}, allower.counts)
		})
	}
}
