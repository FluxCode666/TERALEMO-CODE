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

func TestAdminAsyncTaskRoutesRequireAdminRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) {
		if role := c.GetHeader("X-Test-Role"); role != "" {
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
	})
	h := &handler.Handlers{AsyncImage: handler.NewAsyncImageHandler(service.NewImageTaskService(nil), nil)}
	registerAsyncTaskRoutes(admin, h, nil)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, ""}, {http.MethodGet, "/10/task"}, {http.MethodPost, "/10/task/refresh"}, {http.MethodGet, "/10/task/content"},
	} {
		for _, role := range []string{"", service.RoleUser, service.RoleAdmin} {
			req := httptest.NewRequest(route.method, "/api/v1/admin/async-tasks"+route.path, nil)
			req.Header.Set("X-Test-Role", role)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			want := http.StatusServiceUnavailable // Authorized; storage is deliberately unavailable.
			if role == "" {
				want = http.StatusUnauthorized
			} else if role != service.RoleAdmin {
				want = http.StatusForbidden
			}
			require.Equal(t, want, w.Code, route.method+route.path+role+": "+w.Body.String())
		}
	}
}
