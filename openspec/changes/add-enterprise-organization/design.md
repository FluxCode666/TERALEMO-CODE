## Context

动机见 proposal.md。本文件是后端数据层、后端管理接口、后端计费运行时、前端四路并行实现的共同契约；各路代理 MUST 以本文件的数据模型、接口与错误码为准，发现矛盾时在交付报告中指出，不要各自发明。

### 现状与切入点（已勘察，路径相对 backend/）

**分层惯例**（以 announcement 为模板）：ent schema（`ent/schema/*.go`）→ SQL 迁移（`migrations/NNN_*.sql`，纯 SQL 为权威，ent 只用来生成代码，二者 MUST 手工保持一致）→ `internal/service/xxx.go` 定义模型、错误（`infraerrors.BadRequest/NotFound/Conflict/Forbidden("CODE", msg)`）与仓储接口 → `internal/repository/xxx_repo.go`（`NewXxxRepository(client *dbent.Client) service.XxxRepository`，写操作用 `clientFromContext(ctx, r.client)` 支持事务，错误经 `translatePersistenceError`）→ `internal/service/xxx_service.go` → `internal/handler/admin/xxx_handler.go` 与 `internal/handler/xxx_handler.go` → `internal/handler/dto/xxx.go` → 路由 `internal/server/routes/{admin,user}.go` → wire（`repository/wire.go`、`service/wire.go`、`handler/wire.go` + `handler/handler.go` 的 Handlers 结构，最后 `go generate ./cmd/server` 重新生成 `cmd/server/wire_gen.go`）。事务：`tx := entClient.Tx(ctx)` → `dbent.NewTxContext(ctx, tx)`。
- 新功能 MUST 新建窄接口（`EnterpriseRepository` 等），MUST NOT 给 `UserRepository`、`AdminService`、`UsageLogRepository` 等已有大接口加方法（否则要补全仓库里大量 stub）；需要扩展已有仓储时用「新增独立接口 + 类型断言」方式接入（参考 `usage_service.go:321-330`）。
- 当前用户：`middleware.GetAuthSubjectFromContext(c)`；JWT 中间件每次从 DB 读用户与角色；管理员路由组已挂 `AdminAuthMiddleware`；POST/PUT/DELETE 自动进系统审计（`middleware/audit_log.go`）。
- 管理员建用户：`adminServiceImpl.CreateUser`（`service/admin_user.go:120`）。企业侧建号 MUST 复用该逻辑（可以把建号核心抽成可复用函数），不要走注册接口。
- 迁移：最新 245，本变更从 246 开始；`CREATE INDEX CONCURRENTLY` 必须放 `*_notx.sql`；已应用迁移不可修改。`usage_logs` 可能是分区表，给它建索引前 MUST 参考已有 usage_logs 索引迁移（如 `usage_log_effective_model_index_test.go` 对应的迁移）的写法。
- 测试：`//go:build unit` 单测；repository 集成测试 `//go:build integration` 使用 testcontainers（`repository/integration_harness_test.go`，`testEntTx(t)`、`testEntClient(t)`、`testRedis(t)`）；handler 测试用 enttest + sqlite（按 ent schema 自动建表，所以 ent schema 必须包含新增列）。

**计费链路**：
- 中间件 `server/middleware/api_key_auth.go`：用 API Key 认证快照（`service/api_key_auth_cache.go:32 APIKeyAuthUserSnapshot`，L1 15s / L2 300s，带 `Version`）判断用户状态、订阅、Key 额度；`:263` 余额模式下 `apiKeyBalanceBelowAuthThreshold(apiKey.User.Balance)` 拒绝余额 ≤ 0；`:416-435` `validateAPIKeyGroupAllowed` 对订阅分组提前放行，否则调 `User.CanBindGroup`。
- 前置检查 `BillingCacheService.CheckBillingEligibility`（`service/billing_cache_service.go:744`）：余额分支 `checkBalanceEligibility`（:1025，`GetUserBalance` Redis 优先）+ `checkUserPlatformQuotaEligibility`（:1226）+ Key 限速 + RPM。
- 在途预留 `service/billing_inflight_reservation.go:300 reserveInflight`（Redis Lua，`balance - sum(在途) >= estimate`，可按 scope 复用 `reserveInflightScope` :271）；固定费用 `ReserveFixedCharge`（billing_cache_service.go:800）。
- 结算：handler → worker 池 → `RecordUsage`（`gateway_usage_billing.go:649` / `openai_gateway_usage.go:155`）→ `recordUsageCore`（:765）→ `applyUsageBilling`（:354）→ `buildUsageBillingCommand`（:289，余额模式设 `BalanceCost`）→ `usageBillingRepository.Apply`（`repository/usage_billing_repo.go:22`，单个 PG 事务，`usage_billing_dedup` 幂等；`applyUsageBillingEffects` :174，`deductUsageBillingBalance` :244 允许透支）→ 事务后 `finalizePostUsageBilling`（:393，余额缓存同步、平台配额累加、余额提醒）→ `writeUsageLogBestEffort`（:616，`repository/usage_log_repo_insert.go`）。
- 错误映射 `handler/gateway_handler.go:2496 billingErrorDetails`；窗口重置时间 metadata `withWindowResetsMetadata`（billing_cache_service.go:1451）。
- 批量图片冻结：`service/batch_image_billing_hold.go`，提交入口 `service/batch_image_public.go:290`。
- 个人充值入口：支付 `PaymentService`（`payment_fulfillment.go`）与订单创建、兑换 `RedeemService.redeem`（`redeem_service.go:414`）、优惠码 `promo_service.go`。

