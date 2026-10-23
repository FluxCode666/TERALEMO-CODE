//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestGeminiVideoHandlerOwnershipCatalogAndBillingClaim(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolGemini)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.asyncTasks = service.NewImageTaskService(repository.NewImageTaskStore(rdb))
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, "test-key", req.Header.Get("x-goog-api-key"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"name":"models/veo/operations/task-1","done":false}`))}, nil
	}
	newContext := func(create bool) (*gin.Context, *httptest.ResponseRecorder) {
		c, w := grokMediaSlotContext(context.Background(), create)
		key, _ := middleware.GetAPIKeyFromContext(c)
		key.Group.Platform = service.PlatformVideo
		key.Group.AllowImageGeneration = false // video must not depend on image permission
		price := 0.4
		key.Group.VideoPrice720P = &price
		if create {
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/veo:predictLongRunning", strings.NewReader(`{"instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8}}`))
			c.Params = gin.Params{{Key: "modelAction", Value: "/veo:predictLongRunning"}}
		} else {
			c.Params = gin.Params{{Key: "model", Value: "veo"}, {Key: "operation", Value: "task-1"}}
		}
		return c, w
	}
	c, w := newContext(true)
	h.GeminiVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Len(t, bindings.pending, 1)
	slots.assertReleased(t)
	c, w = newContext(false)
	h.GeminiVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	slots.assertReleased(t)
	page, err := h.asyncTasks.ListUserTasks(context.Background(), 10, 1, 20, "video", "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "veo", page.Items[0].Model)
	require.Equal(t, "gemini", page.Items[0].Protocol)
	for _, other := range []string{"user", "key", "task", "group"} {
		c, w = newContext(false)
		key, _ := middleware.GetAPIKeyFromContext(c)
		switch other {
		case "user":
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11})
		case "key":
			key.ID++
		case "group":
			id := int64(25)
			key.GroupID = &id
		case "task":
			c.Params[1].Value = "another-task"
		}
		calls := upstream.calls
		h.GeminiVideoTasks(c)
		if other == "group" {
			require.Equal(t, 200, w.Code, w.Body.String(), "accepted task remains owned by the original user/key after group reassignment")
			require.Equal(t, calls+1, upstream.calls)
			continue
		}
		require.Equal(t, 404, w.Code, other+": "+w.Body.String())
		require.Equal(t, calls, upstream.calls)
	}
	c, _ = newContext(false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	result := &service.OpenAIForwardResult{ResponseID: "gemini:models/veo/operations/task-1", VideoCount: 2}
	for i := 0; i < 10; i++ {
		bill := prepareGrokVideoCompletionBilling(context.Background(), h, requestLogger(c, "test"), key, subject, result.ResponseID, result)
		if i == 0 {
			require.NotNil(t, bill)
			require.Equal(t, 2, bill.VideoCount)
			require.Equal(t, 8, bill.VideoDurationSeconds)
			require.Equal(t, "720p", bill.VideoResolution)
		} else {
			require.Nil(t, bill)
		}
	}
	panel := &AsyncImageHandler{tasks: h.asyncTasks, openAI: h}
	// The panel can poll using its user's stored key without exposing that key
	// to the browser, and persists the observed completion in the same catalog.
	keyContext, _ := newContext(false)
	storedKey, _ := middleware.GetAPIKeyFromContext(keyContext)
	h.apiKeyService = service.NewAPIKeyService(&dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{storedKey.ID: storedKey}}, nil, nil, nil, nil, nil, nil)
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"name":"models/veo/operations/task-1","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://google.example/v1beta/files/a:download"}}]}}}`))}, nil
	}
	c, w = newContext(false)
	c.Params = gin.Params{{Key: "id", Value: page.Items[0].ID}}
	panel.RefreshUserTask(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "completed", gjson.Get(w.Body.String(), "data.status").String())
	c, w = newContext(false)
	c.Params = gin.Params{{Key: "id", Value: page.Items[0].ID}}
	panel.GetUserTask(c)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "models/veo/operations/task-1", gjson.Get(w.Body.String(), "data.task_id").String())
	require.False(t, gjson.Get(w.Body.String(), "data.user_id").Exists())
	c, w = newContext(false)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11})
	c.Params = gin.Params{{Key: "id", Value: page.Items[0].ID}}
	panel.GetUserTask(c)
	require.Equal(t, 404, w.Code)
}

func TestMiniMaxVideoHandlerOwnershipFilesAndPanelRefresh(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolMiniMax)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.asyncTasks = service.NewImageTaskService(repository.NewImageTaskStore(rdb))
	newContext := func(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
		c, w := grokMediaSlotContext(context.Background(), method == http.MethodPost)
		key, _ := middleware.GetAPIKeyFromContext(c)
		key.Group.Platform = service.PlatformVideo
		key.Group.AllowImageGeneration = false
		price := 0.4
		key.Group.VideoPrice768P = &price
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c, w
	}
	completed := false
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		body := `{"task_id":"task-1","base_resp":{"status_code":0}}`
		if strings.HasSuffix(req.URL.Path, "/query/video_generation") {
			require.Equal(t, "task-1", req.URL.Query().Get("task_id"))
			body = `{"task_id":"task-1","status":"Processing","base_resp":{"status_code":0}}`
			if completed {
				body = `{"task_id":"task-1","status":"Success","file_id":"file-1","base_resp":{"status_code":0}}`
			}
		}
		if strings.HasSuffix(req.URL.Path, "/files/retrieve") {
			require.Equal(t, "file-1", req.URL.Query().Get("file_id"))
			body = `{"file":{"file_id":"file-1","download_url":"https://cdn.example/video.mp4"},"base_resp":{"status_code":0}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	c, w := newContext(http.MethodPost, "/v1/video_generation", `{"model":"MiniMax-Hailuo-2.3","prompt":"waves","duration":10,"resolution":"768P"}`)
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Len(t, bindings.pending, 1)
	slots.assertReleased(t)
	page, err := h.asyncTasks.ListUserTasks(context.Background(), 10, 1, 20, "video", "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "minimax", page.Items[0].Protocol)
	require.Equal(t, "task-1", page.Items[0].TaskID)
	for _, other := range []string{"user", "key", "group", "task", "unbound-file"} {
		c, w = newContext(http.MethodGet, "/v1/query/video_generation?task_id=task-1", "")
		key, _ := middleware.GetAPIKeyFromContext(c)
		switch other {
		case "user":
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11})
		case "key":
			key.ID++
		case "group":
			id := int64(25)
			key.GroupID = &id
		case "task":
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/query/video_generation?task_id=other", nil)
		case "unbound-file":
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/files/retrieve?file_id=file-1", nil)
		}
		calls := upstream.calls
		h.MiniMaxVideoTasks(c)
		if other == "group" {
			require.Equal(t, 200, w.Code, w.Body.String(), "accepted task remains owned by the original user/key after group reassignment")
			require.Equal(t, calls+1, upstream.calls)
			continue
		}
		require.Equal(t, 404, w.Code, other+": "+w.Body.String())
		require.Equal(t, calls, upstream.calls)
	}
	c, w = newContext(http.MethodGet, "/v1/query/video_generation?task_id=task-1", "")
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	slots.assertReleased(t)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	completion := &service.OpenAIForwardResult{ResponseID: "minimax:task-1", VideoCount: 1}
	for i := 0; i < 5; i++ {
		bill := prepareGrokVideoCompletionBilling(context.Background(), h, requestLogger(c, "test"), key, subject, completion.ResponseID, completion)
		if i == 0 {
			require.NotNil(t, bill)
			require.Equal(t, 10, bill.VideoDurationSeconds)
			require.Equal(t, "768p", bill.VideoResolution)
		} else {
			require.Nil(t, bill)
		}
	}
	h.apiKeyService = service.NewAPIKeyService(&dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{key.ID: key}}, nil, nil, nil, nil, nil, nil)
	panel := &AsyncImageHandler{tasks: h.asyncTasks, openAI: h}
	completed = true
	c, w = newContext(http.MethodGet, "/api/v1/user/async-tasks/one", "")
	c.Params = gin.Params{{Key: "id", Value: page.Items[0].ID}}
	panel.RefreshUserTask(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "completed", gjson.Get(w.Body.String(), "data.status").String())
	require.Equal(t, "https://cdn.example/video.mp4", gjson.Get(w.Body.String(), "data.result_url").String())
	require.NotContains(t, w.Body.String(), "test-key")
	for _, other := range []bool{false, true} {
		c, w = newContext(http.MethodGet, "/v1/files/retrieve?file_id=file-1", "")
		if other {
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.ID++
		}
		calls := upstream.calls
		h.MiniMaxVideoTasks(c)
		if other {
			require.Equal(t, 404, w.Code)
			require.Equal(t, calls, upstream.calls)
		} else {
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, "file-1", gjson.Get(w.Body.String(), "file.file_id").String())
		}
	}
	page, err = h.asyncTasks.ListUserTasks(context.Background(), 10, 1, 20, "video", "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1, "file retrieval must not create another catalog task")
}

