package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoContentEndpointOpsPersistenceExcludesTaskResources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ route, path, want string }{
		{"/v1/video-tasks/:resource/content", "/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8vb3BlcmF0aW9ucy9hYmM/content", EndpointVideoTaskContent},
		{"/v1/video-tasks/:resource/content", "/v1/video-tasks/arbitrary-task/content", EndpointVideoTaskContent},
		{"/v1/videos/:id/content", "/v1/videos/arbitrary-task/content", EndpointVideos},
		{"/videos/:id/content", "/videos/arbitrary-task/content", EndpointVideos},
		{"/v1/videos/generations/:id/content", "/v1/videos/generations/arbitrary-task/content", EndpointVideosGenerations},
		{"/videos/generations/:id/content", "/videos/generations/arbitrary-task/content", EndpointVideosGenerations},
		{"/v1/videos/edits/:id/content", "/v1/videos/edits/arbitrary-task/content", EndpointVideosEdits},
		{"/videos/edits/:id/content", "/videos/edits/arbitrary-task/content", EndpointVideosEdits},
		{"/v1/videos/extensions/:id/content", "/v1/videos/extensions/arbitrary-task/content", EndpointVideosExtensions},
		{"/videos/extensions/:id/content", "/videos/extensions/arbitrary-task/content", EndpointVideosExtensions},
		{"/api/v1/user/async-tasks/:id/content", "/api/v1/user/async-tasks/arbitrary-task/content", EndpointUserAsyncTasks + "/:id/content"},
		{"/api/v1/admin/async-tasks/:user_id/:id/content", "/api/v1/admin/async-tasks/719/arbitrary-task/content", EndpointAdminAsyncTasks + "/:user_id/:id/content"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 1)
			defer resetOpsErrorLoggerStateForTest(t)
			repo := &ingressRejectOpsRepo{}
			ops := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops), InboundEndpointMiddleware())
			router.GET(tc.route, func(c *gin.Context) {
				setOpsSelectedAccount(c, 17, service.PlatformVideo)
				c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "Video unavailable"}})
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path+"?token=secret", nil))
			require.Equal(t, http.StatusBadGateway, w.Code)
			require.Equal(t, int64(1), OpsErrorLogQueueLength())
			job := <-opsErrorLogQueue
			flushOpsErrorLogBatch([]opsErrorLogJob{job})
			require.Len(t, repo.entries, 1)
			entry := repo.entries[0]
			require.Equal(t, tc.want, entry.InboundEndpoint)
			require.Equal(t, tc.want, entry.UpstreamEndpoint)
			require.Equal(t, tc.want, entry.RequestPath)
			require.NotContains(t, entry.RequestPath, "arbitrary-task")
			require.NotContains(t, entry.RequestPath, "Z2VtaW5p")
			require.NotContains(t, entry.RequestPath, "secret")
		})
	}
}

func TestVideoContentOpsNormalizationPreservesOtherRequestDiagnostics(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/responses/response-123", "/custom/arbitrary-task/content", "/api/v1/admin/users/719"} {
		require.Equal(t, path, normalizeVideoContentOpsRequestPath(path))
	}
}
