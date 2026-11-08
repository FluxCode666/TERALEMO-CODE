//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestCreationStudioRoutesRequireAuth registers the real user route group with a
// rejecting JWT middleware and proves all four creation endpoints sit inside the
// authenticated group.
func TestCreationStudioRoutesRequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	engine := gin.New()
	v1 := engine.Group("/api/v1")
	handlers := &handler.Handlers{CreationStudio: handler.NewCreationStudioHandler(nil, nil, nil)}
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "unauthorized"})
	})
	auditLog := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })

	require.NotPanics(t, func() {
		RegisterUserRoutes(v1, handlers, jwtAuth, auditLog, nil, middleware.NewPanelRateLimiter(rdb, nil))
	})

	for _, tc := range []struct {
		method string
		target string
	}{
		{http.MethodGet, "/api/v1/user/creation/options?api_key_id=1"},
		{http.MethodPost, "/api/v1/user/creation/estimate"},
		{http.MethodPost, "/api/v1/user/creation/uploads"},
		{http.MethodPost, "/api/v1/user/creation/tasks"},
	} {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.target, strings.NewReader("{}"))
			engine.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}

// TestCreationStudioBodyLimitIsEnforced proves the submit endpoint rejects a
// 2 MiB body through the route-level limit and keeps the contract's reason.
func TestCreationStudioBodyLimitIsEnforced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	engine := gin.New()
	group := engine.Group("/api/v1/user")
	group.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	studio := handler.NewCreationStudioHandler(nil, nil, nil)
	group.POST("/creation/tasks", middleware.RequestBodyLimit(1<<20), studio.Submit)

	payload := `{"prompt":"` + strings.Repeat("a", 2<<20) + `"}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/creation/tasks", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Equal(t, "CREATION_BODY_TOO_LARGE", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
	require.Equal(t, float64(http.StatusRequestEntityTooLarge), gjson.GetBytes(recorder.Body.Bytes(), "code").Float())
}
