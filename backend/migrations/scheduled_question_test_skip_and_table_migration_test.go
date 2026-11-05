//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionTestSkipAndTableMigration(t *testing.T) {
	content, err := FS.ReadFile("263_scheduled_question_test_skip_and_table.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, fragment := range []string{
		"SET LOCAL lock_timeout = '5s'",
		"SET LOCAL statement_timeout = '10min'",
		"ALTER TABLE scheduled_question_test_runs ADD COLUMN IF NOT EXISTS skipped_accounts INTEGER NOT NULL DEFAULT 0 CHECK (skipped_accounts >= 0)",
	} {
		require.Contains(t, sql, fragment)
	}
	require.Equal(t, 1, strings.Count(sql, "ADD COLUMN IF NOT EXISTS"))
	require.NotContains(t, strings.ToUpper(sql), "DROP ")
	require.NotContains(t, strings.ToUpper(sql), "BEGIN;")
}
