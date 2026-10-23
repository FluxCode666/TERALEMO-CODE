//go:build unit

package middleware

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMiniMaxVideoReadAllowsRetrievalAfterBalanceExhaustion(t *testing.T) {
	for _, path := range []string{"/v1/query/video_generation", "/v1/files/retrieve", "/v2/query/video_generation/task-1"} {
		require.True(t, isAsyncVideoTaskRead(http.MethodGet, path))
		require.False(t, isAsyncVideoTaskRead(http.MethodPost, path))
	}
	require.False(t, isAsyncVideoTaskRead(http.MethodPost, "/v1/video_generation"))
	require.False(t, isAsyncVideoTaskRead(http.MethodGet, "/v1/files/retrieve-other"))
	require.False(t, isAsyncVideoTaskRead(http.MethodGet, "/v2/query/video_generation/"))
	require.False(t, isAsyncVideoTaskRead(http.MethodGet, "/v2/query/video_generation/task-1/extra"))
}

func TestPrivateVideoDownloadAllowsOwnedReadAfterBalanceExhaustion(t *testing.T) {
	path := strings.Split(service.VideoContentProxyURL("minimax:task-1", 0), "?")[0]
	require.True(t, isAsyncVideoTaskRead(http.MethodGet, path))
	require.False(t, isAsyncVideoTaskRead(http.MethodPost, path))
	require.False(t, isAsyncVideoTaskRead(http.MethodGet, "/v1/video-tasks/not-a-resource/content"))
}
