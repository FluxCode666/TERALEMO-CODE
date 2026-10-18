package migrations

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestEnterpriseDepartmentTreeMigration(t *testing.T) {
	content, err := FS.ReadFile("251_enterprise_department_tree.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS parent_id BIGINT NULL REFERENCES enterprise_departments(id)",
		"ADD COLUMN IF NOT EXISTS path TEXT NOT NULL", "ADD COLUMN IF NOT EXISTS depth INT NOT NULL",
		"SET path = '/' || id::text || '/', depth = 1", "DROP INDEX IF EXISTS idx_enterprise_departments_name_active",
		"(enterprise_id, parent_id, name)", "deleted_at IS NULL AND parent_id IS NOT NULL",
		"(enterprise_id, name)", "deleted_at IS NULL AND parent_id IS NULL", "CHECK (depth BETWEEN 1 AND 8)",
		"ADD COLUMN IF NOT EXISTS enterprise_department_path TEXT", "'/' || enterprise_department_id::text || '/'",
	} {
		require.Contains(t, sql, fragment)
	}
}
