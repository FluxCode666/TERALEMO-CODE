# FA 设计审查修复报告

基线：`e57526fab`，工作分支 `feature/enterprise-billing`。范围为 `review-fixes.md` 标注 FA 的 13 项；R7 仅结算运行时，R12 仅 batch image，R16 仅前两项。

## 逐项核实与结果

以下路径均相对仓库根目录，行号对应本次交付代码。测试名可直接用 `go test -run` 定位。

| 决议 | 状态 | 代码证据与行为 | 覆盖测试 |
| --- | --- | --- | --- |
| R1 | 已存在，已修复剩余问题 | W3 已有 WS 后续轮刷新。本次补完整 User 快照、部门变化重连、本轮权限上下文与不重复计 RPM 的准入。`backend/internal/handler/openai_gateway_handler.go:2159`、`:2205`、`:2298`；`backend/internal/service/enterprise_gateway_context.go:23`。Live/Grok 已有建连检查（`handler/openai_live.go:84`、`handler/grok_audio.go:38`，均位于 `backend/internal/`）；本次补 Live 成员 ID 冻结与 Redis 序列化（`backend/internal/service/openai_live_types.go:41`、`backend/internal/repository/gateway_cache.go:428`）。 | `TestOpenAIResponsesWebSocket_EnterpriseDepartmentChangeRequiresReconnect`（passthrough、ctx_pool）；`TestOpenAIResponsesWebSocket_EnterpriseMembershipRefreshClosesBeforeNextTurn`；`TestEnterpriseSessionEligibilityRechecksWithoutCountingRPM`；`TestEnterpriseLiveAndGrokRealtimeRejectBeforeCreatingSession`；`TestEnterpriseLiveCallIdentityFreezesPayer`；`TestGatewayCacheLiveCallIdentityAndController` |
| R2 | 已存在，已修复 | `backend/internal/handler/grok_media.go:70` 在任何预留、上游操作前拒绝企业 Grok generate/edit/extend、Seedance 创建任务，返回 403 `ENTERPRISE_FEATURE_UNSUPPORTED`。`:78`、`:556` 为入企前个人任务构建独立个人付款快照，轮询终态不扣企业、不记企业归属。 | `TestEnterpriseAsyncVideoSubmissionRejectedBeforeAnyUpstreamWork`；`TestEnterpriseMemberCanPollHistoricalPersonalVideo`；`TestAsyncVideoHistoricalPersonalPayerSnapshot`；`TestHistoricalAsyncVideoCompletionChargesPersonalWalletAfterJoiningEnterprise`（Grok、Seedance 正金额真实命令及日志） |
| R3 | 已存在，已修复 | `backend/internal/service/api_key_service.go:532`、`:846` 创建/更新企业 Key 无组返回 400；`backend/internal/server/middleware/api_key_auth.go:163`、`api_key_auth_google.go:124` 拦截历史无组 Key，返回 403。`backend/internal/service/enterprise_billing_cache_service.go:172` 在余额、简易、订阅路径之前统一拒绝 nil/非正 ID 分组。 | `TestEnterpriseAPIKeyRequiresExplicitGroupOnCreateAndUpdate`；`TestAPIKeyAuthEnterpriseMembership`；`TestGoogleAPIKeyAuthEnterpriseMembership`；`TestValidateAPIKeyGroupAllowedUngroupedEnterprise`；`TestEnterpriseBillingUngroupedKeyCannotBypassRestrictions` |
| R4 | 已存在，已修复剩余问题 | W3 已有 Claude fallback 企业检查。本次统一 `backend/internal/service/enterprise_gateway_context.go:11` 的判断；`backend/internal/service/gateway_scheduling.go:940` 拒绝切组时保留原 `ErrClaudeCodeOnly`；`backend/internal/handler/gateway_handler.go:1039`、`enterprise_gateway_fallback.go:9` 在 PromptTooLong 切组前拒绝并返回原上游错误。 | `TestEnterpriseGatewayFallbackGroupAccess`；`TestEnterprisePromptTooLongFallbackPreservesOriginalError` |
| R5 | 先前已解决；本次随 R3 补强 | Google 认证已共享企业停用、分组检查并跳过个人余额：`backend/internal/server/middleware/api_key_auth_google.go:116`、`:139`、`:222`。本次仅补所有企业无组 Key 的显式拒绝，保留 Google 错误格式。 | `TestGoogleAPIKeyAuthEnterpriseMembership`（企业/成员停用、分组拒绝、个人余额为零、无组） |
| R6 | 字段与 SQL 先前已解决；本次统一 writer | 新统一函数 `backend/internal/service/usage_log.go:226` 显式复制冻结企业归属，个人付款清 NULL。覆盖 `gateway_usage_billing.go:1358`、`openai_gateway_usage.go:414`、`openai_live.go:858`、`batch_image_settlement.go:285`、`usage_service.go:135`（均位于 `backend/internal/service/`）。四种生产 INSERT 均在 `backend/internal/repository/usage_log_repo_insert.go:229`、`:787`、`:1051`、`:1191`，已有企业列；参数准备在 `:1386`。 | `TestUsageLogPayerIdentityCapturesFrozenScopeAndClearsPersonalScope`；`TestEnterpriseGatewayUsageLogSnapshot`；`TestEnterpriseOpenAIUsageLogSnapshot`；`TestFinalizeLiveCallIsIdempotentAndWritesZeroUsage`；`TestBatchImageSettlementService_SettlesAndChargesSuccessfulImagesOnly`；`TestUsageBillingEnterpriseUsageLogAllInsertPaths`（四种真实 DB 插入） |
| R7 运行时 | 已存在，已修复 | `backend/internal/repository/enterprise_usage_billing.go:13` 按冻结企业 ID 扣余额，`:30` 更新包含软删除归档的限额；均不筛 `deleted_at`。缺行时累加能找到的行，`:67` 记录带请求、Key、企业、部门、成员和金额的 ERROR，保留企业扣款与幂等记录。SQL 故障仍回滚。新请求准入的缺行 fail-closed 保持不变。 | `TestUsageBillingEnterpriseFrozenArchivedScopesStillSettle`；`TestUsageBillingEnterpriseMissingQuotaKeepsBalanceDedupAndAvailableUsage`；`TestUsageBillingEnterpriseMissingQuotaKeepsPaymentAndLogsIncident`；`TestEnterpriseBillingEligibilityCacheFallbackFailsClosed`；`TestUsageBillingEnterpriseConcurrentMembersHaveConsistentLockOrder` |
| R12 batch image | 先前已解决；本次补回归与统一个人日志归属 | 新提交阻断在 `backend/internal/service/batch_image_public.go:209`。历史履约走 `backend/internal/repository/usage_billing_repo.go:117`、`:121` 的个人冻结余额 capture/release，未加成员拦截。`backend/internal/service/batch_image_settlement.go:285` 显式写个人归属。 | `TestUsageBillingEnterpriseHistoricalBatchImageCaptureAndReleaseStayPersonal`：真实 Reserve→Capture/Release，验证入企用户的个人余额、冻结余额、幂等性及企业余额/用量均符合预期；`TestBatchImageSettlementService_SettlesAndChargesSuccessfulImagesOnly` |
| R16 前两项 | 先前已解决 | `backend/internal/service/api_key_service.go:509`、`:1129` 两个绑定 helper 在订阅提前放行前检查企业授权；`:1153` 模型广场可见范围使用企业有效分组集合，包含合法订阅。未改管理员 override。 | `TestEnterpriseAPIKeyGroupAccessLoadsMembershipAndFiltersSubscriptions`；`TestEnterpriseAPIKeyMembershipLoadFailureIsClosed` |
| R17 | 已存在，已修复 | `backend/internal/service/enterprise_billing_cache_service.go:282` 预留未启用、cache 不支持预留、Redis 预留失败与个人一致放行；响应丢失的可能写入先尝试释放。企业余额读取故障、前置额度异常仍拒绝。冻结 `ent:{id}` scope 后续不受用户上下文变更影响。风险已写 `design.md` 的 Risks。 | `TestEnterpriseInflightReservationDisabledOrUnavailableFailsOpen`；`TestEnterpriseInflightReservationRedisFailureReleasesAndFailsOpen`；`TestEnterpriseInflightReservationBalanceReadStillFailsClosed`；`TestEnterpriseFixedChargeReservationFailureDoesNotBypassEligibility`；`TestEnterpriseInflightReservationFrozenScopeRenewsAndReleasesOnce`；`TestEnterpriseInflightRedisReservationDownFailsOpen` |
| R18 | UsageService 先前已解决；UserService 本次修复 | `backend/internal/service/usage_service.go:104` 事务内拒绝成员及关系查询失败。`backend/internal/service/user_service.go:1161` 负向消费经窄接口确认个人身份；关系存在、reader 缺失、查询失败均不扣款，非负调整保持原行为。`backend/internal/service/wire.go:864` 接入 reader。 | `TestEnterpriseLegacyUsageServiceRejectsMembershipAndLookupFailure`；`TestEnterpriseLegacyUserBalanceConsumptionRejectsMembershipAndLookupFailure`；`TestEnterpriseLegacyUserBalanceConsumptionAllowsConfirmedPersonalUser`；`TestEnterpriseLegacyUserBalancePositiveAdjustmentKeepsExistingBehavior` |
| R19 | 已存在，已修复 | `backend/internal/handler/gateway_enterprise_usage.go:18` 实时读取本人当前成员身份和角色可见性，`:44` 统一输出企业余额或 NULL；wallet、Key quota、subscription 三种响应全部接入（`backend/internal/handler/gateway_handler.go:1716`、`:1814`）。身份读取失败不返回个人余额，旧个人认证快照也以当前企业关系为准。Key 自身 `quota.remaining` 保持额度语义。 | `TestGatewayUsageEnterpriseBalanceVisibilityInAllModes`（owner/manager/member×开关×三种模式）；`TestGatewayUsageEnterpriseIdentityFailureNeverReturnsPersonalWallet`；`TestGatewayUsageCurrentMembershipOverridesOldPersonalAuthSnapshot` |
| R20 | 先前已解决；本次补精确对账 | `backend/internal/service/usage_billing.go:52` 保持先指纹后量化；`:89` 已量化 EnterpriseBalanceCost。`backend/internal/repository/usage_billing_repo.go:190` 与 `enterprise_usage_billing.go:61` 使用同一量化字段扣企业余额、累加四档用量。未修改原指纹格式。 | `TestUsageBillingFingerprintPreservesPersonalGoldenHashes`；`TestUsageBillingFingerprintIncludesEnterpriseSnapshot`；`TestUsageBillingEnterpriseQuantizedCostReconcilesExactly`（`0.000078125→0.00007813`，PostgreSQL NUMERIC 直接相等，无浮点 epsilon） |

