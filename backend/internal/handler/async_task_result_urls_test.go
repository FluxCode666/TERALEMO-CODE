//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newResultURLsFixture(t *testing.T) (*AsyncImageHandler, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := repository.NewImageTaskStore(rdb)
	svc := service.NewImageTaskService(store)
	task, err := svc.CreateWithMetadata(t.Context(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, "gpt-image-2", "openai", nil)
	require.NoError(t, err)
	result := `{"data":[{"url":"https://cdn.example/one.png"},{"url":"https://cdn.example/two.png"}]}`
	require.NoError(t, svc.Complete(t.Context(), task.ID, 200, json.RawMessage(result)))
	return NewAsyncImageHandler(svc, nil), task.ID
}

func newUserContext(t *testing.T, target string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	c.Params = params
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	return c, recorder
}

func requireResultURLs(t *testing.T, body []byte, path string) {
	t.Helper()
	urls := gjson.GetBytes(body, path)
	require.True(t, urls.IsArray(), "expected %s to be an array in %s", path, string(body))
	require.Len(t, urls.Array(), 2)
	require.Equal(t, "https://cdn.example/one.png", urls.Array()[0].String())
	require.Equal(t, "https://cdn.example/two.png", urls.Array()[1].String())
}

func TestUserAsyncTaskEndpointsExposeResultURLs(t *testing.T) {
	h, taskID := newResultURLsFixture(t)

	listCtx, listRecorder := newUserContext(t, "/api/v1/user/async-tasks?page=1&page_size=20", nil)
	h.ListUserTasks(listCtx)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	requireResultURLs(t, listRecorder.Body.Bytes(), "data.items.0.result_urls")

	detailCtx, detailRecorder := newUserContext(t, "/api/v1/user/async-tasks/"+taskID, gin.Params{{Key: "id", Value: taskID}})
	h.GetUserTask(detailCtx)
	require.Equal(t, http.StatusOK, detailRecorder.Code)
	requireResultURLs(t, detailRecorder.Body.Bytes(), "data.result_urls")
}

func TestAdminAsyncTaskEndpointsExposeResultURLs(t *testing.T) {
	h, taskID := newResultURLsFixture(t)

	listCtx, listRecorder := newUserContext(t, "/api/v1/admin/async-tasks?page=1&page_size=20", nil)
	h.ListAdminTasks(listCtx)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	requireResultURLs(t, listRecorder.Body.Bytes(), "data.items.0.result_urls")

	detailCtx, detailRecorder := newUserContext(t, "/api/v1/admin/async-tasks/7/"+taskID, gin.Params{
		{Key: "user_id", Value: "7"},
		{Key: "id", Value: taskID},
	})
	h.GetAdminTask(detailCtx)
	require.Equal(t, http.StatusOK, detailRecorder.Code)
	requireResultURLs(t, detailRecorder.Body.Bytes(), "data.result_urls")
}
