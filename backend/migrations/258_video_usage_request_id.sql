-- Video billing preserves the upstream task resource in its stable request ID.
-- Gemini operation names exceed the original usage_logs VARCHAR(64), and may
-- also exceed the dedup tables' VARCHAR(255). Widen them without rewriting
-- IDs or changing the (request_id, api_key_id) uniqueness / archive contract.
ALTER TABLE usage_logs
    ALTER COLUMN request_id TYPE TEXT;

ALTER TABLE usage_billing_dedup
    ALTER COLUMN request_id TYPE TEXT;

ALTER TABLE usage_billing_dedup_archive
    ALTER COLUMN request_id TYPE TEXT;

-- Structured billing logs carry the same stable ID via OpsSystemLogSink.
ALTER TABLE ops_system_logs
    ALTER COLUMN request_id TYPE TEXT;

