//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type taskMetadataLegacyRepo struct {
	APIKeyRepository
	keys  map[int64]*APIKey
	calls []int64
}

func (r *taskMetadataLegacyRepo) GetByID(_ context.Context, id int64) (*APIKey, error) {
	r.calls = append(r.calls, id)
	if key, ok := r.keys[id]; ok {
		return key, nil
	}
	return nil, ErrAPIKeyNotFound
}

type taskMetadataBatchRepo struct {
	taskMetadataLegacyRepo
	calls     int
	requested []int64
	err       error
}

func (r *taskMetadataBatchRepo) GetTaskMetadataByIDs(_ context.Context, ids []int64) (map[int64]APIKeyTaskMetadata, error) {
	r.calls++
	r.requested = append([]int64(nil), ids...)
	return map[int64]APIKeyTaskMetadata{7: {UserID: 10, KeyName: "display-name"}}, r.err
}

func TestAPIKeyTaskMetadataUsesOneDeduplicatedBatch(t *testing.T) {
	for _, failed := range []bool{false, true} {
		repo := &taskMetadataBatchRepo{}
		if failed {
			repo.err = errors.New("batch unavailable")
		}
		svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
		metadata, err := svc.GetTaskMetadataByIDs(context.Background(), []int64{7, 7, 0, -1, 9})
		require.ErrorIs(t, err, repo.err)
		require.Equal(t, 1, repo.calls)
		require.Equal(t, []int64{7, 9}, repo.requested)
		require.Empty(t, repo.taskMetadataLegacyRepo.calls, "failed batches must never fan out into full-key lookups")
		if !failed {
			require.Equal(t, "display-name", metadata[7].KeyName)
		}
	}
}

func TestAPIKeyTaskMetadataLegacyFallbackIsDeduplicatedAndSecretFree(t *testing.T) {
	repo := &taskMetadataLegacyRepo{keys: map[int64]*APIKey{
		7: {ID: 7, UserID: 10, Key: "private-credential", Name: "display-name", User: &User{ID: 10, Email: "owner@example.com", PasswordHash: "private-hash"}, Group: &Group{ID: 3, Name: "Video group"}},
	}}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
	metadata, err := svc.GetTaskMetadataByIDs(context.Background(), []int64{7, 7, 9})
	require.NoError(t, err)
	require.Equal(t, []int64{7, 9}, repo.calls)
	require.Len(t, metadata, 1)
	require.Equal(t, "display-name", metadata[7].KeyName)
	require.Equal(t, "owner@example.com", metadata[7].UserEmail)
	require.Equal(t, int64(3), *metadata[7].GroupID)
	require.NotContains(t, metadata, int64(9))
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-credential")
	require.NotContains(t, string(encoded), "private-hash")

	metadata, err = svc.GetTaskMetadataByIDs(context.Background(), []int64{0, -1})
	require.NoError(t, err)
	require.Empty(t, metadata)
	require.Equal(t, []int64{7, 9}, repo.calls, "empty valid-ID sets must not query the database")
}