**分组访问**：唯一谓词 `User.CanBindGroup`（`service/user.go:84`），调用点：`api_key_service.go:490 canUserBindGroup`、`:1094 canUserBindGroupInternal`、`GetAvailableGroups`（:1051）、网关 `middleware/api_key_auth.go:434`。分组删除清理在 `repository/group_repo.go:950`。

**使用记录查询**：过滤结构 `pkg/usagestats/usage_log_types.go:271 UsageLogFilters`（`UserID==0` 表示不过滤）；列表 `repository/usage_log_repo_query.go:99 ListWithFilters`；统计 `usage_log_repo_stats.go:658`；趋势 / 模型 / 分组 `usage_log_repo_trend.go:280/447/534`；**`shouldUsePreaggregatedTrend`（trend.go:359）在无过滤条件时读全站汇总表，企业过滤 MUST 加入该判断**；`shouldUseFastUsageLogTotal`（query.go:171）；`hydrateUsageLogAssociations`（:275）。普通用户 DTO `dto/mappers.go:685 usageLogFromServiceUser` 含 API Key 明文与用户余额，企业侧 MUST 使用新 DTO。

**当前用户接口**：`handler/auth_handler.go:424 GetCurrentUser`（`/api/v1/auth/me`）、`handler/user_handler.go:111 GetProfile`。

## Goals / Non-Goals

**Goals**
- 非企业成员行为零变化；企业相关逻辑在热路径上对非成员只增加一次「快照里 Enterprise 为空」的判断。
- 企业余额扣减与三级用量累加与现有扣费在同一事务、共享同一幂等键。
- 权限判定集中在一个 service（`EnterpriseAccessService` / 权限解析函数），handler 只负责解析参数与调用。
- 可见范围基于 `usage_logs` 写入时的企业 / 部门快照。

**Non-Goals**
- 企业在线支付充值、企业发票、企业订阅套餐。
- 多级部门、一个成员属于多个部门、一个用户属于多个企业。
- 企业成员使用批量图片（本期直接拒绝）。
- 历史 `usage_logs` 回填企业字段。
- 企业维度的并发数 / RPM 限制（个人维度的继续生效）。

## Decisions

### D1：数据模型（迁移 246 起）

