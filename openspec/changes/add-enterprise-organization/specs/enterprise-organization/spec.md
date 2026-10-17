## Purpose

为平台引入「企业」组织：企业、一级部门、成员与企业内角色，以及系统管理员和企业侧两条成员添加路径。

## ADDED Requirements

### Requirement: 企业内角色与系统角色正交
系统 SHALL 为企业成员维护企业内角色 `owner`（企业负责人）、`manager`（部门负责人）、`member`（员工），该角色 MUST 与系统角色 `admin`/`user` 相互独立。一个用户 MUST 同时最多属于一个企业。系统管理员账号 MUST NOT 被加入企业。

#### Scenario: 用户已属于其他企业
- **WHEN** 管理员或企业负责人尝试把已属于企业 A 的用户加入企业 B
- **THEN** 系统 MUST 返回 409，错误码 `ENTERPRISE_MEMBER_EXISTS`

#### Scenario: 尝试加入系统管理员
- **WHEN** 目标用户的系统角色为 `admin`
- **THEN** 系统 MUST 返回 400，错误码 `ENTERPRISE_MEMBER_ADMIN_FORBIDDEN`

### Requirement: 一级部门组织架构
企业 SHALL 拥有零个或多个一级部门（不支持嵌套）。部门名称在同一企业内 MUST 唯一。成员 MAY 不属于任何部门（直属企业）。`manager` 角色的成员 MUST 属于某个部门，一个部门 MAY 有多个负责人。

#### Scenario: 删除仍有成员的部门
- **WHEN** 删除一个仍有成员的部门
- **THEN** 系统 MUST 返回 409，错误码 `ENTERPRISE_DEPARTMENT_NOT_EMPTY`

#### Scenario: 设置部门负责人但成员没有部门
- **WHEN** 把成员角色设为 `manager` 且其 `department_id` 为空
- **THEN** 系统 MUST 返回 400，错误码 `ENTERPRISE_MANAGER_REQUIRES_DEPARTMENT`

### Requirement: 系统管理员添加员工
系统管理员 SHALL 能够：(a) 把已有的非管理员用户加入指定企业并指定部门与角色；(b) 为企业直接创建新用户（邮箱、密码、用户名）并同时加入企业。两种方式 MUST 在同一事务中创建成员关系与成员限额记录。

#### Scenario: 加入已有用户
- **WHEN** 管理员调用 `POST /api/v1/admin/enterprises/:id/members` 传入 `user_id`
- **THEN** 系统 MUST 创建成员记录，用户的个人余额 MUST 保持不变且被冻结（不再用于扣费）

#### Scenario: 为企业创建新用户
- **WHEN** 管理员调用同一接口传入 `email`、`password`
- **THEN** 系统 MUST 创建新用户（个人余额 0）并加入企业；邮箱已被占用时 MUST 返回 409

### Requirement: 企业侧新增员工
企业负责人 SHALL 能在本企业创建新员工账号（邮箱、密码、用户名、部门、角色）。部门负责人仅当企业设置 `manager_can_manage_members=true` 时，才能在本部门创建角色为 `member` 的员工。企业侧 MUST NOT 能通过 `user_id` 拉入平台上已有的用户。

#### Scenario: 部门负责人越权创建
- **WHEN** 部门负责人在开关关闭时创建员工，或为其他部门创建员工，或创建 `manager`/`owner` 角色
- **THEN** 系统 MUST 返回 403，错误码 `ENTERPRISE_PERMISSION_DENIED`

#### Scenario: 企业负责人创建员工
- **WHEN** 企业负责人提交合法的员工信息
- **THEN** 系统 MUST 创建用户并加入企业，审计日志 MUST 记录 `member.create`

### Requirement: 移出与停用成员
企业负责人和系统管理员 SHALL 能把成员移出企业或在企业内停用成员。部门负责人仅当 `manager_can_manage_members=true` 时可移出 / 停用本部门的 `member`。移出后该用户 MUST 恢复为普通用户，个人余额 MUST 恢复可用；历史使用记录 MUST 仍归属原企业。成员操作 MUST NOT 移出、停用或降级最后一个可用 `owner`；可用同时要求成员启用且未删除、用户账号 active 且未删除。只有目标本身属于可用 owner 集合且操作会使集合为空时才拒绝；判定 MUST 在企业行锁事务内完成。此保护不阻止下文的管理员显式解散及内容审核自动封禁。

