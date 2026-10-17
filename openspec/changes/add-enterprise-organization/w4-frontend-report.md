企业功能 W4 前端交付报告（2026-10-17）

**范围与结果**

实现 W4 4.1–4.7。仅修改 frontend/ 与 openspec/changes/add-enterprise-organization/tasks.md；未改 package.json、pnpm-lock.yaml，未执行任何 git 写操作。全部接口验证使用 mock。

**页面、路由与菜单**

| 路径 | 路由名 | 行为与访问条件 |
| --- | --- | --- |
| /admin/enterprises | AdminEnterprises | 系统管理员可见企业管理菜单；分页列表、搜索/状态过滤、创建、编辑、启停确认、删除确认、余额调整 |
| /admin/enterprises/:id | AdminEnterpriseDetail | 系统管理员；概览、部门、成员、限额、分组、使用记录、余额流水、审计、设置九个 Tab |
| /enterprise | Enterprise | 必须是企业成员；“我的企业”菜单仅 user.enterprise 非空时出现；概览、部门、分组、使用记录可读，其他 Tab 按 overview.permissions 和限额可见性决定 |

企业或成员停用时工作台写操作隐藏；停用成员接口拒绝时显示友好错误与重试。企业级、部门级、成员级限额都展示每日/每周/自然月/累计四档，null 为“不限”，0 保留为禁用。不可见的余额或整个限额对象为 null 时隐藏区块。

企业成员顶栏（桌面/手机）、仪表盘及资料卡不显示个人余额或冻结余额；仅 balance_visible=true 且 balance 非 null 时显示企业余额。隐藏个人购买/充值、兑换、订单菜单、订阅续购、返佣转个人余额、余额提醒和批量图片快捷入口；已有订阅仍可查看。

路由守卫拦截企业成员直达：Redeem、PurchaseSubscription、OrderList、PaymentQRCode、PaymentResult、StripePayment、AirwallexPayment、StripePopup、BatchImageGuide（含 alias /docs/batch-image），跳转 /enterprise。非企业成员访问 /enterprise 跳转 /dashboard，管理端路由保留管理员权限守卫。

**后端接口依赖**

以下路径均以 /api/v1 为前缀，返回沿用现有响应包裹。分页结构为 {items,total,page,page_size,pages}，金额为 number。

| 方法 | 管理端路径 |
| --- | --- |
| GET、POST | /admin/enterprises |
| GET、PUT、DELETE | /admin/enterprises/:id |
| POST | /admin/enterprises/:id/balance |
| GET | /admin/enterprises/:id/balance-logs |
| GET、POST | /admin/enterprises/:id/departments |
| PUT、DELETE | /admin/enterprises/:id/departments/:departmentId |
| GET、POST | /admin/enterprises/:id/members |
| PUT、DELETE | /admin/enterprises/:id/members/:memberId |
| GET | /admin/enterprises/:id/quotas |
| PUT | /admin/enterprises/:id/quotas/:scopeType/:scopeId |
| POST | /admin/enterprises/:id/quotas/:scopeType/:scopeId/reset |
| GET、PUT | /admin/enterprises/:id/allowed-groups |
| GET | /admin/enterprises/:id/group-whitelists |
| PUT | /admin/enterprises/:id/group-whitelists/:scopeType/:scopeId |
| GET | /admin/enterprises/:id/usage |
| GET | /admin/enterprises/:id/usage/stats |
| GET | /admin/enterprises/:id/usage/trend |
| GET | /admin/enterprises/:id/usage/models |
| GET | /admin/enterprises/:id/usage/members |
| GET | /admin/enterprises/:id/usage/departments |
| GET | /admin/enterprises/:id/audit-logs |

| 方法 | 用户侧路径 |
| --- | --- |
| GET | /enterprise/overview |
| GET、POST | /enterprise/departments |
| PUT、DELETE | /enterprise/departments/:departmentId |
| GET、POST | /enterprise/members |
| PUT、DELETE | /enterprise/members/:memberId |
| GET | /enterprise/quotas |
| PUT | /enterprise/quotas/:scopeType/:scopeId |
| POST | /enterprise/quotas/:scopeType/:scopeId/reset |
| GET | /enterprise/groups |
| GET | /enterprise/group-whitelists |
| PUT | /enterprise/group-whitelists/:scopeType/:scopeId |
| GET、PUT | /enterprise/settings |
| GET | /enterprise/usage |
| GET | /enterprise/usage/stats |
| GET | /enterprise/usage/trend |
| GET | /enterprise/usage/models |
| GET | /enterprise/usage/members |
| GET | /enterprise/usage/departments |
| GET | /enterprise/audit-logs |

