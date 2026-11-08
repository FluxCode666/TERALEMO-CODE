//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCreationUploadStoreFixture(t *testing.T) (service.CreationUploadStore, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store, ok := NewImageTaskStore(rdb).(service.CreationUploadStore)
	require.True(t, ok)
	return store, rdb
}

func creationUploadRecord(uploadID string, userID int64, expiresAt int64) *service.CreationUploadRecord {
	return &service.CreationUploadRecord{
		UploadID: uploadID, UserID: userID, ObjectKey: "creation-uploads/" + uploadID + ".png",
		URL: "https://cdn.example/" + uploadID + ".png", ContentType: "image/png",
		SizeBytes: 128, Width: 64, Height: 64, SHA256: "deadbeef",
		CreatedAt: time.Now().Unix(), ExpiresAt: expiresAt,
	}
}

func TestCreationUploadStoreRoundTripAndTTL(t *testing.T) {
	store, rdb := newCreationUploadStoreFixture(t)
	ctx := context.Background()
	expiresAt := time.Now().Add(time.Hour).Unix()
	require.NoError(t, store.SaveCreationUpload(ctx, creationUploadRecord("cupl_aaa", 42, expiresAt)))

	record, err := store.GetCreationUpload(ctx, "cupl_aaa")
	require.NoError(t, err)
	require.Equal(t, "cupl_aaa", record.UploadID)
	require.Equal(t, int64(42), record.UserID)
	require.Equal(t, expiresAt, record.ExpiresAt)

	ttl, err := rdb.TTL(ctx, "creation_upload:cupl_aaa").Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, time.Hour)

	// Metadata expiry is authoritative: once the TTL passes the record is gone.
	rdb.FlushAll(ctx)
	_, err = store.GetCreationUpload(ctx, "cupl_aaa")
	require.ErrorIs(t, err, service.ErrCreationUploadNotFound)
}

func TestCreationUploadStorePrunesExpiredIndexEntries(t *testing.T) {
	store, rdb := newCreationUploadStoreFixture(t)
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, store.SaveCreationUpload(ctx, creationUploadRecord("cupl_live", 42, now+3600)))
	// An entry whose expiry already passed must not count towards the window.
	require.NoError(t, rdb.ZAdd(ctx, "creation_uploads:user:42", redis.Z{Score: float64(now - 60), Member: "cupl_stale"}).Err())

	count, err := store.CountActiveCreationUploads(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	members, err := rdb.ZRange(ctx, "creation_uploads:user:42", 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"cupl_live"}, members)
}

func TestCreationUploadStoreIsolatesUsers(t *testing.T) {
	store, rdb := newCreationUploadStoreFixture(t)
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, store.SaveCreationUpload(ctx, creationUploadRecord("cupl_mine", 42, now+3600)))
	require.NoError(t, store.SaveCreationUpload(ctx, creationUploadRecord("cupl_other", 7, now+3600)))

	mine, err := store.CountActiveCreationUploads(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, int64(1), mine)
	other, err := store.CountActiveCreationUploads(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, int64(1), other)

	keys, err := rdb.Keys(ctx, "creation_uploads:user:*").Result()
	require.NoError(t, err)
	require.Len(t, keys, 2)

	// Ownership is recorded in the payload so submission can reject foreign IDs.
	record, err := store.GetCreationUpload(ctx, "cupl_other")
	require.NoError(t, err)
	require.Equal(t, int64(7), record.UserID)
}

func TestCreationUploadStoreRejectsExpiredRecord(t *testing.T) {
	store, _ := newCreationUploadStoreFixture(t)
	err := store.SaveCreationUpload(context.Background(), creationUploadRecord("cupl_past", 42, time.Now().Add(-time.Minute).Unix()))
	require.ErrorIs(t, err, service.ErrCreationUploadNotFound)
}
