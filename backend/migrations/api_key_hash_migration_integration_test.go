//go:build integration

package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// An explicit DSN keeps this destructive migration test on a dedicated database.
func TestAPIKeyHashMigrationPostgres(t *testing.T) {
	dsn := os.Getenv("ASO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ASO_TEST_POSTGRES_DSN is required for the isolated migration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	schema := pq.QuoteIdentifier(fmt.Sprintf("api_key_hash_test_%d", time.Now().UnixNano()))
	_, err = conn.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := conn.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, cleanupErr)
	})
	_, err = conn.ExecContext(ctx, "SET search_path TO "+schema)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, apiKeyHashMigrationFixture)
	require.NoError(t, err)
	for _, name := range []string{"184_auth_cache_invalidation_outbox.sql", "193_group_profit_control_auth_cache_invalidation.sql"} {
		content, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = conn.ExecContext(ctx, string(content))
		require.NoError(t, err)
	}
	keys := []struct {
		id   int
		raw  string
		hint string
	}{
		{1, "sk-apikey-migration-00123", "sk-apik...0123"},
		{2, "s", "..."},
		{3, "sk-迁移测试字符-安全末尾1234", "sk-迁移测试...1234"},
		{4, "__deleted__4__1234567890", "__delet...7890"},
	}
	for _, key := range keys {
		_, err = conn.ExecContext(ctx, `INSERT INTO api_keys (id, user_id, group_id, key, name, quota, deleted_at)
			VALUES ($1, 1, CASE WHEN $1::bigint = 1 THEN 1 ELSE 2 END, $2, 'fixture', 5,
				CASE WHEN $1::bigint = 4 THEN NOW() ELSE NULL END)`, key.id, key.raw)
		require.NoError(t, err)
	}
	_, err = conn.ExecContext(ctx, "INSERT INTO deleted_api_key_audits (key, key_name) VALUES ($1, 'retained audit metadata')", keys[0].raw)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO idempotency_records (scope,response_body) VALUES
		('user.api_keys.create', jsonb_build_object('id',1,'key',$1::text,'name','retained response')::text),
		('user.api_keys.create', $1),
		('unrelated.scope', '{"value":"unchanged"}'),
		('user.api_keys.create', '"sk-json-scalar-credential"'),
		('user.api_keys.create', '{"id":-1,"key":"sk-deleted-credential"}')`, keys[0].raw)
	require.NoError(t, err)
	content, err := FS.ReadFile("264_api_key_hash_storage.sql")
	require.NoError(t, err)
	apply := func() {
		tx, txErr := conn.BeginTx(ctx, nil)
		require.NoError(t, txErr)
		defer func() { _ = tx.Rollback() }()
		_, txErr = tx.ExecContext(ctx, string(content))
		require.NoError(t, txErr)
		require.NoError(t, tx.Commit())
	}
	assertMigrated := func() {
		for _, key := range keys {
			var raw sql.NullString
			var hash, hint string
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT key, key_hash, key_hint FROM api_keys WHERE id = $1", key.id).Scan(&raw, &hash, &hint))
			require.False(t, raw.Valid)
			sum := sha256.Sum256([]byte(key.raw))
			require.Equal(t, hex.EncodeToString(sum[:]), hash)
			require.Equal(t, key.hint, hint)
		}
		var auditKey, auditName string
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT key, key_name FROM deleted_api_key_audits").Scan(&auditKey, &auditName))
		require.Empty(t, auditKey)
		require.Equal(t, "retained audit metadata", auditName)
		var body string
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT response_body FROM idempotency_records WHERE id = 1").Scan(&body))
		require.NotContains(t, body, keys[0].raw)
		var stored map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &stored))
		require.Equal(t, keys[0].hint, stored["key"])
		require.Equal(t, keys[0].hint, stored["key_hint"])
		require.Equal(t, "retained response", stored["name"])
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT response_body FROM idempotency_records WHERE id = 2").Scan(&body))
		require.JSONEq(t, `{"key":"...","key_hint":"..."}`, body)
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT response_body FROM idempotency_records WHERE id = 3").Scan(&body))
		require.JSONEq(t, `{"value":"unchanged"}`, body)
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT response_body FROM idempotency_records WHERE id = 4").Scan(&body))
		require.JSONEq(t, `{"key":"...","key_hint":"..."}`, body)
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT response_body FROM idempotency_records WHERE id = 5").Scan(&body))
		require.JSONEq(t, `{"id":-1,"key":"...","key_hint":"..."}`, body)
	}
	apply()
	assertMigrated()
	_, err = conn.ExecContext(ctx, "TRUNCATE auth_cache_invalidation_outbox")
	require.NoError(t, err)
	apply()
	assertMigrated()
	var outboxCount int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_cache_invalidation_outbox").Scan(&outboxCount))
	require.Zero(t, outboxCount, "replaying the migration must not enqueue more invalidations")

	sum := sha256.Sum256([]byte(keys[0].raw))
	hash := hex.EncodeToString(sum[:])
	var id int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT id FROM api_keys WHERE key_hash = $1 AND deleted_at IS NULL", hash).Scan(&id))
	require.Equal(t, 1, id, "existing credentials must still resolve by their SHA-256")
	_, err = conn.ExecContext(ctx, "UPDATE api_keys SET key = $1 WHERE id = 1", keys[0].raw)
	require.Error(t, err, "the database must reject storing plaintext after migration")
	_, err = conn.ExecContext(ctx, "INSERT INTO api_keys (id,user_id,key_hash,key_hint,name) VALUES (5,1,$1,'hint','duplicate')", hash)
	require.Error(t, err, "the credential hash must be unique")

	for _, mutation := range []struct {
		name  string
		sql   string
		count int
	}{
		{"usage below quota", "UPDATE api_keys SET quota_used = 1, usage_5h = 1, last_used_at = NOW() WHERE id = 1", 0},
		{"quota exhausted", "UPDATE api_keys SET quota_used = 5 WHERE id = 1", 1},
		{"quota reset", "UPDATE api_keys SET quota_used = 0 WHERE id = 1", 1},
		{"quota changed", "UPDATE api_keys SET quota = 10 WHERE id = 1", 1},
		{"rate limit", "UPDATE api_keys SET rate_limit_5h = 2 WHERE id = 1", 1},
		{"disable", "UPDATE api_keys SET status = 'disabled' WHERE id = 1", 1},
		{"enable", "UPDATE api_keys SET status = 'active' WHERE id = 1", 1},
		{"group transfer", "UPDATE api_keys SET group_id = 2 WHERE id = 1", 1},
		{"group restore", "UPDATE api_keys SET group_id = 1 WHERE id = 1", 1},
		{"user disabled", "UPDATE users SET status = 'disabled' WHERE id = 1", 3},
		{"profit control", "UPDATE groups SET profit_control_enabled = TRUE WHERE id = 1", 1},
		{"exclusive group revoked", "DELETE FROM user_allowed_groups WHERE user_id = 1 AND group_id = 1", 1},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			_, queryErr := conn.ExecContext(ctx, "TRUNCATE auth_cache_invalidation_outbox")
			require.NoError(t, queryErr)
			_, queryErr = conn.ExecContext(ctx, mutation.sql)
			require.NoError(t, queryErr)
			require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_cache_invalidation_outbox").Scan(&outboxCount))
			require.Equal(t, mutation.count, outboxCount)
			if mutation.count > 0 {
				var keyCount int
				require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1", hash).Scan(&keyCount))
				require.Equal(t, 1, keyCount, "the stored hash must be enqueued without being hashed a second time")
			}
		})
	}
	_, err = conn.ExecContext(ctx, "TRUNCATE auth_cache_invalidation_outbox")
	require.NoError(t, err)
	tombstone := sha256.Sum256([]byte("deleted-migration-fixture-1"))
	_, err = conn.ExecContext(ctx, "UPDATE api_keys SET key_hash = $1, deleted_at = NOW() WHERE id = 1", hex.EncodeToString(tombstone[:]))
	require.NoError(t, err)
	var invalidated string
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT cache_key FROM auth_cache_invalidation_outbox").Scan(&invalidated))
	require.Equal(t, hash, invalidated, "deletion must invalidate the original credential")
	_, err = conn.ExecContext(ctx, "INSERT INTO api_keys (id,user_id,key_hash,key_hint,name) VALUES (5,1,$1,$2,'recreated')", hash, keys[0].hint)
	require.NoError(t, err, "a tombstoned hash must allow explicit credential reuse")
}

const apiKeyHashMigrationFixture = `
CREATE TABLE users (id BIGINT PRIMARY KEY, status TEXT DEFAULT 'active', role TEXT DEFAULT 'user', deleted_at TIMESTAMPTZ);
CREATE TABLE groups (
    id BIGINT PRIMARY KEY, status TEXT DEFAULT 'active', is_exclusive BOOLEAN DEFAULT TRUE,
    allow_image_generation BOOLEAN DEFAULT TRUE, platform TEXT DEFAULT 'openai', subscription_type TEXT DEFAULT 'standard',
    rate_multiplier NUMERIC DEFAULT 1, peak_rate_enabled BOOLEAN DEFAULT FALSE, peak_start TEXT DEFAULT '',
    peak_end TEXT DEFAULT '', peak_rate_multiplier NUMERIC DEFAULT 1, profit_control_enabled BOOLEAN DEFAULT FALSE,
    profit_min_margin NUMERIC DEFAULT 0, profit_safety_buffer NUMERIC DEFAULT 0, deleted_at TIMESTAMPTZ
);
CREATE TABLE user_allowed_groups (user_id BIGINT, group_id BIGINT);
CREATE TABLE api_keys (
    id BIGINT PRIMARY KEY, user_id BIGINT NOT NULL, key VARCHAR(128) NOT NULL UNIQUE,
    name TEXT NOT NULL, group_id BIGINT, status TEXT DEFAULT 'active', deleted_at TIMESTAMPTZ,
    ip_whitelist JSONB, ip_blacklist JSONB, expires_at TIMESTAMPTZ, last_used_at TIMESTAMPTZ,
    quota NUMERIC DEFAULT 0, quota_used NUMERIC DEFAULT 0,
    rate_limit_5h NUMERIC DEFAULT 0, rate_limit_1d NUMERIC DEFAULT 0, rate_limit_7d NUMERIC DEFAULT 0,
    usage_5h NUMERIC DEFAULT 0
);
CREATE TABLE deleted_api_key_audits (id BIGSERIAL PRIMARY KEY, key VARCHAR(128) NOT NULL, key_name TEXT NOT NULL);
CREATE TABLE idempotency_records (id BIGSERIAL PRIMARY KEY, scope TEXT NOT NULL, response_body TEXT);
INSERT INTO users (id) VALUES (1);
INSERT INTO groups (id) VALUES (1), (2);
INSERT INTO user_allowed_groups VALUES (1,1);
`