本次没有标为「不适用」的 FA 条目。先前已解决的内容没有重复实现；相关回归随全量测试一起验证。

## 锁序、热路径与共享文件

- 企业余额结算顺序保持：幂等 claim → enterprises → enterprise_quotas 按 ID 升序（MATERIALIZED CTE + FOR UPDATE）→ api_keys → accounts。企业余额模式不锁 users；订阅分支与企业余额扣款互斥。本次未调整 FB 的管理侧锁序。
- 非企业请求仍走原钱包、订阅与 RPM 路径。企业会话专用复核在非企业输入时直接返回；个人指纹有 golden hash 回归。`/v1/usage` 为避免旧个人认证快照泄露钱包，所有请求增加当前企业身份查询，确认非成员才返回原个人响应。
- 保留协调者的 `NewEnterpriseService`、`NewEnterpriseUsageService` provider 与真实缓存。Wire 仅增加 UserService 防御 reader 与 GatewayHandler 企业身份 reader，已重新生成 `wire_gen.go`。
- 保留协调者给订阅分组、当日日志、账号加的清理；本次进一步为提交数据的企业 fixture 清理自身用户、成员、Key、dedup、限额、部门及企业。全量仓储集成用于检查污染。
- 未改 FB 的管理 service、权限、企业查询实现、迁移、前端。`repository/enterprise_*` 的生产改动仅 R7 结算文件；另对原 W3 缓存测试更新 R17 预期与 R3 合法分组 fixture。

