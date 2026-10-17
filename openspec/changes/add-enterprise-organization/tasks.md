## W1 后端数据层（主工作区）

- [x] 1.1 `internal/domain/enterprise.go`：角色、状态、scope_type、审计 action 常量；`EnterpriseSettings` 及默认值与容错反序列化；错误码常量（D7）
- [x] 1.2 ent schema：`enterprise.go`、`enterprise_department.go`、`enterprise_member.go`、`enterprise_quota.go`、`enterprise_balance_log.go`、`enterprise_audit_log.go`、`enterprise_allowed_group.go`、`enterprise_group_whitelist.go`；`usage_log.go` 增加 `enterprise_id`、`enterprise_department_id`；`go generate ./ent`
- [x] 1.3 迁移 `246_add_enterprises.sql`（全部新表、部分唯一索引、外键、COMMENT）、`247_usage_logs_enterprise_columns.sql`（ADD COLUMN IF NOT EXISTS）、`248_usage_logs_enterprise_indexes_notx.sql`（参照已有 usage_logs 索引迁移处理分区表）；迁移内容测试
- [x] 1.4 service 包：模型（`Enterprise`、`EnterpriseDepartment`、`EnterpriseMember`、`EnterpriseQuota`、`EnterpriseBalanceLog`、`EnterpriseAuditLog`、`EnterpriseGroupWhitelist`）、`EnterpriseMemberContext`（`Active`、`AllowsGroup`）、仓储窄接口、`EnterpriseBillingCache` 接口与 no-op 实现
- [x] 1.5 repository 实现：企业 CRUD 与列表（成员数、部门数）、部门 CRUD（人数）、成员 CRUD / 列表过滤 / 按 user_id 唯一性、限额读写 / 重置 / 按 scope 批量读取、余额原子调整 + 流水（同事务）、审计写入与分页、分组池与白名单读写、`GetMemberContextByUserID` 与批量版本、按 enterprise 列出全部成员 user_id（供缓存失效）
- [x] 1.6 repository 集成测试（testcontainers）：唯一约束（一人一企业、部门重名）、软删除后可重新加入、余额调整不为负且流水正确、成员上下文组装（池 / 白名单交集、无部门、停用状态）
- [x] 1.7 wire 注册（repository、no-op cache），`go generate ./cmd/server`，`go build ./...` 与 `go test -tags=unit ./...` 通过

## W2 后端管理接口（主工作区，W1 之后）

- [ ] 2.1 权限纯函数（D5）+ 表驱动单测，覆盖三种角色 × 设置开关 × 目标对象（本部门 / 他部门 / 本人 / owner）
- [ ] 2.2 `EnterpriseService`：企业 CRUD（可选初始 owner）、部门 CRUD（非空不可删）、成员加入已有用户 / 新建用户（复用管理员建号核心逻辑，同事务创建成员与限额行）、成员更新（最后一个 owner 保护、manager 需部门）、移出（软删除成员与限额行、清理成员级白名单）、限额更新（≤ 上级）与重置、分组池与三级白名单（子集校验）、设置、余额调整、审计；所有变更后调用认证缓存失效与 `EnterpriseBillingCache`
- [ ] 2.3 管理端 handler + 路由 `/api/v1/admin/enterprises/...`（D6），余额调整走幂等封装（参照 `UpdateBalance`）
- [ ] 2.4 企业侧成员中间件（加载成员上下文，非成员 403，停用 403）+ handler + 路由 `/api/v1/enterprise/...`（D6），含 `overview`
- [ ] 2.5 使用记录查询（D8）：`UsageLogFilters` 新字段贯穿列表 / 统计 / 趋势 / 模型 / 分组；修正 `shouldUsePreaggregatedTrend`、`shouldUseFastUsageLogTotal`；成员 / 部门排行仓储方法；`EnterpriseUsageLog` DTO；强制范围合并与越权 403
- [ ] 2.6 `/auth/me` 与 `/user/profile` 返回 `enterprise` 对象（按可见性填 balance）
- [ ] 2.7 个人钱包拦截：支付订单创建、余额类兑换码、余额类优惠码对企业成员返回 `ENTERPRISE_MEMBER_WALLET_DISABLED`
- [ ] 2.8 分组删除时清理分组池与白名单；管理员用户列表 `enterprise_id` 过滤与企业摘要
- [ ] 2.9 测试：service 单测（stub 窄接口）、handler 测试（权限越权、可见性开关、范围收敛不泄露、预聚合不被误用）、使用记录仓储集成测试（企业 / 部门过滤、排行）

