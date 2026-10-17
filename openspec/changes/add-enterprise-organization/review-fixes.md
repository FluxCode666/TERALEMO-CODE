# 设计审查修复决议（2026-10-17）

来源：codex 只读设计审查（基准 b43adc76c，未包含 W2/W3 实现）。下面每条给出**决议**与**归属**。修复代理必须先在当前代码（已合并 W2+W3）上核实该问题是否仍存在：已被 W2/W3 解决的，在报告中写明证据并跳过；仍存在的，按决议修复并补测试。

归属：**FA** = 修复代理 A（计费运行时 / 网关 / 分组运行时，原 W3 代理）；**FB** = 修复代理 B（管理接口 / 数据层 / 查询 / 钱包入口 / 前端微调，原 W1+W2 代理）。

## 致命

### R1 WebSocket 后续轮次绕过企业准入、保留建连时付款身份［FA］
证据：`handler/openai_gateway_handler.go` 约 2672（仅建连检查）、2693（每轮检查正常模式直接返回）、2192 `refreshOpenAIWSTurnBillingAPIKey` 只替换 Group 保留旧 User、3170 每轮结算用旧用户。
决议：每轮（BeforeTurn）刷新完整认证快照中的用户与企业上下文。付款主体（个人 ↔ 企业、企业 ID、成员 ID）或部门与建连时不同 → 以明确错误关闭连接，要求重连。付款主体未变时，企业成员每轮重新检查：企业 / 成员状态、分组授权、企业余额、三级限额（只做检查，不重复计 RPM）。每轮结算使用本轮冻结的身份。Grok realtime、OpenAI Live 同样规则：Live 建连时冻结身份写入 LiveCallIdentity/LiveCallRecord（含 Redis 序列化），无法每轮复核的持续会话至少在建连时完成企业准入。

### R2 Grok / Seedance 异步视频按轮询时身份付款，创建返回即释放预留［FA］
决议：本期**拒绝**企业成员提交异步视频任务（创建入口返回 403 `ENTERPRISE_FEATURE_UNSUPPORTED`），与批量图片一致；入企前已创建的个人任务保持原个人结算。列入后续：持久化创建时付款主体并在终态结算。

### R3 无分组 API Key 绕过企业分组池和白名单［FA］
决议：企业成员必须绑定分组。创建 / 更新 Key 时企业成员不带分组 → 400（复用绑定失败错误或 `ENTERPRISE_GROUP_NOT_ALLOWED`）；网关认证对企业成员的无分组 Key（含入企前创建的）→ 403 `ENTERPRISE_GROUP_NOT_ALLOWED`。

### R4 Claude Code fallback 与 PromptTooLong fallback 切换分组后不校验企业授权［FA］
证据：`service/gateway_scheduling.go` 约 922、`handler/gateway_handler.go` 约 1015。
决议：抽一个统一函数（例如 `EnterpriseAllowsGroupForUser(user, groupID)` 或直接 `user.Enterprise.AllowsGroup`）在每次 fallback 跳转前校验；目标分组不被允许 → 不做 fallback，返回原错误。计费与日志的分组归属保持现有 fallback 语义不变。

## 重要

### R5 Google / Gemini 独立认证中间件仍按个人余额拒绝［FA］
证据：`server/middleware/api_key_auth_google.go` 约 203；挂载 `routes/gateway.go` 约 343、516。
决议：与主认证共享企业准入（停用、分组、跳过个人余额、无分组 Key），保留 Google 错误格式。

### R6 usage_logs 多个独立构造入口［FA］
证据：`service/openai_gateway_usage.go` 约 384、`service/openai_live.go` 约 835、`service/batch_image_settlement.go` 约 261、`service/usage_service.go` 约 96；`repository/usage_log_repo_insert.go` 单条 / 批量 CTE / best-effort 批量 / 同步兜底 / 参数类型表 / `prepareUsageLogInsert`。
决议：建立统一的「付款身份 → 日志企业字段」填充函数，覆盖全部生产 writer 与全部插入 SQL 形态；batch image 历史个人任务写 NULL（个人归属）。

