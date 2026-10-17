# 企业计费运行时终审修复报告

基线 `81daaffb0`，分支 `feature/enterprise-billing`，工作目录 `/Users/duegin/project/TERALEMO-CODE-wt-billing`。本次处理 B1–B8、A1，未修改 frontend、content_moderation.go、enterprise_management_members.go，未 push、rebase 或切换分支。

以下路径相对仓库根目录，行号对应最终代码。新增测试均重现问题所需的状态变化或并发交错，并断言付款结果或外部响应。

| 项目 | 修复方式与代码位置 | 回归测试 |
| --- | --- | --- |
| B1 | `backend/internal/handler/grok_media.go:167` 保留历史任务有效订阅；`:834` 在创建时冻结用户、分组、计费模式、订阅 ID 及所属身份，`:848` 终态恢复独立付款快照并校验订阅归属。`backend/internal/service/grok_media.go:377` 的 pending 保存身份，`backend/internal/handler/seedance.go:56` 共享恢复流程。任务终态不因后来入企或当前分组计费模式改变而转扣钱包。 | `TestHistoricalSubscriptionVideoCompletionAfterJoiningEnterprise`：真实创建→入企→终态轮询，覆盖旧 pending 有效订阅、新快照订阅→余额及余额→订阅；`TestHistoricalSeedanceSubscriptionIdentitySurvivesEnterpriseJoin`；`TestHistoricalVideoRejectsForeignSubscriptionIdentity`。位于 `backend/internal/handler/enterprise_async_video_test.go:120`。 |
| B2 | `backend/internal/repository/usage_billing_repo.go:495` 按冻结账号 ID 更新软删除归档行，`:227` 在账号确实不存在时只跳过账号计数并记录日志；付款人扣款、三级用量、幂等 claim 保留。其他 SQL 错误仍返回并回滚事务。 | `TestUsageBillingDeletedAccountAfterConsumptionRetainsPayerSettlement`：个人/企业 × 消费后软删除/物理删除，检查余额、三级四窗口、归档计数、幂等重放；`TestUsageBillingAccountSQLFailureRollsBackAndCanRetry`：账号数值转换真实 SQL 故障使前序扣费及用量回滚，修复后同请求可重试。位于 `backend/internal/repository/usage_billing_deleted_account_integration_test.go:89`。新增已提交 fixture 均使用 `t.Cleanup` 清理。 |
| B3 | `backend/internal/handler/grok_audio.go:158` 将 relay 与结算集中为一个路径：观测到音频就先提交 mandatory 用量任务，再返回任意传输错误；`:168` 复制用户、企业、部门、分组及订阅付款身份。 | `TestGrokRealtimeAudioObservedSurvivesMalformedFrameAndAbnormalTermination`（`backend/internal/service/grok_audio_test.go:56`）：真实 WS 先交付音频，再发非法 JSON、异常关闭或触发上游读取错误；`TestGrokRealtimeAudioSettlesBeforeEveryRelayErrorWithFrozenPayer`（`backend/internal/handler/grok_audio_billing_test.go:35`）：真实 RecordUsage 生成正金额冻结企业命令及日志。 |
| B4 | `backend/internal/service/enterprise_billing_cache_service.go:32` 新增可选代际窄接口，`:60`、`:161` 回源前读取版本、Lua CAS 回填，冲突重读数据库。`backend/internal/repository/enterprise_billing_cache.go:50` 管理失效 INCR 后删除；`:57`、`:234` 结算增量也推进代际，缓存为空时同样推进，阻止旧余额或用量回填。`:19`、`:108` 加入独立 `loaded_at` 最大新鲜度，用量增量不刷新配置或 TTL。原两个 Invalidate 方法签名保留。 | `TestEnterpriseBalanceReloadFencedByInvalidationAndSettlement`；`TestEnterpriseQuotaReloadCannotRestoreOldLimitOrOverwriteUsage`；`TestEnterpriseUsageIncrementsCannotExtendConfigurationFreshness`。位于 `backend/internal/repository/enterprise_billing_cache_race_test.go:37`，覆盖管理收紧、部分 miss 回源与结算增量交错，以及热缓存和旧部署延长 TTL 后的超龄 miss。 |
| B5 | `backend/internal/service/enterprise_gateway_context.go:26` 每轮首先检查 `User.IsActive()`，个人与企业用户统一生效。`backend/internal/handler/openai_gateway_handler.go:2205` 无组、换组也刷新当前用户状态；认证读取失败、缺失用户时要求重连，避免未知状态继续消费。`:2295` 将停用转换为 WS policy violation。 | `TestOpenAIResponsesWebSocket_UserDisabledAfterFirstTurn`（企业/个人 × passthrough/ctx_pool）；`TestOpenAIWSTurnPersonalStatusRefreshIgnoresGroupCompatibility`；`TestOpenAIResponsesWebSocket_PersonalUserStatusLookupFailureClosesBeforeSecondTurn`；`TestOpenAIWSTurnPersonalMissingCurrentUserCloses`；`TestEnterpriseSessionEligibilityRejectsInactiveUserBeforePersonalOrEnterpriseChecks`。主要位于 `backend/internal/handler/openai_gateway_ws_enterprise_test.go:282`。 |
| B6 | `backend/internal/repository/billing_inflight_cache.go:208` 使用企业专用 Lua，原子读取当前企业余额与新鲜度、清理过期预留、计算在途额、判断并登记。`backend/internal/service/enterprise_billing_cache_service.go:361` 缓存 miss 时先回源，再执行同一原子判断。预留关闭或 Redis 故障保持 R17 放行，余额 DB 回源故障仍拒绝。 | `TestEnterpriseInflightRechecksBalanceAtomicallyAfterConcurrentSettlement`（`backend/internal/repository/enterprise_billing_cache_race_test.go:205`）：余额 1，B 读旧值暂停，A 扣至 0.2 并释放，B 原子预留 0.8 被拒；`TestEnterpriseInflightBalanceMissReloadsBeforeAtomicAdmission`。既有 `TestEnterpriseInflightReservationDisabledOrUnavailableFailsOpen`、`TestEnterpriseInflightReservationRedisFailureReleasesAndFailsOpen`、`TestEnterpriseInflightReservationBalanceReadStillFailsClosed` 保留故障语义。 |
| B7 | `backend/internal/repository/enterprise_usage_billing.go:31` 返回实际传入并由结算 SQL 使用的日/周/月起点，`backend/internal/service/usage_billing.go:189` 的 ApplyResult 携带该窗口；`backend/internal/service/gateway_usage_billing.go:438` 向缓存同步传递。`backend/internal/service/enterprise_billing_cache_service.go:316` 与 `backend/internal/repository/enterprise_billing_cache.go:234` 仅累加相同窗口，不同或缺少已提交窗口时删除缓存，等待回源。 | `TestEnterpriseSyncUsageInvalidatesDifferentCommittedWindows`（`backend/internal/repository/enterprise_billing_cache_race_test.go:248`）：独立覆盖日、周一、自然月边界及相同窗口对照；`TestUsageBillingEnterpriseAtomicDedupWindowsAndOverdraft`（`backend/internal/repository/enterprise_usage_billing_integration_test.go:80`）逐列比对 Apply 返回窗口与 PostgreSQL 实际写入值。 |
| B8 | `backend/internal/handler/openai_gateway_handler.go:2535` 在升级前执行可判断的计费准入并使用统一 HTTP 错误映射与 Retry-After；`:2785` 首帧解析后仅补真实目标平台配额检查，避免重复 RPM。升级后由 `backend/internal/handler/enterprise_billing_error.go:73` 发送标准 `type:error` 事件，含 code、status、metadata、retry_after，再关闭连接。身份刷新失败使用明确 `billing_session_reconnect_required`。 | `TestOpenAIResponsesWebSocket_QuotaErrorBeforeUpgradePreservesHTTPDetails`（三级限额 HTTP 429/错误码/metadata/Retry-After）；`TestOpenAIResponsesWebSocket_QuotaErrorAfterFirstTurnPreservesEventDetails`（三级限额 × 两种 ingress，错误事件先于 close）。位于 `backend/internal/handler/openai_gateway_ws_enterprise_test.go:325`、`:358`。 |
| A1 | `backend/internal/handler/gateway_usage_model_stats.go:6` 使用独立字段白名单 DTO，只复制模型名、请求数、token、标准费用和用户实际费用。`backend/internal/handler/gateway_handler.go:1630` 转换后再输出；wallet、quota、subscription、企业余额及 antigravity 共用入口均受保护，不改变内部统计模型。 | `TestGatewayUsageModelStatsWhitelistOmitsUpstreamCosts`（`backend/internal/handler/gateway_usage_model_stats_test.go:45`）：两个入口 × 个人/企业 × 三种模式，断言模型统计精确字段集合及 account_cost/total_account_cost/today_account_cost 缺席。 |

