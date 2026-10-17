企业功能 W2 后端管理接口交付报告（2026-10-17）

完成范围：tasks.md 2.1–2.9。工作区 `/Users/duegin/project/TERALEMO-CODE`，分支 `feature/enterprise`。本次只修改 backend/ 与 tasks.md；由协调者统一提交。

**1. 新增 / 修改文件清单**

下表列出本次 W2 全部文件（路径相对仓库根目录）。W1 的 Ent/schema/迁移未修改。

共 59 个文件（新增 34，修改 25，含本报告和任务清单）。

| 操作 | 文件 |
| --- | --- |
| 修改 | `backend/cmd/server/wire_gen.go` |
| 修改 | `backend/internal/handler/admin/user_handler.go` |
| 修改 | `backend/internal/handler/auth_handler.go` |
| 修改 | `backend/internal/handler/handler.go` |
| 修改 | `backend/internal/handler/user_handler.go` |
| 修改 | `backend/internal/handler/wire.go` |
| 修改 | `backend/internal/pkg/usagestats/usage_log_types.go` |
| 修改 | `backend/internal/repository/group_repo.go` |
| 修改 | `backend/internal/repository/usage_log_repo_query.go` |
| 修改 | `backend/internal/repository/usage_log_repo_stats.go` |
| 修改 | `backend/internal/repository/usage_log_repo_trend.go` |
| 修改 | `backend/internal/repository/user_repo.go` |
| 修改 | `backend/internal/server/api_contract_test.go` |
| 修改 | `backend/internal/server/routes/admin.go` |
| 修改 | `backend/internal/server/routes/user.go` |
| 修改 | `backend/internal/service/admin_group.go` |
| 修改 | `backend/internal/service/group_service.go` |
| 修改 | `backend/internal/service/payment_order.go` |
| 修改 | `backend/internal/service/payment_service.go` |
| 修改 | `backend/internal/service/promo_service.go` |
| 修改 | `backend/internal/service/redeem_service.go` |
| 修改 | `backend/internal/service/usage_service.go` |
| 修改 | `backend/internal/service/user_service.go` |
| 修改 | `backend/internal/service/wire.go` |
| 修改 | `openspec/changes/add-enterprise-organization/tasks.md` |
| 新增 | `backend/W2_ENTERPRISE_REPORT.md` |
| 新增 | `backend/internal/handler/admin/enterprise_handler.go` |
| 新增 | `backend/internal/handler/admin/enterprise_handler_test.go` |
| 新增 | `backend/internal/handler/admin/enterprise_usage_handler.go` |
| 新增 | `backend/internal/handler/admin/enterprise_usage_handler_test.go` |
| 新增 | `backend/internal/handler/admin/user_enterprise.go` |
| 新增 | `backend/internal/handler/admin/user_enterprise_test.go` |
| 新增 | `backend/internal/handler/dto/enterprise.go` |
| 新增 | `backend/internal/handler/enterprise_identity.go` |
| 新增 | `backend/internal/handler/enterprise_identity_test.go` |
| 新增 | `backend/internal/repository/enterprise_group_delete_integration_test.go` |
| 新增 | `backend/internal/repository/enterprise_management_extras_integration_test.go` |
| 新增 | `backend/internal/repository/enterprise_management_service_integration_test.go` |
| 新增 | `backend/internal/repository/enterprise_usage_repo.go` |
| 新增 | `backend/internal/repository/enterprise_usage_repo_integration_test.go` |
| 新增 | `backend/internal/repository/group_enterprise_cleanup.go` |
| 新增 | `backend/internal/server/middleware/enterprise_member.go` |
| 新增 | `backend/internal/server/middleware/enterprise_member_test.go` |
| 新增 | `backend/internal/service/enterprise_errors.go` |
| 新增 | `backend/internal/service/enterprise_group_delete.go` |
| 新增 | `backend/internal/service/enterprise_group_delete_test.go` |
| 新增 | `backend/internal/service/enterprise_management.go` |
| 新增 | `backend/internal/service/enterprise_management_groups.go` |
| 新增 | `backend/internal/service/enterprise_management_members.go` |
| 新增 | `backend/internal/service/enterprise_management_overview.go` |
| 新增 | `backend/internal/service/enterprise_management_quota.go` |
| 新增 | `backend/internal/service/enterprise_management_test.go` |
| 新增 | `backend/internal/service/enterprise_management_types.go` |
| 新增 | `backend/internal/service/enterprise_permissions.go` |
| 新增 | `backend/internal/service/enterprise_permissions_test.go` |
| 新增 | `backend/internal/service/enterprise_usage.go` |
| 新增 | `backend/internal/service/enterprise_usage_test.go` |
| 新增 | `backend/internal/service/enterprise_wallet.go` |
| 新增 | `backend/internal/service/enterprise_wallet_test.go` |

