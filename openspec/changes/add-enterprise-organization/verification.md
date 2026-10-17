# 验证记录（2026-10-18，提交 2afe6f631 及之后的 lint 修复）

## 自动化

| 检查 | 结果 |
|---|---|
| `go build ./...`、`go vet ./internal/...`、`gofmt -l` | 通过 / 无输出 |
| `go test -tags=unit ./...`（全部包） | 通过 |
| `go test -tags=integration ./internal/repository/... -count=1`（全量，testcontainers PostgreSQL 18 + Redis 8） | 通过 |
| `golangci-lint run ./...`（v2.14.0，与 CI 一致，`--max-issues-per-linter=0 --max-same-issues=0`） | 0 issues（修复了 14 处 errcheck） |
| 前端 `pnpm typecheck`、`lint:check`、`test:run`、`check:i18n` | 通过，388 个文件、3037 项测试 |

基线对比：在原始提交 `b0a02a330` 上跑同一套全量集成测试通过，用于识别新代码引入的测试数据污染（已修复两处：企业集成测试提交的分组 / usage_logs / 账号未清理）。

已知不稳定用例：`TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 断言整个 service 测试进程在 20000 次估算前后的 HeapAlloc 增量 < 8 MiB。多个代理并行跑测试、机器高负载时曾以 1%–3% 超出阈值失败；在干净环境中，合并后代码与原始代码完整跑 service 包各两次均通过，单独运行也通过。该用例覆盖的估算路径未被本变更修改。

## 端到端冒烟（真实后端 + PostgreSQL + Redis + 本地 mock 上游）

12 个场景、55 项断言全部通过：创建企业与初始 owner、充值与流水、部门与员工、登录与 `/auth/me` 企业身份、分组池与白名单、无分组 Key 拒绝、员工调用扣企业余额且个人余额不变、`usage_logs` 企业与部门归属、成员 / 部门限额 429（含 `Retry-After` 与 metadata）、白名单外分组 403、三种角色使用记录范围、成员与企业停用、个人钱包拦截、企业余额耗尽 403、解散企业。

终审修复合并后，在最终构建上追加计费回归 10 项全部通过：扣款等于新增 usage 之和且个人余额不变；限额收紧立即 429、放开立即恢复（缓存代际失效）；余额清零 403、充值立即恢复（余额缓存与原子预留）；`/v1/usage` 不含 `account_cost`。

## 浏览器走查（ego-browser）

owner：概览、成员、使用记录（成员 / 部门排行、趋势图单点可见）、顶栏企业余额。member：余额不可见、仅概览 / 部门 / 限额 / 分组 / 使用记录 Tab、`/redeem` 被拦截回「我的企业」。admin：企业列表（格式化时间）、详情（统计卡图标、删除与刷新并排、余额流水等 Tab）。

## 审查轮次

1. 设计漏洞审查 → `review-fixes.md`（R1–R20、X1、C1、F2/F3），由 FA / FB / W4 修复。
2. 前后端契约审查 → C1–C3、F1–F6、E1–E2。
3. 终审：权限与数据泄露（无严重问题，A1–A3 已修复）；计费正确性与并发（B1–B8 已修复，见 `final-billing-fixes-report.md`）。

## 验证边界

- mock 上游只验证 OpenAI Chat Completions 非流式路径；WebSocket、Live、Grok 实时语音、异步视频、Gemini 等入口由单元 / 集成测试覆盖，未在真实上游联调。
- 支付渠道未配置真实服务商，钱包拦截在支付关闭与开启两种开关下以接口返回验证。
- 认证缓存延迟双删、预留关闭或 Redis 故障时放行等残余风险见 `design.md` Risks。