```
enterprises
  id BIGSERIAL PK
  name VARCHAR(100) NOT NULL                     -- 未删除记录内唯一（部分唯一索引）
  status VARCHAR(20) NOT NULL DEFAULT 'active'   -- active | disabled
  balance DECIMAL(20,8) NOT NULL DEFAULT 0
  total_recharged DECIMAL(20,8) NOT NULL DEFAULT 0
  low_balance_threshold DECIMAL(20,8) NULL       -- NULL = 关闭提醒
  contact_name VARCHAR(100) NOT NULL DEFAULT ''
  contact_email VARCHAR(255) NOT NULL DEFAULT ''
  contact_phone VARCHAR(50) NOT NULL DEFAULT ''
  notes TEXT NOT NULL DEFAULT ''
  settings JSONB NOT NULL DEFAULT '{}'           -- EnterpriseSettings，见 D4
  created_at / updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ NULL

enterprise_departments
  id BIGSERIAL PK, enterprise_id BIGINT NOT NULL REFERENCES enterprises(id)
  name VARCHAR(100) NOT NULL                     -- (enterprise_id, name) 未删除内唯一
  sort_order INT NOT NULL DEFAULT 0
  created_at / updated_at / deleted_at

enterprise_members
  id BIGSERIAL PK, enterprise_id BIGINT NOT NULL REFERENCES enterprises(id)
  user_id BIGINT NOT NULL REFERENCES users(id)   -- 未删除记录内 user_id 全局唯一（一人一企业）
  department_id BIGINT NULL REFERENCES enterprise_departments(id)
  role VARCHAR(20) NOT NULL DEFAULT 'member'     -- owner | manager | member
  status VARCHAR(20) NOT NULL DEFAULT 'active'   -- active | disabled
  title VARCHAR(100) NOT NULL DEFAULT ''
  joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  created_at / updated_at / deleted_at           -- 移出 = 软删除；重新加入生成新行
  索引：(enterprise_id)、(department_id)

enterprise_quotas
  id BIGSERIAL PK, enterprise_id BIGINT NOT NULL
  scope_type VARCHAR(20) NOT NULL                -- enterprise | department | member
  scope_id BIGINT NOT NULL                       -- enterprises.id | enterprise_departments.id | enterprise_members.id
  daily_limit_usd / weekly_limit_usd / monthly_limit_usd / total_limit_usd DECIMAL(20,10) NULL
  daily_usage_usd / weekly_usage_usd / monthly_usage_usd / total_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0
  daily_window_start / weekly_window_start / monthly_window_start TIMESTAMPTZ NULL
  created_at / updated_at / deleted_at
  (scope_type, scope_id) 未删除内唯一；索引 (enterprise_id)
  -- 每个企业 / 部门 / 成员创建时在同一事务内创建对应限额行（无上限），删除 / 移出时软删除

enterprise_balance_logs
  id BIGSERIAL PK, enterprise_id BIGINT NOT NULL
  type VARCHAR(20) NOT NULL                      -- add | subtract
  amount DECIMAL(20,8) NOT NULL                  -- 正数
  balance_before / balance_after DECIMAL(20,8) NOT NULL
  operator_user_id BIGINT NULL, notes TEXT NOT NULL DEFAULT ''
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  索引 (enterprise_id, created_at DESC)

enterprise_audit_logs
  id BIGSERIAL PK, enterprise_id BIGINT NOT NULL
  actor_user_id BIGINT NOT NULL, actor_role VARCHAR(20) NOT NULL   -- admin | owner | manager
  action VARCHAR(64) NOT NULL                    -- 见 D9
  target_type VARCHAR(32) NOT NULL DEFAULT '', target_id BIGINT NULL
  detail JSONB NOT NULL DEFAULT '{}'
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  索引 (enterprise_id, created_at DESC)

enterprise_allowed_groups                         -- 管理员配置的可用分组池
  enterprise_id BIGINT NOT NULL REFERENCES enterprises(id)
  group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  PK (enterprise_id, group_id)

enterprise_group_whitelists                       -- 三级白名单
  id BIGSERIAL PK, enterprise_id BIGINT NOT NULL
  scope_type VARCHAR(20) NOT NULL, scope_id BIGINT NOT NULL
  group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  UNIQUE (scope_type, scope_id, group_id)；索引 (enterprise_id)

usage_logs 新增列
  enterprise_id BIGINT NULL, enterprise_department_id BIGINT NULL
  部分索引 (enterprise_id, created_at) WHERE enterprise_id IS NOT NULL
         (enterprise_department_id, created_at) WHERE enterprise_department_id IS NOT NULL
```

- 软删除（groups 删除）若不是物理删除，`ON DELETE CASCADE` 不会触发，因此 `group_repo.go:950` 的分组删除清理 MUST 同时删除两张分组表中的该分组。
- ent schema 与 SQL 同步；`usage_logs` 的新列也要加进 `ent/schema/usage_log.go`。
- 领域常量放 `internal/domain/enterprise.go`：角色、状态、scope_type、审计 action、错误码。

### D2：成员上下文（热路径共享结构）

```go
// service 包
type EnterpriseMemberContext struct {
    EnterpriseID     int64
    EnterpriseName   string
    EnterpriseStatus string   // active | disabled
    MemberID         int64    // enterprise_members.id，即 member 级 scope_id
    MemberStatus     string
    Role             string   // owner | manager | member
    DepartmentID     *int64
    // 分组访问
    GroupPoolConfigured      bool
    GroupPoolIDs             []int64
    GroupWhitelistConfigured bool
    GroupWhitelistIDs        []int64 // 已配置层级（企业/部门/成员）白名单的交集
}

func (c *EnterpriseMemberContext) Active() bool            // 企业与成员都 active
func (c *EnterpriseMemberContext) AllowsGroup(id int64) bool // (pool 未配置 || 在 pool) && (白名单未配置 || 在白名单)
```

- `service.User` 新增字段 `Enterprise *EnterpriseMemberContext`（`nil` = 非企业成员）。`User.CanBindGroup` 改为：`Enterprise != nil` 时，若 `GroupPoolConfigured` 则 `base = 在 pool 中`，否则 `base = 原个人规则`；结果为 `base && AllowsGroup`。非成员逻辑不变。
- 数据层提供 `EnterpriseRepository.GetMemberContextByUserID(ctx, userID) (*EnterpriseMemberContext, error)`（未加入企业返回 `nil, nil`）与批量版本，一次查询（或固定少量查询）组装完整上下文。
- 认证快照 `APIKeyAuthUserSnapshot` 增加企业上下文字段并提升 `Version`；`api_key_repo.go` 加载用户时附带企业上下文；面板请求（API Key 创建、可选分组）通过 `userService`/`EnterpriseRepository` 加载同一上下文。
- 失效：成员加入 / 移出 / 角色、部门、状态变更、企业状态变更、分组池或任意白名单变更后，MUST 调用现有 `InvalidateAuthCacheByUserID` 失效受影响成员（企业级变更 = 全体成员）。

