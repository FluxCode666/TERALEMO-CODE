企业功能 W3 后端计费运行时交付报告（2026-10-17）

1. 交付与提交

工作区仅为 `/Users/duegin/project/TERALEMO-CODE-wt-billing`，分支 `feature/enterprise-billing`，基线 `b43adc76c`（包含数据层 `e7854119f`）。实现 W3 3.1–3.11；提交哈希见最终交付消息。未操作主仓库，未 push、切换分支或 rebase。未实现 W2 管理接口、使用记录查询、/auth/me 或个人钱包入口拦截。

2. 热路径与非企业兼容性

- `User.Enterprise` 作为请求身份快照；认证快照升级 v25，深拷贝部门与分组列表，旧 v24 缓存拒绝使用。API Key 仓储三种加载入口补充成员上下文；面板创建、改绑、候选分组和模型广场可见分组通过 EnterpriseRepository 补加载。没有扩展 UserRepository 等已有大接口。
- 配置企业分组池时替代个人授权，否则保持个人规则；之后与企业白名单相交。订阅分组仍需本人订阅，并通过企业限制。标准与 Google 原生鉴权、动态及链式 fallback、无组旧 Key 均覆盖。
- 企业余额分支读取 `billing:ent_balance:{id}` 和三级限额 hash；缓存 miss 以 singleflight 合并回源，限额批量读取。缺任一级行、错误 scope、损坏限额和 Redis/DB 双故障均拒绝。日、周一开始自然周、自然月过期用量按零；累计不重置。
- 企业余额模式跳过个人余额；个人平台配额、Key 总额度/限速和 RPM 继续生效。订阅不扣企业余额、不累加企业限额。现有在途预留开启时改用 `ent:{id}`，支持续期；固定费用同样使用企业钱包并检查已知费用。
- 非成员保持原个人余额、订阅、预留和限额路径，不读企业余额或企业限额缓存/数据库。认证缓存命中不新增数据库查询；认证缓存 miss 为判定成员身份增加一次上下文查询，企业上下文最多三次查询。冷路径批量图片与 legacy UsageService.Create 额外查成员。
- 个人结算指纹完全沿用原拼接顺序与原始金额，只在 EnterpriseID 非零时追加企业字段；固定黄金哈希测试覆盖有/无 payload 和第九位金额边界。
- 企业结算金额先统一量化至 8 位，企业余额与三级用量同一事务、同一幂等键。事务后只同步企业缓存，异步根据事务 RETURNING 余额重建扣前余额；严格 `old > threshold && new <= threshold` 时向所有 active owner 去重发送邮件，复用 EmailService，失败记录日志。

3. 加锁顺序

每个 Apply 先占用 `usage_billing_dedup` 幂等键。原有效分支顺序为订阅 → 个人 users → api_keys → accounts。

企业余额分支固定为 `dedup → enterprises → enterprise_quotas（按 id 升序）→ api_keys → accounts`，不锁个人 users；企业费与个人费/订阅费混用在任何资金写入前拒绝。订阅分支保留 `dedup → user_subscriptions → api_keys → accounts`，不锁企业或限额。

三级限额使用单条 SQL 的 `MATERIALIZED` CTE，先 `ORDER BY id FOR UPDATE` 全量锁定需要的行，再执行 UPDATE；不依赖 UPDATE 执行计划的行顺序。COUNT 守卫校验全部预期 scope，缺行时整体回滚企业余额与幂等占位；无部门成员只处理企业、成员两级。W1 成员仓储采用 enterprises → users 顺序；企业结算没有追加 users 锁，从而避免形成 enterprise/user 反向依赖。W2 后续管理事务仍须保持企业行优先顺序。两成员 16 笔并发集成验证无死锁且总额一致。

4. 新错误与协议映射

运行时 Go 错误统一定义在 `service/enterprise_billing_errors.go`，使用 `ErrEnterpriseBilling*` / `ErrEnterpriseGateway*` 前缀，与管理侧变量隔离。

| 错误码 | HTTP / 行为 |
| --- | --- |
| ENTERPRISE_DISABLED、ENTERPRISE_MEMBER_DISABLED、ENTERPRISE_GROUP_NOT_ALLOWED | 403 |
| ENTERPRISE_BALANCE_INSUFFICIENT | 403 |
| ENTERPRISE_QUOTA_EXCEEDED、DEPARTMENT_QUOTA_EXCEEDED、MEMBER_QUOTA_EXCEEDED | 429；Retry-After；metadata 含 scope、window、window_resets_at；累计窗口无重置时间 |
| ENTERPRISE_FEATURE_UNSUPPORTED | 403；批量图片提交 |
| BILLING_SERVICE_ERROR | 503；缺限额、缓存/数据库不可用、企业 legacy 事务不可用 |

