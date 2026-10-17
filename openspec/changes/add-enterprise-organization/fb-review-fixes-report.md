# FB 设计审查修复报告

基准：`e57526fab`；工作区：`feature/enterprise`。本报告覆盖 FB 的 R7（仓储）、R8–R15、R16 第三项、X1，以及决议文件追加的 C1、F2/F3 后端部分。没有执行 Git 写操作；协调者对迁移运行器与测试 fixture 清理的调整保持不变。

## 逐条核实与结果

下列路径均相对仓库根目录。测试均纳入后文的全量验收。

| 编号 | 状态 | 当前实现证据 | 测试名 |
|---|---|---|---|
| R7（仓储） | 先前已解决，补回归测试 | `backend/internal/repository/enterprise_repo_helpers.go:106` 通过 UPDATE 归档限额，保留原 scope 行；X1 的 `enterprise_dissolve_repo.go:21` 同样只软删除限额。 | `TestEnterpriseScopeDeletionArchivesQuotaRows`；`TestEnterpriseDissolveArchivesMembersQuotasAndBalance` |
| R8 | 已存在，本次已修复 | `backend/internal/service/enterprise_auth_cache_invalidation.go:15` 复制、去重成员 ID，立即失效，再于 2 秒后使用脱离请求取消且有 5 秒超时的 context 二次失效。企业管理、分组删除及企业账号状态修改统一接入。解散使用事务中保存的原成员 ID。 | `TestEnterpriseAuthCacheDelayedInvalidationSurvivesRequestCancellation`；`TestEnterpriseAuthCacheInvalidationIgnoresMissingInvalidatorAndInvalidUsers`；`TestEnterpriseGroupDeleteInvalidatesAllAffectedMembersAfterSuccess` |
| R9 | 已存在，本次已修复 | `backend/migrations/249_enterprise_group_configuration.sql:3` 新增池配置标记与白名单 scope 表，并回填现存配置；`backend/internal/repository/enterprise_context_repo.go:32`、`:88` 从持久标记构造原字段名；`enterprise_group_repo.go:109`、`:201` 将 PUT 空数组解释为取消配置。删除最后一组仍保留标记，配置空集合拒绝全部。 | `TestEnterpriseGroupConfigurationPersistsEmptyAndExplicitClear`；`TestEnterpriseGroupConfigurationMigrationBackfillsExistingRows`；`TestEnterpriseConfiguredEmptyPoolDoesNotFallBackToPublicGroups`；`TestEnterpriseWhitelistCannotGrantGroupsUnderConfiguredEmptyParent`；`TestEnterpriseGroupConfigurationMigration`；`TestEnterpriseGeneratedSchemaSQLiteParity` |
| R10 | 已存在，本次已修复 | `backend/internal/repository/enterprise_group_repo.go:47` 在删除旧关联前锁定排序后的旧、新分组并集（FOR KEY SHARE）；`group_repo.go:470` 普通软删除显式 FOR UPDATE；关联清理统一池→白名单→scope 顺序。 | `TestEnterpriseGroupDeleteAndReferenceReplacementDoNotDeadlock` 的 pool、whitelist 子用例；测试通过 pg_blocking_pids 确认真正阻塞后推进事务。 |
| R11：返利转余额 | 已存在，本次已修复 | `backend/internal/service/affiliate_service.go:436` 入口拒绝；`backend/internal/repository/affiliate_repo.go:236` 在事务内锁 user 并重查成员，先于解冻、扣返利额度与流水。 | `TestEnterpriseAffiliateTransferRejectedBeforeQuotaMutation`；`TestEnterpriseAffiliateTransferPreservesQuotaAndLedger` |
| R11：首绑赠金 | 已存在，本次已修复 | `backend/internal/service/auth_oauth_first_bind.go:83` 企业成员只跳过个人余额赠送，绑定与其他权益继续，消费首绑标记。 | `TestEnterpriseEmailFirstBindSucceedsWithoutBalanceGift`；`TestEnterpriseOAuthFirstBindSkipsOnlyBalanceGift` |
| R11：管理员个人调账 | 已存在，本次已修复 | `backend/internal/service/admin_user.go:623` 保留显式个人余额调整，对企业成员跳过充值返利。 | `TestAdminEnterprisePersonalBalanceAdjustmentSkipsAffiliateRebate` |
| R11：企业新建账号 | 已存在，本次已修复 | `backend/internal/service/enterprise_management.go:346` 显式传个人 Balance=0，管理员与企业侧共用建号路径。 | `TestEnterpriseCreatedAccountsExplicitlyStartWithZeroPersonalBalance` |
| R12：历史支付履约 | 已存在，本次已修复 | `backend/internal/service/redeem_service.go:441` 钱包限制仅应用于公开兑换策略，可信支付/管理履约继续入个人钱包。 | `TestEnterpriseHistoricalPaymentFulfillmentCreditsPersonalWallet`（含重复回调）；`TestEnterpriseTrustedRedeemDoesNotApplyPublicWalletGuard` |
| R12：退款及失败补偿 | 先前已解决，补回归测试 | `backend/internal/service/payment_refund.go:660`、`:866` 保持个人账户扣减/补偿，没有成员拦截及企业扣款。 | `TestEnterpriseHistoricalRefundCompensationStaysPersonal`；增强 `TestExecuteRefundUsesActualAvailableBalanceDeduction` |
| R12：历史批量图片 | 不适用本代理 | 决议归 FA；由 FA 核实个人冻结余额 capture/release，本次没有修改批量图片文件。 | 由 FA 报告提供。 |
| R13 | 先前已解决，补回归测试 | `backend/internal/service/enterprise_usage.go:53` 起的企业统计、趋势、模型与排行类型仅含允许字段；`:174`、`:210`、`:254` 显式构造 DTO。`backend/internal/handler/admin/enterprise_usage_handler.go:19` 仅依赖企业专用服务。D6 企业接口未定义 usage/groups，不会暴露通用 GroupStat。 | `TestEnterpriseUsageAggregatesWhitelistSensitiveSourceFields`；既有 `TestEnterpriseUsageDTORedactsSensitiveAssociations` |
| R14 | 先前已解决，补回归测试 | `backend/internal/service/enterprise_usage.go:163`、`:185`、`:210`、`:229`、`:240` 在企业窄能力缺失时返回错误；Stats 明确传完整 UsageLogFilters。企业 handler 没有调用 DashboardService 或无企业范围的旧 helper；通用趋势/模型/分组兼容方法已对企业过滤 fail-closed。 | `TestEnterpriseUsageMissingCapabilitiesCannotFallBackToLegacyQueries`：嵌入 nil 的旧接口，任何旧签名降级都会 panic。 |
| R15 | 已存在，本次已修复 | `backend/internal/service/admin_user.go:197`、`:380` 接入 `enterprise_user_guard.go:34`；企业→user 加锁，关系变化整笔重试。升 admin 返回400，最后可用 owner 停账号返回409，成员账号删除返回409并提示先移出。`backend/internal/repository/enterprise_member_repo.go:181` owner 计数同时要求账号 active 且未删除；`:48` 入企时锁后重查 role。 | `TestAdminUserEnterpriseConstraints`；`TestAdminDeleteUserRequiresEnterpriseRemoval`；`TestEnterpriseExistingAdminUserOperationsPreserveConstraints`；`TestEnterpriseConcurrentAccountOwnerDisableRetainsOneOwner`；`TestEnterpriseActiveOwnersExcludeUnavailableUsers`；`TestEnterpriseMemberCreateRechecksLockedUserRole`；`TestEnterpriseUserAccountUpdateReusesCallerTransaction` |
| R16 第三项 | 先前已满足，仅确认 | `backend/internal/service/admin_group.go:1348` 的 AdminUpdateAPIKeyGroupID 与 `:1476` 的 ReplaceUserGroup 保留管理员 override；没有加入企业绑定限制。网关检查由 FA 负责。 | 既有 `TestAdminService_AdminUpdateAPIKeyGroupID_BindActiveGroup`、`TestAdminService_AdminUpdateAPIKeyGroupID_Unbind`、`TestAdminService_AdminUpdateAPIKeyGroupID_ExclusiveGroup_AddsAllowedGroup` |
| X1 | 已存在，本次已修复；原 W2 偏差已关闭 | `backend/internal/service/enterprise_dissolve.go:20` 显式管理员解散，`backend/internal/repository/enterprise_dissolve_repo.go:12` 同事务归档成员/限额/部门/企业并清理关联，保留企业余额。审计含原值、成员数及余额，失败整笔回滚。前端列表、详情共用 EnterpriseDeleteDialog，收到 ENTERPRISE_NOT_EMPTY 后需再次确认才带 dissolve=true。 | `TestEnterpriseServiceDissolveRequiresExplicitAdminIntent`；`TestEnterpriseServiceDissolveAuditFailureDoesNotInvalidate`；`TestEnterpriseHTTPDissolveRequiresExplicitQuery`；`TestEnterpriseHTTPDissolveRejectsNonAdmin`；`TestEnterpriseDissolveArchivesMembersQuotasAndBalance`；`TestEnterpriseDissolveRollsBackWhenAuditFails`；`EnterpriseDeleteDialog.spec.ts` |
| C1 | 已存在，本次已修复 | `backend/internal/handler/auth_user_response.go:17` 统一企业身份与隐藏余额处理；`auth_handler.go:118` 为登录、注册、2FA、token fallback 共用；`passkey_handler.go:125` 同样接入。OAuth JSON 终态与 pending exchange 使用同一个用户构造函数，/auth/me 与 /user/profile 统一读取身份。 | `TestAuthSuccessUserEnterpriseUsesSharedResponse`；`TestAuthTokenFallbackAndOAuthResponsesIncludeEnterprise`；`TestAuthEnterpriseLookupFailureDoesNotIssueTokenResponse`；增强 `TestLogin2FACompletesPendingOAuthBindAndConsumesSession`、`TestExchangePendingOAuthCompletionLoginFalseFalseBindsIdentityWithoutAdoption`、`TestEnterpriseIdentityOnCurrentUserAndProfile` |
| F2/F3（后端） | 已存在，本次已修复 | `backend/internal/service/enterprise_management_quota.go:69` 将可查看或按原权限可编辑的行纳入列表，不扩大部门/角色范围；`:134` 捕获 PUT 响应的查看权限，`:173` 仅标记返回副本。`backend/internal/handler/dto/enterprise.go:79` 统一返回 usage_visible；隐藏时四档用量和现有三项重置时间为 null；管理员可见。 | `TestEnterpriseQuotaReadVisibilitySeparatesLimitsFromUsage`；`TestEnterpriseQuotaWriteVisibilityPreservesAccounting`；`TestEnterpriseQuotaDTOHidesUsageAndResetTimes`；`TestEnterpriseHTTPQuotaResponsesApplyUsageVisibility`（GET/PUT/reset，合计22子例） |

