-- 企业归属保存调用时快照，历史日志不回填；nullable ADD COLUMN 不重写大表。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS enterprise_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS enterprise_department_id BIGINT NULL;
COMMENT ON COLUMN usage_logs.enterprise_id IS '调用时企业 id；NULL 表示个人调用或历史记录';
COMMENT ON COLUMN usage_logs.enterprise_department_id IS '调用时企业部门 id 快照；NULL 表示直属企业或无企业';
