//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAuthSuccessUserEnterpriseUsesSharedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	balance := 42.5
	for _, identity := range []*service.EnterpriseIdentity{nil,
		{ID: 2, Name: "Acme", Role: "member", MemberStatus: "active", EnterpriseStatus: "active", FrozenBalance: &balance, Balance: &balance},
		{ID: 2, Name: "Acme", Role: "owner", BalanceVisible: true, FrozenBalance: &balance, Balance: &balance, MemberStatus: "active", EnterpriseStatus: "active"},
	} {
		for _, path := range []string{"register", "login"} {
			t.Run(path, func(t *testing.T) {
				h, client := newOAuthPendingFlowTestHandler(t, false)
				h.enterpriseIdentity = identityReaderStub{identity: identity}
				if path == "login" {
					hash, err := h.authService.HashPassword("secret-123")
					require.NoError(t, err)
					_, err = client.User.Create().SetEmail("enterprise-response@example.com").SetPasswordHash(hash).SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(context.Background())
					require.NoError(t, err)
				}
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+path, strings.NewReader(`{"email":"enterprise-response@example.com","password":"secret-123"}`))
				c.Request.Header.Set("Content-Type", "application/json")
				if path == "login" {
					h.Login(c)
				} else {
					h.Register(c)
				}
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				assertAuthEnterpriseResponse(t, w.Body.Bytes(), true, identity)
			})
		}
	}
}

func TestAuthTokenFallbackAndOAuthResponsesIncludeEnterprise(t *testing.T) {
	balance := 19.5
	identity := &service.EnterpriseIdentity{ID: 3, Name: "Acme", Role: "member", FrozenBalance: &balance, Balance: &balance}
	user := &service.User{ID: 11, Email: "response@example.com", Role: service.RoleUser, Status: service.StatusActive}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "auth-response-secret", ExpireHour: 1}}
	// Missing refresh storage deliberately exercises the legacy token fallback.
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	respondWithTokenPair(c, auth, user, identityReaderStub{identity: identity})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertAuthEnterpriseResponse(t, w.Body.Bytes(), true, identity)

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/oauth/complete", nil)
	h := &AuthHandler{enterpriseIdentity: identityReaderStub{identity: identity}}
	h.writeOAuthTokenPairResponse(c, &service.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600}, user)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertAuthEnterpriseResponse(t, w.Body.Bytes(), false, identity)
	require.Equal(t, &balance, identity.Balance, "visibility filtering must not mutate the source identity")
}

func TestAuthEnterpriseLookupFailureDoesNotIssueTokenResponse(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	respondWithTokenPair(c, nil, &service.User{ID: 11, Status: service.StatusActive}, identityReaderStub{err: errors.New("enterprise unavailable")})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), "access_token")
}
