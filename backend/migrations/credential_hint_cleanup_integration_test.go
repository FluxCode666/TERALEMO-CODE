//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCredentialHintCleanupPostgres(t *testing.T) {
	dsn := os.Getenv("ASO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ASO_TEST_POSTGRES_DSN is required for the isolated migration test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	schema := pq.QuoteIdentifier(fmt.Sprintf("credential_hint_test_%d", time.Now().UnixNano()))
	_, err = conn.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := conn.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, cleanupErr)
	})
	_, err = conn.ExecContext(ctx, "SET search_path TO "+schema)
	require.NoError(t, err)
	for _, name := range []string{"180_audit_logs.sql", "057_add_idempotency_records.sql"} {
		content, readErr := FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = conn.ExecContext(ctx, string(content))
		require.NoError(t, err)
	}
	_, err = conn.ExecContext(ctx, `
		CREATE TABLE ops_error_logs (id BIGSERIAL PRIMARY KEY, api_key_prefix VARCHAR(32), attempted_key_prefix VARCHAR(32), message TEXT);
		INSERT INTO ops_error_logs (api_key_prefix, attempted_key_prefix, message) VALUES
			('4b9e6a1', NULL, 'retained'), (NULL, '4b9e6a1', 'retained'),
			('', '...', 'retained'), ('sk-...1234', '', 'retained');
		INSERT INTO audit_logs (actor_user_id, actor_email, auth_method, credential_masked, action, method, path, request_id, request_body)
		SELECT n, 'audit@example.invalid', CASE n % 3 WHEN 0 THEN 'jwt' WHEN 1 THEN 'admin_api_key' ELSE 'unknown' END,
			CASE n % 5 WHEN 0 THEN '' WHEN 1 THEN 'x-api-key 4b9e6a****d573' WHEN 2 THEN 'Bearer eyJhbG****1234' WHEN 3 THEN 'unknown-fragment' ELSE '...' END,
			'test.action', 'PUT', '/api/v1/user', n::text, repeat('retained ', 32)
		FROM generate_series(1, 150000) AS n;
	`)
	require.NoError(t, err)

	type replayFixture struct {
		scope string
		body  any
		want  string
	}
	nulResponse := `{"id":44,"api_key_masked":"a\u0000bc***"}`
	require.True(t, json.Valid([]byte(nulResponse)), "escaped NUL is valid JSON even though JSONB cannot represent it")
	fixtures := []replayFixture{
		{"admin.channel_monitors.duplicate", nulResponse, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", `{"data":{"id":45,"api_key_masked":"a\u0000bc***"}}`, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", `{"id":42,"name":"retained","api_key_masked":"4b9e***","api_key":"legacy-key"}`, `{"id":42,"name":"retained","api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", `{"code":0,"data":{"id":43,"name":"retained","api_key_masked":"4b9e***","api_key":"legacy-key"}}`, `{"code":0,"api_key_masked":"...","data":{"id":43,"name":"retained","api_key_masked":"..."}}`},
		{"admin.channel_monitors.duplicate", `{"data":["legacy-key"]}`, `{"api_key_masked":"...","data":{"api_key_masked":"..."}}`},
		{"admin.channel_monitors.duplicate", `{"api_key_masked":"4b9e`, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", `"legacy-key"`, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", `null`, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", `[]`, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", ``, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.create", `{"api_key_masked":"4b9e***"}`, `{"api_key_masked":"..."}`},
		{"admin.channel_monitors.duplicate", nil, ""},
		{"unrelated.scope", `{"value":"unchanged"}`, `{"value":"unchanged"}`},
		{"admin.channelXmonitors.duplicate", `{"value":"unchanged"}`, `{"value":"unchanged"}`},
	}
	for i, fixture := range fixtures {
		_, err = conn.ExecContext(ctx, `INSERT INTO idempotency_records
			(scope,idempotency_key_hash,request_fingerprint,status,response_status,response_body,expires_at)
			VALUES ($1,$2,'fingerprint','succeeded',200,$3,'2030-01-01')`, fixture.scope, fmt.Sprint(i+1), fixture.body)
		require.NoError(t, err)
	}
	content, err := FS.ReadFile("265_credential_hint_cleanup.sql")
	require.NoError(t, err)
	apply := func(checkConcurrentAccess bool) {
		tx, txErr := conn.BeginTx(ctx, nil)
		require.NoError(t, txErr)
		defer func() { _ = tx.Rollback() }()
		start := time.Now()
		_, txErr = tx.ExecContext(ctx, string(content))
		require.NoError(t, txErr)
		t.Logf("cleanup 150000 indexed audit rows (concurrent check %t): %s", checkConcurrentAccess, time.Since(start))
		if checkConcurrentAccess {
			other, connErr := db.Conn(ctx)
			require.NoError(t, connErr)
			defer func() { _ = other.Close() }()
			_, connErr = other.ExecContext(ctx, "SET search_path TO "+schema+"; SET lock_timeout = '500ms'; SET statement_timeout = '2s'")
			require.NoError(t, connErr)
			var count int
			require.NoError(t, other.QueryRowContext(ctx, "SELECT count(*) FROM audit_logs").Scan(&count))
			require.Equal(t, 150000, count, "ordinary readers must not be blocked by a table-level exclusive lock")
			_, connErr = other.ExecContext(ctx, "INSERT INTO audit_logs (id, action) VALUES (200000, 'concurrent.insert')")
			require.NoError(t, connErr, "unrelated inserts must remain available until the cleanup transaction commits")
			var strongLocks int
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND mode = 'AccessExclusiveLock'`).Scan(&strongLocks))
			require.Zero(t, strongLocks)
		}
		require.NoError(t, tx.Commit())
	}
	assertCleaned := func() {
		var count int
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT count(*) FROM audit_logs WHERE credential_masked NOT IN ('', '...')").Scan(&count))
		require.Zero(t, count)
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs WHERE id <= 150000
			AND actor_user_id = id AND actor_email = 'audit@example.invalid' AND action = 'test.action'
			AND path = '/api/v1/user' AND request_id = id::text AND request_body = repeat('retained ', 32)
			AND credential_masked = CASE WHEN id % 5 = 0 THEN '' ELSE '...' END`).Scan(&count))
		require.Equal(t, 150000, count, "preserve event data, IDs, and empty credentials")
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM ops_error_logs WHERE
			COALESCE(api_key_prefix, '') NOT IN ('', '...') OR COALESCE(attempted_key_prefix, '') NOT IN ('', '...')`).Scan(&count))
		require.Zero(t, count)
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT count(*) FROM ops_error_logs WHERE message = 'retained'").Scan(&count))
		require.Equal(t, 4, count)
		var prefix, attempted sql.NullString
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT api_key_prefix, attempted_key_prefix FROM ops_error_logs WHERE id = 1").Scan(&prefix, &attempted))
		require.Equal(t, "...", prefix.String)
		require.False(t, attempted.Valid)
		for i, fixture := range fixtures {
			var body sql.NullString
			var scope, status, fingerprint, hash string
			var statusCode int
			var expiresAt time.Time
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT response_body,scope,status,request_fingerprint,idempotency_key_hash,response_status,expires_at
				FROM idempotency_records WHERE id = $1`, i+1).Scan(&body, &scope, &status, &fingerprint, &hash, &statusCode, &expiresAt))
			if fixture.body == nil {
				require.False(t, body.Valid)
			} else {
				require.JSONEq(t, fixture.want, body.String)
			}
			require.Equal(t, fixture.scope, scope)
			require.Equal(t, "succeeded", status)
			require.Equal(t, "fingerprint", fingerprint)
			require.Equal(t, fmt.Sprint(i+1), hash)
			require.Equal(t, 200, statusCode)
			require.Equal(t, 2030, expiresAt.Year())
		}
	}
	apply(true)
	assertCleaned()
	apply(false)
	assertCleaned()
}