F2/F3 的前端交互调整由 W4 负责。本次只给 service EnterpriseQuota 新增排除 JSON 的响应可见性元数据，保留运行时计费字段类型；没有将裁剪后的数值写回数据库。现有 DTO 没有 total_resets_at（总用量没有自然窗口），因此裁剪所有四档用量和已有三项重置字段。

## 实现要点与契约

- 迁移只新增 249，246–248 未改；没有恢复分区迁移运行器分支或已移除的迁移集成测试。
- `EnterpriseMemberContext.GroupPoolConfigured` / `GroupWhitelistConfigured` 字段名不变，只切换为持久化标记来源。未配置与配置为空严格区分；PUT 空数组取消配置，分组删除保留配置标记。
- 按协调者后续调整，scope 表改用普通 `id BIGSERIAL PRIMARY KEY` 与 `UNIQUE(scope_type, scope_id)`，保留 enterprise_id 索引与 created_at。已移除自定义生成器与迁移元数据模板，恢复原标准 Ent 命令；`git diff HEAD -- backend/ent/generate.go` 为空。SQLite parity 测试验证自增 ID、唯一约束、企业索引及重复插入失败；仓储继续以 scope 唯一约束执行 ON CONFLICT。
- R15 暴露的新冲突码为 `ENTERPRISE_MEMBER_DELETE_FORBIDDEN`（409），已同步 D7。修复了 UserRepository.Update 忽略外层事务造成自身等待 user 锁的问题，GetByID/allowed-groups 读取也复用事务 client。
- 企业解散中没有 soft-delete 字段的关联表采用物理清理；企业、成员、部门、限额保留软删除归档。企业余额不转入个人，历史在途结算依赖 R7 冻结身份。
- 生产 Wire 注入返利钱包守卫与管理员用户守卫，并重新生成 wire_gen.go。
- design.md 已记录 R8/R17 风险；group-access、organization、visibility 规格同步无分组 Key、配置空语义、管理员 override、解散及追加可见性契约。原 W2 报告的 owner 企业删除偏差已标为 X1 解决。