现有接口：GET /admin/users（已有用户搜索，role=user；客户端排除已入企业用户）；GET /admin/groups/all（管理员配置分组池候选）；GET /auth/me 与 GET /user/profile（企业身份、余额可见性；变更后通过 /auth/me 刷新）。

创建企业 owner 支持省略、{user_id}、{email,password,username?}；管理员添加成员也支持已有/新建两路径，企业侧仅新建。成员 scope_id 使用 enterprise_members.id，移出部门 PUT department_id=0，四档限额全量发送且保留 null/0，重置发送 {windows:[...]}，白名单清空发送 {group_ids:[]}。

**契约解释与假设**

- D6 未展开 stats/trend/models 的完整 JSON，使用现有用量 DTO 字段：统计 total_requests/total_tokens/total_actual_cost；趋势 date/requests/total_tokens/actual_cost；模型 model/requests/total_tokens/actual_cost。趋势、模型和排行按数组返回。管理端详情企业级限额按 quota 字段读取，用户概览则按明确约定的 enterprise_quota 读取。
- manager 的 /enterprise/groups 返回分组池，而其 /group-whitelists 只允许读取本部门成员级白名单，无法完整获知企业/部门上级范围。候选使用已公开的 /enterprise/groups 与可见白名单筛选，提交越界由后端 ENTERPRISE_GROUP_OUT_OF_SCOPE 校验；没有新增未经约定的接口。owner/admin 可以基于完整三级白名单本地收窄候选。
- 普通 member 分组页只读 /enterprise/groups，不主动请求白名单、成员、设置或审计；部门列表按 D5 对全部角色开放。
- 现有兑换页与支付流程整体关闭；保留现有订阅查看。批量图片指南及快捷入口也随本期不支持企业成员的约定关闭。

**验收结果**

| 命令 | 结果 |
| --- | --- |
| pnpm install --frozen-lockfile | 通过；依赖文件无修改 |
| pnpm typecheck | 通过 |
| pnpm lint:check | 通过 |
| pnpm test:run | 379 个文件、2953 项测试全部通过（43.57 秒） |
| pnpm check:i18n | 3 项通过，zh/en key 对齐且静态引用齐全 |
| pnpm build --outDir dist | 通过；含构建脚本内再次执行的 i18n 检查与 vue-tsc -b；Vite 构建 18.21 秒 |
| git diff --check | 通过 |

另使用 ego-browser 对真实 Vue 组件与 mock API 进行浏览器检查：owner 概览及趋势/排行、普通 member 余额隐藏与 Tab、管理员九 Tab 及新增成员弹窗均正常，无捕获的运行时异常。320/768/1024/1440 四档宽度的趋势图正确适应容器，无整页横向溢出；暗色模式使用现有 Tailwind 样式，图表刻度与图例按主题调整。临时浏览器验收页面与截图已清理。

测试覆盖 API 路径/payload、初始 owner 两条路径、成员添加与 manager 权限、四档限额 null/0 与重置、停用只读、D6 脱敏 DTO、隐藏余额、菜单/守卫、钱包快捷入口、国际化防冲突。

**偏差与未完成项**

构建默认会写入 backend/internal/web/dist，为遵守禁止修改 backend/ 的约束，执行 pnpm build --outDir dist；其余验收命令保持原样。产物输出 frontend/dist，使用同一构建脚本、i18n 检查、vue-tsc 与 Vite 配置。

W4 4.1–4.7 已全部勾选，无 W4 未完成项。本次不包含后端联调、真实网关计费或集成冒烟（tasks 第 5 部分由协调者执行）；W4 功能使用 API mock 验证。无新增依赖。

**新增/修改文件清单**