### D3：企业余额与限额运行时

- **前置检查**（`CheckBillingEligibility` 余额分支）：`user.Enterprise != nil` 时：
  1. `!Active()` → `ENTERPRISE_DISABLED` / `ENTERPRISE_MEMBER_DISABLED`（中间件已拦一次，这里兜底）。
  2. 企业余额（Redis `billing:ent_balance:{enterpriseID}`，miss 时 singleflight 回源 DB，TTL 与个人余额一致）≤ 0 或低于 `MinimumBalanceReserve` → `ENTERPRISE_BALANCE_INSUFFICIENT`。
  3. 三级限额：读取成员、部门（若有）、企业三条限额（Redis hash `billing:ent_quota:{scope_type}:{scope_id}` 缓存 limits/usage/window_start，miss 时一次 DB 查询回填）；按 D3.1 窗口规则惰性归零后，任一档 `limit != nil && usage >= limit` → 对应错误。
  4. Redis 与 DB 都失败 → `ErrBillingServiceUnavailable`（fail-closed）。
  5. 个人 `user_platform_quota`、Key 限速、RPM 照常检查；**跳过个人余额检查**。
- **中间件**：企业成员跳过 `apiKeyBalanceBelowAuthThreshold(User.Balance)`；在订阅分组提前放行之前检查 `Enterprise.Active()` 与 `AllowsGroup`。
- **在途预留**：企业成员的 scope 改为 `ent:{enterpriseID}`，余额取企业余额；`ReserveFixedCharge` 同样处理。
- **结算**：`UsageBillingCommand` 新增 `EnterpriseID`、`EnterpriseDepartmentID`、`EnterpriseMemberID`、`EnterpriseBalanceCost`。企业成员余额模式下 `BalanceCost = 0`、`EnterpriseBalanceCost = actual_cost`。指纹只在 `EnterpriseID != 0` 时追加企业字段，保证升级前后非企业请求指纹不变。`applyUsageBillingEffects` 在同一事务中：
  1. `UPDATE enterprises SET balance = balance - $cost WHERE id = $id RETURNING balance`（允许透支，标记 overdraft）。
  2. 对三条限额行按 `id` 升序一条 SQL 累加四档用量，在 SQL 里用 CASE 完成窗口过期归零与 window_start 推进（参考 `incrementUsageBillingAPIKeyRateLimit`）。
  3. 加锁顺序固定为「enterprises 行 → 限额行（按 id 升序）」，避免死锁。
- **事务后**：扣减（或删除）企业余额缓存、对三条限额缓存执行「存在才累加」的 Lua（或直接删除），企业余额下穿 `low_balance_threshold` 时异步邮件通知全部启用的 owner。
- **usage_logs**：`buildRecordUsageLog` 写入 `enterprise_id`、`enterprise_department_id`（取自结算时的成员上下文快照）。
- **订阅计费**：保持原样，不扣企业余额、不计企业限额。
- **批量图片**：企业成员提交时直接返回 `ENTERPRISE_FEATURE_UNSUPPORTED`。
- legacy `postUsageBilling`（repo 为 nil 时）对企业成员走相同的企业扣费，或者明确报错，二选一并加测试。

#### D3.1 窗口规则
日：全局时区当日 0 点；周：全局时区本周一 0 点；月：全局时区本月 1 日 0 点（自然月，不同于 user_platform_quota 的 30 天滚动）；累计：不重置。`window_start` 早于当前窗口起点即视为过期，用量按 0 计算，累加时归零后再加。复用 `pkg/timezone` 的 StartOfDay/StartOfWeek，若无 StartOfMonth 则补充。

### D4：企业设置（`enterprises.settings` JSONB）

```go
type EnterpriseSettings struct {
    BalanceVisibleToManager         bool `json:"balance_visible_to_manager"`          // 默认 true
    BalanceVisibleToMember          bool `json:"balance_visible_to_member"`           // 默认 false
    MemberQuotaVisibleToMember      bool `json:"member_quota_visible_to_member"`      // 默认 true
    MemberQuotaVisibleToManager     bool `json:"member_quota_visible_to_manager"`     // 默认 true
    DepartmentQuotaVisibleToManager bool `json:"department_quota_visible_to_manager"` // 默认 true
    ManagerCanManageMembers         bool `json:"manager_can_manage_members"`          // 默认 false
    ManagerCanEditMemberQuota       bool `json:"manager_can_edit_member_quota"`       // 默认 false
    ManagerCanEditMemberGroups      bool `json:"manager_can_edit_member_groups"`      // 默认 false
}
```
JSON 中缺失的键 MUST 取上面的默认值（反序列化前先填默认值），因此用 `bool` 而不是 `*bool` 时需要自定义 `UnmarshalJSON` 或先用默认结构体再 `json.Unmarshal` 覆盖。

