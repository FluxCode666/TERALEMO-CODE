-- Scheduled question-test rounds and per-account notification state.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS scheduled_question_test_plans (
    id               BIGSERIAL PRIMARY KEY,
    name             VARCHAR(100) NOT NULL CHECK (char_length(btrim(name)) > 0),
    account_ids      JSONB NOT NULL CHECK (jsonb_typeof(account_ids) = 'array' AND jsonb_array_length(account_ids) BETWEEN 1 AND 200),
    question_ids     JSONB NOT NULL CHECK (jsonb_typeof(question_ids) = 'array' AND jsonb_array_length(question_ids) BETWEEN 1 AND 20),
    model_id         VARCHAR(100) NOT NULL DEFAULT '',
    cron_expression  VARCHAR(100) NOT NULL CHECK (char_length(btrim(cron_expression)) > 0),
    enabled          BOOLEAN NOT NULL DEFAULT true,
    min_pass_ratio   INTEGER NOT NULL DEFAULT 80 CHECK (min_pass_ratio BETWEEN 0 AND 100),
    cooldown_minutes INTEGER NOT NULL DEFAULT 360 CHECK (cooldown_minutes BETWEEN 0 AND 10080),
    notify_enabled   BOOLEAN NOT NULL DEFAULT true,
    last_run_at      TIMESTAMPTZ,
    next_run_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS scheduled_question_test_plans_due_idx
    ON scheduled_question_test_plans (next_run_at, id) WHERE enabled = true;

CREATE TABLE IF NOT EXISTS scheduled_question_test_runs (
    id               BIGSERIAL PRIMARY KEY,
    plan_id          BIGINT NOT NULL REFERENCES scheduled_question_test_plans(id) ON DELETE CASCADE,
    status           VARCHAR(20) NOT NULL CHECK (status IN ('success', 'failed')),
    started_at       TIMESTAMPTZ NOT NULL,
    finished_at      TIMESTAMPTZ NOT NULL,
    total_accounts   INTEGER NOT NULL CHECK (total_accounts >= 0),
    passed_accounts  INTEGER NOT NULL CHECK (passed_accounts BETWEEN 0 AND total_accounts),
    alerted_accounts INTEGER NOT NULL CHECK (alerted_accounts BETWEEN 0 AND total_accounts),
    notifications_suppressed BOOLEAN NOT NULL DEFAULT false,
    error_message    TEXT NOT NULL DEFAULT '',
    summary          JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(summary) = 'array'),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS scheduled_question_test_runs_plan_started_idx
    ON scheduled_question_test_runs (plan_id, started_at DESC, id DESC);

-- Account IDs deliberately have no FK: deleted accounts still produce round errors.
CREATE TABLE IF NOT EXISTS scheduled_question_test_account_states (
    id            BIGSERIAL PRIMARY KEY,
    plan_id       BIGINT NOT NULL REFERENCES scheduled_question_test_plans(id) ON DELETE CASCADE,
    account_id    BIGINT NOT NULL CHECK (account_id > 0),
    state         VARCHAR(10) NOT NULL CHECK (state IN ('ok', 'alert')),
    last_alert_at TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (plan_id, account_id)
);
