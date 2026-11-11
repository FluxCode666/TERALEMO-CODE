package middleware

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func isTotpEnrollmentRoute(method, path string) bool {
	switch method + " " + path {
	case "GET /api/v1/auth/me", "POST /api/v1/auth/logout", "POST /api/v1/auth/revoke-all-sessions",
		"GET /api/v1/user/totp/status", "GET /api/v1/user/totp/verification-method",
		"POST /api/v1/user/totp/send-code", "POST /api/v1/user/totp/setup", "POST /api/v1/user/totp/enable":
		return true
	default:
		return false
	}
}

func enforceTotpEnrollment(c *gin.Context, auth *service.AuthService, user *service.User, claims *service.JWTClaims) bool {
	requiresSetup, err := auth.ValidateConsoleSession(c.Request.Context(), user, claims)
	if err != nil {
		if errors.Is(err, service.ErrTokenRevoked) {
			AbortWithError(c, http.StatusUnauthorized, "TOKEN_REVOKED", "Session has been revoked; please sign in again")
		} else {
			AbortWithError(c, http.StatusServiceUnavailable, "SESSION_UNAVAILABLE", "Session verification unavailable")
		}
		return false
	}
	if requiresSetup && !isTotpEnrollmentRoute(c.Request.Method, c.Request.URL.Path) {
		AbortWithError(c, http.StatusForbidden, "TOTP_SETUP_REQUIRED", "Configure TOTP two-factor authentication to continue")
		return false
	}
	return true
}
