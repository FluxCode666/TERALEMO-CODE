SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE scheduled_question_test_plans
    ADD COLUMN IF NOT EXISTS report_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS email_enabled BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS feishu_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS feishu_webhook_url TEXT NOT NULL DEFAULT '' CHECK (char_length(feishu_webhook_url) <= 500),
    ADD COLUMN IF NOT EXISTS feishu_secret TEXT NOT NULL DEFAULT '' CHECK (char_length(feishu_secret) <= 200),
    ADD COLUMN IF NOT EXISTS auto_pause_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS auto_resume_enabled BOOLEAN NOT NULL DEFAULT false;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'scheduled_question_test_plans'::regclass
          AND conname = 'scheduled_question_test_plans_feishu_webhook_required'
    ) THEN
        ALTER TABLE scheduled_question_test_plans
            ADD CONSTRAINT scheduled_question_test_plans_feishu_webhook_required
            CHECK (NOT feishu_enabled OR feishu_webhook_url <> '');
    END IF;
END $$;

ALTER TABLE scheduled_question_test_runs
    ADD COLUMN IF NOT EXISTS paused_accounts INTEGER NOT NULL DEFAULT 0 CHECK (paused_accounts >= 0),
    ADD COLUMN IF NOT EXISTS resumed_accounts INTEGER NOT NULL DEFAULT 0 CHECK (resumed_accounts >= 0),
    ADD COLUMN IF NOT EXISTS auto_pause_skipped BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS notification_results JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(notification_results) = 'array');

ALTER TABLE scheduled_question_test_account_states
    ADD COLUMN IF NOT EXISTS auto_paused BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS auto_paused_at TIMESTAMPTZ;
