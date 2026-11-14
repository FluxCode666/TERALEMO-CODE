//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMassEmailCampaignsMigration(t *testing.T) {
	content, err := FS.ReadFile("266_mass_email_campaigns.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	for _, fragment := range []string{
		"SET LOCAL lock_timeout = '5s'",
		"SET LOCAL statement_timeout = '10min'",
		"CREATE TABLE IF NOT EXISTS mass_email_campaign",
		"subject VARCHAR(200) NOT NULL CHECK (char_length(btrim(subject)) > 0)",
		"body_html TEXT NOT NULL DEFAULT ''",
		"recipient_filter JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(recipient_filter) = 'object')",
		"status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'sending', 'completed', 'cancelled', 'failed'))",
		"scheduled_at TIMESTAMPTZ",
		"total_count INTEGER NOT NULL DEFAULT 0 CHECK (total_count >= 0)",
		"sent_count INTEGER NOT NULL DEFAULT 0 CHECK (sent_count >= 0)",
		"failed_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0)",
		"opened_count INTEGER NOT NULL DEFAULT 0 CHECK (opened_count >= 0)",
		"started_at TIMESTAMPTZ",
		"finished_at TIMESTAMPTZ",
		"CREATE INDEX IF NOT EXISTS idx_mass_email_campaign_status_created",
		"CREATE INDEX IF NOT EXISTS idx_mass_email_campaign_due ON mass_email_campaign (scheduled_at, id) WHERE status = 'scheduled'",
		"CREATE TABLE IF NOT EXISTS mass_email_recipient",
		"campaign_id BIGINT NOT NULL REFERENCES mass_email_campaign(id) ON DELETE CASCADE",
		"email VARCHAR(255) NOT NULL CHECK (char_length(btrim(email)) > 0)",
		"status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed'))",
		"attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0)",
		"claimed_at TIMESTAMPTZ",
		"tracking_token VARCHAR(32) NOT NULL",
		"first_opened_at TIMESTAMPTZ",
		"last_opened_at TIMESTAMPTZ",
		"open_count INTEGER NOT NULL DEFAULT 0 CHECK (open_count >= 0)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_mass_email_recipient_campaign_email ON mass_email_recipient (campaign_id, lower(email))",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_mass_email_recipient_tracking_token ON mass_email_recipient (tracking_token)",
		"CREATE INDEX IF NOT EXISTS idx_mass_email_recipient_claim",
		"CREATE INDEX IF NOT EXISTS idx_mass_email_recipient_open",
	} {
		require.Contains(t, sql, fragment)
	}

	// 两张表 + 两张表各自的时间戳字段。
	require.Equal(t, 2, strings.Count(sql, "id BIGSERIAL PRIMARY KEY"))
	require.Equal(t, 2, strings.Count(sql, "created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()"))
	require.Equal(t, 2, strings.Count(sql, "updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()"))

	// 只允许收件人表引用任务表，且必须级联删除；不要给 users/enterprises 建外键（删号后历史仍保留）。
	require.Equal(t, 1, strings.Count(sql, "REFERENCES mass_email_campaign(id) ON DELETE CASCADE"))
	require.NotContains(t, sql, "REFERENCES users")
	require.NotContains(t, sql, "REFERENCES enterprises")

	// 幂等：不得出现 DROP / BEGIN，迁移执行器自己管事务。
	require.NotContains(t, strings.ToUpper(sql), "DROP TABLE")
	require.NotContains(t, strings.ToUpper(sql), "DROP INDEX")
	require.NotContains(t, strings.ToUpper(sql), "BEGIN;")
}