## 文件清单

FB 所有权文件共 103 个。共享的 `frontend/src/api/__tests__/enterprise.spec.ts` 只涉及删除 API 的增补测试；W4 的其他前端编辑与协调者更新的 tasks.md 由各自报告负责。

后端（含 Ent/Wire 生成文件），88 个文件：

```text
修改 backend/cmd/server/wire_gen.go
修改 backend/ent/client.go
修改 backend/ent/ent.go
修改 backend/ent/enterprise.go
修改 backend/ent/enterprise/enterprise.go
修改 backend/ent/enterprise/where.go
修改 backend/ent/enterprise_create.go
修改 backend/ent/enterprise_update.go
新增 backend/ent/enterprisegroupwhitelistscope.go
新增 backend/ent/enterprisegroupwhitelistscope/enterprisegroupwhitelistscope.go
新增 backend/ent/enterprisegroupwhitelistscope/where.go
新增 backend/ent/enterprisegroupwhitelistscope_create.go
新增 backend/ent/enterprisegroupwhitelistscope_delete.go
新增 backend/ent/enterprisegroupwhitelistscope_query.go
新增 backend/ent/enterprisegroupwhitelistscope_update.go
修改 backend/ent/hook/hook.go
修改 backend/ent/intercept/intercept.go
修改 backend/ent/migrate/schema.go
修改 backend/ent/mutation.go
修改 backend/ent/predicate/predicate.go
修改 backend/ent/runtime/runtime.go
修改 backend/ent/schema/enterprise.go
新增 backend/ent/schema/enterprise_group_whitelist_scope.go
修改 backend/ent/tx.go
新增 backend/internal/handler/admin/enterprise_dissolve_handler_test.go
修改 backend/internal/handler/admin/enterprise_handler.go
新增 backend/internal/handler/admin/enterprise_quota_visibility_handler_test.go
修改 backend/internal/handler/auth_dingtalk_oauth.go
修改 backend/internal/handler/auth_email_oauth.go
新增 backend/internal/handler/auth_enterprise_response_test.go
修改 backend/internal/handler/auth_handler.go
修改 backend/internal/handler/auth_linuxdo_oauth.go
修改 backend/internal/handler/auth_oauth_pending_flow.go
修改 backend/internal/handler/auth_oauth_pending_flow_test.go
修改 backend/internal/handler/auth_oidc_oauth.go
新增 backend/internal/handler/auth_user_response.go
修改 backend/internal/handler/auth_wechat_oauth.go
修改 backend/internal/handler/dto/enterprise.go
新增 backend/internal/handler/dto/enterprise_quota_visibility_test.go
修改 backend/internal/handler/enterprise_identity_test.go
修改 backend/internal/handler/passkey_handler.go
修改 backend/internal/handler/user_handler.go
修改 backend/internal/handler/wire.go
修改 backend/internal/repository/affiliate_repo.go
修改 backend/internal/repository/api_key_repo_enterprise_unit_test.go
新增 backend/internal/repository/enterprise_affiliate_integration_test.go
修改 backend/internal/repository/enterprise_context_repo.go
新增 backend/internal/repository/enterprise_dissolve_integration_test.go
新增 backend/internal/repository/enterprise_dissolve_repo.go
修改 backend/internal/repository/enterprise_group_delete_integration_test.go
修改 backend/internal/repository/enterprise_group_repo.go
修改 backend/internal/repository/enterprise_member_repo.go
修改 backend/internal/repository/enterprise_repo.go
修改 backend/internal/repository/enterprise_repo_helpers.go
新增 backend/internal/repository/enterprise_review_fixes_integration_test.go
新增 backend/internal/repository/enterprise_user_guard_integration_test.go
修改 backend/internal/repository/group_repo.go
修改 backend/internal/repository/user_repo.go
修改 backend/internal/service/admin_group.go
修改 backend/internal/service/admin_service.go
修改 backend/internal/service/admin_user.go
修改 backend/internal/service/affiliate_service.go
新增 backend/internal/service/auth_enterprise_first_bind_test.go
修改 backend/internal/service/auth_oauth_first_bind.go
修改 backend/internal/service/enterprise.go
新增 backend/internal/service/enterprise_auth_cache_invalidation.go
新增 backend/internal/service/enterprise_auth_cache_invalidation_test.go
新增 backend/internal/service/enterprise_dissolve.go
新增 backend/internal/service/enterprise_dissolve_test.go
新增 backend/internal/service/enterprise_group_configuration_test.go
修改 backend/internal/service/enterprise_management.go
修改 backend/internal/service/enterprise_management_groups.go
修改 backend/internal/service/enterprise_management_quota.go
修改 backend/internal/service/enterprise_management_test.go
新增 backend/internal/service/enterprise_quota_visibility_test.go
新增 backend/internal/service/enterprise_usage_review_test.go
新增 backend/internal/service/enterprise_user_guard.go
新增 backend/internal/service/enterprise_user_guard_test.go
修改 backend/internal/service/enterprise_wallet.go
新增 backend/internal/service/enterprise_wallet_entries_test.go
新增 backend/internal/service/enterprise_wire.go
修改 backend/internal/service/group_service.go
修改 backend/internal/service/payment_refund_test.go
修改 backend/internal/service/redeem_service.go
修改 backend/internal/service/wire.go
新增 backend/migrations/249_enterprise_group_configuration.sql
修改 backend/migrations/enterprise_ent_schema_test.go
修改 backend/migrations/enterprise_migration_test.go
```

