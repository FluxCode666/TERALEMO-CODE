-- Historical partial masks cannot be upgraded without the original secret.
-- Keep event/replay metadata but replace credential fragments with a fixed mask.
-- Separate from schema changes so the cleanup holds no ACCESS EXCLUSIVE locks.
-- The runner commits once per file; batches bound statement work, not lock lifetime.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

DO $$
DECLARE
    last_id BIGINT := 0;
    batch_end BIGINT;
BEGIN
    LOOP
        SELECT max(id) INTO batch_end FROM (
            SELECT id FROM audit_logs WHERE id > last_id ORDER BY id LIMIT 5000
        ) AS batch;
        EXIT WHEN batch_end IS NULL;
        UPDATE audit_logs SET credential_masked = '...'
        WHERE id > last_id AND id <= batch_end
          AND credential_masked <> '' AND credential_masked <> '...';
        last_id := batch_end;
    END LOOP;
END;
$$;

-- Error-log prefix snapshots predate the shared hint policy as well.
DO $$
DECLARE
    last_id BIGINT := 0;
    batch_end BIGINT;
BEGIN
    LOOP
        SELECT max(id) INTO batch_end FROM (
            SELECT id FROM ops_error_logs WHERE id > last_id ORDER BY id LIMIT 5000
        ) AS batch;
        EXIT WHEN batch_end IS NULL;
        UPDATE ops_error_logs
        SET api_key_prefix = CASE WHEN api_key_prefix IS NULL OR api_key_prefix = '' THEN api_key_prefix ELSE '...' END,
            attempted_key_prefix = CASE WHEN attempted_key_prefix IS NULL OR attempted_key_prefix = '' THEN attempted_key_prefix ELSE '...' END
        WHERE id > last_id AND id <= batch_end
          AND (COALESCE(api_key_prefix, '') NOT IN ('', '...')
               OR COALESCE(attempted_key_prefix, '') NOT IN ('', '...'));
        last_id := batch_end;
    END LOOP;
END;
$$;

-- Monitor duplication stores its response before replay. Never remask an old
-- partial value as though it were a complete credential; discard the fragment.
DO $$
DECLARE
    last_id BIGINT := 0;
    record_row RECORD;
    payload JSONB;
    safe_payload JSONB;
BEGIN
    LOOP
        SELECT id, response_body INTO record_row
        FROM idempotency_records
        WHERE id > last_id AND starts_with(scope, 'admin.channel_monitors.')
          AND response_body IS NOT NULL
        ORDER BY id LIMIT 1;
        EXIT WHEN NOT FOUND;
        last_id := record_row.id;
        BEGIN
            payload := record_row.response_body::jsonb;
        -- JSONB also rejects valid JSON containing unsupported Unicode escapes.
        EXCEPTION WHEN invalid_text_representation OR untranslatable_character THEN
            payload := NULL;
        END;
        IF jsonb_typeof(payload) IS DISTINCT FROM 'object' THEN
            safe_payload := jsonb_build_object('api_key_masked', '...');
        ELSE
            safe_payload := (payload - 'api_key' - 'api_key_masked') || jsonb_build_object('api_key_masked', '...');
            IF payload ? 'data' THEN
                IF jsonb_typeof(payload->'data') = 'object' THEN
                    safe_payload := jsonb_set(safe_payload, '{data}',
                        ((payload->'data') - 'api_key' - 'api_key_masked') || jsonb_build_object('api_key_masked', '...'));
                ELSE
                    safe_payload := jsonb_set(safe_payload, '{data}', jsonb_build_object('api_key_masked', '...'));
                END IF;
            END IF;
        END IF;
        IF safe_payload IS DISTINCT FROM payload THEN
            UPDATE idempotency_records SET response_body = safe_payload::text WHERE id = record_row.id;
        END IF;
    END LOOP;
END;
$$;