所有调用 billingErrorDetails 的网关入口均保留企业错误码与 metadata；Anthropic、OpenAI、Responses、Google 的原 JSON 外壳保持。Google 鉴权通过 ErrorInfo.reason 返回企业码。流式开始后沿用现有 SSE error 或 Responses response.failed 终止事件，附带企业 metadata，不改已提交的 HTTP 状态。

5. 验收

环境：Go 1.27.2 / darwin arm64。以下命令均在 `backend/` 执行，最终退出码均为 0。

| 命令 | 结果 | 命令墙钟耗时 |
| --- | --- | --- |
| `go build ./...` | 通过 | 6.41 秒 |
| `go vet ./internal/...` | 通过 | 1.49 秒 |
| `go test -tags=unit ./...` | 通过；57 个包成功，60 个包无测试，0 失败 | 219.27 秒 |
| `go test -tags=integration ./internal/repository/... -run 'Enterprise\|UsageBilling\|APIKey' -count=1` | 通过；repository 包 7.110 秒，使用 Docker PostgreSQL / Redis | 13.77 秒 |
| `golangci-lint run ./...` | 本机未安装 golangci-lint，按条件跳过 | — |

`git diff --check` 通过；71 个变更 Go 文件均通过 gofmt 检查。wire 已重新生成；本次未修改 Ent schema，因此无需再次生成 Ent。

测试涵盖 CanBindGroup 表驱动、订阅组与候选分组、v25 快照往返和旧版拒绝、标准/Google 中间件、动态 fallback、无组 Key、企业余额/三级四窗口/自然月、缺行及缓存兜底、个人规则保留、跨成员预留与续期、事务幂等/回滚/透支/并发、订阅豁免、四条 usage_logs INSERT、通知阈值/并发去重，以及两种 WS ingress 的身份变化与部门归属。

验证中修复了测试夹具问题：新日志测试 request_id 超过 64 字符；新增 enterprise_members 外键使旧 suite 的 DELETE users 失败（先清理成员并断言清理成功）；旧日志参数断言使用末尾相对位置，新列追加后改为原字段的固定列索引；新增 sqlmock 测试补齐连接关闭预期；OpenAI 测试改用测试价格源已有的模型，并增强实际金额及个人钱包零扣减断言。没有降低原断言要求。

首次全量单测的既有内存上限测试曾超出 8 MiB 阈值；该测试单独连续运行三次及最终完整单测均通过，未修改其实现或阈值。

6. 偏差、路径审计与边界

- 企业在途预留启用时，Redis 不可用或首次估算超余额直接拒绝；个人原 fail-open 和首次预留行为保持。
- WS 后续轮次刷新企业上下文，并在上游调用前检查企业余额、限额、状态和分组。部门/白名单变化按该轮新快照结算；加入、移出、换企业、重新加入生成新 MemberID 或已有企业身份刷新失败时要求重连，以重新建立正确的钱包预留。非企业→非企业保持旧用户快照；唯一额外读例外是无组个人 WS 的后续轮次增加一次认证缓存查询，以检测新加入企业。
- 已配置企业分组限制时，无组旧 Key 被拒绝，需要重绑允许分组；未配置限制的企业与非企业原样处理。
- EnterpriseRepository 的上下文 SQL 从 PostgreSQL ANY 数组改为参数化 IN，保持最多三次查询，并让既有 SQLite 仓储/handler 测试使用相同真实加载路径。
- legacy postUsageBilling 的企业余额模式明确返回 ErrEnterpriseBillingLegacyUnsupported；UsageService.Create 的个人扣费旁路也在写日志/扣款前拒绝企业成员。订阅 legacy 保留原行为。
- 全仓 `INSERT INTO usage_logs` 生产 SQL 共四处，均在 repository/usage_log_repo_insert.go：批量主写、批量 best-effort、单条 RETURNING 与单条无 RETURNING fallback；列清单、SELECT/RETURNING、类型表及参数全部补企业/部门列，并对四入口实际写库验证。
- 正常 Gateway、OpenAI、固定费用与 cyber 复用统一 RecordUsage，均写请求企业快照；WS 正常及 cyber 使用该轮快照。OpenAI Live 额外补齐 handler identity → LiveCallRecord → Redis → finalizeLiveCall 的企业/部门链路；Live 原有费用为零的 TODO 保持，本次未新增 Live 定价。
- 批量图片新提交同时校验认证快照和数据库成员关系，企业请求在建任务/冻结个人钱之前拒绝。历史个人批任务继续使用原个人冻结资金和原日志归属，不根据后来加入的企业回填。
- 邮件为异步 best-effort，基于提交后的余额检测单次下穿；幂等重放不调用 finalize。不增加持久化通知 outbox；SMTP 失败会记录日志。
- 没有发现其他未处理的生产 usage_logs INSERT。W2 的认证主动失效、管理侧依赖注入合并与最终全链路联调仍由协调者负责；本分支不代做 W2/W4 或部署。

