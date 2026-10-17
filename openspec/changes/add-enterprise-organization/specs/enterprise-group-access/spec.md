## Purpose

限制企业成员可使用的分组：系统管理员为企业配置可用分组池，企业在池内为企业、部门、成员三级配置分组白名单。

## ADDED Requirements

### Requirement: 企业可用分组池
系统管理员 SHALL 能为企业配置「可用分组池」（`enterprise_allowed_groups`）。分组池可以包含公开分组和专属分组：
- 分组池已配置时（enterprises.group_pool_configured=true），企业成员能否使用某个分组 MUST 只由分组池（以及下文的白名单）决定，成员个人的 `allowed_groups` 与 `restrict_public_groups` 在企业期间 MUST 被忽略；分组池中的专属分组 MUST 视为已授权给全体成员。
- 分组池未配置时，成员按个人规则（`User.CanBindGroup` 现有逻辑）判断，再叠加白名单。
- 分组删除只清理关联，MUST 保留分组池配置标记；已配置但无关联分组时 MUST 拒绝全部。管理端 PUT 空 group_ids 明确取消配置，不能将分组被删除误判为用户取消配置。
- 只有系统管理员可以修改分组池，企业侧 MUST 只读。

#### Scenario: 分组池授予专属分组
- **WHEN** 分组池包含专属分组 G，且某员工个人 `allowed_groups` 不含 G
- **THEN** 该员工 MUST 能创建绑定 G 的 API Key，并能通过网关使用

#### Scenario: 分组池外的公开分组
- **WHEN** 分组池非空且不包含公开分组 P
- **THEN** 企业成员 MUST NOT 能绑定或使用 P

### Requirement: 三级分组白名单
企业 SHALL 支持企业级、部门级、成员级三级分组白名单（`enterprise_group_whitelists`，按 `scope_type` + `scope_id` 存储）。某一级是否配置 MUST 由 enterprise_group_whitelist_scopes 的 scope 标记决定；标记不存在才表示该级不限制。已配置空层级 MUST 拒绝全部。成员的有效可用分组 MUST 为：可用分组池（已配置时）与所有已配置层级（企业级、成员所在部门级、成员级）白名单的交集。
- 写入时下级 MUST 是上级已配置白名单与分组池的子集，越界返回 400，错误码 `ENTERPRISE_GROUP_OUT_OF_SCOPE`。
- 上级收紧后，下级残留的越界分组 MUST 在运行时因取交集而失效，无需级联清理。
- PUT 空 group_ids MUST 删除该级 scope 标记以明确取消配置；删除分组 MUST 只删除关联分组，不能删除 scope 标记。
- 订阅类型分组同样受分组池与白名单约束；成员仍需自身持有有效订阅。

#### Scenario: 只配置企业级白名单
- **WHEN** 企业级白名单为 {A, B}，部门级与成员级均未配置
- **THEN** 全体成员只能使用 A、B

#### Scenario: 成员级更严格
- **WHEN** 企业级为 {A, B}、员工 X 的成员级为 {A}
- **THEN** X 只能使用 A，其他员工可使用 A、B

#### Scenario: 部门级越界写入
- **WHEN** 企业级为 {A, B}，负责人把某部门白名单设为 {A, C}
- **THEN** 系统 MUST 返回 400，错误码 `ENTERPRISE_GROUP_OUT_OF_SCOPE`

### Requirement: 白名单的配置权限
企业负责人 SHALL 能配置企业级、任意部门级、任意成员级白名单。部门负责人仅当企业设置 `manager_can_edit_member_groups=true`（默认 `false`）时，才能配置本部门员工的成员级白名单，且不得超出本部门的有效范围。系统管理员 SHALL 能配置任意层级。所有变更 MUST 写审计日志 `group_whitelist.update`。

#### Scenario: 部门负责人越权
- **WHEN** 开关关闭时部门负责人修改员工白名单，或修改其他部门成员、部门级、企业级白名单
- **THEN** 系统 MUST 返回 403，错误码 `ENTERPRISE_PERMISSION_DENIED`

### Requirement: 绑定与运行时双重生效
分组访问限制 SHALL 在以下位置生效：
1. 创建 / 修改 API Key 时：企业成员 MUST 绑定分组；无分组或不在有效可用分组内的分组 MUST 被拒绝（400，沿用绑定失败错误或 ENTERPRISE_GROUP_NOT_ALLOWED）。
2. 可选分组列表（`GetAvailableGroups` 及分组选择相关接口）MUST 只返回有效可用分组。
3. 网关请求时：企业成员的无分组 Key（包括入企前创建的）或绑定的分组不在有效可用分组内时 MUST 返回 403，错误码 `ENTERPRISE_GROUP_NOT_ALLOWED`；该校验 MUST 先于订阅分组的提前放行逻辑执行。

系统管理员通过管理端改 Key 分组或批量替换用户分组时 SHALL 允许显式覆盖绑定限制；该操作 MUST NOT 绕过网关对企业有效分组范围的检查。

分组池、任一层级白名单、成员部门或成员关系变化后，受影响成员的 API Key 认证缓存 MUST 失效，使新规则在缓存刷新周期内生效。删除分组时 MUST 同步清理分组池与白名单中的该分组。

#### Scenario: 白名单收紧后旧 Key 被拒
- **WHEN** 员工已有绑定分组 B 的 API Key，负责人把其白名单改为 {A}
- **THEN** 该 Key 的后续网关请求 MUST 返回 403 `ENTERPRISE_GROUP_NOT_ALLOWED`

#### Scenario: 成员移出企业
- **WHEN** 成员被移出企业
- **THEN** 其分组访问 MUST 恢复为个人规则

#### Scenario: 删除最后一个允许分组
- **WHEN** 分组删除使已配置分组池或某一级白名单不再包含任何分组
- **THEN** 配置标记 MUST 保留，该层 MUST 拒绝全部分组，直到有权限的操作者重新配置或显式取消配置

#### Scenario: 企业成员使用旧无分组 Key
- **WHEN** 用户入企前创建的无分组 Key 发起企业网关请求
- **THEN** 系统 MUST 返回 403 ENTERPRISE_GROUP_NOT_ALLOWED，不能绕过分组授权