### R7 成员移出 / 部门删除后在途请求结算缺少限额行［FA 运行时 + FB 仓储］
决议：结算使用请求时冻结的身份（enterprise_id、department_id、member_id）。`Apply` 内按 scope 更新限额行时**不过滤 deleted_at**（软删除的归档行照常累加）；企业行同样按 id 扣减不看 deleted_at。若仍出现限额行完全缺失：照常扣企业余额并写日志、累加能找到的行，`slog.Error` 记录缺失，**禁止因此漏扣企业余额**。前置检查（新请求准入）仍对缺行 fail-closed。FB 确保软删除不物理删除限额行。

### R8 认证缓存失效与并发回填竞态［FA］
决议：不新建代际基础设施。企业成员关系相关的失效统一走一个 helper：立即失效一次，并在约 2 秒后（可配置常量）再失效一次（延迟双删），覆盖「读旧 → 提交 → 失效 → 旧值回填」窗口。FB 的所有调用点改用这个 helper（FA 提供，FB 调用；若 FA 尚未提供，FB 先在 service 包实现 `InvalidateEnterpriseMembersAuthCache(ctx, userIDs)` 并由 FA 复用，二者只保留一个实现）。在 design.md 风险一节记录。

### R9 删除最后一个允许分组使「受限」变「不限」［FB］
决议：持久区分「未配置」与「已配置（可能为空 = 全部拒绝）」。新增迁移 249：`enterprises.group_pool_configured BOOLEAN NOT NULL DEFAULT FALSE`；新表 `enterprise_group_whitelist_scopes(enterprise_id, scope_type, scope_id, created_at, PRIMARY KEY(scope_type, scope_id))`，存在即表示该层已配置。语义：
- 管理端 / 企业侧 PUT `{group_ids:[...]}` 非空 → 配置；**空数组 → 显式取消该层配置**（API 不变，前端不用改）。
- 删除分组只删关联行，不动配置标志 → 若因此变空，该层仍为「已配置」，拒绝全部。
- `GetMemberContext*` 的 `GroupPoolConfigured` / `GroupWhitelistConfigured` 改为读标志，不再由行数推断。
同步更新 ent schema、数据层测试与 W2 测试。

### R10 分组删除清理与白名单 / 分组池重写的锁环［FB］
决议：分组池与白名单重写在删除旧关联行之前，先按 id 升序对「旧 ∪ 新」分组 `SELECT ... FROM groups WHERE id = ANY($1) ORDER BY id FOR KEY SHARE`。分组删除路径保持先锁 groups。补一个并发集成测试（删分组 vs 重写白名单）证明不死锁。

### R11 个人钱包入账遗漏［FB］
决议：
- affiliate 返利转余额（`TransferAffiliateQuota`，`repository/affiliate_repo.go` 约 289）→ 企业成员拒绝 `ENTERPRISE_MEMBER_WALLET_DISABLED`，返利额度保留不动。
- OAuth / 邮箱首绑赠金（`service/auth_oauth_first_bind.go` 约 80）→ 企业成员跳过赠金，绑定照常成功。
- 管理员调整个人余额（`admin_user.go` 约 516）→ 允许（管理员显式操作，钱包冻结、退出后可用），但跳过充值返利；不改接口。
- 企业侧与管理员为企业新建账号 → 显式个人余额 0，不发全局默认赠金。

### R12 历史支付履约、批量冻结任务、退款补偿［FB 查验 + FA 查验 batch image］
决议：
- 支付回调履约（`payment_fulfillment.go` → `redeemForPaymentFulfillment` → 共享 `redeem`）**不得**因企业成员身份拦截：只拦用户主动发起的兑换 / 下单。核实 W2 的拦截是否落在共享 `redeem` 内，若是，改为只在公开兑换入口拦截。
- 退款及失败补偿：保持原个人账户逻辑，不拦截，不改扣企业。
- 入企前创建的批量图片任务：继续用个人冻结余额 capture/release（只拦新提交）。