完整文件清单（含本报告与 W3 tasks 勾选）：

- `backend/cmd/server/wire_gen.go`
- `backend/internal/handler/batch_image_handler.go`
- `backend/internal/handler/enterprise_billing_error.go`
- `backend/internal/handler/enterprise_billing_error_test.go`
- `backend/internal/handler/gateway_handler.go`
- `backend/internal/handler/gateway_handler_chat_completions.go`
- `backend/internal/handler/gateway_handler_responses.go`
- `backend/internal/handler/gateway_systemone.go`
- `backend/internal/handler/gateway_web_search.go`
- `backend/internal/handler/gemini_cached_contents_handler.go`
- `backend/internal/handler/gemini_v1beta_handler.go`
- `backend/internal/handler/grok_audio.go`
- `backend/internal/handler/grok_media.go`
- `backend/internal/handler/openai_alpha_search.go`
- `backend/internal/handler/openai_chat_completions.go`
- `backend/internal/handler/openai_embeddings.go`
- `backend/internal/handler/openai_gateway_count_tokens.go`
- `backend/internal/handler/openai_gateway_handler.go`
- `backend/internal/handler/openai_gateway_ws_enterprise_test.go`
- `backend/internal/handler/openai_images.go`
- `backend/internal/handler/openai_live.go`
- `backend/internal/handler/stream_error_event.go`
- `backend/internal/repository/api_key_repo.go`
- `backend/internal/repository/api_key_repo_enterprise_integration_test.go`
- `backend/internal/repository/api_key_repo_enterprise_unit_test.go`
- `backend/internal/repository/enterprise_billing_cache.go`
- `backend/internal/repository/enterprise_billing_cache_test.go`
- `backend/internal/repository/enterprise_context_repo.go`
- `backend/internal/repository/enterprise_usage_billing.go`
- `backend/internal/repository/enterprise_usage_billing_integration_test.go`
- `backend/internal/repository/enterprise_usage_billing_unit_test.go`
- `backend/internal/repository/gateway_cache.go`
- `backend/internal/repository/gateway_cache_live_test.go`
- `backend/internal/repository/usage_billing_repo.go`
- `backend/internal/repository/usage_log_repo_insert.go`
- `backend/internal/repository/usage_log_repo_insert_shape_unit_test.go`
- `backend/internal/repository/usage_log_repo_request_type_test.go`
- `backend/internal/repository/usage_log_session_id_unit_test.go`
- `backend/internal/repository/user_repo_apikey_group_filter_integration_test.go`
- `backend/internal/repository/wire.go`
- `backend/internal/server/middleware/api_key_auth.go`
- `backend/internal/server/middleware/api_key_auth_enterprise_test.go`
- `backend/internal/server/middleware/api_key_auth_google.go`
- `backend/internal/service/api_key_auth_cache.go`
- `backend/internal/service/api_key_auth_cache_impl.go`
- `backend/internal/service/api_key_enterprise_access_test.go`
- `backend/internal/service/api_key_service.go`
- `backend/internal/service/balance_notify_service.go`
- `backend/internal/service/batch_image_enterprise_test.go`
- `backend/internal/service/batch_image_public.go`
- `backend/internal/service/billing_cache_service.go`
- `backend/internal/service/billing_inflight_reservation.go`
- `backend/internal/service/enterprise_balance_notify.go`
- `backend/internal/service/enterprise_balance_notify_test.go`
- `backend/internal/service/enterprise_billing_cache_service.go`
- `backend/internal/service/enterprise_billing_cache_service_test.go`
- `backend/internal/service/enterprise_billing_errors.go`
- `backend/internal/service/enterprise_gateway_context.go`
- `backend/internal/service/enterprise_gateway_context_test.go`
- `backend/internal/service/enterprise_usage_billing_test.go`
- `backend/internal/service/gateway_scheduling.go`
- `backend/internal/service/gateway_usage_billing.go`
- `backend/internal/service/openai_gateway_usage.go`
- `backend/internal/service/openai_live.go`
- `backend/internal/service/openai_live_lifecycle_test.go`
- `backend/internal/service/openai_live_types.go`
- `backend/internal/service/usage_billing.go`
- `backend/internal/service/usage_log.go`
- `backend/internal/service/usage_service.go`
- `backend/internal/service/user.go`
- `backend/internal/service/wire.go`
- `openspec/changes/add-enterprise-organization/tasks.md`
- `openspec/changes/add-enterprise-organization/w3-backend-billing-report.md`
