package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAdminAsyncTasksGlobalPaginationFiltersAndUserIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	catalog := store.(service.AsyncTaskStore)
	svc := service.NewImageTaskService(store)
	ctx := context.Background()
	now := time.Now().Unix()
	group := int64(24)
	for i := 0; i < 205; i++ {
		userID := int64(10 + i%2)
		require.NoError(t, catalog.SaveAsyncTask(ctx, &service.AsyncTaskRecord{AsyncTask: service.AsyncTask{
			ID: fmt.Sprintf("task_%03d", i), TaskID: fmt.Sprintf("remote-%03d", i), Model: "MiniMax-H3", Kind: "video", Protocol: "minimax", Status: "processing", APIKeyID: userID * 2, GroupID: &group, CreatedAt: now + int64(i), ExpiresAt: now + 3600,
		}, UserID: userID, RequestID: "private-route"}))
	}
	filter := service.AdminAsyncTaskFilter{Page: 1, PageSize: 20}
	page, err := svc.ListAdminTasks(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, 205, page.Total)
	require.Len(t, page.Items, 20)
	require.Equal(t, "remote-204", page.Items[0].TaskID)
	filter.Page = 11
	page, err = svc.ListAdminTasks(ctx, filter)
	require.NoError(t, err)
	require.Len(t, page.Items, 5)
	require.Equal(t, "remote-004", page.Items[0].TaskID)
	filter.Page = 12
	page, err = svc.ListAdminTasks(ctx, filter)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Equal(t, 205, page.Total)

	filter = service.AdminAsyncTaskFilter{Page: 1, PageSize: 100, UserID: 11, APIKeyID: 22, GroupID: 24, Kind: "video", Protocol: "minimax", Status: "processing", Search: " MINIMAX-H3 "}
	page, err = svc.ListAdminTasks(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, 102, page.Total)
	for _, task := range page.Items {
		require.Equal(t, int64(11), task.UserID)
	}
	filter.Search = "remote-203"
	page, err = svc.ListAdminTasks(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	filter.Status = "completed"
	page, err = svc.ListAdminTasks(ctx, filter)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	filter.Status, filter.GroupID = "processing", 99
	page, err = svc.ListAdminTasks(ctx, filter)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	public, err := svc.ListUserTasks(ctx, 10, 1, 100, "", "", "")
	require.NoError(t, err)
	require.Equal(t, 103, public.Total)
	data, err := json.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(data), "user_id")
	require.NotContains(t, string(data), "private-route")
}

func TestAdminAsyncTasksBackfillsExistingRecordsAndPrunesExpiredIndex(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	svc := service.NewImageTaskService(store)
	ctx := context.Background()
	now := time.Now().Unix()
	old := service.AsyncTaskRecord{AsyncTask: service.AsyncTask{ID: "old-image", TaskID: "old-image", Kind: "image", Protocol: "openai", Status: "completed", APIKeyID: 3, CreatedAt: now - 60, ExpiresAt: now + 600}, UserID: 7, RequestID: "old-image"}
	data, err := json.Marshal(old)
	require.NoError(t, err)
	key := asyncTaskKey(old.UserID, old.ID)
	// This represents a record created before the global index existed, including
	// one no longer in the bounded per-user index.
	require.NoError(t, rdb.Set(ctx, key, data, 10*time.Minute).Err())
	ttl := rdb.TTL(ctx, key).Val()
	require.NoError(t, rdb.Set(ctx, "async_task:8:wrong-owner", data, time.Hour).Err())
	require.NoError(t, rdb.Set(ctx, "async_task:9:corrupt", "broken", time.Hour).Err())
	page, err := svc.ListAdminTasks(ctx, service.AdminAsyncTaskFilter{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, int64(7), page.Items[0].UserID)
	require.Equal(t, ttl, rdb.TTL(ctx, key).Val())
	require.True(t, mr.Exists(asyncTasksGlobalReady))

	// A later write is indexed immediately, without another scan.
	newTask := old
	newTask.ID, newTask.TaskID, newTask.UserID, newTask.CreatedAt = "new-image", "new-image", 8, now
	require.NoError(t, store.(service.AsyncTaskStore).SaveAsyncTask(ctx, &newTask))
	page, err = svc.ListAdminTasks(ctx, service.AdminAsyncTaskFilter{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	require.Equal(t, "new-image", page.Items[0].ID)
	// Missing/expired records never count; their expired index members are pruned.
	require.NoError(t, rdb.ZAdd(ctx, asyncTasksGlobalIndex, redis.Z{Score: float64(now), Member: "async_task:7:expired"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, asyncTasksGlobalExpiry, redis.Z{Score: float64(now - 1), Member: "async_task:7:expired"}).Err())
	require.NoError(t, store.(service.AsyncTaskStore).SaveAsyncTask(ctx, &newTask))
	require.Equal(t, int64(2), rdb.ZCard(ctx, asyncTasksGlobalIndex).Val(), "writes prune expired global index entries without an admin reader")
	mr.FastForward(11 * time.Minute)
	page, err = svc.ListAdminTasks(ctx, service.AdminAsyncTaskFilter{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Equal(t, int64(2), rdb.ZCard(ctx, asyncTasksGlobalIndex).Val())
}