## W3 后端计费运行时（独立 worktree，W1 之后）

- [ ] 3.1 `service.User.Enterprise`；`CanBindGroup` 企业逻辑；`APIKeyService` 绑定与 `GetAvailableGroups` 过滤（订阅分组也要过 `AllowsGroup`）；面板侧加载用户时附带成员上下文
- [ ] 3.2 认证快照加入企业上下文并提升 Version；`api_key_repo` 加载
- [ ] 3.3 中间件：企业 / 成员停用、`ENTERPRISE_GROUP_NOT_ALLOWED`（先于订阅提前放行）、企业成员跳过个人余额阈值
- [ ] 3.4 `EnterpriseBillingCache` Redis 实现：企业余额缓存（读、存在才扣减、失效）、三级限额 hash（读、存在才累加、失效）
- [ ] 3.5 `CheckBillingEligibility` 企业分支（D3 前置检查，fail-closed）；`reserveInflight` 与 `ReserveFixedCharge` 企业 scope
- [ ] 3.6 结算：`UsageBillingCommand` 企业字段与指纹兼容；`buildUsageBillingCommand`；`Apply` 事务内扣企业余额 + 三级用量累加（SQL 窗口归零，固定加锁顺序）；`UsageBillingApplyResult.NewEnterpriseBalance`；legacy 路径
- [ ] 3.7 事务后：企业余额缓存同步、限额缓存同步、企业余额下穿阈值邮件通知 owner
- [ ] 3.8 `usage_logs` 写入 `enterprise_id`、`enterprise_department_id`（insert SQL 列清单、`buildRecordUsageLog`、OpenAI 侧）
- [ ] 3.9 批量图片对企业成员返回 `ENTERPRISE_FEATURE_UNSUPPORTED`
- [ ] 3.10 `billingErrorDetails` 新错误映射（429 + Retry-After + metadata；403）
- [ ] 3.11 测试：`CanBindGroup` 表驱动；中间件；前置检查各分支（余额、三级限额、窗口过期归零、缓存失败兜底、非成员零变化）；`Apply` 集成测试（扣企业余额不动个人余额、三级累加、幂等重放、窗口归零、透支标记）；指纹对非企业请求不变

## W4 前端（主工作区，只改 frontend/）

- [x] 4.1 类型 `src/types/enterprise.ts`；`User.enterprise`；API `src/api/admin/enterprises.ts`（注册 `adminAPI`）、`src/api/enterprise.ts`；API 单测
- [x] 4.2 管理端：企业列表页（创建 / 编辑 / 停用 / 删除 / 余额调整）与详情页（概览、部门、成员、限额、分组池与白名单、使用记录、余额流水、审计、设置）
- [x] 4.3 用户侧 `/enterprise` 页面：按 `overview.permissions` 显示 Tab；成员新建 / 编辑 / 移出；限额查看与编辑；白名单编辑；使用记录（范围内过滤、成员 / 部门排行、趋势）；设置；审计
- [x] 4.4 企业成员钱包适配：顶栏 / 仪表盘余额改为企业余额（按可见性）、隐藏充值 / 购买 / 余额兑换入口、路由守卫
- [x] 4.5 路由、侧边栏菜单（管理端「企业管理」、用户侧「我的企业」）
- [x] 4.6 i18n（zh/en 完全对齐，加入防冲突测试）
- [x] 4.7 组件 / 页面测试（权限 Tab 显隐、可见性 null 处理、表单校验），`pnpm typecheck`、`pnpm lint:check`、`pnpm test:run`、`pnpm check:i18n` 通过

## 5. 集成与验证

- [ ] 5.1 合并 W3 分支，重新生成 wire，`go build ./...`
- [ ] 5.2 `go test -tags=unit ./...`、`go test -tags=integration ./internal/repository/...`、`golangci-lint run ./...`
- [ ] 5.3 前端全量检查与构建
- [ ] 5.4 独立审查：权限与可见性、计费正确性与并发、前端；修复审查发现
- [ ] 5.5 端到端冒烟：创建企业 → 充值 → 建部门与员工 → 配置限额与白名单 → 员工调用扣企业余额 → 超限 429 → 各角色查看使用记录范围正确
