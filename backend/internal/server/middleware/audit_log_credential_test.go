//go:build unit

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAuditJWTRequestMasksAdditionalAPIKeyWithSharedHint(t *testing.T) {
	for _, credential := range []string{"4b9e6a1f80c2d573", "sk-0123456789abcdef", "tenant_" + strings.Repeat("a", 32), "sk-" + strings.Repeat("b", 64), ""} {
		t.Run(credential, func(t *testing.T) {
			user := &service.User{ID: 1, Email: "audit@example.invalid", Role: "user", Status: service.StatusActive, TokenVersion: 1}
			router, authService := newJWTTestEnv(map[int64]*service.User{1: user})
			repo := &auditCaptureRepository{}
			auditService := service.NewAuditLogService(repo, nil)
			auditService.Start()
			t.Cleanup(auditService.Stop)
			router.Use(gin.HandlerFunc(NewAuditLogMiddleware(auditService)))
			router.PUT("/api/v1/user", func(c *gin.Context) { c.Status(http.StatusOK) })
			token, err := authService.GenerateToken(t.Context(), user)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPut, "/api/v1/user", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("x-api-key", credential)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			auditService.Stop()
			repo.mu.Lock()
			defer repo.mu.Unlock()
			require.Len(t, repo.logs, 1)
			want := "Bearer ..."
			if credential != "" {
				want = "x-api-key " + service.MaskAPIKey(credential)
			}
			require.Equal(t, want, repo.logs[0].CredentialMasked)
			require.NotContains(t, repo.logs[0].CredentialMasked, token)
		})
	}
}

func TestMaskedRequestCredentialUnknownTokenIsFullyHidden(t *testing.T) {
	for _, authorization := range []string{"Bearer abcdef1234567890", "Basic dXNlcjpwYXNz", "credential-with-no-scheme", "custom-secret-scheme abcdef"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		ctx.Request.Header.Set("Authorization", authorization)
		hint := MaskedRequestCredential(ctx)
		require.Contains(t, hint, "...")
		require.NotContains(t, hint, "abcdef")
		require.NotContains(t, hint, "custom-secret-scheme")
		require.NotContains(t, hint, "credential-with-no-scheme")
	}
}

func TestMaskedRequestCredentialAdminAPIKeyUsesSharedHint(t *testing.T) {
	for _, secret := range []string{"4b9e6a1f80c2d573", "sk-0123456789abcdef", "admin-" + strings.Repeat("c", 64)} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		ctx.Request.Header.Set("Authorization", "Bearer "+secret)
		ctx.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
		require.Equal(t, "Bearer "+service.MaskAPIKey(secret), MaskedRequestCredential(ctx))
	}
}