### D5：权限矩阵（企业侧接口）

| 操作 | owner | manager | member |
|---|---|---|---|
| 查看概览（企业名、本人角色 / 部门） | ✓ | ✓ | ✓ |
| 查看企业余额 | ✓ | `balance_visible_to_manager` | `balance_visible_to_member` |
| 部门列表（id、名称、人数） | ✓ | ✓ | ✓ |
| 部门增删改 | ✓ | ✗ | ✗ |
| 成员列表 | 全企业 | 本部门 | ✗（403） |
| 新建员工账号 | 任意部门、任意角色 | `manager_can_manage_members`：仅本部门、仅 member | ✗ |
| 修改成员（部门 / 角色 / 状态 / 职位） | ✓ | `manager_can_manage_members`：仅本部门 member 的状态与职位 | ✗ |
| 移出成员 | ✓（最后一个 owner 除外） | `manager_can_manage_members`：仅本部门 member | ✗ |
| 查看企业级限额 | ✓ | ✗ | ✗ |
| 查看部门限额 | ✓ | `department_quota_visible_to_manager`（仅本部门） | ✗ |
| 查看成员限额 | ✓ | `member_quota_visible_to_manager`（本部门） | `member_quota_visible_to_member`（仅本人） |
| 修改企业 / 部门限额 | ✓ | ✗ | ✗ |
| 修改成员限额 | ✓ | `manager_can_edit_member_quota`（本部门 member，≤ 部门上限） | ✗ |
| 重置限额用量 | ✓ | ✗ | ✗ |
| 查看分组池 / 可选分组 | ✓ | ✓ | ✓（仅本人有效可用分组） |
| 修改企业级 / 部门级白名单 | ✓ | ✗ | ✗ |
| 修改成员级白名单 | ✓ | `manager_can_edit_member_groups`（本部门 member） | ✗ |
| 修改企业设置 | ✓ | ✗ | ✗ |
| 使用记录 / 统计 | 全企业 | 本部门 | 本人 |
| 审计日志 | ✓ | ✗ | ✗ |

- 企业 `disabled` 时企业侧所有写操作返回 403 `ENTERPRISE_DISABLED`；成员 `disabled` 时企业侧所有接口返回 403 `ENTERPRISE_MEMBER_DISABLED`。
- 非企业成员访问 `/api/v1/enterprise/*` 返回 403 `ENTERPRISE_NOT_MEMBER`。
- 系统管理员通过 `/api/v1/admin/enterprises/*` 拥有全部权限（含余额调整与分组池）。
- 权限判定实现为纯函数（输入：调用者成员上下文 + 企业设置 + 目标对象），便于表驱动单测。

### D6：API 契约

统一响应包裹与分页沿用现有 `response.Success` / `response.Paginated`（`{items,total,page,page_size,pages}`）。金额为 number（USD）。限额字段 `null` 表示不限。

**通用对象**
```jsonc
// Enterprise（管理端）
{ "id":1, "name":"Acme", "status":"active", "balance":120.5, "total_recharged":500,
  "low_balance_threshold":null, "contact_name":"", "contact_email":"", "contact_phone":"",
  "notes":"", "settings":{...EnterpriseSettings}, "member_count":12, "department_count":3,
  "created_at":"...", "updated_at":"..." }
// Department
{ "id":3, "enterprise_id":1, "name":"研发部", "sort_order":0, "member_count":5, "created_at":"..." }
// Member
{ "id":7, "enterprise_id":1, "user_id":42, "email":"a@x.com", "username":"alice",
  "department_id":3, "department_name":"研发部", "role":"manager", "status":"active",
  "title":"", "joined_at":"...", "user_status":"active" }
// Quota（scope_type: enterprise|department|member；scope_id 对应 enterprise.id / department.id / member.id）
{ "scope_type":"member", "scope_id":7,
  "daily_limit_usd":10, "weekly_limit_usd":null, "monthly_limit_usd":200, "total_limit_usd":null,
  "daily_usage_usd":1.2, "weekly_usage_usd":3.4, "monthly_usage_usd":20.1, "total_usage_usd":88.0,
  "daily_resets_at":"...", "weekly_resets_at":"...", "monthly_resets_at":"..." }
// BalanceLog
{ "id":1, "type":"add", "amount":100, "balance_before":20, "balance_after":120,
  "operator_user_id":1, "operator_email":"admin@x.com", "notes":"", "created_at":"..." }
// AuditLog
{ "id":1, "actor_user_id":42, "actor_email":"a@x.com", "actor_role":"owner", "action":"member.create",
  "target_type":"member", "target_id":7, "detail":{...}, "created_at":"..." }
// GroupWhitelist（每个 scope 一条）
{ "scope_type":"department", "scope_id":3, "group_ids":[1,2] }
// 分组简要
{ "id":1, "name":"Claude", "platform":"anthropic", "is_exclusive":false, "subscription_type":"standard", "status":"active" }
// EnterpriseUsageLog（企业侧脱敏记录）
{ "id":1, "user":{"id":42,"username":"alice","email":"a@x.com"}, "department_id":3,
  "api_key":{"id":9,"name":"dev"}, "group":{"id":1,"name":"Claude"}, "model":"claude-x",
  "input_tokens":0, "output_tokens":0, "cache_creation_tokens":0, "cache_read_tokens":0,
  "actual_cost":0.01, "billing_type":0, "request_type":0, "stream":true,
  "duration_ms":1000, "first_token_ms":200, "created_at":"..." }
```

