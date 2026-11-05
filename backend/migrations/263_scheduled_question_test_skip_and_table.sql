SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE scheduled_question_test_runs
    ADD COLUMN IF NOT EXISTS skipped_accounts INTEGER NOT NULL DEFAULT 0 CHECK (skipped_accounts >= 0);
