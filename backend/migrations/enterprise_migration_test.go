package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseOrganizationMigration(t *testing.T) {
	content, err := FS.ReadFile("246_add_enterprises.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, table := range []string{"enterprises", "enterprise_departments", "enterprise_members", "enterprise_quotas", "enterprise_balance_logs", "enterprise_audit_logs", "enterprise_allowed_groups", "enterprise_group_whitelists"} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table+" (")
		require.Contains(t, sql, "COMMENT ON TABLE "+table+" IS '")
	}
	for _, definition := range []string{
		"balance DECIMAL(20,8) NOT NULL DEFAULT 0",
		"total_recharged DECIMAL(20,8) NOT NULL DEFAULT 0",
		"low_balance_threshold DECIMAL(20,8) NULL",
		"amount DECIMAL(20,8) NOT NULL",
		"balance_before DECIMAL(20,8) NOT NULL",
		"balance_after DECIMAL(20,8) NOT NULL",
		"settings JSONB NOT NULL DEFAULT '{}'::jsonb",
		"detail JSONB NOT NULL DEFAULT '{}'::jsonb",
		"ON enterprises (name) WHERE deleted_at IS NULL",
		"ON enterprise_departments (enterprise_id, name) WHERE deleted_at IS NULL",
		"ON enterprise_members (user_id) WHERE deleted_at IS NULL",
		"ON enterprise_quotas (scope_type, scope_id) WHERE deleted_at IS NULL",
		"ON enterprise_group_whitelists (scope_type, scope_id, group_id)",
		"PRIMARY KEY (enterprise_id, group_id)",
		"enterprise_id BIGINT NOT NULL REFERENCES enterprises(id)",
		"user_id BIGINT NOT NULL REFERENCES users(id)",
		"department_id BIGINT NULL REFERENCES enterprise_departments(id)",
		"group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE",
		"ON enterprise_balance_logs (enterprise_id, created_at DESC)",
		"ON enterprise_audit_logs (enterprise_id, created_at DESC)",
	} {
		require.Contains(t, sql, definition)
	}
	for _, period := range []string{"daily", "weekly", "monthly", "total"} {
		require.Contains(t, sql, period+"_limit_usd DECIMAL(20,10) NULL")
		require.Contains(t, sql, period+"_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0")
	}
	for _, period := range []string{"daily", "weekly", "monthly"} {
		require.Contains(t, sql, period+"_window_start TIMESTAMPTZ NULL")
	}
	require.NotContains(t, sql, "DROP TABLE")
	require.NotContains(t, sql, "CREATE INDEX CONCURRENTLY")
}

func TestUsageLogsEnterpriseColumnsMigration(t *testing.T) {
	content, err := FS.ReadFile("247_usage_logs_enterprise_columns.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS enterprise_id BIGINT NULL")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS enterprise_department_id BIGINT NULL")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USAGE_LOGS")
	require.NotContains(t, sql, "REFERENCES")
}

func TestUsageLogsEnterpriseIndexesMigration(t *testing.T) {
	content, err := FS.ReadFile("248_usage_logs_enterprise_indexes_notx.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_enterprise_created ON usage_logs (enterprise_id, created_at) WHERE enterprise_id IS NOT NULL")
	require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_enterprise_department_created ON usage_logs (enterprise_department_id, created_at) WHERE enterprise_department_id IS NOT NULL")
	for _, statement := range strings.Split(string(content), ";") {
		var lines []string
		for _, line := range strings.Split(statement, "\n") {
			if comment := strings.Index(line, "--"); comment >= 0 {
				line = line[:comment]
			}
			lines = append(lines, line)
		}
		statement = strings.TrimSpace(strings.Join(lines, "\n"))
		if statement != "" {
			require.True(t, strings.HasPrefix(statement, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "), statement)
		}
	}
}

func TestEnterpriseGroupConfigurationMigration(t *testing.T) {
	content, err := FS.ReadFile("249_enterprise_group_configuration.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, definition := range []string{
		"ALTER TABLE enterprises ADD COLUMN IF NOT EXISTS group_pool_configured BOOLEAN NOT NULL DEFAULT FALSE",
		"CREATE TABLE IF NOT EXISTS enterprise_group_whitelist_scopes (",
		"id BIGSERIAL PRIMARY KEY",
		"CONSTRAINT uq_enterprise_group_whitelist_scopes_scope UNIQUE (scope_type, scope_id)",
		"created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"ON enterprise_group_whitelist_scopes (enterprise_id)",
		"UPDATE enterprises e SET group_pool_configured = TRUE WHERE EXISTS (SELECT 1 FROM enterprise_allowed_groups a WHERE a.enterprise_id = e.id)",
		"INSERT INTO enterprise_group_whitelist_scopes (enterprise_id, scope_type, scope_id) SELECT DISTINCT enterprise_id, scope_type, scope_id FROM enterprise_group_whitelists ON CONFLICT (scope_type, scope_id) DO NOTHING",
	} {
		require.Contains(t, sql, definition)
	}
	require.NotContains(t, strings.ToUpper(sql), "DROP TABLE")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM")
	require.NotContains(t, sql, "PRIMARY KEY (scope_type, scope_id)")
}