**管理端 `/api/v1/admin/enterprises`**
| 方法 路径 | 说明 |
|---|---|
| `GET /` | 列表：`page,page_size,search,status` |
| `POST /` | 创建：`{name,status?,notes?,contact_*?,low_balance_threshold?,settings?,owner?}`，`owner` 为 `{user_id}` 或 `{email,password,username?}`，可选 |
| `GET /:id` | 详情（含 settings 与企业级 quota） |
| `PUT /:id` | 更新基础信息 / 状态 / settings / low_balance_threshold（指针语义，未传不改） |
| `DELETE /:id` | 软删除，仅无成员时允许 |
| `POST /:id/balance` | `{operation:"add"|"subtract", amount, notes?}` → 返回企业 |
| `GET /:id/balance-logs` | 分页 |
| `GET/POST /:id/departments`，`PUT/DELETE /:id/departments/:departmentId` | 部门 |
| `GET /:id/members` | 分页：`search,department_id,role,status` |
| `POST /:id/members` | `{user_id}` 或 `{email,password,username?}`，加 `{department_id?,role,title?}` |
| `PUT /:id/members/:memberId` | `{department_id?(传 0 表示移出部门),role?,status?,title?}` |
| `DELETE /:id/members/:memberId` | 移出企业 |
| `GET /:id/quotas` | 全部限额：`scope_type?` 过滤 |
| `PUT /:id/quotas/:scopeType/:scopeId` | `{daily_limit_usd,weekly_limit_usd,monthly_limit_usd,total_limit_usd}`（全量，null=不限） |
| `POST /:id/quotas/:scopeType/:scopeId/reset` | `{windows:["daily"|"weekly"|"monthly"|"total"]}` |
| `GET/PUT /:id/allowed-groups` | 分组池 `{group_ids:[...]}`，GET 返回分组简要数组 |
| `GET /:id/group-whitelists` | 全部白名单数组 |
| `PUT /:id/group-whitelists/:scopeType/:scopeId` | `{group_ids:[...]}`，空数组 = 删除该级白名单 |
| `GET /:id/usage` | 使用记录分页：`page,page_size,start_date,end_date,user_id,department_id,model,api_key_id` |
| `GET /:id/usage/stats` | 统计（同过滤） |
| `GET /:id/usage/trend` | `granularity=day|hour` + 同过滤 |
| `GET /:id/usage/models` | 模型分布 |
| `GET /:id/usage/members` | 按成员汇总排行：`[{user_id,username,email,department_id,department_name,requests,total_tokens,actual_cost}]` |
| `GET /:id/usage/departments` | 按部门汇总：`[{department_id,department_name,requests,total_tokens,actual_cost}]` |
| `GET /:id/audit-logs` | 分页：`action?` |

管理员用户列表（`GET /admin/users`）新增可选过滤 `enterprise_id`，并在返回项中附带 `enterprise:{id,name,role}|null`（P2）。

