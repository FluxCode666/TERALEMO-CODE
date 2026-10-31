//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountTestQuestionsMigration(t *testing.T) {
	content, err := FS.ReadFile("260_add_account_test_questions.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, fragment := range []string{
		"SET LOCAL lock_timeout = '5s'",
		"CREATE TABLE IF NOT EXISTS account_test_questions",
		"id BIGSERIAL PRIMARY KEY",
		"name VARCHAR(100) NOT NULL UNIQUE",
		"content TEXT NOT NULL",
		"answer_patterns JSONB NOT NULL DEFAULT '[]'::jsonb",
		"enabled BOOLEAN NOT NULL DEFAULT true",
		"sort_order INTEGER NOT NULL DEFAULT 0",
		"created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"CREATE INDEX IF NOT EXISTS account_test_questions_sort_order_id_idx ON account_test_questions (sort_order, id)",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, strings.ToUpper(sql), "DROP TABLE")
}
