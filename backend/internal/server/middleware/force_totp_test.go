//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForceTotpMiddlewareOnlyAllowsEnrollmentAndRestoresAccess(t *testing.T) {
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}
			repo := &bmSettingRepo{values: map[string]string{service.SettingKeyForceTotp: "true"}}
			settings := service.NewSettingService(repo, cfg)
			user := &service.User{ID: 12, Email: "enroll@example.com", Role: role, Status: service.StatusActive}
			users := &stubJWTUserRepo{users: map[int64]*service.User{12: user}}
			auth := service.NewAuthService(nil, users, nil, nil, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
			userService := service.NewUserService(users, nil, nil, nil)
			token, err := auth.GenerateToken(context.Background(), user)
			require.NoError(t, err)
			router := gin.New()
			router.Use(gin.HandlerFunc(NewJWTAuthMiddleware(auth, userService, settings, nil)))
			for _, route := range []struct{ method, path string }{
				{"GET", "/api/v1/auth/me"}, {"GET", "/api/v1/user/totp/status"}, {"GET", "/api/v1/user/totp/verification-method"},
				{"POST", "/api/v1/user/totp/send-code"}, {"POST", "/api/v1/user/totp/setup"}, {"POST", "/api/v1/user/totp/enable"},
				{"GET", "/api/v1/user/profile"}, {"POST", "/api/v1/user/totp/disable"}, {"GET", "/api/v1/admin/settings"},
				{"POST", "/api/v1/user/totp/setup/extra"}, {"PUT", "/api/v1/user/totp/setup"},
			} {
				router.Handle(route.method, route.path, func(ctx *gin.Context) { ctx.Status(200) })
			}
			request := func(method, path string) *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(method, path, nil)
				request.Header.Set("Authorization", "Bearer "+token)
				router.ServeHTTP(recorder, request)
				return recorder
			}
			for _, route := range []struct{ method, path string }{
				{"GET", "/api/v1/auth/me"}, {"GET", "/api/v1/user/totp/status"}, {"GET", "/api/v1/user/totp/verification-method"},
				{"POST", "/api/v1/user/totp/send-code"}, {"POST", "/api/v1/user/totp/setup"}, {"POST", "/api/v1/user/totp/enable"},
			} {
				require.Equal(t, 200, request(route.method, route.path).Code, route.path)
			}
			for _, route := range []struct{ method, path string }{
				{"GET", "/api/v1/user/profile"}, {"POST", "/api/v1/user/totp/disable"}, {"GET", "/api/v1/admin/settings"},
				{"POST", "/api/v1/user/totp/setup/extra"}, {"PUT", "/api/v1/user/totp/setup"},
			} {
				result := request(route.method, route.path)
				require.Equal(t, 403, result.Code)
				require.Contains(t, result.Body.String(), "TOTP_SETUP_REQUIRED")
			}
			user.TotpEnabled = true
			require.Equal(t, 200, request("GET", "/api/v1/user/profile").Code)
			user.TotpEnabled = false
			repo.values[service.SettingKeyForceTotp] = "false"
			require.Equal(t, 200, request("GET", "/api/v1/user/profile").Code)
		})
	}
}

func TestForceTotpAdminJWTAndAPIKeyModelIsolation(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeSimple, JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}
	settings := service.NewSettingService(&bmSettingRepo{values: map[string]string{service.SettingKeyForceTotp: "true"}}, cfg)
	admin := &service.User{ID: 4, Email: "admin@example.com", Role: service.RoleAdmin, Status: service.StatusActive}
	users := &stubJWTUserRepo{users: map[int64]*service.User{4: admin}}
	auth := service.NewAuthService(nil, users, nil, nil, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	userService := service.NewUserService(users, nil, nil, nil)
	token, err := auth.GenerateToken(context.Background(), admin)
	require.NoError(t, err)
	router := gin.New()
	router.GET("/api/v1/admin/settings", gin.HandlerFunc(NewAdminAuthMiddleware(auth, userService, settings, nil)), func(ctx *gin.Context) { ctx.Status(200) })
	key := &service.APIKey{ID: 1, Key: "model-key", Status: service.StatusActive, User: admin}
	keys := service.NewAPIKeyService(&stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { copyKey := *key; return &copyKey, nil }}, nil, nil, nil, nil, nil, cfg)
	router.POST("/v1/responses", gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)), func(ctx *gin.Context) { ctx.Status(200) })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, 403, recorder.Code)
	require.Contains(t, recorder.Body.String(), "TOTP_SETUP_REQUIRED")
	request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request.Header.Set("Authorization", "Bearer model-key")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
}
