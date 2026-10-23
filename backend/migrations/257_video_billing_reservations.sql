-- Intentionally no foreign keys: accepted tasks must remain payable after a
-- key, account, group or enterprise membership is removed.
CREATE TABLE IF NOT EXISTS video_billing_reservations (
    id VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    subscription_id BIGINT NULL,
    enterprise_id BIGINT NOT NULL DEFAULT 0,
    enterprise_department_id BIGINT NOT NULL DEFAULT 0,
    enterprise_member_id BIGINT NOT NULL DEFAULT 0,
    enterprise_department_path_ids JSONB NOT NULL DEFAULT '[]',
    platform VARCHAR(32) NOT NULL,
    protocol VARCHAR(32) NOT NULL,
    endpoint TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    file_id TEXT NOT NULL DEFAULT '',
    account_id BIGINT NOT NULL DEFAULT 0,
    hold_amount NUMERIC(20,8) NOT NULL CHECK (hold_amount >= 0),
    actual_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (actual_amount >= 0 AND actual_amount <= hold_amount),
    contract JSONB NOT NULL,
    snapshot JSONB NOT NULL DEFAULT '{}',
    state VARCHAR(20) NOT NULL DEFAULT 'preparing'
        CHECK (state IN ('preparing','accepted','settled','released','manual')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner VARCHAR(128) NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS video_billing_reservations_task_idx
    ON video_billing_reservations(protocol,task_id) WHERE task_id <> '';
CREATE INDEX IF NOT EXISTS video_billing_reservations_file_idx
    ON video_billing_reservations(protocol,file_id) WHERE file_id <> '';
CREATE INDEX IF NOT EXISTS video_billing_reservations_due_idx
    ON video_billing_reservations(next_attempt_at,lease_until) WHERE state IN ('preparing','accepted');
CREATE INDEX IF NOT EXISTS video_billing_reservations_payer_idx
    ON video_billing_reservations(user_id,enterprise_id) WHERE state IN ('preparing','accepted','manual');
CREATE INDEX IF NOT EXISTS video_billing_reservations_key_idx
    ON video_billing_reservations(api_key_id) WHERE state IN ('preparing','accepted','manual');
CREATE INDEX IF NOT EXISTS video_billing_reservations_quota_scope_idx
    ON video_billing_reservations(user_id,platform);
COMMENT ON TABLE video_billing_reservations IS 'Durable video price contracts and frozen funds; manual obligations never expire automatically';
