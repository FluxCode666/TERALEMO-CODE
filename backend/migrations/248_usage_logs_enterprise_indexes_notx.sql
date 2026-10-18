-- 与 226 / 233 一致：仅保留可幂等重放的并发索引 SQL，失效索引由 runner 预先清理。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_enterprise_created
    ON usage_logs (enterprise_id, created_at)
    WHERE enterprise_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_enterprise_department_created
    ON usage_logs (enterprise_department_id, created_at)
    WHERE enterprise_department_id IS NOT NULL;
