ALTER TABLE enterprise_allowed_groups
    ADD COLUMN IF NOT EXISTS rate_multiplier DECIMAL(10,4) NULL;

COMMENT ON COLUMN enterprise_allowed_groups.rate_multiplier IS 'Enterprise billing rate override; NULL falls back to the user or group rate';
