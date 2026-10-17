-- Keep configured-empty authorization scopes distinct from absent configuration.
-- Explicit PUT [] clears configuration; deleting a group must never clear it.
ALTER TABLE enterprises ADD COLUMN IF NOT EXISTS group_pool_configured BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS enterprise_group_whitelist_scopes (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    scope_type VARCHAR(20) NOT NULL,
    scope_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_enterprise_group_whitelist_scopes_scope UNIQUE (scope_type, scope_id)
);
CREATE INDEX IF NOT EXISTS idx_enterprise_group_whitelist_scopes_enterprise
    ON enterprise_group_whitelist_scopes (enterprise_id);

-- Preserve every previously configured nonempty pool/whitelist, including archive
-- rows. There is no way to infer an empty configuration from the old join rows.
UPDATE enterprises e
SET group_pool_configured = TRUE
WHERE EXISTS (SELECT 1 FROM enterprise_allowed_groups a WHERE a.enterprise_id = e.id);
INSERT INTO enterprise_group_whitelist_scopes (enterprise_id, scope_type, scope_id)
SELECT DISTINCT enterprise_id, scope_type, scope_id FROM enterprise_group_whitelists
ON CONFLICT (scope_type, scope_id) DO NOTHING;

COMMENT ON COLUMN enterprises.group_pool_configured IS 'Configured pool may be empty after group deletion: empty configured pools deny all; PUT [] cancels configuration';
COMMENT ON TABLE enterprise_group_whitelist_scopes IS 'Presence denotes a configured whitelist layer, including empty deny-all layers after group deletion';
COMMENT ON TABLE enterprise_allowed_groups IS 'Enterprise group pool rows; group_pool_configured determines whether the pool restricts members';
COMMENT ON TABLE enterprise_group_whitelists IS 'Enterprise whitelist group rows; enterprise_group_whitelist_scopes preserves configured-empty authorization layers';