- 修改 `frontend/src/api/__tests__/adminUIRequest.spec.ts`
- 修改 `frontend/src/api/admin/index.ts`
- 修改 `frontend/src/api/admin/users.ts`
- 修改 `frontend/src/api/adminUIRequest.ts`
- 修改 `frontend/src/api/index.ts`
- 修改 `frontend/src/components/layout/AppHeader.vue`
- 修改 `frontend/src/components/layout/AppSidebar.vue`
- 修改 `frontend/src/components/user/dashboard/UserDashboardQuickActions.vue`
- 修改 `frontend/src/components/user/dashboard/UserDashboardStats.vue`
- 修改 `frontend/src/components/user/dashboard/__tests__/UserDashboardStats.spec.ts`
- 修改 `frontend/src/components/user/profile/ProfileInfoCard.vue`
- 修改 `frontend/src/i18n/__tests__/localesNoKeyCollision.spec.ts`
- 修改 `frontend/src/i18n/locales/en/admin/index.ts`
- 修改 `frontend/src/i18n/locales/en/common.ts`
- 修改 `frontend/src/i18n/locales/en/index.ts`
- 修改 `frontend/src/i18n/locales/zh/admin/index.ts`
- 修改 `frontend/src/i18n/locales/zh/common.ts`
- 修改 `frontend/src/i18n/locales/zh/index.ts`
- 修改 `frontend/src/router/__tests__/feature-access.spec.ts`
- 修改 `frontend/src/router/index.ts`
- 修改 `frontend/src/router/meta.d.ts`
- 修改 `frontend/src/types/index.ts`
- 修改 `frontend/src/views/user/AffiliateView.vue`
- 修改 `frontend/src/views/user/DashboardView.vue`
- 修改 `frontend/src/views/user/ProfileView.vue`
- 修改 `frontend/src/views/user/SubscriptionsView.vue`
- 修改 `frontend/src/views/user/UserOrdersView.vue`
- 修改 `frontend/src/views/user/__tests__/AffiliateView.spec.ts`
- 修改 `frontend/src/views/user/__tests__/UserOrdersView.filters.spec.ts`
- 新增 `frontend/src/api/__tests__/enterprise.spec.ts`
- 新增 `frontend/src/api/admin/enterprises.ts`
- 新增 `frontend/src/api/enterprise.ts`
- 新增 `frontend/src/components/layout/__tests__/AppHeader.enterprise.spec.ts`
- 新增 `frontend/src/components/layout/__tests__/AppSidebar.enterprise.spec.ts`
- 新增 `frontend/src/components/user/dashboard/__tests__/UserDashboardQuickActions.enterprise.spec.ts`
- 新增 `frontend/src/features/enterprise/EnterpriseWorkspace.vue`
- 新增 `frontend/src/features/enterprise/__tests__/EnterpriseAdminForms.spec.ts`
- 新增 `frontend/src/features/enterprise/__tests__/EnterpriseMemberForm.spec.ts`
- 新增 `frontend/src/features/enterprise/__tests__/EnterpriseWorkspace.spec.ts`
- 新增 `frontend/src/features/enterprise/components/EnterpriseBalanceForm.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseBasicsForm.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseGroupsPanel.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseLogsPanel.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseMemberForm.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseOrganizationPanel.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseQuotaDisplay.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseQuotaPanel.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseSettingsPanel.vue`
- 新增 `frontend/src/features/enterprise/components/EnterpriseUsagePanel.vue`
- 新增 `frontend/src/features/enterprise/memberOptions.ts`
- 新增 `frontend/src/features/enterprise/useEnterpriseAction.ts`
- 新增 `frontend/src/i18n/locales/en/admin/enterprises.ts`
- 新增 `frontend/src/i18n/locales/en/enterprise.ts`
- 新增 `frontend/src/i18n/locales/zh/admin/enterprises.ts`
- 新增 `frontend/src/i18n/locales/zh/enterprise.ts`
- 新增 `frontend/src/types/enterprise.ts`
- 新增 `frontend/src/views/admin/EnterpriseDetailView.vue`
- 新增 `frontend/src/views/admin/EnterprisesView.vue`
- 新增 `frontend/src/views/user/EnterpriseView.vue`
- 新增 `frontend/src/views/user/__tests__/EnterpriseView.spec.ts`
- 新增 `frontend/ENTERPRISE_W4_REPORT.md`
- 修改 `openspec/changes/add-enterprise-organization/tasks.md`（仅勾选 W4 4.1–4.7）。