**2. 路由表与 W4 逐项核对**

以下表格路径已含 `/api/v1`。逐项解析 W4 交付清单，与 `TestEnterpriseHTTPRoutesMatchW4Contract` 中的实际注册路径核对：53/53 一致，其中管理端 29 条、企业侧 24 条。每行最后一列为与 W4 清单的核对结果。

管理端统一经过现有 AdminAuthMiddleware；企业侧统一经过 JWT 与 EnterpriseMemberAuth。企业侧非成员返回 ENTERPRISE_NOT_MEMBER，成员停用时所有接口返回 ENTERPRISE_MEMBER_DISABLED，企业停用时所有写操作返回 ENTERPRISE_DISABLED。

| 方法 | 路径 | 权限 / 约束 | W4 |
| --- | --- | --- | --- |
| GET | `/api/v1/admin/enterprises` | 系统管理员 | 一致 |
| POST | `/api/v1/admin/enterprises` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id` | 系统管理员 | 一致 |
| PUT | `/api/v1/admin/enterprises/:id` | 系统管理员 | 一致 |
| DELETE | `/api/v1/admin/enterprises/:id` | 系统管理员；企业无成员 | 一致 |
| POST | `/api/v1/admin/enterprises/:id/balance` | 系统管理员；幂等调整 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/balance-logs` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/departments` | 系统管理员 | 一致 |
| POST | `/api/v1/admin/enterprises/:id/departments` | 系统管理员 | 一致 |
| PUT | `/api/v1/admin/enterprises/:id/departments/:departmentId` | 系统管理员 | 一致 |
| DELETE | `/api/v1/admin/enterprises/:id/departments/:departmentId` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/members` | 系统管理员 | 一致 |
| POST | `/api/v1/admin/enterprises/:id/members` | 系统管理员 | 一致 |
| PUT | `/api/v1/admin/enterprises/:id/members/:memberId` | 系统管理员 | 一致 |
| DELETE | `/api/v1/admin/enterprises/:id/members/:memberId` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/quotas` | 系统管理员 | 一致 |
| PUT | `/api/v1/admin/enterprises/:id/quotas/:scopeType/:scopeId` | 系统管理员 | 一致 |
| POST | `/api/v1/admin/enterprises/:id/quotas/:scopeType/:scopeId/reset` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/allowed-groups` | 系统管理员 | 一致 |
| PUT | `/api/v1/admin/enterprises/:id/allowed-groups` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/group-whitelists` | 系统管理员 | 一致 |
| PUT | `/api/v1/admin/enterprises/:id/group-whitelists/:scopeType/:scopeId` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/usage` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/usage/stats` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/usage/trend` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/usage/models` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/usage/members` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/usage/departments` | 系统管理员 | 一致 |
| GET | `/api/v1/admin/enterprises/:id/audit-logs` | 系统管理员 | 一致 |
| GET | `/api/v1/enterprise/overview` | 全部启用成员；余额、限额、设置按可见性返回 | 一致 |
| GET | `/api/v1/enterprise/departments` | 全部启用成员 | 一致 |
| POST | `/api/v1/enterprise/departments` | owner | 一致 |
| PUT | `/api/v1/enterprise/departments/:departmentId` | owner | 一致 |
| DELETE | `/api/v1/enterprise/departments/:departmentId` | owner；非空部门不可删 | 一致 |
| GET | `/api/v1/enterprise/members` | owner 全企业；manager 仅本部门；member 403 | 一致 |
| POST | `/api/v1/enterprise/members` | owner；manager 需 manager_can_manage_members，仅本部门 member，仅新建账号 | 一致 |
| PUT | `/api/v1/enterprise/members/:memberId` | owner；manager 需管理开关，仅本部门 member 的状态/职位 | 一致 |
| DELETE | `/api/v1/enterprise/members/:memberId` | owner；manager 需管理开关，仅本部门 member；保护最后 active owner | 一致 |
| GET | `/api/v1/enterprise/quotas` | owner 全部；manager 按两个可见性开关读取本部门/成员；member 按开关读取本人 | 一致 |
| PUT | `/api/v1/enterprise/quotas/:scopeType/:scopeId` | owner；manager 需 manager_can_edit_member_quota，仅本部门 member | 一致 |
| POST | `/api/v1/enterprise/quotas/:scopeType/:scopeId/reset` | owner | 一致 |
| GET | `/api/v1/enterprise/groups` | owner/manager 返回池；member 仅本人有效分组；空池按 D6 回退 | 一致 |
| GET | `/api/v1/enterprise/group-whitelists` | owner 全部；manager 仅本部门成员级；member 403 | 一致 |
| PUT | `/api/v1/enterprise/group-whitelists/:scopeType/:scopeId` | owner；manager 需 manager_can_edit_member_groups，仅本部门 member | 一致 |
| GET | `/api/v1/enterprise/settings` | owner | 一致 |
| PUT | `/api/v1/enterprise/settings` | owner | 一致 |
| GET | `/api/v1/enterprise/usage` | owner 全企业；manager 本部门；member 本人；请求只能收窄 | 一致 |
| GET | `/api/v1/enterprise/usage/stats` | owner 全企业；manager 本部门；member 本人；请求只能收窄 | 一致 |
| GET | `/api/v1/enterprise/usage/trend` | owner 全企业；manager 本部门；member 本人；请求只能收窄 | 一致 |
| GET | `/api/v1/enterprise/usage/models` | owner 全企业；manager 本部门；member 本人；请求只能收窄 | 一致 |
| GET | `/api/v1/enterprise/usage/members` | owner 全企业；manager 本部门；member 本人；请求只能收窄 | 一致 |
| GET | `/api/v1/enterprise/usage/departments` | owner；全企业范围 | 一致 |
| GET | `/api/v1/enterprise/audit-logs` | owner | 一致 |

既有接口依赖核对：

| 方法与路径 | 本次行为 / 核对结果 |
| --- | --- |
| GET /api/v1/auth/me | 用户增加 enterprise 身份；非成员为 null；不可见余额为 null；与 D6/W4 一致 |
| GET /api/v1/user/profile | 与 /auth/me 相同；与 D6/W4 一致 |
| GET /api/v1/admin/users | 新增 enterprise_id 过滤，逐项返回 enterprise:{id,name,role} 或 null；批量读取身份；W4 的 role=user 搜索继续可用 |
| GET /api/v1/admin/groups/all | 保留现有管理员接口，供 W4 选择分组池候选 |

字段与 JSON 结构核对：

- 统一响应包裹及分页 `{items,total,page,page_size,pages}` 沿用项目惯例，金额为 number。
- 企业创建 owner 支持省略、`{user_id}`、`{email,password,username?}`；管理端加成员支持两种路径，企业侧仅新建账号。
- 管理端详情返回 `quota`；企业概览返回 `enterprise_quota`、`department_quota`、`member_quota`、10 个权限布尔值及按权限范围计算的 `stats.today_cost/month_cost`。
- 不可见的余额、限额、设置明确返回 null；各 `*_visible` 字段与 D6 一致。
- 成员 scope_id 使用 enterprise_members.id；`department_id=0` 移出部门；四档限额必须全传，null 为不限、0 为禁用；重置读取 `{windows:[...]}`。
- 分组池 GET 返回分组简要数组；白名单 GET 返回 scope 数组；PUT 接收 `{group_ids:[...]}`，空数组删除该级白名单。manager 候选按 W4 的假设展示分组池，服务端严格校验上级范围。
- 使用记录使用专用 EnterpriseUsageLog；统计字段 `total_requests/total_tokens/total_actual_cost`；趋势字段 `date/requests/total_tokens/actual_cost`；模型字段 `model/requests/total_tokens/actual_cost`；趋势、模型、排行返回数组。与 W4 的解释一致，未发现需要前端修正的假设。

**3. 权限、事务与缓存实现说明**

权限纯函数集中在 enterprise_permissions.go。服务端每次从仓储重新加载调用者成员关系、企业状态与设置，目标成员/部门/scope 必须归属同一企业；不信任请求传入的角色或企业 ID。表驱动单测覆盖三种角色、可见性与授权开关、本部门/他部门/本人/owner、企业停用与成员停用。前端 permissions 仅用于展示，所有写操作均在服务层再次授权。

owner 管理企业组织、限额、白名单与设置；manager 仅在相应开关开启时管理本部门普通 member，无法改部门/角色；member 无组织写权限。系统管理员通过独立管理路由拥有管理权限，余额调整和分组池写入仅限管理员。余额调整接入 executeAdminIdempotentJSON。

管理写操作使用 Ent 事务，并先锁企业行；CountActiveOwners、最后 active owner 保护与修改在同一事务中完成。创建成员与限额行同事务；新账号复用 adminServiceImpl.CreateUser，默认订阅分配延至提交后。非空部门不能删除，移出成员时软删成员/限额并清理成员级白名单。限额非 null 档对所有已配置上级同档做上限校验；白名单验证分组存在且 active，并验证池及已配置父级的子集关系。所有变更在同事务写审计，detail 记录 before/after。

提交后按受影响用户调用 InvalidateAuthCacheByUserID；企业级变更通过 ListMemberUserIDs 失效全体成员。余额调整调用 InvalidateEnterpriseBalance；限额更新/重置调用 InvalidateEnterpriseQuota。分组删除在既有删除事务内清理池与白名单，并失效所有被引用企业的未软删成员，包括 disabled 成员，确保删除最后一项限制时其他分组权限也刷新。

使用记录强制 owner→企业、manager→企业+消费时部门、member→企业+本人，再合并请求过滤；member 传他人 user_id 或 manager 跨部门返回 403。默认最近 30 天，最大 366 天。所有列表/统计/趋势/模型/分组/排行均使用 usage_logs 的消费快照，不按当前成员关系过滤；企业过滤禁用全站预聚合和快速总数。缺少窄查询能力时直接失败，不回退到无企业过滤的查询。DTO 不含 API Key 明文、个人余额、通知设置、上游账号、成本倍率或端点。

支付建单、余额兑换码、余额优惠码在副作用前检查成员关系，停用成员同样冻结个人钱包，返回 ENTERPRISE_MEMBER_WALLET_DISABLED；非余额类兑换继续原逻辑。采用新增窄接口、类型断言和 setter/provider 注入，未扩大 UserRepository、AdminService、UsageLogRepository、GroupRepository 等现有大接口。

**4. 验收结果**

| 命令（cwd=backend） | 结果 |
| --- | --- |
| `go generate ./cmd/server` | 通过，wire_gen.go 已重新生成 |
| `go build ./...` | 通过，退出码 0，4.930s |
| `go vet ./internal/...` | 通过，退出码 0，3.923s |
| `go test -tags=unit ./...` | 通过，退出码 0，211.802s；57 个测试包通过，60 个包无测试，含 api_contract_test |
| `go test -tags=integration ./internal/repository/... -run 'Enterprise\|UsageLog' -count=1` | 通过，退出码 0，9.382s；Docker/testcontainers PostgreSQL/Redis 集成测试，repository 5.783s |
| `go test -tags=integration ./internal/repository/... -run 'TestGroupRepoSuite/TestDelete' -count=1` | 通过，退出码 0，5.306s；覆盖既有分组删除及级联删除回归 |
| `golangci-lint run ./...` | 本机未安装，未运行；已通过 go vet |
| `gofmt -l`（本次全部 Go 文件） | 通过，无需格式化文件 |
| `git diff --check` | 通过 |

最终验收的命令与退出码记录于 `/tmp/enterprise-w2-verification.json`；完整日志位于 `/tmp/enterprise-w2-{build,vet,unit,integration,group-delete}-verified.log`。首次最终验收曾因本机磁盘空间不足失败；清理孤立 Go 临时目录及旧编译缓存后串行重跑，以上结果均来自重跑且全部通过。

主要新增证据：53 条路由契约测试、权限矩阵、handler 越权/可见性/范围收敛、me/profile 身份与 null、钱包副作用前拒绝与非余额放行、分组删除缓存失效；Docker 集成覆盖两 owner 并发降级保护、企业用户筛选、池/白名单删除清理、事务 client 普通 ctx 删除，以及污染全站预聚合后企业/部门统计仍隔离、成员调部门及离开后历史快照仍归原企业。

**5. 偏差与未完成项**

- 未新增迁移，未恢复协调者撤掉的分区表迁移分支、applyMigrationsOnConnection 重构或 enterprise_migrations_integration_test.go。
- D6 没有规定同一成员在查询期跨部门时排行的单一部门标签；成员排行按 user_id 汇总，标签取本次查询范围内最近一条消费的部门快照，金额仍包含该范围内该成员全部消费。
- 设计同时要求“最后 active owner 不可移出/停用/降级”和“企业无成员才能删除”，因此配置过 owner 的企业无法经现有管理操作清空后删除。实现严格保护最后 active owner，等待后续明确归档流程；未引入管理员绕过。
- 新建账号复用现有管理员建号逻辑；为保证数据库事务原子性，默认订阅的外部副作用移到提交后执行。
- W2 2.1–2.9 无功能未完成项。W3 合并、真实网关扣费与跨前后端端到端验收属于后续集成阶段，本次未实施。
