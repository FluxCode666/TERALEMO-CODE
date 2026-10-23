//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrivateMiniMaxVideoAPIAndPanelNeverExposeUpstreamURL(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolMiniMax, "https://metaso.cn/api/minimax/v2", "true")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.asyncTasks = service.NewImageTaskService(repository.NewImageTaskStore(rdb))
	newContext := func(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
		c, w := grokMediaSlotContext(context.Background(), method == http.MethodPost)
		key, _ := middleware.GetAPIKeyFromContext(c)
		key.Group.Platform = service.PlatformVideo
		price := 0.4
		key.Group.VideoPrice768P = &price
		body := ""
		if method == http.MethodPost {
			body = `{"model":"MiniMax-H3","prompt":"waves","duration":6,"resolution":"768P"}`
		}
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c, w
	}
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		if req.URL.Host == "files.metaso.cn" {
			require.Empty(t, req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("x-goog-api-key"))
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}, "Location": {"https://files.metaso.cn/leak"}}, Body: io.NopCloser(strings.NewReader("private-video")), ContentLength: 13}, nil
		}
		require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		body := `{"task_id":"task-private"}`
		if req.Method == http.MethodGet {
			require.Equal(t, "/api/minimax/v2/query/video_generation/task-private", req.URL.Path)
			body = `{"task":{"id":"task-private","status":"succeeded","content":{"url":"https://files.metaso.cn/video.mp4?signature=secret"},"duration":6,"resolution":"768P"}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	c, w := newContext(http.MethodPost, "/v2/video_generation")
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	c, w = newContext(http.MethodGet, "/v2/query/video_generation/task-private")
	c.Params = gin.Params{{Key: "task_id", Value: "task-private"}}
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "metaso")
	require.NotContains(t, w.Body.String(), "signature")
	proxyURL := gjson.Get(w.Body.String(), "task.content.url").String()
	require.True(t, strings.HasPrefix(proxyURL, "/v1/video-tasks/"))
	require.Len(t, bindings.reservations.settlements, 1)
	page, err := h.asyncTasks.ListUserTasks(context.Background(), 10, 1, 20, "video", "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	// The raw link stays in server-side task storage; every panel surface masks it.
	require.Contains(t, page.Items[0].ResultURL, "metaso")
	panel := &AsyncImageHandler{tasks: h.asyncTasks, openAI: h}
	c, _ = newContext(http.MethodGet, "/")
	key, _ := middleware.GetAPIKeyFromContext(c)
	h.apiKeyService = service.NewAPIKeyService(&dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{key.ID: key}}, nil, nil, nil, nil, nil, nil)
	for _, action := range []func(*gin.Context){panel.ListUserTasks, panel.GetUserTask, panel.RefreshUserTask} {
		c, w = newContext(http.MethodGet, "/api/v1/user/async-tasks")
		c.Params = gin.Params{{Key: "id", Value: page.Items[0].ID}}
		action(c)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "metaso")
		require.NotContains(t, w.Body.String(), "signature")
		require.Contains(t, w.Body.String(), "/api/v1/user/async-tasks/")
	}
	parsed, err := url.Parse(proxyURL)
	require.NoError(t, err)
	resource := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/v1/video-tasks/"), "/content")
	for _, other := range []string{"user", "key", "group", "task", "valid"} {
		c, w = newContext(http.MethodGet, proxyURL)
		c.Params = gin.Params{{Key: "resource", Value: resource}}
		key, _ := middleware.GetAPIKeyFromContext(c)
		switch other {
		case "user":
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11})
		case "key":
			key.ID++
		case "group":
			group := int64(25)
			key.GroupID = &group
		case "task":
			otherURL, _ := url.Parse(service.VideoContentProxyURL("minimax:another-task", 0))
			c.Params[0].Value = strings.TrimSuffix(strings.TrimPrefix(otherURL.Path, "/v1/video-tasks/"), "/content")
		}
		before := upstream.calls
		h.NativeVideoContent(c)
		if other == "valid" || other == "group" {
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, "private-video", w.Body.String())
			require.Empty(t, w.Header().Get("Location"))
		} else {
			require.Equal(t, 404, w.Code, other+": "+w.Body.String())
			require.Equal(t, before, upstream.calls)
		}
	}
	c, w = newContext(http.MethodGet, "/api/v1/user/async-tasks/one/content")
	c.Params = gin.Params{{Key: "id", Value: page.Items[0].ID}}
	panel.DownloadUserTask(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "private-video", w.Body.String())
	require.Len(t, bindings.reservations.settlements, 1, "downloads do not charge successful tasks twice")
	slots.assertReleased(t)
}
