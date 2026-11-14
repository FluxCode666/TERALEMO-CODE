//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func backupConfigTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{Backup: admin.NewBackupHandler(nil, nil)}}
	stepUp := middleware.StepUpAuthMiddleware(func(context *gin.Context) {
		context.AbortWithStatus(http.StatusForbidden)
	})
	registerBackupRoutes(router.Group("/api/v1/admin"), handlers, stepUp)
	return router
}

func TestDatabaseBackupRoutesAreNotRegistered(t *testing.T) {
	router := backupConfigTestRouter()
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, ""},
		{http.MethodGet, ""},
		{http.MethodGet, "/archive"},
		{http.MethodDelete, "/archive"},
		{http.MethodGet, "/archive/download-url"},
		{http.MethodPost, "/archive/restore"},
		{http.MethodGet, "/schedule"},
		{http.MethodPut, "/schedule"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(route.method, "/api/v1/admin/backups"+route.path, nil))
			require.Equal(t, http.StatusNotFound, response.Code)
		})
	}
}

func TestObjectStorageConfigRoutesRemainRegistered(t *testing.T) {
	router := backupConfigTestRouter()
	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.Len(t, routes, 6)
	for _, path := range []string{"/s3-config", "/image-storage"} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			require.True(t, routes[method+" /api/v1/admin/backups"+path])
		}
		require.True(t, routes[http.MethodPost+" /api/v1/admin/backups"+path+"/test"])
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/admin/backups"+path, nil))
		require.Equal(t, http.StatusForbidden, response.Code, "configuration writes must retain step-up authentication")
	}
}