#### Scenario: 移出最后一个负责人
- **WHEN** 移出、停用或降级企业唯一启用的 `owner`
- **THEN** 系统 MUST 返回 409，错误码 `ENTERPRISE_LAST_OWNER`

#### Scenario: 停用成员
- **WHEN** 成员被停用
- **THEN** 该成员仍可登录面板，但其网关请求 MUST 被拒绝，错误码 `ENTERPRISE_MEMBER_DISABLED`

#### Scenario: 移出或降级账号已停用的负责人
- **WHEN** 企业有两个 owner，其中一个用户账号已停用，管理员或另一个 owner 移出或降级该不可用 owner
- **THEN** 系统 MUST 允许操作，不返回 `ENTERPRISE_LAST_OWNER`，可用 owner 数量保持为 1

#### Scenario: 平台内容审核自动封禁唯一可用负责人
- **WHEN** 企业唯一可用 owner 命中内容审核自动封禁阈值
- **THEN** 系统 MUST 停用该用户账号，并调用认证缓存延迟双删，不因最后 owner 身份豁免
- **AND** 系统 MUST 独立尝试写入 `member.disabled_by_moderation` 审计，操作者身份为 `system`，操作者 ID 为 0，详情含 user_id、member_id 与触发原因摘要且不含审核原文；审计失败只记录 ERROR，MUST NOT 回滚封禁
- **AND** 管理端企业列表与详情 MUST 返回整数 `active_owner_count:0`；列表 MUST 批量统计可用 owner 数量，避免 N+1

### Requirement: 企业状态
企业 SHALL 有 `active`/`disabled` 两种状态。企业被停用时，全体成员的网关请求 MUST 被拒绝（错误码 `ENTERPRISE_DISABLED`），企业侧管理接口对非系统管理员 MUST 只读。删除企业 MUST 是软删除；默认仅当企业没有成员时允许。系统管理员 MAY 显式指定 dissolve=true 解散仍有成员的企业。解散 MUST 在一个事务内软删除全部成员、限额与部门，清理白名单和分组池配置，再软删除企业；限额归档行 MUST 保留用于在途请求按冻结身份结算。成员操作的最后 active owner 保护不阻止管理员显式解散。

#### Scenario: 删除仍有成员的企业
- **WHEN** 管理员删除仍有成员的企业且未指定 dissolve=true
- **THEN** 系统 MUST 返回 409，错误码 `ENTERPRISE_NOT_EMPTY`

### Requirement: 审计日志
系统 SHALL 为企业的成员增删、角色 / 部门 / 状态变更、部门增删改、限额变更、余额调整、设置变更写入审计日志，包含操作者用户 ID、操作者身份（`admin`/`owner`/`manager`，平台自动操作为 `system`）、动作、目标与变更详情。企业负责人和系统管理员 SHALL 能分页查看本企业审计日志。

#### Scenario: 调整成员限额
- **WHEN** 企业负责人修改某员工的月度限额
- **THEN** 审计日志 MUST 记录 `quota.update`，详情包含修改前后的值

#### Scenario: 管理员显式解散企业
- **WHEN** 管理员调用 DELETE /api/v1/admin/enterprises/:id?dissolve=true
- **THEN** 企业及全部成员、部门、限额 MUST 同事务归档，成员恢复个人身份与钱包；企业余额随企业归档且 MUST NOT 转入成员个人余额
- **AND** 系统 MUST 失效解散前全体成员的认证缓存，并写 enterprise.delete 审计，detail 含成员数与剩余余额

#### Scenario: 二次确认解散
- **WHEN** 管理员在列表或详情删除企业，后端返回 ENTERPRISE_NOT_EMPTY
- **THEN** 前端 MUST 提示成员将恢复为个人用户、企业余额随企业归档，并仅在第二次确认后带 dissolve=true 重试；取消确认 MUST 不解散
