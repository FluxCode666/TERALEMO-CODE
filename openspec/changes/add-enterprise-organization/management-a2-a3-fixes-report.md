# 企业管理侧 A2 / A3 修复报告

基准：`feature/enterprise`，`81daaffb0`。本次仅修改 `backend/` 和本变更目录；未执行任何 git 写操作，未修改前端或其他代理负责的计费、网关运行时文件。运行环境为 Go 1.27.2、PostgreSQL/Redis testcontainers。

## A2：平台内容审核自动封禁

- `backend/internal/service/content_moderation.go:1997`：维持平台风控优先。账号更新成功后调用现有延迟双删 helper（立即一次、约 2 秒后再一次），然后尝试企业审计；最后可用 owner 不豁免。
- `backend/internal/service/content_moderation_enterprise.go:20`：生产 provider 注入成员查询与审计仓储；`backend/internal/service/wire.go:992` 与重新生成的 `backend/cmd/server/wire_gen.go:285` 接入该 provider。
- `backend/internal/service/content_moderation_enterprise.go:38`：账号更新成功后，以独立于请求取消、5 秒超时的 context 查询成员并写审计。action 为 `member.disabled_by_moderation`，actor_role 为 `system`，actor_user_id 为 0，target_type 为 member。detail 仅包含 user_id、member_id、固定原因摘要、违规次数、封禁阈值和统计窗口小时数；不复制原文、摘录、关键词或上游错误。查询/写入失败只记 ERROR，已完成的封禁和认证缓存失效不回滚。
- `backend/internal/domain/enterprise.go` 与 `backend/internal/service/enterprise.go`：增加系统审计身份和自动封禁 action 常量。审计列原本是无枚举限制的字符串，不需要数据库迁移。
- `backend/internal/repository/enterprise_repo.go:57`、`:153`、`:165`：详情与列表加载 `active_owner_count`；列表只对当前页企业 ID 做一条分组聚合查询，空页跳过查询。统计要求成员 owner、active、未删除，且用户账号 active、未删除，与 CountActiveOwners 相同；查询不加企业行锁。
- `backend/internal/service/enterprise.go:90`、`backend/internal/handler/dto/enterprise.go:24`：增加 int 字段及 DTO 映射，值为 0 时仍输出。D6 的 Enterprise 对象及 D5/D9/Risks、organization spec 已同步。

封禁路径没有引入 users → enterprises 的事务或反向行锁；成员查询和审计追加发生在账号更新返回成功之后。

## A3：可用 owner 口径

- `backend/internal/service/enterprise_management_members.go:246`、`:277`、`:301`：更新和移出共用判定，仅当目标本身为 owner、成员 active、账号 active 时才判断是否移除了最后可用 owner。仓储 LEFT JOIN 排除已删除账号，其 UserStatus 为空，因此不计入集合。
- 管理端和企业侧共用该 service，原企业行锁与外层事务保持。不可用 owner 可以降级、停用成员状态或移出；唯一可用 owner 仍返回 409 ENTERPRISE_LAST_OWNER。
- `backend/internal/service/enterprise_management_test.go:253`：为原 owner fixture 补真实账号状态，保持既有最后 owner 测试语义。

## 新增回归测试

| 文件 | 测试名 | 覆盖内容 |
|---|---|---|
| `backend/internal/service/content_moderation_enterprise_test.go:54` | `TestContentModerationAutoBanEnterpriseOwnerAuditsAndInvalidates` | owner 正常封禁、system 审计、字段白名单、不含原文、请求取消后第二次缓存失效、已停用账号不重复审计 |
| 同文件 `:108` | `TestContentModerationAutoBanEnterpriseAuditFailuresDoNotUndoBan` | 审计写入失败、成员查询失败、审计 writer 缺失均保留封禁与缓存失效，记录 ERROR |
| 同文件 `:144` | `TestContentModerationAutoBanNonMemberPreservesBehavior` | 非企业成员照常封禁，无企业审计 |
| 同文件 `:164` | `TestContentModerationFailedBanDoesNotAuditOrInvalidate` | 账号停用失败时不审计、不失效 |
| `backend/internal/repository/enterprise_moderation_integration_test.go:36` | `TestEnterpriseModerationBanOnlyOwnerAuditsWithoutEnterpriseLock` | 真实封禁唯一 owner、持有企业行锁仍可完成、实际审计入库且脱敏、列表/详情 owner 计数变 0、立即/延迟缓存失效、非成员行为 |
| `backend/internal/repository/enterprise_active_owner_count_integration_test.go:17` | `TestEnterpriseRepositoryActiveOwnerCountMatchesAvailableOwners` | 与 guard 计数对齐、成员/账号停用及软删除、多企业批量列表、空页和零计数 |
| `backend/internal/handler/dto/enterprise_active_owner_count_test.go:14` | `TestEnterpriseDTOIncludesActiveOwnerCount` | 列表与详情 JSON 保留 0 和非零整数 |
| `backend/internal/service/enterprise_member_owner_availability_test.go:26` | `TestEnterpriseMemberLastOwnerGuardUsesAccountAvailability` | 管理端/企业侧 × 降级/停用/移出 × 四种可用性，共 24 子场景，检查事务和加锁先于读取 |
| `backend/internal/repository/enterprise_member_owner_availability_integration_test.go:36` | `TestEnterpriseMemberUnavailableOwnerCanBeChanged` | 两条路径移出/降级/停用账号 disabled 或 deleted 的 owner，共 12 子场景 |
| 同文件 `:89` | `TestEnterpriseMemberOnlyAvailableOwnerRemainsProtected` | 两条路径唯一可用 owner 的三类变更均拒绝，共 6 子场景 |
| 同文件 `:120` | `TestEnterpriseMemberConcurrentOwnerMutationsRetainAvailableOwner` | 两个可用 owner 并发变更、另有停用账号 owner，保证一成功一拒绝，共 6 子场景 |

