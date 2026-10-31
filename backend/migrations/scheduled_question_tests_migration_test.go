//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionTestsMigration(t *testing.T) {
	content, err := FS.ReadFile("261_add_scheduled_question_tests.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, fragment := range []string{
		"SET LOCAL lock_timeout = '5s'",
		"SET LOCAL statement_timeout = '10min'",
		"CREATE TABLE IF NOT EXISTS scheduled_question_test_plans",
		"name VARCHAR(100) NOT NULL CHECK (char_length(btrim(name)) > 0)",
		"account_ids JSONB NOT NULL CHECK (jsonb_typeof(account_ids) = 'array' AND jsonb_array_length(account_ids) BETWEEN 1 AND 200)",
		"question_ids JSONB NOT NULL CHECK (jsonb_typeof(question_ids) = 'array' AND jsonb_array_length(question_ids) BETWEEN 1 AND 20)",
		"model_id VARCHAR(100) NOT NULL DEFAULT ''",
		"cron_expression VARCHAR(100) NOT NULL CHECK (char_length(btrim(cron_expression)) > 0)",
		"enabled BOOLEAN NOT NULL DEFAULT true",
		"min_pass_ratio INTEGER NOT NULL DEFAULT 80 CHECK (min_pass_ratio BETWEEN 0 AND 100)",
		"cooldown_minutes INTEGER NOT NULL DEFAULT 360 CHECK (cooldown_minutes BETWEEN 0 AND 10080)",
		"notify_enabled BOOLEAN NOT NULL DEFAULT true",
		"last_run_at TIMESTAMPTZ",
		"next_run_at TIMESTAMPTZ",
		"CREATE INDEX IF NOT EXISTS scheduled_question_test_plans_due_idx ON scheduled_question_test_plans (next_run_at, id) WHERE enabled = true",
		"CREATE TABLE IF NOT EXISTS scheduled_question_test_runs",
		"status VARCHAR(20) NOT NULL CHECK (status IN ('success', 'failed'))",
		"passed_accounts INTEGER NOT NULL CHECK (passed_accounts BETWEEN 0 AND total_accounts)",
		"alerted_accounts INTEGER NOT NULL CHECK (alerted_accounts BETWEEN 0 AND total_accounts)",
		"notifications_suppressed BOOLEAN NOT NULL DEFAULT false",
		"summary JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(summary) = 'array')",
		"CREATE INDEX IF NOT EXISTS scheduled_question_test_runs_plan_started_idx ON scheduled_question_test_runs (plan_id, started_at DESC, id DESC)",
		"CREATE TABLE IF NOT EXISTS scheduled_question_test_account_states",
		"account_id BIGINT NOT NULL CHECK (account_id > 0)",
		"state VARCHAR(10) NOT NULL CHECK (state IN ('ok', 'alert'))",
		"last_alert_at TIMESTAMPTZ",
		"UNIQUE (plan_id, account_id)",
	} {
		require.Contains(t, sql, fragment)
	}
	require.Equal(t, 2, strings.Count(sql, "REFERENCES scheduled_question_test_plans(id) ON DELETE CASCADE"))
	require.Equal(t, 3, strings.Count(sql, "id BIGSERIAL PRIMARY KEY"))
	require.Equal(t, 2, strings.Count(sql, "created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()"))
	require.Equal(t, 2, strings.Count(sql, "updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()"))
	require.NotContains(t, sql, "REFERENCES accounts")
	require.NotContains(t, strings.ToUpper(sql), "DROP TABLE")
	require.NotContains(t, strings.ToUpper(sql), "BEGIN;")
}