### R13 聚合响应泄露账号成本与上游端点［FB］
决议：企业侧与管理端企业使用接口的 stats / models / groups / 排行 / overview stats 全部使用字段白名单 DTO（只含请求数、token、actual_cost 等），不得返回 `TotalAccountCost`、`AccountCost`、`UpstreamEndpoints`、`EndpointPaths`、账号信息。

### R14 企业查询可能被兼容降级丢弃范围［FB］
证据：`service/usage_service.go` 约 320 / 348 / 378 的接口断言失败回退旧签名；`DashboardService` 同类逻辑。
决议：企业范围查询必须走显式的企业查询接口；断言失败返回错误，**禁止回退**到不带企业字段的旧签名。核实企业 handler 没有调用审查列出的无范围 helper（dashboard 全站统计、GetAllGroupUsageSummary、API Key / 用户趋势与排行、GetUserBreakdownStats、端点统计、GetBatchUserUsageStats / GetBatchAPIKeyUsageStats、按用户的聚合、GetByID 等）；用到的必须改为带企业范围的版本。

### R15 原有用户管理接口破坏企业约束［FB］
决议：
- `UpdateUser` 把企业成员提升为 admin → 400 `ENTERPRISE_MEMBER_ADMIN_FORBIDDEN`。
- 停用用户账号：若其为企业最后一个可用 owner → 409 `ENTERPRISE_LAST_OWNER`。
- `DeleteUser`：用户仍是企业成员 → 409（提示先移出企业）。
- `CountActiveOwners` 同时要求用户账号 active 且未删除。
- 保持「企业行 → 其他」的加锁顺序，不引入 `users → enterprises` 反向事务。

## 一般

### R16 模型广场、订阅绑定、管理员改组［FA 前两项，FB 第三项］
- `GetUserGroupVisibility`（api_key_service.go 约 1110）接入企业有效集合［FA］。
- 两个绑定 helper 在订阅提前返回之前检查企业授权［FA，核实 W3 是否已做］。
- 管理员改 Key 分组 / 批量替换分组：允许管理员 override（网关运行时仍会拦截不被允许的分组），不改［FB 仅在报告确认］。

### R17 企业在途预留的故障语义［FA］
决议：保持与个人一致（预留关闭或 Redis 故障时放行），因为三级限额与余额前置检查已 fail-closed；在 design.md 风险一节说明透支上界取决于并发。只需核实不会因 scope 变化导致崩溃或漏释放。

### R18 旧直接扣款 API［FA］
决议：`UsageService.Create`、`UserService.UpdateBalance` 用于消费扣款时，企业成员输入直接返回错误（无生产调用，加防御 + 单测）。

### R19 `/v1/usage` 返回个人余额［FA］
决议：企业成员请求时，按企业可见性（以成员自身角色判定 `balance_visible`）返回企业余额；不可见时 `balance` / `remaining` 返回 `null`（若响应结构不允许 null，则省略字段并加 `balance_visible:false`），**不得**返回个人余额。

### R20 企业金额字段纳入量化契约［FA］
决议：`EnterpriseBalanceCost` 加入 `quantizeMonetaryFields`；企业扣款与三级累加使用同一量化后的金额；保持「先计算指纹、后量化」顺序。

## 额外决议

### X1 企业删除（W2 偏差）［FB，含前端微调］
「最后 owner 保护」只约束企业侧和管理员的成员操作；管理员删除企业时提供**解散**：`DELETE /api/v1/admin/enterprises/:id?dissolve=true`，在一个事务内软删除全部成员、限额行、白名单与分组池配置、部门，再软删除企业，失效全体成员认证缓存，审计 `enterprise.delete`（detail 含成员数与剩余余额）。不带 `dissolve` 且仍有成员 → 409 `ENTERPRISE_NOT_EMPTY`。前端：删除确认时若后端返回 `ENTERPRISE_NOT_EMPTY`，弹出二次确认（说明会把全部成员恢复为个人用户、企业余额随企业归档），确认后带 `dissolve=true` 重试。在途请求按 R7 冻结身份结算。