前端删除确认流程，8 个文件：

```text
修改 frontend/src/api/__tests__/enterprise.spec.ts
修改 frontend/src/api/admin/enterprises.ts
新增 frontend/src/features/enterprise/__tests__/EnterpriseDeleteDialog.spec.ts
新增 frontend/src/features/enterprise/components/EnterpriseDeleteDialog.vue
修改 frontend/src/i18n/locales/en/admin/enterprises.ts
修改 frontend/src/i18n/locales/zh/admin/enterprises.ts
修改 frontend/src/views/admin/EnterpriseDetailView.vue
修改 frontend/src/views/admin/EnterprisesView.vue
```

文档与报告，7 个文件：

```text
修改 openspec/changes/add-enterprise-organization/design.md
新增 openspec/changes/add-enterprise-organization/fb-review-fixes-report.md
修改 openspec/changes/add-enterprise-organization/review-fixes.md
修改 openspec/changes/add-enterprise-organization/specs/enterprise-group-access/spec.md
修改 openspec/changes/add-enterprise-organization/specs/enterprise-organization/spec.md
修改 openspec/changes/add-enterprise-organization/specs/enterprise-visibility/spec.md
修改 openspec/changes/add-enterprise-organization/w2-backend-api-report.md
```

## 验收结果

后端结果为 2026-10-18 恢复标准 Ent 生成流程后的最新全量验收，退出码均为 0。前端沿用上一轮结果（本次未修改前端）。后端工作目录为 `backend/`，前端工作目录为 `frontend/`。