资金与兼容性约束：结算锁序仍为幂等 claim → enterprises → enterprise_quotas 按 ID 升序 → api_keys → accounts；企业余额模式不扣个人钱包。未修改 Ent schema、迁移或 wire 构造参数，无需代码生成。仅新增可选窄接口，既有测试 stub 已适配。

验收环境为 Go 1.27.2 / darwin arm64；仓储集成使用 Docker PostgreSQL/Redis。以下 Go 与 gofmt 命令均在 `backend/` 执行。

| 验收命令 | 最终结果 |
| --- | --- |
| `go build ./...` | PASS，3.64 秒 |
| `go vet ./internal/...` | PASS，1.11 秒 |
| `go test -tags=unit ./...` | PASS，49.95 秒；handler 41.169 秒，service 复用上一完整轮已通过的结果 |
| `go test -tags=integration ./internal/repository/... -count=1` | PASS，全量 repository 23.492 秒，命令总耗时 30.76 秒 |
| `gofmt -l .` | PASS，无输出 |
| `git diff --check` | PASS，无输出 |

早期多路编译曾耗尽磁盘并遇并发测试重命名的瞬态失败；已停止重复编译、清理可再生成的 Go build cache，最终验收基于冻结源码串行执行。完整单测还发现新增 fixture 漏传订阅计费缓存、用户 stub 漏实现头像读取、分组零倍率，以及旧 WS fixture 缺少真实认证和计费依赖，已补齐并保留原金额、日志和拒绝行为断言。既有 `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 首轮曾测得 8,537,720 字节，超过其 8,388,608 字节上限；未修改测试阈值或生产实现，下一完整轮 service 已通过，最终完整轮复用该通过结果。最终日志为 `/tmp/final-billing-{build,vet,unit,integration,gofmt}.log`。

提交列表：

1. `3b520514c` — `fix(enterprise): 修复计费运行时终审问题并补充时序回归`，包含 B1–B8、A1 生产修复及回归测试。
2. 本报告所在提交 — `docs(enterprise): 记录计费终审修复与全量验收结果`。

两次提交均以 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` 结尾。

遗留边界：旧 pending 没有创建时计费身份，只能保留轮询时仍有效的订阅，无法从缺失数据重建原历史模式；新 pending 身份快照仍受既有 Redis 任务缓存丢失/过期影响。企业异步视频新提交继续按既定范围拒绝。企业代际键刻意不设 TTL，防止版本遗忘使旧回填通过；持续三次 CAS 冲突会暂时拒绝准入，优先保护资金。R17 的预留关闭/Redis 故障放行仍可能产生并发透支。WS 状态变化仍依赖现有认证缓存失效机制；本次将无法确认状态的后续轮次改为拒绝。Grok/Live 无逐轮回调的持续会话仍按建连身份计费，本次未扩展实时撤销或 Live 定价。
