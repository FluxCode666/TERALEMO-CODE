//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// catalogIDStore is an in-memory AsyncTaskStore + ImageTaskStore used to assert
// that ObserveVideoTask persists the catalog ID produced by
// AsyncVideoTaskCatalogID rather than a locally duplicated hash.
type catalogIDStore struct {
	tasks map[string]*AsyncTaskRecord
}

func (s *catalogIDStore) Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	return nil
}

func (s *catalogIDStore) Get(ctx context.Context, id string) (*ImageTaskRecord, error) {
	return nil, ErrImageTaskNotFound
}

func (s *catalogIDStore) SaveAsyncTask(ctx context.Context, task *AsyncTaskRecord) error {
	s.tasks[task.ID] = task
	return nil
}

func (s *catalogIDStore) GetAsyncTask(ctx context.Context, userID int64, id string) (*AsyncTaskRecord, error) {
	record, ok := s.tasks[id]
	if !ok || record.UserID != userID {
		return nil, ErrImageTaskNotFound
	}
	return record, nil
}

func (s *catalogIDStore) ListAsyncTasks(ctx context.Context, userID int64) ([]AsyncTaskRecord, error) {
	return nil, nil
}

func TestAsyncVideoTaskCatalogIDMatchesObservedTaskID(t *testing.T) {
	cases := []struct {
		name      string
		requestID string
		wantTask  string
	}{
		{name: "seedance prefix", requestID: "seedance:task-1", wantTask: "task-1"},
		{name: "gemini prefix", requestID: "gemini:models/veo/operations/abc", wantTask: "models/veo/operations/abc"},
		{name: "minimax prefix", requestID: "minimax:12345", wantTask: "12345"},
		{name: "grok without prefix", requestID: "req-abc", wantTask: "req-abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &catalogIDStore{tasks: map[string]*AsyncTaskRecord{}}
			svc := NewImageTaskService(store)
			key := &APIKey{ID: 9, UserID: 7}
			require.NoError(t, svc.ObserveVideoTask(context.Background(), key, tc.requestID, "video-model", "seedance",
				&OpenAIForwardResult{AsyncTaskStatus: "processing"}, false))

			want := AsyncVideoTaskCatalogID(7, 9, tc.requestID)
			require.Len(t, store.tasks, 1)
			record, ok := store.tasks[want]
			require.True(t, ok, "ObserveVideoTask must persist the catalog ID from AsyncVideoTaskCatalogID")
			require.Equal(t, want, record.ID)
			require.Equal(t, tc.wantTask, record.TaskID)
			require.Equal(t, int64(7), record.UserID)
			require.Equal(t, tc.requestID, record.RequestID)
		})
	}
}

func TestAsyncVideoTaskCatalogIDIsStableAndScoped(t *testing.T) {
	id := AsyncVideoTaskCatalogID(7, 9, "seedance:task-1")
	require.Equal(t, id, AsyncVideoTaskCatalogID(7, 9, "seedance:task-1"))
	require.NotEqual(t, id, AsyncVideoTaskCatalogID(8, 9, "seedance:task-1"))
	require.NotEqual(t, id, AsyncVideoTaskCatalogID(7, 10, "seedance:task-1"))
	require.NotEqual(t, id, AsyncVideoTaskCatalogID(7, 9, "seedance:task-2"))
	require.Len(t, id, len("vidtask_")+64)
	require.Equal(t, "vidtask_", id[:len("vidtask_")])
}