| 工作目录 | 命令 | 结果 | 耗时 |
|---|---|---|---|
| `backend` | `go build ./...` | 通过 | 17.918s |
| `backend` | `go vet ./internal/...` | 通过 | 3.411s |
| `backend` | `go test -tags=unit ./...` | 通过 | 242.010s |
| `backend` | `go test -tags=integration ./internal/repository/... -count=1` | 通过 | 27.400s |
| `backend` | `gofmt -l .` | 通过；无输出 | 0.534s |
| `frontend` | `pnpm typecheck` | 通过 | 13.542s |
| `frontend` | `pnpm lint:check` | 通过 | 9.437s |
| `frontend` | `pnpm test:run` | 通过；385 个文件、3013 项测试 | 42.981s |
| `frontend` | `pnpm check:i18n` | 通过 | 3.249s |

本次已通过原标准命令执行 `go generate ./ent`；先前 `go generate ./cmd/server` 的生成代码全部保留。迁移仅修改尚未正式应用的 249，没有新增 250。`ent/entc.go` 与 `ent/migrate/enterprise_scope_pk.go` 已删除，`git diff HEAD -- backend/ent/generate.go` 无输出。`git diff --check` 无输出。全量仓储集成测试没有过滤 `-run`，覆盖原有 GroupRepoSuite 及新增测试；协调者的 `enterpriseGroupFixture` 清理仍在 `backend/internal/repository/enterprise_repo_integration_test.go:41`。