**企业侧 `/api/v1/enterprise`**（JWT 认证 + 企业成员中间件）
| 方法 路径 | 说明 |
|---|---|
| `GET /overview` | 见下方结构 |
| `GET /departments` | 全部角色可用 |
| `POST /departments`，`PUT/DELETE /departments/:departmentId` | owner |
| `GET /members` | owner 全部 / manager 本部门；过滤 `search,department_id,role,status`，分页 |
| `POST /members` | 新建员工 `{email,password,username?,department_id?,role,title?}` |
| `PUT /members/:memberId`，`DELETE /members/:memberId` | 按 D5 |
| `GET /quotas` | 返回调用者可见的限额（按 D5 过滤） |
| `PUT /quotas/:scopeType/:scopeId`，`POST /quotas/:scopeType/:scopeId/reset` | 按 D5 |
| `GET /groups` | 可选分组：owner/manager 返回分组池（池为空时返回全部 active 公开分组）；member 返回本人有效可用分组 |
| `GET /group-whitelists`，`PUT /group-whitelists/:scopeType/:scopeId` | 按 D5（manager 只能读写本部门成员级） |
| `GET/PUT /settings` | owner；GET 对 manager/member 返回 403 |
| `GET /usage`，`/usage/stats`，`/usage/trend`，`/usage/models`，`/usage/members`，`/usage/departments` | 同管理端，范围按角色收敛；`/usage/departments` 仅 owner |
| `GET /audit-logs` | owner |

`GET /enterprise/overview` 响应：
```jsonc
{
  "enterprise": { "id":1, "name":"Acme", "status":"active" },
  "me": { "member_id":7, "user_id":42, "role":"manager", "status":"active",
          "department_id":3, "department_name":"研发部", "title":"" },
  "balance_visible": true, "balance": 120.5,          // 不可见时 balance=null
  "enterprise_quota": {...Quota} | null,               // 仅 owner
  "department_quota_visible": true, "department_quota": {...Quota} | null,
  "member_quota_visible": true, "member_quota": {...Quota} | null,  // 本人
  "settings": {...EnterpriseSettings} | null,          // 仅 owner
  "permissions": {
    "manage_departments": false, "manage_members": false, "create_members": false,
    "edit_member_quota": false, "edit_member_groups": false, "edit_quotas": false,
    "edit_settings": false, "view_members": true, "view_audit_logs": false,
    "view_department_usage": false
  },
  "stats": { "today_cost": 1.2, "month_cost": 30.5 }  // 按调用者可见范围
}
```

**当前用户**：`GET /api/v1/auth/me` 与 `GET /api/v1/user/profile` 的用户对象新增
```jsonc
"enterprise": { "id":1, "name":"Acme", "role":"member", "department_id":3, "department_name":"研发部",
                "member_status":"active", "enterprise_status":"active",
                "balance_visible": false, "balance": null } | null
```

### D7：错误码

| 码 | HTTP | 场景 |
|---|---|---|
| `ENTERPRISE_NOT_FOUND` | 404 | |
| `ENTERPRISE_DEPARTMENT_NOT_FOUND` / `ENTERPRISE_MEMBER_NOT_FOUND` | 404 | |
| `ENTERPRISE_NAME_EXISTS` / `ENTERPRISE_DEPARTMENT_NAME_EXISTS` | 409 | |
| `ENTERPRISE_MEMBER_EXISTS` | 409 | 用户已属于某企业 |
| `ENTERPRISE_MEMBER_ADMIN_FORBIDDEN` | 400 | 加入系统管理员 |
| `ENTERPRISE_MANAGER_REQUIRES_DEPARTMENT` | 400 | |
| `ENTERPRISE_LAST_OWNER` | 409 | |
| `ENTERPRISE_DEPARTMENT_NOT_EMPTY` / `ENTERPRISE_NOT_EMPTY` | 409 | |
| `ENTERPRISE_NOT_MEMBER` | 403 | 非成员访问企业侧接口 |
| `ENTERPRISE_PERMISSION_DENIED` | 403 | |
| `ENTERPRISE_DISABLED` / `ENTERPRISE_MEMBER_DISABLED` | 403 | 面板与网关共用 |
| `ENTERPRISE_BALANCE_INSUFFICIENT` | 403 | 网关；管理端 subtract 超额用 400 同码 |
| `ENTERPRISE_QUOTA_EXCEEDED` / `DEPARTMENT_QUOTA_EXCEEDED` / `MEMBER_QUOTA_EXCEEDED` | 429 | metadata：`scope`,`window`,`window_resets_at` |
| `ENTERPRISE_QUOTA_EXCEEDS_PARENT` | 400 | |
| `ENTERPRISE_GROUP_OUT_OF_SCOPE` | 400 | 白名单越界 |
| `ENTERPRISE_GROUP_NOT_ALLOWED` | 403 | 网关 |
| `ENTERPRISE_MEMBER_WALLET_DISABLED` | 403 | 个人支付 / 余额兑换 |
| `ENTERPRISE_FEATURE_UNSUPPORTED` | 403 | 批量图片等 |
| `ENTERPRISE_INVALID_SCOPE` | 400 | scope_type 非法或 scope 不属于该企业 |

### D8：使用记录查询

