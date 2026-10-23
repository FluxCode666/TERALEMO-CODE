//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type adminTaskMetadataRepo struct {
	service.APIKeyRepository
	metadata  map[int64]service.APIKeyTaskMetadata
	err       error
	calls     int
	requested []int64
}

func (r *adminTaskMetadataRepo) GetTaskMetadataByIDs(_ context.Context, ids []int64) (map[int64]service.APIKeyTaskMetadata, error) {
	r.calls++
	r.requested = append([]int64(nil), ids...)
	return r.metadata, r.err
}

func TestAdminAsyncTasksBatchIdentityMetadata(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(strconv.FormatBool(failed), func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			store := repository.NewImageTaskStore(rdb)
			groupID, movedGroupID := int64(7), int64(8)
			repo := &adminTaskMetadataRepo{metadata: map[int64]service.APIKeyTaskMetadata{
				1: {UserID: 10, KeyName: "key-one", UserEmail: "owner@example.com", UserName: "Owner", GroupID: &groupID, GroupName: "Video group"},
				2: {UserID: 11, KeyName: "other-owner-key", UserEmail: "other@example.com", GroupID: &groupID, GroupName: "Other group"},
				3: {UserID: 10, KeyName: "moved-key", GroupID: &movedGroupID, GroupName: "Moved group"},
			}}
			if failed {
				repo.err = errors.New("metadata database unavailable")
			}
			panel := NewAsyncImageHandler(service.NewImageTaskService(store), &OpenAIGatewayHandler{
				apiKeyService: service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil),
			})
			for i, keyID := range []int64{1, 1, 2, 3, 4} {
				record := &service.AsyncTaskRecord{AsyncTask: service.AsyncTask{
					ID: "task-" + strconv.Itoa(i), TaskID: "upstream-" + strconv.Itoa(i),
					APIKeyID: keyID, GroupID: &groupID, Kind: "video", Status: "processing",
					CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(),
				}, UserID: 10}
				require.NoError(t, store.(service.AsyncTaskStore).SaveAsyncTask(context.Background(), record))
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/async-tasks", nil)
			panel.ListAdminTasks(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, repo.calls, "the whole page must use one metadata batch")
			require.ElementsMatch(t, []int64{1, 2, 3, 4}, repo.requested)
			items := make(map[string]gjson.Result)
			for _, item := range gjson.Get(w.Body.String(), "data.items").Array() {
				items[item.Get("id").String()] = item
			}
			require.Len(t, items, 5)
			for _, item := range items {
				require.Equal(t, int64(10), item.Get("user_id").Int())
				require.Equal(t, groupID, item.Get("group_id").Int())
				require.Positive(t, item.Get("api_key_id").Int(), "missing metadata must not remove routing identities")
			}
			if failed {
				for _, item := range items {
					require.False(t, item.Get("key_name").Exists())
					require.False(t, item.Get("user_email").Exists())
				}
				return
			}
			require.Equal(t, "key-one", items["task-0"].Get("key_name").String())
			require.Equal(t, "Video group", items["task-1"].Get("group_name").String())
			require.Equal(t, "owner@example.com", items["task-0"].Get("user_email").String())
			require.False(t, items["task-2"].Get("key_name").Exists(), "a mismatched key owner must not decorate the task")
			require.Equal(t, "moved-key", items["task-3"].Get("key_name").String())
			require.False(t, items["task-3"].Get("group_name").Exists(), "current key group must not replace the recorded task group")
			require.False(t, items["task-4"].Get("key_name").Exists())
			require.NotContains(t, w.Body.String(), "other-owner-key")
			require.NotContains(t, w.Body.String(), "other@example.com")
		})
	}
}