## 验收

在 `backend/` 串行执行，全部通过：

| 命令 | 结果 | 耗时 |
| --- | --- | --- |
| `go build ./...` | PASS | 12.9 秒 |
| `go vet ./internal/...` | PASS | 11.0 秒 |
| `go test -tags=unit ./...` | PASS，57 个包通过；service 213.470 秒 | 225.9 秒 |
| `go test -tags=integration ./internal/repository/... -count=1` | PASS，全量无筛选，repository 23.219 秒 | 30.4 秒 |
| `gofmt -l .` | PASS，无输出 | 0.6 秒 |

`git diff --check` 通过。全量仓储集成包含原有 Group、UsageLog 套件，未出现之前的 fixture 污染失败。日志保存于 `/tmp/fa-final-{build,vet,unit,integration,gofmt}.log`。提交哈希随交付消息提供。

## 遗留风险与后续

- 异步视频尚未持久保存创建时付款主体。本期企业提交一律拒绝，历史任务保持个人付款；后续开放企业异步视频前，需落库创建身份并在终态结算。
- Live/Grok 无逐轮回调的持续会话按决议仅在建连准入并冻结身份；会话期间变更不会立即断流。Live 仍保留原有零金额日志及既有计费 TODO，本次未扩展价格计算。
- R17 按决议允许 Redis 预留故障时放行；三级限额、余额前置检查仍 fail-closed。透支规模取决于并发在途数与最终费用，没有固定金额上界；释放失败依靠 TTL 回收。
- R7 缺限额行属于数据完整性故障，现会产生 ERROR 并保留扣款。归档可结算依赖 FB 不物理删除企业及限额行；本次有真实软删除回归。
- R8/R9 等 FB 管理与数据层修复尚未合入本分支；本次运行时代码不依赖配置标志来源变化，仍需协调者合并后整体验证。认证快照仍受既有缓存失效窗口影响。
