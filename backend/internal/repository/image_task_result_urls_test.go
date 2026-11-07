//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newImageTaskStoreFixture(t *testing.T) (service.ImageTaskStore, service.AsyncTaskStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	catalog, ok := store.(service.AsyncTaskStore)
	require.True(t, ok)
	return store, catalog
}

func TestImageTaskStoreFillsAllResultURLs(t *testing.T) {
	store, catalog := newImageTaskStoreFixture(t)
	svc := service.NewImageTaskService(store)
	ctx := context.Background()
	task, err := svc.CreateWithMetadata(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, "gpt-image-2", "openai", nil)
	require.NoError(t, err)
	result := `{"data":[{"url":"https://cdn.example/a.png"},{"url":"https://cdn.example/b.png"},{"url":"https://cdn.example/c.png"}]}`
	require.NoError(t, svc.Complete(ctx, task.ID, 200, json.RawMessage(result)))

	record, err := catalog.GetAsyncTask(ctx, 7, task.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"https://cdn.example/a.png", "https://cdn.example/b.png", "https://cdn.example/c.png"}, record.ResultURLs)
	require.Equal(t, "https://cdn.example/a.png", record.ResultURL)

	encoded, err := json.Marshal(record.AsyncTask)
	require.NoError(t, err)
	urls := gjson.GetBytes(encoded, "result_urls")
	require.True(t, urls.IsArray())
	require.Len(t, urls.Array(), 3)
	require.Equal(t, "https://cdn.example/c.png", urls.Array()[2].String())
}

func TestImageTaskStoreOmitsResultURLsWithoutResults(t *testing.T) {
	store, catalog := newImageTaskStoreFixture(t)
	svc := service.NewImageTaskService(store)
	ctx := context.Background()
	task, err := svc.CreateWithMetadata(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, "gpt-image-2", "openai", nil)
	require.NoError(t, err)

	record, err := catalog.GetAsyncTask(ctx, 7, task.ID)
	require.NoError(t, err)
	require.Empty(t, record.ResultURLs)

	encoded, err := json.Marshal(record.AsyncTask)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(encoded, "result_urls").Exists(), "empty result_urls must be omitted")
}

func TestImageTaskStoreBoundsResultURLsAndSkipsBlanks(t *testing.T) {
	store, catalog := newImageTaskStoreFixture(t)
	svc := service.NewImageTaskService(store)
	ctx := context.Background()
	task, err := svc.CreateWithMetadata(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, "gpt-image-2", "openai", nil)
	require.NoError(t, err)

	items := make([]map[string]string, 0, 12)
	items = append(items, map[string]string{"url": "  "})
	for i := 0; i < 11; i++ {
		items = append(items, map[string]string{"url": "https://cdn.example/" + string(rune('a'+i)) + ".png"})
	}
	encoded, err := json.Marshal(map[string]any{"data": items})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, task.ID, 200, encoded))

	record, err := catalog.GetAsyncTask(ctx, 7, task.ID)
	require.NoError(t, err)
	require.Len(t, record.ResultURLs, 10, "result_urls is bounded to 10 entries")
	require.Equal(t, "https://cdn.example/a.png", record.ResultURLs[0], "blank urls are skipped")
	require.Equal(t, "https://cdn.example/j.png", record.ResultURLs[9])
	// ResultURL keeps its pre-existing behaviour: the raw first data[].url entry.
	require.Equal(t, "  ", record.ResultURL)
}
