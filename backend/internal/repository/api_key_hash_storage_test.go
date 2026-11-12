package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestAPIKeyRepositoryHashStorageAndAuthentication(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "hash-storage@example.com")
	const credential = "sk-storage-private-0123456789abcdef"
	key := &service.APIKey{UserID: user.ID, Key: credential, Name: "Stored hash", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	require.Equal(t, credential, key.Key, "only the creation result retains the credential")
	require.Equal(t, service.HashAPIKey(credential), key.KeyHash)
	require.Equal(t, "sk-stor...cdef", key.KeyHint)

	var raw sql.NullString
	var storedHash, storedHint string
	require.NoError(t, scanSingleRow(ctx, repo.sql, "SELECT key, key_hash, key_hint FROM api_keys WHERE id = ?", []any{key.ID}, &raw, &storedHash, &storedHint))
	require.False(t, raw.Valid, "the database must not retain the plaintext")
	require.Equal(t, key.KeyHash, storedHash)
	require.Equal(t, key.KeyHint, storedHint)

	for name, load := range map[string]func() (*service.APIKey, error){
		"id":   func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
		"key":  func() (*service.APIKey, error) { return repo.GetByKey(ctx, credential) },
		"auth": func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, credential) },
	} {
		t.Run(name, func(t *testing.T) {
			loaded, err := load()
			require.NoError(t, err)
			require.Empty(t, loaded.Key)
			require.Equal(t, key.KeyHash, loaded.KeyHash)
			require.Equal(t, key.KeyHint, loaded.KeyHint)
		})
	}
	exists, err := repo.ExistsByKey(ctx, credential)
	require.NoError(t, err)
	require.True(t, exists)
	_, err = repo.GetByKeyForAuth(ctx, key.KeyHash)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound, "a stored hash is never a bearer credential")
	_, err = repo.GetByKeyForAuth(ctx, key.KeyHint)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	duplicate := &service.APIKey{UserID: user.ID, Key: credential, Name: "Duplicate", Status: service.StatusActive}
	require.ErrorIs(t, repo.Create(ctx, duplicate), service.ErrAPIKeyExists)

	listed, _, err := repo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{Search: key.KeyHint})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].Key)
	hashes, err := repo.ListKeyHashesByUserID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{key.KeyHash}, hashes)
	ownerHash, ownerID, err := repo.GetKeyHashAndOwnerID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, key.KeyHash, ownerHash)
	require.Equal(t, user.ID, ownerID)

	require.NoError(t, repo.Delete(ctx, key.ID))
	require.NoError(t, repo.Create(ctx, duplicate), "deleting a key releases its hash for reuse")
	_, err = repo.GetByKey(ctx, credential)
	require.NoError(t, err)
}

func TestAPIKeyRepositoryPostgresHashAuthenticationSmoke(t *testing.T) {
	dsn := os.Getenv("ASO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ASO_TEST_POSTGRES_DSN is required for the isolated authentication smoke test")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	ctx = dbent.NewTxContext(ctx, tx)
	user := mustCreateAPIKeyRepoUser(t, ctx, tx.Client(), "postgres-hash-smoke@example.com")
	repo := newAPIKeyRepositoryWithSQL(tx.Client(), tx)
	const credential = "sk-postgres-private-0123456789abcdef"
	key := &service.APIKey{UserID: user.ID, Key: credential, Name: "Postgres hash smoke", Status: service.StatusActive, Quota: 1}
	require.NoError(t, repo.Create(ctx, key))
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, NewAPIKeyCache(rdb), &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60},
	})
	for i := 0; i < 2; i++ {
		loaded, lookupErr := svc.GetByKey(ctx, credential)
		require.NoError(t, lookupErr)
		require.Equal(t, key.ID, loaded.ID)
		require.Empty(t, loaded.Key)
		require.Equal(t, key.KeyHash, loaded.KeyHash)
	}
	cacheKey := apiKeyAuthCachePrefix + key.KeyHash
	require.True(t, mr.Exists(cacheKey))
	require.NoError(t, svc.UpdateQuotaUsed(ctx, key.ID, 1))
	require.False(t, mr.Exists(cacheKey), "quota exhaustion evicts by the stored hash")
	loaded, err := svc.GetByKey(ctx, credential)
	require.NoError(t, err)
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, loaded.Status)

	var raw sql.NullString
	var storedHash string
	require.NoError(t, scanSingleRow(ctx, tx, "SELECT key, key_hash FROM api_keys WHERE id = $1", []any{key.ID}, &raw, &storedHash))
	require.False(t, raw.Valid)
	require.Equal(t, key.KeyHash, storedHash)
	require.NoError(t, svc.Delete(ctx, key.ID, user.ID))
	require.False(t, mr.Exists(cacheKey))
	_, err = svc.GetByKey(ctx, credential)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	var invalidations int
	require.NoError(t, scanSingleRow(ctx, tx, "SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1", []any{key.KeyHash}, &invalidations))
	require.Positive(t, invalidations)
}

func TestAPIKeyAuthCacheUsesStoredHashForEveryInvalidation(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "hash-cache@example.com")
	group, err := client.Group.Create().SetName("Hash cache group").Save(ctx)
	require.NoError(t, err)
	const credential = "sk-cache-private-0123456789abcdef"
	key := &service.APIKey{UserID: user.ID, Key: credential, Name: "Cached hash", GroupID: &group.ID, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, NewAPIKeyCache(rdb), &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60},
	})
	cacheKey := apiKeyAuthCachePrefix + key.KeyHash
	for name, invalidate := range map[string]func(){
		"key":   func() { svc.InvalidateAuthCacheByHash(ctx, key.KeyHash) },
		"user":  func() { svc.InvalidateAuthCacheByUserID(ctx, user.ID) },
		"group": func() { svc.InvalidateAuthCacheByGroupID(ctx, group.ID) },
	} {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 2; i++ {
				loaded, loadErr := svc.GetByKey(ctx, credential)
				require.NoError(t, loadErr)
				require.Empty(t, loaded.Key, "both database and cache hits exclude the credential")
				require.Equal(t, key.KeyHash, loaded.KeyHash)
			}
			require.Equal(t, []string{cacheKey}, mr.Keys())
			payload, getErr := mr.Get(cacheKey)
			require.NoError(t, getErr)
			require.NotContains(t, payload, credential)
			invalidate()
			require.False(t, mr.Exists(cacheKey), "the stored hash must not be hashed again")
		})
	}
	_, err = svc.GetByKey(ctx, credential)
	require.NoError(t, err)
	key.Status = service.StatusDisabled
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Status: true}))
	svc.InvalidateAuthCacheByHash(ctx, key.KeyHash)
	loaded, err := svc.GetByKey(ctx, credential)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, loaded.Status)
	for _, name := range mr.Keys() {
		require.False(t, strings.Contains(name, credential))
	}
}
