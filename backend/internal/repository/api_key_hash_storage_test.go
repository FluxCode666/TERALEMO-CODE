package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
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
	require.Equal(t, "sk-...cdef", key.KeyHint)

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

func TestAPIKeyServiceGeneratedPrefixBoundariesWithRepository(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "generated-boundary@example.invalid")
	for _, prefixLength := range []int{64, 65} {
		cfg := &config.Config{Default: config.DefaultConfig{APIKeyPrefix: strings.Repeat("p", prefixLength)}}
		svc := service.NewAPIKeyService(repo, NewUserRepository(client, nil), nil, nil, nil, nil, cfg)
		created, err := svc.Create(ctx, user.ID, service.CreateAPIKeyRequest{Name: "Generated boundary"})
		if prefixLength == 65 {
			require.ErrorIs(t, err, service.ErrAPIKeyTooLong)
			require.Nil(t, created)
			continue
		}
		require.NoError(t, err)
		require.Len(t, created.Key, service.MaxAPIKeyCredentialBytes)
		loaded, err := svc.GetByKey(ctx, created.Key)
		require.NoError(t, err)
		require.Equal(t, created.ID, loaded.ID)
	}
	count, err := repo.CountByUserID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count, "an invalid prefix must not persist a credential")
}

func TestAPIKeyServiceLegacyShortCredentialStillAuthenticates(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "legacy-short@example.invalid")
	const credential = "0123456789abcdef"
	key := &service.APIKey{UserID: user.ID, Key: credential, Name: "Legacy short", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	require.Equal(t, "...", key.KeyHint)
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})
	loaded, err := svc.GetByKey(ctx, credential)
	require.NoError(t, err)
	require.Equal(t, key.ID, loaded.ID)
	require.Equal(t, "...", loaded.KeyHint)
	require.ErrorIs(t, svc.ValidateCustomKey(credential), service.ErrAPIKeyTooShort)
}

func TestAPIKeyRotationPreservesRecordAndInvalidatesOldCredential(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "rotation-owner@example.invalid")
	other := mustCreateAPIKeyRepoUser(t, ctx, client, "rotation-other@example.invalid")
	group, err := client.Group.Create().SetName("Rotation group").Save(ctx)
	require.NoError(t, err)
	expiry := time.Now().Add(time.Hour).Truncate(time.Second)
	const oldCredential = "sk-rotate-test-legacy-0123456789abcdef"
	key := &service.APIKey{
		UserID: owner.ID, Name: "Rotation", Key: oldCredential, Status: service.StatusActive,
		GroupID: &group.ID, Quota: 100, QuotaUsed: 9, ExpiresAt: &expiry, RateLimit5h: 20,
		IPWhitelist: []string{"192.0.2.10"},
	}
	require.NoError(t, repo.Create(ctx, key))
	require.NoError(t, client.APIKey.UpdateOneID(key.ID).SetUsage5h(3).Exec(ctx))
	before, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, NewAPIKeyCache(redisClient), &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 32, L1TTLSeconds: 60, L2TTLSeconds: 60},
	})
	_, err = svc.GetByKey(ctx, oldCredential)
	require.NoError(t, err)
	cacheKey := apiKeyAuthCachePrefix + key.KeyHash
	require.True(t, redisServer.Exists(cacheKey))
	_, err = svc.Rotate(ctx, key.ID, other.ID)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	require.True(t, redisServer.Exists(cacheKey))

	rotated, err := svc.Rotate(ctx, key.ID, owner.ID)
	require.NoError(t, err)
	require.NotEqual(t, oldCredential, rotated.Key)
	require.Equal(t, service.MaskAPIKey(rotated.Key), rotated.KeyHint)
	require.False(t, redisServer.Exists(cacheKey))
	_, err = svc.GetByKey(ctx, oldCredential)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	active, err := svc.GetByKey(ctx, rotated.Key)
	require.NoError(t, err)
	require.Equal(t, key.ID, active.ID)

	after, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Empty(t, after.Key)
	require.Equal(t, service.HashAPIKey(rotated.Key), after.KeyHash)
	require.Equal(t, rotated.KeyHint, after.KeyHint)
	before.Key, before.KeyHash, before.KeyHint, before.UpdatedAt = after.Key, after.KeyHash, after.KeyHint, after.UpdatedAt
	require.Equal(t, before, after)

	err = repo.RotateCredential(ctx, key.ID, owner.ID, key.KeyHash, "unused-after-stale-write")
	require.ErrorIs(t, err, service.ErrAPIKeyRotationConflict)
	require.NoError(t, client.APIKey.UpdateOneID(key.ID).SetDeletedAt(time.Now()).Exec(ctx))
	_, err = svc.Rotate(ctx, key.ID, owner.ID)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	count, err := client.APIKey.Query().Where(apikey.IDEQ(key.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
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
