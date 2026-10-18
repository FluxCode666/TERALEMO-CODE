-- Frozen funds belong to the payer captured at batch submission. Historical jobs
-- retain NULL snapshots and continue to settle against their personal wallet.
ALTER TABLE enterprises
    ADD COLUMN IF NOT EXISTS frozen_balance DECIMAL(20,8) NOT NULL DEFAULT 0;

ALTER TABLE batch_image_jobs
    ADD COLUMN IF NOT EXISTS enterprise_id BIGINT,
    ADD COLUMN IF NOT EXISTS enterprise_department_id BIGINT,
    ADD COLUMN IF NOT EXISTS enterprise_member_id BIGINT;
