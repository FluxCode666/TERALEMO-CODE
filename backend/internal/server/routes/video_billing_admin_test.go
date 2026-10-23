//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoBillingAdminRoutesAreRegisteredAndRestricted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) {
		if role := c.GetHeader("X-Test-Role"); role != "" {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
	})
	registerVideoBillingReservationRoutes(admin, &handler.Handlers{}, nil)
	for _, route := range []struct{ method, path string }{{http.MethodGet, ""}, {http.MethodPost, "/r-1/release"}, {http.MethodPost, "/r-1/settle"}} {
		for _, role := range []string{"", service.RoleUser, service.RoleAdmin} {
			request := httptest.NewRequest(route.method, "/api/v1/admin/video-billing-reservations"+route.path, nil)
			request.Header.Set("X-Test-Role", role)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			want := http.StatusServiceUnavailable
			if role == "" {
				want = http.StatusUnauthorized
			} else if role == service.RoleUser {
				want = http.StatusForbidden
			}
			require.Equal(t, want, w.Code, route.method+route.path+role+": "+w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"), route.method+route.path+role)
		}
	}
}

func TestVideoBillingAdminRoutesAuthRejectionIsNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{}}
	auth := middleware.AdminAuthMiddleware(func(c *gin.Context) {
		status := http.StatusUnauthorized
		if c.GetHeader("Authorization") != "" {
			status = http.StatusForbidden
		}
		middleware.AbortWithError(c, status, "ACCESS_DENIED", "Access denied")
	})
	audit := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), handlers, auth, audit, stepUp, nil, nil)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/video-billing-reservations"},
		{http.MethodPost, "/api/v1/admin/video-billing-reservations/r-1/release"},
		{http.MethodPost, "/api/v1/admin/video-billing-reservations/r-1/settle"},
		{http.MethodGet, "/api/v1/admin/users"},
	} {
		for _, token := range []string{"", "Bearer non-admin-token"} {
			request := httptest.NewRequest(route.method, route.path, nil)
			request.Header.Set("Authorization", token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			wantStatus := http.StatusUnauthorized
			if token != "" {
				wantStatus = http.StatusForbidden
			}
			require.Equal(t, wantStatus, w.Code, route.method+route.path)
			wantCache := "no-store"
			if route.path == "/api/v1/admin/users" {
				wantCache = ""
			}
			require.Equal(t, wantCache, w.Header().Get("Cache-Control"), route.method+route.path)
		}
	}
}
