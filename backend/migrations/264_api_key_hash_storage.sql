-- Forward-only credential migration. Drain old application instances before
-- applying it: the cleared credentials cannot be reconstructed for a downgrade.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_hash VARCHAR(64);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_hint VARCHAR(32);
ALTER TABLE api_keys ALTER COLUMN key DROP NOT NULL;

CREATE OR REPLACE FUNCTION enqueue_auth_cache_invalidation_hash(hashed_key TEXT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF hashed_key IS NULL OR hashed_key = '' THEN
        RETURN;
    END IF;
    INSERT INTO auth_cache_invalidation_outbox (cache_key) VALUES (hashed_key);
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM enqueue_auth_cache_invalidation_hash(OLD.key_hash);
        RETURN OLD;
    END IF;

    IF OLD.key_hash IS DISTINCT FROM NEW.key_hash
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
       OR OLD.user_id IS DISTINCT FROM NEW.user_id
       OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at
       OR OLD.quota IS DISTINCT FROM NEW.quota
       OR (OLD.quota > 0 AND OLD.quota_used >= OLD.quota)
          IS DISTINCT FROM (NEW.quota > 0 AND NEW.quota_used >= NEW.quota)
       OR OLD.rate_limit_5h IS DISTINCT FROM NEW.rate_limit_5h
       OR OLD.rate_limit_1d IS DISTINCT FROM NEW.rate_limit_1d
       OR OLD.rate_limit_7d IS DISTINCT FROM NEW.rate_limit_7d THEN
        PERFORM enqueue_auth_cache_invalidation_hash(OLD.key_hash);
        IF NEW.deleted_at IS NULL AND NEW.key_hash IS DISTINCT FROM OLD.key_hash THEN
            PERFORM enqueue_auth_cache_invalidation_hash(NEW.key_hash);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_user_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.role IS NOT DISTINCT FROM NEW.role
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT k.key_hash FROM api_keys AS k
    WHERE k.user_id = OLD.id AND k.deleted_at IS NULL AND k.key_hash <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.is_exclusive IS NOT DISTINCT FROM NEW.is_exclusive
       AND OLD.allow_image_generation IS NOT DISTINCT FROM NEW.allow_image_generation
       AND OLD.platform IS NOT DISTINCT FROM NEW.platform
       AND OLD.subscription_type IS NOT DISTINCT FROM NEW.subscription_type
       AND OLD.rate_multiplier IS NOT DISTINCT FROM NEW.rate_multiplier
       AND OLD.peak_rate_enabled IS NOT DISTINCT FROM NEW.peak_rate_enabled
       AND OLD.peak_start IS NOT DISTINCT FROM NEW.peak_start
       AND OLD.peak_end IS NOT DISTINCT FROM NEW.peak_end
       AND OLD.peak_rate_multiplier IS NOT DISTINCT FROM NEW.peak_rate_multiplier
       AND OLD.profit_control_enabled IS NOT DISTINCT FROM NEW.profit_control_enabled
       AND OLD.profit_min_margin IS NOT DISTINCT FROM NEW.profit_min_margin
       AND OLD.profit_safety_buffer IS NOT DISTINCT FROM NEW.profit_safety_buffer
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT k.key_hash FROM api_keys AS k
    WHERE k.group_id = OLD.id AND k.deleted_at IS NULL AND k.key_hash <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_allowed_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_user_id BIGINT;
    target_group_id BIGINT;
BEGIN
    IF TG_OP = 'UPDATE'
       AND (OLD.user_id IS DISTINCT FROM NEW.user_id
            OR OLD.group_id IS DISTINCT FROM NEW.group_id) THEN
        IF EXISTS (SELECT 1 FROM groups g WHERE g.id = OLD.group_id AND g.is_exclusive = TRUE) THEN
            INSERT INTO auth_cache_invalidation_outbox (cache_key)
            SELECT k.key_hash FROM api_keys AS k
            WHERE k.user_id = OLD.user_id AND k.group_id = OLD.group_id
              AND k.deleted_at IS NULL AND k.key_hash <> '';
        END IF;
        target_user_id := NEW.user_id;
        target_group_id := NEW.group_id;
    ELSIF TG_OP = 'UPDATE' THEN
        RETURN NEW;
    ELSIF TG_OP = 'INSERT' THEN
        target_user_id := NEW.user_id;
        target_group_id := NEW.group_id;
    ELSE
        target_user_id := OLD.user_id;
        target_group_id := OLD.group_id;
    END IF;

    IF EXISTS (SELECT 1 FROM groups g WHERE g.id = target_group_id AND g.is_exclusive = TRUE) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT k.key_hash FROM api_keys AS k
        WHERE k.user_id = target_user_id AND k.group_id = target_group_id
          AND k.deleted_at IS NULL AND k.key_hash <> '';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP FUNCTION IF EXISTS enqueue_auth_cache_invalidation(TEXT);

-- Preserve the hash on repeated runs; never hash a hash or a display hint.
UPDATE api_keys
SET key_hash = COALESCE(NULLIF(key_hash, ''), encode(sha256(convert_to(key, 'UTF8')), 'hex')),
    key_hint = CASE WHEN left(key, 3) = 'sk-' THEN 'sk-' ELSE '' END || '...' ||
        right(key, GREATEST(0, LEAST(4,
            char_length(key) - CASE WHEN left(key, 3) = 'sk-' THEN 3 ELSE 0 END - 24,
            (char_length(key) - CASE WHEN left(key, 3) = 'sk-' THEN 3 ELSE 0 END) / 4)))
WHERE key IS NOT NULL;

ALTER TABLE api_keys ALTER COLUMN key_hash SET NOT NULL;
ALTER TABLE api_keys ALTER COLUMN key_hint SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS api_keys_key_hash_key ON api_keys (key_hash);

UPDATE api_keys SET key = NULL WHERE key IS NOT NULL;
-- Retain historical audit metadata without retaining credential material.
UPDATE deleted_api_key_audits SET key = '' WHERE key <> '';

-- Old creation responses may also have been persisted for idempotent replay.
-- Preserve replay metadata while removing the one-time credential field.
DO $$
DECLARE
    record_row RECORD;
    payload JSONB;
    hint TEXT;
BEGIN
    FOR record_row IN
        SELECT id, response_body FROM idempotency_records
        WHERE scope = 'user.api_keys.create' AND response_body IS NOT NULL
    LOOP
        BEGIN
            payload := record_row.response_body::jsonb;
        -- JSONB also rejects valid JSON containing unsupported Unicode escapes.
        EXCEPTION WHEN invalid_text_representation OR untranslatable_character THEN
            UPDATE idempotency_records
            SET response_body = jsonb_build_object('key', '...', 'key_hint', '...')::text
            WHERE id = record_row.id;
            CONTINUE;
        END;
        IF jsonb_typeof(payload) <> 'object' THEN
            UPDATE idempotency_records
            SET response_body = jsonb_build_object('key', '...', 'key_hint', '...')::text
            WHERE id = record_row.id;
            CONTINUE;
        END IF;
        SELECT key_hint INTO hint FROM api_keys WHERE id::text = payload->>'id';
        hint := COALESCE(hint, '...');
        UPDATE idempotency_records
        SET response_body = ((payload - 'key' - 'key_hint') || jsonb_build_object('key', hint, 'key_hint', hint))::text
        WHERE id = record_row.id;
    END LOOP;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'api_keys'::regclass AND conname = 'api_keys_no_plaintext_key') THEN
        ALTER TABLE api_keys ADD CONSTRAINT api_keys_no_plaintext_key CHECK (key IS NULL);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'api_keys'::regclass AND conname = 'api_keys_key_hash_format') THEN
        ALTER TABLE api_keys ADD CONSTRAINT api_keys_key_hash_format CHECK (key_hash ~ '^[0-9a-f]{64}$');
    END IF;
END;
$$;

COMMENT ON COLUMN api_keys.key IS 'Retired credential column; always NULL';
COMMENT ON COLUMN api_keys.key_hash IS 'SHA-256 hex of the original API credential; no pepper';
COMMENT ON COLUMN api_keys.key_hint IS 'Display-only masked credential identifier';
