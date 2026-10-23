package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAsyncTaskCatalogOwnershipFilteringAndTerminalState(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	catalog := store.(service.AsyncTaskStore)
	svc := service.NewImageTaskService(store)
	ctx := context.Background()
	image, err := svc.CreateWithMetadata(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, "image-model", "openai", nil)
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, image.ID, 200, json.RawMessage(`{"data":[{"url":"https://cdn.example/image.png"}]}`)))
	key := &service.APIKey{ID: 10, UserID: 7}
	require.NoError(t, svc.ObserveVideoTask(ctx, key, "seedance:task", "video-model", "seedance", &service.OpenAIForwardResult{AsyncTaskStatus: "processing"}, false))
	page, err := svc.ListUserTasks(ctx, 7, 1, 20, "", "", "")
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	page, err = svc.ListUserTasks(ctx, 7, 1, 20, "video", "processing", "VIDEO-MODEL")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	id := page.Items[0].ID
	_, err = svc.GetUserTask(ctx, 8, id)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	other, err := svc.ListUserTasks(ctx, 8, 1, 20, "", "", "")
	require.NoError(t, err)
	require.Empty(t, other.Items)
	stale, err := catalog.GetAsyncTask(ctx, 7, id)
	require.NoError(t, err)
	require.NoError(t, svc.ObserveVideoTask(ctx, key, "seedance:task", "", "seedance", &service.OpenAIForwardResult{AsyncTaskStatus: "completed", AsyncTaskURL: "https://cdn.example/video.mp4"}, false))
	// A slower, older processing poll must never revert a completed observation.
	require.NoError(t, catalog.SaveAsyncTask(ctx, stale))
	completed, err := svc.GetUserTask(ctx, 7, id)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Status)
	require.Equal(t, "video-model", completed.Model)
	require.Equal(t, "https://cdn.example/video.mp4", completed.ResultURL)
	public, err := json.Marshal(completed.AsyncTask)
	require.NoError(t, err)
	require.NotContains(t, string(public), "user_id")
	require.NotContains(t, string(public), "request_id")
	page, err = svc.ListUserTasks(ctx, 7, 2, 1, "", "", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	mr.FastForward(25 * time.Hour)
	page, err = svc.ListUserTasks(ctx, 7, 1, 20, "", "", "")
	require.NoError(t, err)
	require.Empty(t, page.Items)
}