- `UsageLogFilters` 新增 `EnterpriseID int64`、`EnterpriseDepartmentID int64`；所有 repository 查询（列表、统计、趋势、模型、分组）支持这两个过滤；`shouldUsePreaggregatedTrend` 与 `shouldUseFastUsageLogTotal` 把它们视为有效过滤。
- 企业侧 handler 先由权限服务得出强制范围（owner → EnterpriseID；manager → EnterpriseID + EnterpriseDepartmentID；member → EnterpriseID + UserID），再与请求参数合并：请求参数只能收窄，不能放宽；`member` 传入他人 `user_id` → 403。
- 成员 / 部门排行用新的 repository 方法按 `user_id` / `enterprise_department_id` 分组聚合，带强制范围与时间范围（默认最近 30 天，最大 366 天）。
- 列表必须返回 `EnterpriseUsageLog` DTO。

### D9：审计 action
`enterprise.create|update|delete`、`balance.adjust`、`department.create|update|delete`、`member.add|create|update|remove`、`quota.update|reset`、`settings.update`、`group_pool.update`、`group_whitelist.update`。`detail` 记录修改前后的关键字段。

### D10：前端
- 管理端：`/admin/enterprises` 列表页 + `/admin/enterprises/:id` 详情页（Tab：概览 / 部门 / 成员 / 限额 / 分组 / 使用记录 / 余额流水 / 审计 / 设置）；菜单加在 `AppSidebar.vue adminNavItems`。
- 用户侧：`/enterprise` 页面（Tab 按 `overview.permissions` 显隐：概览 / 部门 / 成员 / 限额 / 分组 / 使用记录 / 设置 / 审计）；菜单项在 `buildSelfNavItems()` 中，`featureFlag: () => !!authStore.user?.enterprise`。
- 企业成员（`user.enterprise != null`）：顶栏与仪表盘隐藏个人余额，按 `enterprise.balance_visible` 改为显示企业余额；隐藏充值 / 购买 / 兑换余额入口（路由守卫与菜单都要处理）。
- 复用 `UsageTable` / `UsageStatsCards` 时传入适配后的数据或新写轻量表格，不要让企业侧 DTO 缺字段导致组件报错。
- i18n：`locales/{zh,en}/admin/enterprises.ts`、`locales/{zh,en}/enterprise.ts`、`nav.*`；加入 `localesNoKeyCollision.spec.ts`。
- API：`src/api/admin/enterprises.ts`（注册到 `adminAPI`）、`src/api/enterprise.ts`；类型 `src/types/enterprise.ts`，`User` 增加 `enterprise` 字段。

### D11：实现分工（防止并行冲突）

| 工作流 | 负责范围 | 工作区 |
|---|---|---|
| W1 后端数据层 | D1 全部、D2 的 `EnterpriseMemberContext` 结构与仓储方法、领域常量与错误、仓储集成测试 | 主工作区 |
| W2 后端管理接口 | 企业 / 部门 / 成员 / 限额配置 / 分组池与白名单 / 设置 / 余额调整 / 审计的 service 与权限函数、管理端与企业侧 handler 与路由、D8 使用记录查询、`/auth/me` 企业字段、个人钱包入口拦截、分组删除清理、管理员用户列表企业过滤、缓存失效调用 | 主工作区（W1 完成后） |
| W3 后端计费运行时 | D2 认证快照与 `CanBindGroup`/`GetAvailableGroups`、中间件、D3 全部、`usage_logs` 写入、批量图片拦截、错误映射、余额提醒、`EnterpriseBillingCache` 实现 | 独立 worktree（W1 完成后） |
| W4 前端 | D10 全部 | 主工作区，只改 `frontend/` |

W2 与 W3 的接口边界：W1 在 service 包定义
```go
type EnterpriseBillingCache interface {
    InvalidateEnterpriseBalance(ctx context.Context, enterpriseID int64) error
    InvalidateEnterpriseQuota(ctx context.Context, scopeType string, scopeID int64) error
}
```
并提供 no-op 实现注入 wire；W2 在余额调整、限额修改 / 重置后调用；W3 用 Redis 实现替换 no-op。认证缓存失效统一用现有 `InvalidateAuthCacheByUserID`。

## Risks / Trade-offs

- **企业行热点**：同一企业所有成员的结算都要更新 `enterprises` 行与企业级限额行，高并发大企业可能出现行锁竞争。结算在 worker 池异步执行、事务很短，第一版可接受；后续可改为 Redis 累计 + 批量落库。
- **限额超支**：前置检查按 `usage >= limit` 判断，不计本次预估，超支幅度取决于并发在途请求数，与现有平台配额一致。
- **快照延迟**：认证快照 L1 15s / L2 300s；变更时主动失效，失效失败时最长 5 分钟内生效。
- **历史数据**：上线前的 `usage_logs` 没有企业字段，企业视图看不到加入企业前或上线前的记录（预期行为）。
- **自然月与 30 天滚动不一致**：企业月度限额按自然月，与个人平台配额不同，需要在 UI 文案中注明。
