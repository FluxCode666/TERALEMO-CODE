-- Independent per-second video rates. Existing 720p prices are not copied.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS video_price_768p DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS video_price_2k DECIMAL(20,8);

COMMENT ON COLUMN groups.video_price_768p IS '768p 视频生成每秒单价 (USD/s)，NULL 表示未配置';
COMMENT ON COLUMN groups.video_price_2k IS '2K 视频生成每秒单价 (USD/s)，NULL 表示未配置';
COMMENT ON COLUMN usage_logs.video_resolution IS '计费用视频分辨率 480p/720p/768p/1080p/2k';