验收时曾发现认证新增共享测试辅助函数仅在 unit 构建下可见，已将其放到默认构建同样可见的测试文件；上表是修正后的最终结果。

完整日志位于 `/tmp/enterprise-fb-standard-ent-*.log`、`/tmp/enterprise-fb-frontend-final-*.log`；结构化结果为 `/tmp/enterprise-fb-standard-ent-validation.json` 与 `/tmp/enterprise-fb-frontend-final-validation.json`。本次 FB 实现范围没有未完成项，FA 的运行时修复仍以其独立报告和合并后验收为准。

## 遗留风险与边界

1. R8 延迟双删降低旧值回填风险，旧读取晚于第二次删除、进程退出或缓存失效失败时仍可能存在旧快照，受现有 L1/L2 TTL 约束，不承诺线性撤权。
2. R17 依决议保留 Redis 故障/禁用预留时放行；前置余额与限额读取仍 fail-closed。未成功预留时，透支上界随同时在途请求数与单请求消费变化，运行时由 FA 验证。
3. 249 能回填现存关联，无法推断升级前已经被删空、且原模型未保存配置标记的历史配置。
4. 已恢复标准 Ent 生成流程；升级 Ent 时继续运行 SQLite parity 测试，检查普通自增主键、scope 唯一约束与企业索引。
5. 解散时记录的余额是事务内归档值；按 R7，已准入在途请求仍可能在之后更新该归档余额与用量。
6. R1–R6、R7 结算侧、R12 批量图片、R16 前两项、R17–R20 的运行时实现与验收由 FA 负责；本代理没有修改这些被划定的运行时文件。
