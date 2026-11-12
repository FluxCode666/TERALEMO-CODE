package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyHashMigrationClearsCredentialsAfterBackfill(t *testing.T) {
	content, err := FS.ReadFile("264_api_key_hash_storage.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	backfill := strings.Index(sql, "UPDATE api_keys SET key_hash =")
	clear := strings.Index(sql, "UPDATE api_keys SET key = NULL")
	require.GreaterOrEqual(t, backfill, 0)
	require.Greater(t, clear, backfill)
	for _, statement := range []string{
		"SET LOCAL lock_timeout = '5s'",
		"SET LOCAL statement_timeout = '30s'",
		"ADD COLUMN IF NOT EXISTS key_hash VARCHAR(64)",
		"ADD COLUMN IF NOT EXISTS key_hint VARCHAR(32)",
		"ALTER COLUMN key DROP NOT NULL",
		"encode(sha256(convert_to(key, 'UTF8')), 'hex')",
		"COALESCE(NULLIF(key_hash, ''),",
		"WHEN char_length(key) >= 12 THEN left(key, 7) || '...' || right(key, 4)",
		"ELSE left(key, LEAST(3, char_length(key) / 2)) || '...'",
		"ALTER COLUMN key_hash SET NOT NULL",
		"ALTER COLUMN key_hint SET NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS api_keys_key_hash_key ON api_keys (key_hash)",
		"CHECK (key IS NULL)",
		"CHECK (key_hash ~ '^[0-9a-f]{64}$')",
		"UPDATE deleted_api_key_audits SET key = '' WHERE key <> ''",
		"scope = 'user.api_keys.create' AND response_body IS NOT NULL",
		"payload - 'key' - 'key_hint'",
		"jsonb_build_object('key', hint, 'key_hint', hint)",
	} {
		require.Contains(t, sql, statement)
	}
	require.NotContains(t, sql, "DELETE FROM deleted_api_key_audits")
	require.NotContains(t, sql, "CREATE INDEX CONCURRENTLY")
}

func TestAPIKeyHashMigrationUsesStoredHashesForInvalidation(t *testing.T) {
	content, err := FS.ReadFile("264_api_key_hash_storage.sql")
	require.NoError(t, err)
	sql := string(content)
	functions := sql[:strings.Index(sql, "-- Preserve the hash")]
	for _, name := range []string{"api_key", "user", "group", "allowed_group"} {
		require.Contains(t, functions, "CREATE OR REPLACE FUNCTION enqueue_"+name+"_auth_cache_invalidation()")
	}
	require.Contains(t, functions, "VALUES (hashed_key)")
	require.Contains(t, functions, "enqueue_auth_cache_invalidation_hash(OLD.key_hash)")
	require.Contains(t, functions, "enqueue_auth_cache_invalidation_hash(NEW.key_hash)")
	require.Contains(t, functions, "SELECT k.key_hash FROM api_keys AS k")
	require.Contains(t, functions, "OLD.quota_used >= OLD.quota")
	for _, field := range []string{"rate_limit_5h", "rate_limit_1d", "rate_limit_7d", "profit_control_enabled", "profit_min_margin", "profit_safety_buffer"} {
		require.Contains(t, functions, "OLD."+field)
		require.Contains(t, functions, "NEW."+field)
	}
	require.NotContains(t, functions, "convert_to(")
	require.NotContains(t, functions, "OLD.key)")
	require.NotContains(t, functions, "NEW.key)")
	require.NotContains(t, functions, "k.key,")
}