func TestMiniMaxVideoRejectsUnpricedAndUnacceptedTasks(t *testing.T) {
	for _, scenario := range []string{"missing price", "upstream business error"} {
		t.Run(scenario, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolMiniMax)
			upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"task_id":"not-accepted","base_resp":{"status_code":1004,"status_msg":"authentication failed"}}`))}, nil
			}
			c, w := grokMediaSlotContext(context.Background(), true)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = service.PlatformVideo
			key.Group.VideoPrice480P, key.Group.VideoPrice720P, key.Group.VideoPrice768P, key.Group.VideoPrice1080P, key.Group.VideoPrice2K = nil, nil, nil, nil, nil
			if scenario != "missing price" {
				price := 0.4
				key.Group.VideoPrice768P = &price
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/video_generation", strings.NewReader(`{"model":"MiniMax-Hailuo-2.3","prompt":"waves"}`))
			h.MiniMaxVideoTasks(c)
			require.Empty(t, bindings.pending)
			if scenario == "missing price" {
				require.Equal(t, 400, w.Code, w.Body.String())
				require.Zero(t, upstream.calls)
			} else {
				require.Equal(t, 502, w.Code, w.Body.String())
				require.Equal(t, 1, upstream.calls, "rejected creates must never be retried")
			}
			slots.assertReleased(t)
		})
	}
}

func TestMiniMaxV2HandlerCatalogOwnershipPanelAndBilling(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolMiniMax, "https://metaso.cn/api/minimax/v2")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.asyncTasks = service.NewImageTaskService(repository.NewImageTaskStore(rdb))
	newContext := func(create bool) (*gin.Context, *httptest.ResponseRecorder) {
		c, w := grokMediaSlotContext(context.Background(), create)
		key, _ := middleware.GetAPIKeyFromContext(c)
		key.Group.Platform = service.PlatformVideo
		price := 0.4
		key.Group.VideoPrice768P = &price
		if create {
			c.Request = httptest.NewRequest(http.MethodPost, "/v2/video_generation", strings.NewReader(`{"model":"MiniMax-H3","content":[{"type":"text","text":"waves"}],"duration":7,"ratio":"16:9","resolution":"768P"}`))
			c.Request.Header.Set("Content-Type", "application/json")
		} else {
			c.Request = httptest.NewRequest(http.MethodGet, "/v2/query/video_generation/task-h3", nil)
			c.Params = gin.Params{{Key: "task_id", Value: "task-h3"}}
		}
		return c, w
	}
	completed := false
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		body := `{"task_id":"task-h3"}`
		if req.Method == http.MethodGet {
			require.Equal(t, "/api/minimax/v2/query/video_generation/task-h3", req.URL.Path)
			require.Empty(t, req.URL.RawQuery)
			body = `{"task":{"id":"task-h3","status":"running"}}`
			if completed {
				body = `{"task":{"id":"task-h3","status":"succeeded","content":{"url":"https://files.metaso.cn/video.mp4"},"duration":7,"resolution":"768P"}}`
			}
		} else {
			require.Equal(t, "/api/minimax/v2/video_generation", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	c, w := newContext(true)
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Len(t, bindings.pending, 1)
	slots.assertReleased(t)
	page, err := h.asyncTasks.ListUserTasks(context.Background(), 10, 1, 20, "video", "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "MiniMax-H3", page.Items[0].Model)
	require.Equal(t, "minimax", page.Items[0].Protocol)
	c, w = newContext(false)
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "running", gjson.Get(w.Body.String(), "task.status").String())
	for _, other := range []string{"user", "key", "group", "task"} {
		c, w = newContext(false)
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
			c.Params[0].Value = "another-task"
		}
		calls := upstream.calls
		h.MiniMaxVideoTasks(c)
		if other == "group" {
			require.Equal(t, 200, w.Code, w.Body.String(), "accepted task remains owned by the original user/key after group reassignment")
			require.Equal(t, calls+1, upstream.calls)
			continue
		}
		require.Equal(t, 404, w.Code, other+": "+w.Body.String())
		require.Equal(t, calls, upstream.calls)
	}
	c, _ = newContext(false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	h.apiKeyService = service.NewAPIKeyService(&dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{key.ID: key}}, nil, nil, nil, nil, nil, nil)
	panel := &AsyncImageHandler{tasks: h.asyncTasks, openAI: h}
	completed = true
	c, w = newContext(false)
	c.Params = gin.Params{{Key: "id", Value: page.Items[0].ID}}
	panel.RefreshUserTask(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "completed", gjson.Get(w.Body.String(), "data.status").String())
	require.Equal(t, "https://files.metaso.cn/video.mp4", gjson.Get(w.Body.String(), "data.result_url").String())
	require.Len(t, bindings.reservations.settlements, 1, "successful panel refresh durably settles exactly once")
	before := upstream.calls
	panel.RefreshUserTask(c)
	require.Equal(t, before, upstream.calls, "completed panel tasks do not submit or poll again")
	require.Len(t, bindings.reservations.settlements, 1)
	slots.assertReleased(t)
}
