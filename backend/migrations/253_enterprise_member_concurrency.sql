-- NULL preserves individual concurrency; a positive value applies to all members.
ALTER TABLE enterprises
    ADD COLUMN IF NOT EXISTS member_concurrency INT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'enterprises'::regclass
          AND conname = 'enterprises_member_concurrency_positive'
    ) THEN
        ALTER TABLE enterprises ADD CONSTRAINT enterprises_member_concurrency_positive
            CHECK (member_concurrency IS NULL OR member_concurrency > 0);
    END IF;
END $$;

COMMENT ON COLUMN enterprises.member_concurrency IS 'Unified member concurrency; NULL preserves individual values and uses platform defaults for new accounts';

-- Validate text before casting, including values too large for PostgreSQL INT.
WITH configured AS (
    SELECT CASE
        WHEN value ~ '^[0-9]+$'
             AND length(ltrim(value, '0')) BETWEEN 1 AND 10
        THEN CASE WHEN ltrim(value, '0')::bigint BETWEEN 1 AND 2147483647
                  THEN ltrim(value, '0')::int END
    END AS concurrency
    FROM settings WHERE key = 'default_concurrency'
), defaults AS (
    SELECT COALESCE((SELECT concurrency FROM configured), 5) AS concurrency
)
UPDATE users u
SET concurrency = defaults.concurrency, updated_at = NOW()
FROM defaults
WHERE u.deleted_at IS NULL
  AND u.concurrency = 1
  AND EXISTS (
      SELECT 1 FROM enterprise_members m
      WHERE m.user_id = u.id
        AND m.deleted_at IS NULL
        AND m.account_created_by_enterprise = TRUE
  );