func TestAdminAsyncTasksCrossUserRefreshDownloadAndAuditIdentity(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolMiniMax, "https://metaso.cn/api/minimax/v2", "true")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := repository.NewImageTaskStore(rdb)
	h.asyncTasks = service.NewImageTaskService(store)
	panel := NewAsyncImageHandler(h.asyncTasks, h)
	c, w := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Key, key.Name, key.User.Email = "system-key-secret", "Video key", "owner@example.com"
	key.Group.Platform, key.Group.Name = service.PlatformVideo, "Video group"
	price := 0.4
	key.Group.VideoPrice720P = &price
	key.Group.VideoPrice768P = &price
	h.apiKeyService = service.NewAPIKeyService(&dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{key.ID: key}}, nil, nil, nil, nil, nil, nil)
	upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
		if req.URL.Host == "files.metaso.cn" {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video")), ContentLength: 5}, nil
		}
		body := `{"task_id":"admin-task"}`
		if req.Method == http.MethodGet {
			body = `{"task":{"id":"admin-task","status":"succeeded","content":{"url":"https://files.metaso.cn/result.mp4"}}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/v2/video_generation", strings.NewReader(`{"model":"MiniMax-H3","prompt":"waves","duration":6}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	page, err := h.asyncTasks.ListUserTasks(context.Background(), 10, 1, 20, "", "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	id := page.Items[0].ID
	newAdminContext := func(path string) (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		c.Params = gin.Params{{Key: "user_id", Value: "10"}, {Key: "id", Value: id}}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		return c, w
	}
	other := &service.AsyncTaskRecord{AsyncTask: service.AsyncTask{ID: "other-user-image", TaskID: "other-image", APIKeyID: 21, Kind: "image", Status: "completed", CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix()}, UserID: 11}
	require.NoError(t, store.(service.AsyncTaskStore).SaveAsyncTask(context.Background(), other))
	c, w = newAdminContext("/admin/async-tasks")
	panel.ListAdminTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(2), gjson.Get(w.Body.String(), "data.total").Int())
	require.Contains(t, w.Body.String(), "owner@example.com")
	require.Contains(t, w.Body.String(), "Video key")
	require.NotContains(t, w.Body.String(), "system-key-secret")
	require.NotContains(t, w.Body.String(), "request_id")

	c, w = newAdminContext("/admin/async-tasks/10/" + id + "/refresh")
	panel.RefreshAdminTask(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "completed", gjson.Get(w.Body.String(), "data.status").String())
	require.Equal(t, int64(10), gjson.Get(w.Body.String(), "data.user_id").Int())
	require.Equal(t, "https://files.metaso.cn/result.mp4", gjson.Get(w.Body.String(), "data.result_url").String(), "admin can inspect the original URL")
	actor, _ := middleware.GetAuthSubjectFromContext(c)
	require.Equal(t, int64(99), actor.UserID, "refresh preserves the admin audit actor")
	require.Len(t, bindings.reservations.settlements, 1)
	page, err = h.asyncTasks.ListUserTasks(context.Background(), 10, 1, 20, "", "", "")
	require.NoError(t, err)
	public := h.gatewayService.PublicVideoTask(context.Background(), 10, page.Items[0])
	require.NotContains(t, public.ResultURL, "metaso")

	c, w = newAdminContext("/admin/async-tasks/10/" + id + "/content")
	panel.DownloadAdminTask(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "video", w.Body.String())
	actor, _ = middleware.GetAuthSubjectFromContext(c)
	require.Equal(t, int64(99), actor.UserID)
	require.Len(t, bindings.reservations.settlements, 1)
	before := upstream.calls
	c, w = newAdminContext("/admin/async-tasks/11/" + id)
	c.Params[0].Value = "11"
	panel.GetAdminTask(c)
	require.Equal(t, 404, w.Code)
	require.Equal(t, before, upstream.calls)
	for _, query := range []string{"page=bad", "page=-1", "page_size=101", "user_id=0", "api_key_id=-2", "group_id=bad", "kind=other", "status=other"} {
		c, w = newAdminContext("/admin/async-tasks?" + query)
		panel.ListAdminTasks(c)
		require.Equal(t, 400, w.Code, query)
	}
	slots.assertReleased(t)
}