事务内夹具使用 testEntTx 的 t.Cleanup 回滚。需要跨连接可见的自动封禁及并发测试提交夹具，均显式 t.Cleanup 删除企业审计、成员、企业、用户；自动封禁测试还清理 content_moderation_logs。

## 验收

新增 A2/A3 回归全部通过；全量验收未全绿，唯一剩余失败为未修改的既有内存阈值用例。以下命令工作目录均为 `backend/`，集成测试实际运行了 PostgreSQL/Redis testcontainers，没有因 Docker 缺失跳过。

| 命令 | 结果 | 耗时 |
|---|---|---|
| `go build ./...` | 通过 | 64.968s |
| `go vet ./internal/...` | 通过 | 14.140s |
| `go test -tags=unit ./...` | 未通过；两轮仅 `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 失败 | 295.543s / 214.262s |
| `go test -tags=unit ./internal/service -run '^TestInflightEstimate_AccountMappingNoDBAndBoundedMemory$' -count=1` | 隔离复跑通过 | 5.744s |
| 新增 unit 定向回归（service + DTO，`-count=1 -v`） | 6 个顶层测试及全部子场景通过 | 8.061s |
| 新增 integration 定向回归（repository，`-count=1 -v`） | 5 个顶层测试及全部子场景通过 | 8.142s |
| `go test -tags=integration ./internal/repository/... -count=1` | 最终全量复跑通过，未带 `-run` 过滤；无新增夹具污染导致的失败 | 25.965s |
| `gofmt -l .` | 通过，无输出 | 0.471s |
| `git diff --check -- backend openspec/changes/add-enterprise-organization` | 通过，无输出（只读检查） | — |

全量单测两次失败的 HeapAlloc 增量分别为 8,483,040 和 8,647,200 字节，阈值为 8,388,608 字节。失败位置为 `backend/internal/service/billing_inflight_reservation_test.go:464`；该测试及对应计费文件均未被本次修改。该断言测量整个 service 测试进程强制 GC 前后的堆占用，隔离运行通过；本次没有调整阈值、跳过用例或修改另一代理负责的计费模块，不能据此宣称全量单测通过。

只读调查发现可交给计费代理的具体线索：`billing_inflight_reservation_test.go:77` 直接构造 GatewayService，未初始化倍率 resolver/cache；`gateway_usage_billing.go:20` 在 resolver 缺失时每次重新创建；`user_group_rate_resolver.go:25` 在 cache 为 nil 时创建带后台 janitor 的 go-cache。因此该用例约 20,100 次估算会创建同量临时缓存，finalizer 与 janitor 的异步退出可能影响测试末尾立即采集的 HeapAlloc。这是有源码支持的候选原因，未做堆分析，不认定已证明具体存活对象；A2/A3 不在这条调用链中。

第一次全量集成耗时 31.667s，在两项既有时间敏感用例失败：`TestAccountRepoSuite/TestListWithFilters_SortByCurrentUpstreamBillingRateDuringPeak` 与 `TestConcurrencyCacheSuite/TestCleanupExpiredAccountSlots`。随后两项隔离复跑通过（5.775s），再次执行不带过滤的全量集成通过。前者混用宿主与 PostgreSQL 时间构造高峰窗口，后者混用宿主与 Redis 时间构造过期槽位；失败时的时钟差未直接采集，因此仅记录为瞬时失败，不认定具体根因。这两类数据未被新增测试写入，且其源文件无本次修改。

早期并行定向编译曾因磁盘空间不足退出，已收拢为串行验证；最终编译与新增测试均正常。完整命令、退出码、耗时及日志路径记录在 `/tmp/enterprise-management-validation.json`；最终全量集成日志为 `/tmp/enterprise-management-integration-retry.log`，两轮全量单测日志为 `/tmp/enterprise-management-unit.log` 和 `/tmp/enterprise-management-unit-retry.log`。新增用例的逐项 PASS 记录在 `/tmp/enterprise-management-new-unit.log`、`/tmp/enterprise-management-new-integration.log`。

## 风险与边界

1. 按决议，平台自动封禁可以使 active_owner_count 变为 0，需要管理员恢复或补充负责人。
2. 审计是封禁后的独立尽力写入，成员归属以成功封禁后的读取为准；查询或写入故障仅记 ERROR，不保证与封禁原子提交。
3. 延迟双删沿用现有能力：慢于第二次删除的旧读取、进程退出或缓存失效故障仍可能留下旧快照，受现有 L1/L2 TTL 限制；不承诺线性撤权。
4. 全量单测尚有上述既有内存阈值失败，需要计费模块协调者继续核查完整 service 测试进程的堆占用；本次 A2/A3 新增回归均已通过。
