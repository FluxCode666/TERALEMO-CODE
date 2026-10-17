## Purpose

按企业内角色分层控制使用记录、统计、余额与限额的可见范围，并提供企业级可见性开关。

## ADDED Requirements

### Requirement: 使用记录按角色分层可见
企业侧使用记录与统计接口 SHALL 按调用者的企业内角色确定可见范围：
- `member`：仅本人（`usage_logs.user_id = 本人`）。
- `manager`：本部门（`usage_logs.enterprise_department_id = 本部门`）。
- `owner`：全企业（`usage_logs.enterprise_id = 本企业`）。

范围 MUST 基于消费发生时写入 `usage_logs` 的企业与部门快照，而不是成员当前所属关系。系统管理员通过管理端接口 MAY 查看任意企业的记录。调用者传入的 `user_id`、`department_id`、`api_key_id` 等过滤条件 MUST 与可见范围取交集，越界时 MUST 返回 403 或空结果，MUST NOT 扩大范围。

#### Scenario: 员工尝试查看他人记录
- **WHEN** `member` 请求企业使用记录并传入其他成员的 `user_id`
- **THEN** 系统 MUST 返回 403，错误码 `ENTERPRISE_PERMISSION_DENIED`

#### Scenario: 部门负责人查看本部门
- **WHEN** `manager` 请求企业使用记录且不带过滤
- **THEN** 结果 MUST 只包含 `enterprise_department_id` 等于其部门的记录，包括已调离或已移出成员在该部门期间产生的记录

#### Scenario: 成员离开后企业仍可见
- **WHEN** 某成员被移出企业后，`owner` 查询该成员在企业期间的记录
- **THEN** 这些记录 MUST 仍然可见

#### Scenario: 不得走全站预聚合
- **WHEN** 企业范围的趋势查询没有其他过滤条件
- **THEN** 系统 MUST 直接查询 `usage_logs` 并按企业 / 部门过滤，MUST NOT 读取全站预聚合表

### Requirement: 企业侧使用记录脱敏
企业侧返回的他人使用记录 MUST 使用专用 DTO：MUST NOT 包含 API Key 明文、用户余额与通知设置、上游账号信息、账号成本倍率、上游端点；成员信息仅包含 ID、用户名、邮箱和部门。`member` 查看本人记录时 MAY 使用同一 DTO。

#### Scenario: 负责人查看成员记录
- **WHEN** `owner` 获取成员使用记录列表
- **THEN** 每条记录的 `api_key` 字段 MUST 只包含 `id` 与 `name`

### Requirement: 企业可见性开关
企业 SHALL 有以下设置（JSON 字段 `settings`），系统管理员与企业负责人可修改，`owner` 始终可见全部数据，不受开关影响：

| 键 | 默认 | 含义 |
|---|---|---|
| `balance_visible_to_manager` | `true` | 部门负责人能否看到企业余额 |
| `balance_visible_to_member` | `false` | 员工能否看到企业余额 |
| `member_quota_visible_to_member` | `true` | 员工能否看到本人限额与用量 |
| `member_quota_visible_to_manager` | `true` | 部门负责人能否看到本部门成员的限额与用量 |
| `department_quota_visible_to_manager` | `true` | 部门负责人能否看到本部门限额与用量 |
| `manager_can_manage_members` | `false` | 部门负责人能否在本部门新增 / 移出 / 停用员工 |
| `manager_can_edit_member_quota` | `false` | 部门负责人能否修改本部门员工限额（不得超过部门限额） |
| `manager_can_edit_member_groups` | `false` | 部门负责人能否配置本部门员工的分组白名单（不得超出部门有效范围） |

不可见时接口 MUST 不返回对应字段（返回 `null` 并附 `*_visible: false`），而不是返回 403，以便前端隐藏对应区块。

#### Scenario: 员工看不到企业余额
- **WHEN** `balance_visible_to_member=false` 且员工请求 `GET /api/v1/enterprise/overview`
- **THEN** 响应 `balance` MUST 为 `null`，`balance_visible` MUST 为 `false`

#### Scenario: 部门负责人修改成员限额被拒
- **WHEN** `manager_can_edit_member_quota=false` 时部门负责人修改员工限额
- **THEN** 系统 MUST 返回 403，错误码 `ENTERPRISE_PERMISSION_DENIED`

### Requirement: 当前用户接口暴露企业身份
当前用户接口（`GET /api/v1/auth/me` 及登录响应中的用户对象）SHALL 为企业成员附加 `enterprise` 对象：`id`、`name`、`role`、`department_id`、`department_name`、`member_status`、`enterprise_status`、`balance_visible`，以及在可见时附加的 `balance`。非企业成员该字段 MUST 为 `null`。企业成员的个人 `balance` 字段 MUST 照常返回但前端 MUST 不展示个人余额与个人充值入口。

#### Scenario: 企业成员登录
- **WHEN** 企业员工获取当前用户信息
- **THEN** 响应包含 `enterprise.role = "member"`，前端菜单显示「我的企业」并隐藏个人充值相关入口
