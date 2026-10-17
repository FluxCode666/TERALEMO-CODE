## Purpose

企业成员的余额模式消费改由企业余额承担，并在请求前后执行企业 / 部门 / 成员三级限额的检查与累加。

## ADDED Requirements

### Requirement: 企业余额与余额流水
企业 SHALL 拥有 `balance`（decimal(20,8)）与 `total_recharged`。只有系统管理员能通过 `POST /api/v1/admin/enterprises/:id/balance` 调整余额，`operation` 为 `add` 或 `subtract`，金额 MUST 大于 0；`subtract` 后余额 MUST NOT 小于 0（否则 400 `ENTERPRISE_BALANCE_INSUFFICIENT`）。每次调整 MUST 在同一事务内写入 `enterprise_balance_logs`（类型、金额、调整前后余额、操作人、备注），`add` MUST 同时累加 `total_recharged`。调整后 MUST 失效企业余额缓存。

#### Scenario: 管理员充值
- **WHEN** 管理员给企业 `add` 100
- **THEN** 企业余额 +100、`total_recharged` +100，流水记录 `balance_before`/`balance_after`，审计日志记录 `balance.adjust`

### Requirement: 成员消费扣企业余额
对于企业状态 `active`、成员状态 `active` 的企业成员，余额模式（非订阅计费）的每次结算 MUST 从企业余额扣除 `actual_cost`，MUST NOT 修改成员个人余额。扣费 MUST 与现有 `usage_billing_dedup` 幂等机制在同一个数据库事务内完成；余额不足时与个人余额一致，允许扣成负数并标记透支。订阅计费（分组为订阅类型且成员持有有效订阅）MUST 保持现有行为，不扣企业余额，也不计入企业限额。

#### Scenario: 员工调用模型
- **WHEN** 员工通过余额模式分组产生 0.02 USD 的费用
- **THEN** 企业余额减少 0.02，员工个人余额不变，`usage_logs` 记录 `enterprise_id` 与 `enterprise_department_id`

#### Scenario: 重复结算
- **WHEN** 同一 `request_id` 的结算被重放
- **THEN** 企业余额与三级用量 MUST 只变化一次

### Requirement: 企业成员请求前检查
企业成员的网关请求 SHALL 依次检查：企业状态（停用 → 403 `ENTERPRISE_DISABLED`）、成员状态（停用 → 403 `ENTERPRISE_MEMBER_DISABLED`）、分组访问（见 enterprise-group-access）。余额模式下 MUST 用企业余额代替个人余额做余额检查与在途预留（不足 → 403 `ENTERPRISE_BALANCE_INSUFFICIENT`），MUST NOT 因成员个人余额为 0 而拒绝。在途预留 MUST 以企业为维度累计，避免多个成员并发透支同一企业余额。个人维度的 `user_platform_quota`、API Key 额度与限速、RPM MUST 继续生效。

#### Scenario: 个人余额为 0 的员工
- **WHEN** 员工个人余额为 0、企业余额为 50
- **THEN** 请求 MUST 被放行

#### Scenario: 企业余额耗尽
- **WHEN** 企业余额 ≤ 0
- **THEN** 全体成员的余额模式请求 MUST 返回 403 `ENTERPRISE_BALANCE_INSUFFICIENT`

### Requirement: 三级限额
企业、部门、成员三个层级 SHALL 各有一条限额记录（`enterprise_quotas`，`scope_type` ∈ {`enterprise`,`department`,`member`}），包含 `daily`/`weekly`/`monthly`/`total` 四档 USD 上限与对应用量。上限 `NULL` 表示不限，`0` 表示禁用，`> 0` 表示上限。窗口规则：
- 日：全局时区自然日；周：全局时区自然周（周一开始）；月：全局时区自然月；累计：永不自动重置。
- 窗口惰性重置：读取或累加时发现窗口已过期，先清零再计算。

请求前 MUST 检查成员、所在部门（若有）与企业三级，任一档 `usage >= limit` 即拒绝，返回 429，错误码分别为 `ENTERPRISE_QUOTA_EXCEEDED`、`DEPARTMENT_QUOTA_EXCEEDED`、`MEMBER_QUOTA_EXCEEDED`，metadata 带 `scope`、`window` 与 `window_resets_at`（累计档无重置时间），并设置 `Retry-After`。结算时 MUST 在扣费事务内把 `actual_cost` 同时累加到三级的四档用量。

缓存或数据库不可用导致无法读取限额时，企业成员请求 MUST 拒绝（503 `billing_service_error`），而不是放行。

#### Scenario: 部门月度超限
- **WHEN** 某部门月度上限 1000，本月已用 1000
- **THEN** 该部门全体成员的余额模式请求 MUST 返回 429 `DEPARTMENT_QUOTA_EXCEEDED`，`window=monthly`

#### Scenario: 成员调离部门
- **WHEN** 员工从部门 A 调到部门 B
- **THEN** 此后的消费计入部门 B 的用量；部门 A 已累计的用量保持不变

#### Scenario: 手动重置
- **WHEN** 企业负责人或管理员对某成员执行「重置累计用量」
- **THEN** 该成员 `total_usage_usd` 清零，审计日志记录 `quota.reset`

### Requirement: 限额配置约束
设置部门或成员限额时，非 `NULL` 的某档上限 MUST NOT 超过上级同档已配置的非 `NULL` 上限（成员 ≤ 部门 ≤ 企业，成员没有部门时直接与企业比较），否则返回 400 `ENTERPRISE_QUOTA_EXCEEDS_PARENT`。上级后续调低不级联修改下级，运行时三级同时检查即可保证约束。

#### Scenario: 员工限额高于部门
- **WHEN** 部门日上限 50，负责人把员工日上限设为 80
- **THEN** 系统 MUST 返回 400 `ENTERPRISE_QUOTA_EXCEEDS_PARENT`

### Requirement: 个人钱包冻结
企业成员在企业期间 MUST NOT 能创建任何支付订单、MUST NOT 能兑换余额类兑换码或领取余额类优惠码（返回 403 `ENTERPRISE_MEMBER_WALLET_DISABLED`）。批量图片等依赖个人余额冻结的功能对企业成员 MUST 返回 403 `ENTERPRISE_FEATURE_UNSUPPORTED`。成员被移出企业后上述限制 MUST 立即解除。

#### Scenario: 员工兑换余额码
- **WHEN** 企业员工兑换余额类兑换码
- **THEN** 系统 MUST 返回 403 `ENTERPRISE_MEMBER_WALLET_DISABLED`，兑换码保持未使用

### Requirement: 企业余额不足提醒
企业 SHALL 支持可选的余额提醒阈值 `low_balance_threshold`（`NULL` 表示关闭）。结算后企业余额首次由高于阈值降到阈值及以下时，系统 SHOULD 向企业全部启用状态的 `owner` 邮箱发送提醒，同一次下穿只发送一次。

#### Scenario: 余额下穿阈值
- **WHEN** 阈值 100，一次结算使余额从 100.5 降到 99.8
- **THEN** 系统向全部企业负责人发送一次余额不足邮件
