package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoEndpointNormalizationAcrossPrefixesAndResourceIDs(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/api/v3/contents/generations/tasks", EndpointSeedanceTasks},
		{"/api/v3/contents/generations/tasks/task-001", EndpointSeedanceTasks},
		{"/v1beta/models/veo-3.1:predictLongRunning", EndpointGeminiVideoCreate},
		{"/v1beta/models/veo-3.1/operations/abc-123", EndpointGeminiVideoOperation},
		{"/v1/video_generation", EndpointMiniMaxVideoCreate},
		{"/v1/query/video_generation", EndpointMiniMaxVideoQuery},
		{"/v1/files/retrieve", EndpointMiniMaxVideoFile},
		{"/v2/video_generation", EndpointMiniMaxV2VideoCreate},
		{"/v2/query/video_generation/task-123", EndpointMiniMaxV2VideoQuery},
		{"/v1/videos/generations", EndpointVideosGenerations},
		{"/v1/videos/req_abc/content", EndpointVideos},
		{"/v1/video-tasks/short-resource/content", EndpointVideoTaskContent},
		{"/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8vb3BlcmF0aW9ucy9hYmM/content", EndpointVideoTaskContent},
		{"/videos/generations/opaque-resource/content", EndpointVideosGenerations},
		{"/v1/videos/generations/opaque-resource/content", EndpointVideosGenerations},
		{"/videos/edits/opaque-resource/content", EndpointVideosEdits},
		{"/v1/videos/edits/opaque-resource/content", EndpointVideosEdits},
		{"/videos/extensions/opaque-resource/content", EndpointVideosExtensions},
		{"/v1/videos/extensions/opaque-resource/content", EndpointVideosExtensions},
		{"/videos/opaque-resource/content", EndpointVideos},
	} {
		for _, prefix := range []string{"", "/openai", "/proxy/nested"} {
			for _, query := range []string{"", "?task_id=resource-secret&key=key-secret"} {
				path := prefix + tc.path + query
				t.Run(path, func(t *testing.T) {
					got := NormalizeInboundEndpoint(path)
					require.Equal(t, tc.want, got)
					require.NotContains(t, got, "secret")
					require.NotContains(t, got, "task-123")
				})
			}
		}
	}
}

func TestVideoEndpointMiddlewareKeepsTaskAndOperationIDsOutOfTelemetry(t *testing.T) {
	for _, tc := range []struct{ route, path, want string }{
		{"/api/v3/contents/generations/tasks/:id", "/api/v3/contents/generations/tasks/arbitrary-task", EndpointSeedanceTasks},
		{"/v1beta/models/*action", "/v1beta/models/veo:predictLongRunning", EndpointGeminiVideoCreate},
		{"/v1beta/models/*action", "/v1beta/models/veo/operations/arbitrary-operation", EndpointGeminiVideoOperation},
		{"/v2/query/video_generation/:task_id", "/v2/query/video_generation/arbitrary-task", EndpointMiniMaxV2VideoQuery},
		{"/v1/video-tasks/:resource/content", "/v1/video-tasks/Z2VtaW5pOm1vZGVscy92ZW8vb3BlcmF0aW9ucy9hYmM/content", EndpointVideoTaskContent},
		{"/v1/videos/generations/:request_id/content", "/v1/videos/generations/arbitrary-task/content", EndpointVideosGenerations},
		{"/videos/edits/:request_id/content", "/videos/edits/arbitrary-task/content", EndpointVideosEdits},
		{"/api/v1/user/async-tasks/:id/content", "/api/v1/user/async-tasks/arbitrary-task/content", EndpointUserAsyncTasks + "/:id/content"},
		{"/api/v1/admin/async-tasks/:user_id/:id/content", "/api/v1/admin/async-tasks/719/arbitrary-task/content", EndpointAdminAsyncTasks + "/:user_id/:id/content"},
		{"/api/v1/admin/async-tasks/:user_id/:id/refresh", "/api/v1/admin/async-tasks/719/arbitrary-task/refresh", EndpointAdminAsyncTasks + "/:user_id/:id/refresh"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			router := gin.New()
			router.Use(InboundEndpointMiddleware())
			var inbound string
			router.GET(tc.route, func(c *gin.Context) { inbound = GetInboundEndpoint(c); c.Status(http.StatusOK) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path+"?key=secret", nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, tc.want, inbound)
			require.False(t, strings.Contains(inbound, "arbitrary-"))
		})
	}
}

func TestVideoDeriveUpstreamEndpointDoesNotBecomeResponses(t *testing.T) {
	for _, endpoint := range []string{EndpointVideoTaskContent, EndpointSeedanceTasks, EndpointGeminiVideoCreate, EndpointGeminiVideoOperation,
		EndpointMiniMaxVideoCreate, EndpointMiniMaxVideoQuery, EndpointMiniMaxVideoFile, EndpointMiniMaxV2VideoCreate, EndpointMiniMaxV2VideoQuery,
		EndpointVideos, EndpointVideosGenerations, EndpointVideosEdits, EndpointVideosExtensions} {
		for _, platform := range []string{service.PlatformVideo, service.PlatformOpenAI, service.PlatformGrok} {
			require.Equal(t, endpoint, DeriveUpstreamEndpoint(endpoint, endpoint, platform), "%s %s", platform, endpoint)
		}
	}
	for _, endpoint := range []string{EndpointGeminiVideoCreate, EndpointGeminiVideoOperation} {
		require.Equal(t, endpoint, DeriveUpstreamEndpoint(endpoint, endpoint, service.PlatformGemini))
	}
	require.Empty(t, GetInboundEndpoint(nil))
	require.Equal(t, EndpointResponses, GetUpstreamEndpoint(nil, service.PlatformOpenAI))
}

func TestVideoEndpointNormalizationUnknownBase64URLResource(t *testing.T) {
	for _, resource := range []string{
		"Z2VtaW5pOm1vZGVscy92ZW8vb3BlcmF0aW9ucy9hYmM",
		"Az9_-" + strings.Repeat("b", 27),
	} {
		got := NormalizeInboundEndpoint("/custom/resources/" + resource + "/download?token=secret")
		require.Equal(t, "/custom/resources/:id/download", got)
		require.NotContains(t, got, resource)
	}
	for _, resource := range []string{"ordinary-route-name", strings.Repeat("g", 31), "long-segment.with-dot-that-is-not-base64url"} {
		require.Equal(t, "/custom/"+resource, NormalizeInboundEndpoint("/custom/"+resource))
	}
}
