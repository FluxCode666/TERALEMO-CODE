-- 管理后台邮件群发：任务表与收件人表。
-- 编号接在 259 之后（260~263 已被占用），因此使用 264。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS mass_email_campaign (
    id               BIGSERIAL PRIMARY KEY,
    subject          VARCHAR(200) NOT NULL CHECK (char_length(btrim(subject)) > 0),
    body_html        TEXT NOT NULL DEFAULT '',
    recipient_filter JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(recipient_filter) = 'object'),
    status           VARCHAR(20) NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'scheduled', 'sending', 'completed', 'cancelled', 'failed')),
    scheduled_at     TIMESTAMPTZ,
    total_count      INTEGER NOT NULL DEFAULT 0 CHECK (total_count >= 0),
    sent_count       INTEGER NOT NULL DEFAULT 0 CHECK (sent_count >= 0),
    failed_count     INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    opened_count     INTEGER NOT NULL DEFAULT 0 CHECK (opened_count >= 0),
    created_by       BIGINT,
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    last_error       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mass_email_campaign_status_created
    ON mass_email_campaign (status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_mass_email_campaign_created_at
    ON mass_email_campaign (created_at DESC, id DESC);

-- 定时到期扫描：status = 'scheduled' AND scheduled_at <= now()
CREATE INDEX IF NOT EXISTS idx_mass_email_campaign_due
    ON mass_email_campaign (scheduled_at, id)
    WHERE status = 'scheduled';

-- 收件人：不给 users / enterprises 建外键，账号或企业被删除后历史发送记录仍需保留。
CREATE TABLE IF NOT EXISTS mass_email_recipient (
    id               BIGSERIAL PRIMARY KEY,
    campaign_id      BIGINT NOT NULL REFERENCES mass_email_campaign(id) ON DELETE CASCADE,
    email            VARCHAR(255) NOT NULL CHECK (char_length(btrim(email)) > 0),
    name             VARCHAR(100) NOT NULL DEFAULT '',
    user_id          BIGINT,
    enterprise_id    BIGINT,
    status           VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sent', 'failed')),
    error            TEXT NOT NULL DEFAULT '',
    attempts         INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claimed_at       TIMESTAMPTZ,
    sent_at          TIMESTAMPTZ,
    tracking_token   VARCHAR(32) NOT NULL,
    first_opened_at  TIMESTAMPTZ,
    last_opened_at   TIMESTAMPTZ,
    open_count       INTEGER NOT NULL DEFAULT 0 CHECK (open_count >= 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 同一任务内按邮箱小写去重（表达式唯一索引，ent schema 无法表达，以本迁移为准）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_mass_email_recipient_campaign_email
    ON mass_email_recipient (campaign_id, lower(email));

CREATE UNIQUE INDEX IF NOT EXISTS idx_mass_email_recipient_tracking_token
    ON mass_email_recipient (tracking_token);

-- 领取 pending 收件人（含过期租约回收）：WHERE campaign_id = ? AND status = 'pending'
--   AND (claimed_at IS NULL OR claimed_at < now() - interval '5 minutes')
CREATE INDEX IF NOT EXISTS idx_mass_email_recipient_claim
    ON mass_email_recipient (campaign_id, status, claimed_at, id);

-- 详情页「已打开 / 未打开」筛选。
CREATE INDEX IF NOT EXISTS idx_mass_email_recipient_open
    ON mass_email_recipient (campaign_id, open_count, id);

CREATE INDEX IF NOT EXISTS idx_mass_email_recipient_email
    ON mass_email_recipient (campaign_id, email);
