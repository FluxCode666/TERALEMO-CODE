-- 企业组织与账务。纯 SQL 为生产结构权威，Ent schema 仅用于代码生成。
CREATE TABLE IF NOT EXISTS enterprises (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    balance DECIMAL(20,8) NOT NULL DEFAULT 0,
    total_recharged DECIMAL(20,8) NOT NULL DEFAULT 0,
    low_balance_threshold DECIMAL(20,8) NULL,
    contact_name VARCHAR(100) NOT NULL DEFAULT '',
    contact_email VARCHAR(255) NOT NULL DEFAULT '',
    contact_phone VARCHAR(50) NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprises_name_active
    ON enterprises (name) WHERE deleted_at IS NULL;
COMMENT ON TABLE enterprises IS '企业计费主体；余额模式调用由企业统一承担';
COMMENT ON COLUMN enterprises.balance IS '企业 USD 余额；管理员扣减不得为负，调用结算允许透支';
COMMENT ON COLUMN enterprises.total_recharged IS '管理员累计正向充值 USD，不因扣减或调用结算减少';
COMMENT ON COLUMN enterprises.low_balance_threshold IS 'USD 低余额提醒阈值；NULL 关闭提醒';
COMMENT ON COLUMN enterprises.settings IS 'EnterpriseSettings；缺失的 JSON 键按领域默认值读取';

CREATE TABLE IF NOT EXISTS enterprise_departments (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id),
    name VARCHAR(100) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprise_departments_name_active
    ON enterprise_departments (enterprise_id, name) WHERE deleted_at IS NULL;
COMMENT ON TABLE enterprise_departments IS '企业一级部门；未删除部门在企业内名称唯一';

CREATE TABLE IF NOT EXISTS enterprise_members (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    department_id BIGINT NULL REFERENCES enterprise_departments(id),
    role VARCHAR(20) NOT NULL DEFAULT 'member',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    title VARCHAR(100) NOT NULL DEFAULT '',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprise_members_user_active
    ON enterprise_members (user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_enterprise_members_enterprise ON enterprise_members (enterprise_id);
CREATE INDEX IF NOT EXISTS idx_enterprise_members_department ON enterprise_members (department_id);
COMMENT ON TABLE enterprise_members IS '企业成员；user_id 在未删除记录内全局唯一，重新加入创建新成员行';
COMMENT ON COLUMN enterprise_members.role IS '企业角色 owner / manager / member，与系统角色独立';
COMMENT ON COLUMN enterprise_members.department_id IS 'NULL 表示直属企业；成员级 scope_id 使用本表 id';

CREATE TABLE IF NOT EXISTS enterprise_quotas (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    scope_type VARCHAR(20) NOT NULL,
    scope_id BIGINT NOT NULL,
    daily_limit_usd DECIMAL(20,10) NULL,
    weekly_limit_usd DECIMAL(20,10) NULL,
    monthly_limit_usd DECIMAL(20,10) NULL,
    total_limit_usd DECIMAL(20,10) NULL,
    daily_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    weekly_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    monthly_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    total_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    daily_window_start TIMESTAMPTZ NULL,
    weekly_window_start TIMESTAMPTZ NULL,
    monthly_window_start TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprise_quotas_scope_active
    ON enterprise_quotas (scope_type, scope_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_enterprise_quotas_enterprise ON enterprise_quotas (enterprise_id);
COMMENT ON TABLE enterprise_quotas IS '企业 / 部门 / 成员三级 USD 限额与用量；创建和软删跟随 scope 同事务进行';
COMMENT ON COLUMN enterprise_quotas.scope_type IS 'enterprise / department / member；scope_id 分别引用对应实体 id';
COMMENT ON COLUMN enterprise_quotas.daily_limit_usd IS 'NULL 不限制，0 禁用；周/月/累计限额语义相同';
COMMENT ON COLUMN enterprise_quotas.daily_window_start IS '全局时区当日零点；窗口过期由结算 SQL 惰性归零';
COMMENT ON COLUMN enterprise_quotas.weekly_window_start IS '全局时区本周一零点';
COMMENT ON COLUMN enterprise_quotas.monthly_window_start IS '全局时区自然月首日零点；累计用量不自动重置';

CREATE TABLE IF NOT EXISTS enterprise_balance_logs (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    type VARCHAR(20) NOT NULL,
    amount DECIMAL(20,8) NOT NULL,
    balance_before DECIMAL(20,8) NOT NULL,
    balance_after DECIMAL(20,8) NOT NULL,
    operator_user_id BIGINT NULL,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_enterprise_balance_logs_enterprise_created
    ON enterprise_balance_logs (enterprise_id, created_at DESC);
COMMENT ON TABLE enterprise_balance_logs IS '管理员企业余额调整流水；与企业余额更新处于同一事务';
COMMENT ON COLUMN enterprise_balance_logs.type IS 'add / subtract；amount 始终存储正数 USD';

CREATE TABLE IF NOT EXISTS enterprise_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    actor_user_id BIGINT NOT NULL,
    actor_role VARCHAR(20) NOT NULL,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL DEFAULT '',
    target_id BIGINT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_enterprise_audit_logs_enterprise_created
    ON enterprise_audit_logs (enterprise_id, created_at DESC);
COMMENT ON TABLE enterprise_audit_logs IS '企业独立审计日志；只追加，保留企业管理操作快照';
COMMENT ON COLUMN enterprise_audit_logs.actor_role IS 'admin / owner / manager';

CREATE TABLE IF NOT EXISTS enterprise_allowed_groups (
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id),
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (enterprise_id, group_id)
);
COMMENT ON TABLE enterprise_allowed_groups IS '管理员配置的企业可用分组池；空池回退成员个人规则';

CREATE TABLE IF NOT EXISTS enterprise_group_whitelists (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    scope_type VARCHAR(20) NOT NULL,
    scope_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprise_group_whitelists_scope_group
    ON enterprise_group_whitelists (scope_type, scope_id, group_id);
CREATE INDEX IF NOT EXISTS idx_enterprise_group_whitelists_enterprise ON enterprise_group_whitelists (enterprise_id);
COMMENT ON TABLE enterprise_group_whitelists IS '三级分组白名单；每一级空列表表示不限制，运行时取已配置层级交集';
