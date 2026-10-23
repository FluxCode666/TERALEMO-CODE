ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS video_billing_mode VARCHAR(20) NOT NULL DEFAULT 'video',
    ADD COLUMN IF NOT EXISTS video_price_per_request DECIMAL(20,8);
