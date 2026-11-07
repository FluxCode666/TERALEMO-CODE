# 验证手册：创作页（Creation Studio）

> 状态：已冻结（2026-11-07，契约 `docs/CREATION_STUDIO.md` v1.1）。
> 本文件既是验收矩阵，也是上线前证据索引。所有「待实现」必须替换为可重复执行的测试名、命令输出、SQL、日志查询或截图路径；只写「人工验证通过」不算证据。
> 证据放在 `docs/evidence/creation-studio/<YYYY-MM-DD>/`。**不要**提交含真实 Key、真实提示词或个人信息的截图。

## 1. 关键不变量（违反即阻断上线）

- **I1** 生图只走 `POST /v1/images/{generations,edits}/async`，不存在同步生图路径。
- **I2** 提交只通过进程内重放进入网关，不存在绕过鉴权或计费准入的第二条通道；内层路径必须命中 7 条白名单之一。
- **I3** 生图预检只在带创作页重放标记时执行；不带标记的 API 请求行为与升级前完全一致。
- **I4** 余额口径：≤0 或低于最低保留 → 403 `INSUFFICIENT_BALANCE`；余额为正但不足 → 402 `insufficient_balance`；配额 → 429。图片与视频一致。
- **I5** 视频计费：提交时冻结、成功封顶、失败释放、不扣成负数（沿用既有实现）。
- **I6** 创作页产生的任务出现在 `/api/v1/user/async-tasks`；图片任务的 `result_urls` 包含全部结果。
- **I7** API Key 明文与上传对象 key 不出现在面板响应和日志中；提示词、上传图片与 multipart 正文不出现在审计表中。
- **I8** 对象存储未开启时：生图与上传不可用，纯文生视频可用；不创建任务、不写 Redis。
- **I9** 上传只能由其所有者在有效期内使用。
- **I10** 既有行为零回归：`go test -tags=unit ./...` 全绿，能力表抽取逐项等价，`wire_gen.go` 无 diff，没有迁移文件。

## 2. Requirement → Evidence 矩阵

状态词：`待实现` / `通过` / `失败` / `豁免（附批准链接）`。

### 2.1 creation-studio-submission

| ID | Requirement | 必备自动化证据 | 补充证据 | 状态 |
| --- | --- | --- | --- | --- |
| S01 | 进程内重放复用网关链路 | B15：fake dispatcher 断言内层 Authorization、RemoteAddr、XFF、重放标记；外层 Cookie 未透传 | 实机：Ops 中可见内层请求 | 待实现 |
| S02 | 路径白名单 | B10：白名单外路径和含 `/` 的 Gemini 模型名报错 | 代码搜索：只有一处构造内层请求 | 待实现 |
| S03 | Key 归属 | B15：他人 Key → 404 `CREATION_KEY_NOT_FOUND` | — | 待实现 |
| S04 | 原生映射 · 图片 | B10 golden：OpenAI/Grok 的生成与编辑；编辑走 JSON data URL | 实机 Q5 | 待实现 |
| S05 | 原生映射 · Seedance | B10 golden：content 顺序与 role、显式参数 | 实机 Q5 | 待实现 |
| S06 | 原生映射 · Gemini | B10 golden：内联 bytesBase64Encoded、sampleCount=1、路径 | 实机 Q5 | 待实现 |
| S07 | 原生映射 · MiniMax v1/v2 | B10 golden：v1 使用 legacy 字段；H3 走 `/v2` 且使用 content | 实机（v2 账号） | 待实现 |
| S08 | 原生映射 · Grok 视频 | B10 golden：`image.url`、duration、resolution | 实机（Grok 账号确认 `image` 字段被接受） | 待实现 |
| S09 | 不构造跨协议数量字段 | B10 断言 | — | 待实现 |
| S10 | 目录 ID 一致 | B1 四种前缀；B15 响应中的 `catalog_id` 等于目录记录 `id` | 实机 `/async-tasks` 命中 | 通过 |
| S11 | 错误归一 | B14 十条规则表驱动测试 | curl 样本（脱敏） | 待实现 |
| S12 | 生图预检不影响 API 用户 | B13：无标记时 fake billing 调用次数为 0 | 实机：直接 curl `/v1/images/generations/async` 在余额不足时仍返回 202（与升级前一致） | 待实现 |
| S13 | 生图预检 403/402/429 | B13：余额 0、余额不足、订阅超限、Key 额度、企业钱包；失败时不创建任务；余额够 1K 但不够 4K 时选 1K 能通过 | 实机 Q6 | 待实现 |
| S14 | 预检临时预留立即释放 | B13：Release 被调用 | Redis 中无残留在途 | 待实现 |
| S15 | 低于最低保留余额的归一 | B14 规则 1；B13 预检直接输出 | 实机：设置 `minimum_balance_reserve`，余额介于 0 与保留之间时提交视频 → 403 `INSUFFICIENT_BALANCE` | 通过 |
| S16 | Key 明文不泄露 | B15 canary | 日志 grep `sk-canary-creation` 无结果 | 通过（按契约 v1.2；R2-04 用户已接受现状，见 §6.12） |
| S17 | 审计正文省略 | B17 | `SELECT request_body FROM audit_logs WHERE action LIKE 'user.creation.%' LIMIT 5` | 通过 |
| S18 | 递归与超时防护 | B15 | — | 待实现 |
| S19 | 企业成员可用 | B13 企业用例 | 实机：企业成员提交视频成功，用量记入企业 | 通过 |
| S20 | `result_urls` | B2 仓储与四个 handler 用例 | 实机：n=2 时 `/async-tasks/{id}` 返回 2 个链接 | 通过 |

### 2.2 creation-studio-uploads

| ID | Requirement | 必备自动化证据 | 补充证据 | 状态 |
| --- | --- | --- | --- | --- |
| U01 | 存储关闭 → 404 | B15 Upload 用例 | 实机 Q6 | 通过 |
| U02 | 大小上限 | B7：11 MiB → 413；路由 body limit | — | 通过 |
| U03 | 类型嗅探 | B7：gif、svg、改了扩展名的文件 → 415 | — | 待实现 |
| U04 | 像素上限 | B7：5000 像素宽、16 像素以下 → 422；webp 可解码 | — | 待实现 |
| U05 | 每用户 100 张 | B7 第 101 张 → 429 | — | 待实现 |
| U06 | 对象路径按用户隔离 | B7 key 格式断言 | 存储桶中抽查路径 | 待实现 |
| U07 | 有效期与 TTL | B8 TTL；B7 `expires_at = min(24h, presign)` | Redis `TTL creation_upload:*` | 待实现 |
| U08 | 提交时归属与过期 | B7 Resolve：他人 → NotFound，剩 14 分钟 → Expired | 实机 | 通过 |
| U09 | 读回字节不走 HTTP | B6 fake Reader；Gemini 与编辑映射调用 ReadObject | 自建 MinIO 环境下 Gemini 首帧成功 | 待实现 |
| U10 | 审计不记录 multipart | B17 | SQL 抽查 | 通过 |

### 2.3 creation-studio-model-catalog

| ID | Requirement | 必备自动化证据 | 补充证据 | 状态 |
| --- | --- | --- | --- | --- |
| M01 | 只下发该分组的模型 | B11 快照 | 页面截图 | 通过 |
| M02 | kind 只有四种 | B11 遍历断言 | — | 待实现 |
| M03 | 每协议键集合与契约 §3.3 一致 | B11 快照逐行核对 | — | 待实现 |
| M04 | 能力表同源且等价 | B4、B5 | 抽取 diff 审阅 | 待实现 |
| M05 | 账号能力覆盖 | B11 | 管理端配置截图 | 待实现 |
| M06 | 存储关闭时的可用性 | B11 | 页面截图 | 通过 |
| M07 | 无账号、composite、无分组 | B11 | — | 待实现 |
| M08 | 多账号 partial | B11 | — | 待实现 |

### 2.4 estimate

| ID | Requirement | 必备自动化证据 | 补充证据 | 状态 |
| --- | --- | --- | --- | --- |
| E01 | 无持久副作用 | B12 fake 调用次数 0 | Redis/DB 快照对比 | 待实现 |
| E02 | 视频估算 = 冻结额 | B12 | §4 SQL 核对 | 通过 |
| E03 | 图片估算 = 预检金额 = 所选档位实际计价 | B12：`EstimateCreationImageCost` 判档表驱动测试、与 `calculateOpenAIImageCost` 等价测试；B13：预检金额 = `/estimate` 的 `estimated_cost` | §4 第 4 步 SQL 核对 | 通过 |
| E06 | Grok 生图档位与所选值一致 | B10：Grok 请求体写 `size` 不写 `resolution`；B12：`1k`→1K | 实机：选 1k 后 `usage_logs` 中的计费档为 1K | 待实现 |
| E04 | 未定价 | B12：`priced=false`；提交 → 400 `video_pricing_not_configured` | 页面截图 | 待实现 |
| E05 | sufficient 三种范围 | B12 | — | 待实现 |

### 2.5 creation-studio-console

| ID | Requirement | 必备自动化证据 | 补充证据 | 状态 |
| --- | --- | --- | --- | --- |
| C01 | 路由与鉴权 | Q4 | 截图 | 通过 |
| C02 | 三处菜单、企业成员可见 | Q4 | 截图 | 通过 |
| C03 | 描述符渲染 | Q2 | 截图 | 通过 |
| C04 | 模式切换不影响历史筛选 | Q1 | 实机 | 通过 |
| C05 | 表单与上传校验 | Q1、Q3 | 截图 | 通过 |
| C06 | 提交互斥 | Q1 | 实机 | 通过 |
| C07 | 轮询与 refresh 节流 | Q1（fake timer） | — | 通过 |
| C08 | 加载更多 | Q1 | — | 通过 |
| C09 | 全部结果网格 | Q3 | 实机 n=4 截图 | 通过 |
| C10 | 本地快照与复用参数 | Q1（useCreationSnapshots）、Q3 | 实机：复用未过期参考图与已过期参考图各一次 | 通过 |
| C11 | 结果链接安全 | Q3 | — | 通过 |
| C12 | 错误映射 | Q3 | Q6 截图 | 通过 |
| C13 | i18n | `pnpm check:i18n` | — | 通过 |
| C14 | 无障碍 | Q2、Q3 角色断言 | 读屏走查 | 通过 |
| C15 | 暗色与移动端 | — | Q7 截图 | 通过 |

## 3. 命令

```bash
# backend
cd backend
go build ./...
go test -tags=unit ./internal/service/... ./internal/handler/... ./internal/repository/... ./internal/server/... -count=1
go test -tags=unit ./... -count=1
~/go/bin/golangci-lint run --timeout=30m --max-issues-per-linter=0 --max-same-issues=0 ./...
git diff --stat -- cmd/server/wire_gen.go migrations go.mod      # 期望为空

# frontend
cd frontend
pnpm lint:check
pnpm typecheck
pnpm test:run
pnpm check:i18n
```

## 4. 计费一致性手工核对（上线前必做，测试环境）

1. 视频：用同一组参数先调 `/creation/estimate`，再调 `/creation/tasks`，记录两次的 `estimated_cost`。
2. `SELECT id, task_id, reserved_amount, state FROM video_billing_reservations ORDER BY id DESC LIMIT 1;`，三个值应当相等。
3. 任务完成后确认实际扣费 ≤ `reserved_amount`；构造一次失败的提交，确认 `state=released`。
4. 生图（按所选档位）：
   - `size=1024x1024, n=2`：`/estimate` 的 `estimated_cost` 应等于「1K 档单价 × 2 × 图片倍率」，`selected_tier=1K`；
   - 余额设为略低于该值 → 提交返回 402，Redis 中没有新增 `image_task:*`；余额设为 0 → 403；
   - 余额设为「够 1K 不够 4K」→ 选 1K 提交成功；
   - 成功后 `SELECT actual_cost, image_size FROM usage_logs ORDER BY id DESC LIMIT 1;`：`image_size` 与 `selected_tier` 相同时，`actual_cost` 应等于 `estimated_cost`；若上游返回了更高档尺寸，记录这一实例（已知限制，契约 §11.3）。
   - Grok：选 `1k`，`usage_logs.image_size` 应为 1K。
5. 设置 `billing.minimum_balance_reserve=1`、余额 0.5：生图与视频提交都返回 403 `INSUFFICIENT_BALANCE`。
6. 直接用 API Key curl `/v1/images/generations/async`，余额不足时行为与升级前一致（202，之后任务失败）。

## 5. 发布与回滚

- 没有开关。回滚即回退代码；已提交的任务仍可在 `/async-tasks` 查询（执行器与目录格式向后兼容，`result_urls` 为可选字段）。
- 发布说明必须写明：为存储桶 `<prefix>creation-uploads/` 配置生命周期规则（建议 3 天过期）；存储端点需能被视频厂商从公网访问。

## 6. QA 证据（由 qa 角色填写）

> 格式：`### <ID> <标题>`，下面写命令或截图路径和结论。截图命名：`<ID>-<场景>-<zh|en>-<light|dark>.png`。

### 6.1 表单与上传校验

2026-11-07，round1，仅执行 Q1–Q4；§2 的“通过”仅表示本轮 Vitest 自动化通过，不代表已完成补充实机证据。Q5–Q7、真后端、截图、读屏、计费 SQL 均未执行；后端 S/U/M/E 项不依据前端 mock 结果改成通过。

环境：macOS，本地 Node v22.23.1、pnpm 10.30.2、Vitest 2.1.9、jsdom；时间相关用例固定时钟 `1794038400` Unix 秒，存储用例每例清空 localStorage，组件卸载、定时器与 spy 清理。测试使用 `fixtures.ts`；页面隔离 creation API、keys API、async-tasks API、auth/app store、AppLayout 外壳，并 stub Lightbox（断言真实 TaskCard 发来的 items/index，不将灯箱内部算已覆盖）；表单、估算、上传、提交、轮询、快照及其余页面内部 UI 组件运行真实实现。路由测试导入真实 `router/index.ts` 与真实 guard，不复制模拟 guard；替换目的页面、store、导航加载/预取和批量图片访问依赖，避免无关网络副作用，保留真实 AppSidebar 导航规则。

证据等级 A（本地自动化，不是 CI 或真实后端证据）。日志依本轮任务要求存放在 `~/creation-studio/qa/`，不放 `/tmp`，不提交日志或真实数据。

| 范围 | 文件（均在 frontend/src 下） | 用例 | 通过 | 失败 |
| --- | --- | --- | --- | --- |
| Q1 / F7 | `__tests__/creation-studio/useCreationForm.spec.ts` | 15 | 12 | 3 |
| Q1 / F9 | `__tests__/creation-studio/useCreationEstimate.spec.ts` | 8 | 7 | 1 |
| Q1 / F10 | `__tests__/creation-studio/useCreationSubmit.spec.ts` | 7 | 7 | 0 |
| Q1 / F11 | `__tests__/creation-studio/useCreationFeed.spec.ts` | 7 | 7 | 0 |
| Q1 / F8 | `__tests__/creation-studio/useCreationUploads.spec.ts` | 15 | 10 | 5 |
| Q1 / F12 | `__tests__/creation-studio/useCreationSnapshots.spec.ts` | 8 | 7 | 1 |
| Q2 | `__tests__/creation-studio/ParamField.spec.ts` | 9 | 7 | 2 |
| Q3 | `__tests__/creation-studio/CreationStudioView.spec.ts` | 96 | 80 | 16 |
| Q4 | `__tests__/integration/creation-studio.spec.ts` | 9 | 9 | 0 |
| 合计 | 9 个 spec，另有 `helpers.ts`、`errorCases.ts` 两个测试辅助文件 | 174 | 146 | 28 |

门禁（执行目录 `frontend/`，不使用带 --fix 的 lint）：

| 命令 | 结果 | 日志 |
| --- | --- | --- |
| `pnpm test:run src/__tests__/creation-studio src/__tests__/integration/creation-studio.spec.ts` | 146 通过、28 失败，9 文件中 3 通过、6 失败 | `~/creation-studio/qa/round1-focused.txt` |
| `pnpm test:run` | 3842 通过、28 失败，454 文件中 448 通过、6 失败；失败均在本轮新增测试内 | `~/creation-studio/qa/round1-test-run.txt` |
| `pnpm lint:check` | 退出码 0，0 error、0 warning | `~/creation-studio/qa/round1-lint-check.txt` |
| `pnpm typecheck` | 退出码 0，0 类型错误；项目 tsconfig 排除测试目录，不能据此宣称测试文件已获 tsc 检查 | `~/creation-studio/qa/round1-typecheck.txt` |

未 skip、only、todo；不修改业务代码、既有测试或 tasks.md，不提交 Git。早期测试环境修正日志为 `round1-focused-initial.txt`、`round1-focused-refined.txt`，不将其中 matcher/spy 兼容问题计入业务缺陷。

C05 已通过部分：提示词空/全空白/8001 禁用、8000 Unicode rune 合法；文件 MIME/10MiB 前端拦截；413/415/422/429 字段文案；900 秒有效/899 秒过期；所有失败不写成功快照。U02–U05 的后端嗅探、像素校验和上传数量真限额仍待 B7/Q6。

以下为本轮 28 个失败断言归并后的缺陷。所有权均为 **frontend**，均保留失败用例，不以 skip 或错误预期迁就现状。

| 缺陷 | 现象 | 可复现用例名 | 契约/规范依据 | 建议修法 |
| --- | --- | --- | --- | --- |
| R1-01（高） | 上传成功对象变为 ready，但页面/computed 仍认为上传中，带图提交被阻断 | `publishes reactive ready/uploading/imageRefs after upload resolves`；`successful upload transitions the rendered thumbnail to ready and permits submit` | F8；设计 §4.5、§4.7 | startUpload/applyUpload/进度回调始终更新 reactive entry，不再修改被 ref 包装前的原始对象 |
| R1-02 | 未知 kind 残留标签并进入 invalidParamKeys，阻断表单 | `warns and completely skips unknown kinds without blocking submission`；`skips the entire unknown-kind field including its label` | F7/F13；契约 §3.2 | 在默认值、描述符过滤、校验与 ParamField 渲染各入口一致过滤未知 kind |
| R1-03 | integer 的 `2junk`、`1.5` 字符串被 parseInt 截断后当作合法整数 | `rejects invalid integer n=2junk`；`rejects invalid integer n=1.5` | 契约 §5.1、§5.2 V5 | 严格解析纯整数字符串并 Number.isInteger，再校验范围与 step |
| R1-04（高） | 入参变动后的 300ms 窗口旧估算仍能写回，可能错用上一 Key/参数的余额判断 | `invalidates an in-flight result immediately when request inputs change` | F9；契约 §4.2.2 | schedule 立刻递增 generation 并 abort；旧响应不得在防抖期更新 estimate |
| R1-05 | 未定价仅有费用不可用及按钮阻断，缺少 warning callout | `shows pricing warning callout and unavailable cost, disabling unpriced submissions` | 设计 §5、§7.2 | priced=false 时显示 pricingMissing 的 warning，不依赖先发生 submit error |
| R1-06 | 提交 402/403 后费用区未切 insufficient | `402 balance error marks the cost summary insufficient`；403 同名变体 | 设计 §7.2 | 由提交余额错误驱动费用状态并刷新估算，避免继续显示余额充足 |
| R1-07 | retry_after 固定不递减，到点仍没有重试按钮 | `counts down retry_after to zero and restores the retry action` | 契约 §6.1；设计 §5、§7.2 | 按接收时刻计算 deadline，用定时器推进剩余秒数，归零恢复重试并卸载清理 |
| R1-08 | IMAGE_GENERATION_FORBIDDEN 展示 danger，规范要求 warning | `403 'IMAGE_GENERATION_FORBIDDEN' renders errors.'imageForbidden' above submit with 'warning' tone` | 设计 §7.2 | imageForbidden 加入 warning 分类；同时按规范同步生图不可用态 |
| R1-09 | 上传 404 storage-disabled 不刷新 options；提交同码仍显示通用 danger | `upload 404 storage-disabled refreshes options and renders the unavailable upload field`；`submit storage-disabled keeps text video usable with a warning and unavailable upload state` | 契约 §6.1；设计 §4.5、§7.1 | 上传错误通知页面刷新 options，清理图片；提交同码使用上传不可用 warning，保留纯文本视频能力 |
| R1-10 | 上传 413/415/422 错误无 id，aria-describedby 仅指 hint，不关联错误 | `upload 413 CREATION_UPLOAD_TOO_LARGE is displayed inside the image field, not submit callout`；415/422 同名变体 | 设计 §7.1、§10 | 每个错误稳定 id，上传区域 aria-describedby 同时引用 hint/error，并播报错误 |
| R1-11 | 达每日上传数后直接隐藏入口，没有 aria-disabled 状态 | `upload 429 CREATION_UPLOAD_LIMIT_EXCEEDED is displayed inside the image field, not submit callout` | 设计 §4.5、§7.1 | 保留禁用入口并 aria-disabled=true，禁用点击/键盘/拖放，保留既有缩略图 |
| R1-12 | 多文件 pick 同时发起上传而非逐张 | `uploads multiple files sequentially with observable progress` | 契约 §2.1；F8 | 使用逐张 await 的上传队列，同时保持每张进度/失败重试 |
| R1-13 | 422 的 metadata.max_side_px 被忽略，继续显示 options 上限 | `honors upload invalid-image metadata.max_side_px` | 设计 §7.1 | maxSide 优先采用响应 metadata，再回落 upload_limits |
| R1-14 | 上传网络中断映射 upstream 而非 uploadFailed | `keeps retryable 0 failure in place with uploadFailed copy` | 设计 §4.5、§7.1 | 在上传上下文将无响应/超时明确映射 uploadFailed，不改通用提交兜底 |
| R1-15 | 首帧文件校验失败同时出现在尾帧等其它图片字段 | `keeps local validation errors scoped to the originating image role` | 设计 §4.5、§7.1（字段内错误） | field error 持有 role；无 entryId 的预检错误也按 role 过滤/清理 |
| R1-16 | localStorage getItem 抛错时返回空，但不按契约覆盖写回空 envelope | `recovers a storage read exception by writing an empty envelope` | 契约 §8.1 | 读取异常分支也尝试 writeEmpty；写失败仍吞掉，不影响提交 |
| R1-17 | image 描述符 disabled=true 仍提供可用 file input | `disables image upload for a disabled descriptor` | 契约 §3.1、§3.2；设计 §4.5 | ParamField 合并 descriptor.disabled、全局 disabled 和 images.available，禁用入口并显示原因 |
| R1-18 | 网格过滤恶意 URL 后仍向未过滤的灯箱数组传过滤后索引，点击安全图选中了 javascript 项，预览空白 | `blocks javascript result URLs in images, download links and the preview` | 契约 §7.4；设计 §4.9 | 网格、灯箱统一使用过滤后的 URL 列表或稳定原始索引；本轮未发现 javascript 被实际写入 DOM 或执行 |
| R1-19 | 回填参考图触发 img error 后不进入 expired，也不阻断提交 | `resource error on a restored image marks its thumbnail expired` | 设计 §4.5（过期来源③） | 图片 error 事件按 entry id 通知 composable 标过期，清理预览并播报 |
| R1-20 | estimate 400 modelUnavailable / 404 keyNotFound 不静默刷新 options/Key | `estimate 400 CREATION_MODEL_NOT_AVAILABLE silently refreshes options`；`estimate 404 CREATION_KEY_NOT_FOUND silently refreshes keys` | 设计 §7.2 末段 | 监听估算错误 reason，仅刷新对应 options/Key，不出提交 callout |

### 6.2 参数映射（描述符 → 原生请求）

Q3 的 `CreationStudioView uniform request mapping (contract 5.1, not backend 5.4)` 共 18 例通过：OpenAI 图片、Grok 图片、Seedance、Gemini、MiniMax v1、MiniMax H3 v2、Grok 视频各验证默认参数完整请求体（7 例）及图片 `role/upload_id`（7 例）；另验证手工修改尺寸/质量/n（1 例），Seedance/Gemini/MiniMax 的 reference role、数量与顺序（3 例）。每例断言真实 API 边界收到的完整对象或明确字段，未 mock 请求构造函数。

前端保持统一协议字段：Grok 图片仍提交 `params.resolution='1k'`；Seedance watermark 是字符串 `'false'`；整数为数字；images 只有 role/upload_id，不携带 URL、data URL 或字节；estimate 不含 prompt。成功上传的响应性缺陷 R1-01 会阻断正常带图路径，因此图片映射用契约 §8 的真实快照回填有效图独立验证，**不将此视为上传链路通过**。

契约 §5.4 的 Grok resolution→原生 size、Gemini durationSeconds/sampleCount、MiniMax resolution 大写、原生路径/图片内联等全部由后端构造。本轮未运行后端，不用前端测试冒充 B10 golden JSON；M03、S04–S09 保留待实现。

### 6.3 错误分支

`errorCases.ts` 独立从冻结契约 §6.1 抄录 44 个 reason/status→key/tone 场景（含 GROUP/API_KEY/企业/容量别名、未知 4xx/5xx 兜底），没有从生产映射表生成期望。页面逐例真实提交失败，断言 i18n 标题、后端 message、位于提交按钮上方、输入保留、失败不写快照；其中 43 例通过，IMAGE_GENERATION_FORBIDDEN 色调失败（R1-08）。两种 storage-disabled reason 单独走不可用态测试；estimate 错误按费用区/静默刷新另测；上传 413/415/422/429 按字段内错误另测。C12 仍为失败，原因包括 R1-05–R1-11、R1-13–R1-15、R1-20，不只按 key 正确就判通过。

402/403 文案和位置正确，但费用区 insufficient 不正确；429 的 reason/key 正确，但倒计时不正确；404/410 上传过期标记通过；ASYNC_IMAGE_DISABLED 刷新 options 后页面不可用态通过；企业钱包失败无个人充值入口通过。后端 §6.2 归一、真实 402/403/429/410 响应及 Q6 截图未验证。

### 6.4 提交—轮询—展示全链路

本轮是 API 边界 fake 的页面集成，不是真后端端到端。C06 提交互斥、C07 10 秒刷新所有已加载页/隐藏暂停/前台补刷/视频每任务 30 秒节流、C08 加载更多去重与失败保留均通过；相应用例见 `useCreationSubmit.spec.ts`、`useCreationFeed.spec.ts`。页面提交成功的 `inserts a successful task first and persists the submitted snapshot` 验证顶部 processing 卡片、提示词、进度角色及真实 localStorage 快照。

C09 的 `renders all %i result_urls with ordered safe images and downloads` 四例（1–4 张）通过；Feed 对旧记录 result_url 的回落与过期卡片通过。C11 的 DOM 结果链接拦截通过，但灯箱索引不一致导致 R1-18，故整项失败。没有虚构真实完成时间、计费额或真实目录 ID 时间线；S10、S20 等后端目录一致性仍待 B1/B2/B15/Q5。

C01/C02 的 `__tests__/integration/creation-studio.spec.ts` 9 例通过：真实 guard 未登录→`/login?redirect=/creation...`；完整冻结 meta 与路由顺序；普通/管理员×普通/简易模式入口唯一、位置/高亮/aria-current；企业 owner/manager/member 两种模式均可访问。C04 页面模式切换不改历史筛选通过。

### 6.5 复用参数与参考图过期

快照用户隔离、v1 envelope、catalog 优先/task 回落、倒序/100 条/7 天清理、JSON 损坏/错误版本恢复、QuotaExceededError 删最旧一半只重试一次、二次失败不抛错均通过。读取异常未尝试写回空为 R1-16，因此 C10 不通过。

900 秒可用、899 秒过期、30 秒定时检查、回填过期缩略图阻断提交、提交返回 404/410 将本次图片标过期均通过。加载失败触发过期缺失（R1-19），存储切不可用可移除本地缩略图，但上传错误未触发 options 刷新（R1-09）。真实上传归属/Redis TTL/跨用户访问仍待 B7/B8/Q6，不把本地快照验证当作 U08 后端证据。

### 6.6 中英文、暗色与空态截图

本轮 Q3 以真实英文 locale 结合项目现有测试用 messageCompiler 验证文案及插值，未 mock t()。ParamField 单测只断语义键。无 Key、无模型、无视频账号、存储关闭与文本视频可用的 DOM 场景通过；未定价 warning 缺失。未运行中英文/亮暗/移动端截图、读屏或 `pnpm check:i18n`，C13/C15 保留待实现，C14 因上传错误 aria 关联失败为失败。

### 6.7 实机走查记录（ego-browser）

按本轮范围不执行 Q5–Q7，不启动浏览器或其它 agent，不连接真后端。下一轮等待后端完成，再由 orchestrator 另行安排 ego-browser 实机、真实错误分支与截图；本轮不作可上线结论。

### 6.8 round2 回归（2026-11-07）

第一轮 §6.1–§6.7 保留为历史证据；本节覆盖其当前状态。R1-01、R1-02、R1-03、R1-04、R1-05、R1-06、R1-07、R1-08、R1-09、R1-10、R1-11、R1-12、R1-13、R1-14、R1-15、R1-16、R1-17、R1-18、R1-19、R1-20 **全部已修复**：原 174 用例逐项通过，未改变原断言或 skip。新增页面用例 `normalizes panel HTTP 429 string code RATE_LIMITED without reason to retryable rate-limit copy`，断言 warning、rateLimited、保留上游 message、可重试、不出现倒计时。初次回归 9 文件 175/175（`~/creation-studio/qa/round2-focused.txt`）。

新增 `frontend/src/__tests__/creation-studio/AsyncTasksResults.spec.ts`：`makes every safe result_urls image reachable from the async-tasks page`，对应 Q5 实机发现的第二张图片不可达；frontend 已补共享历史页多图链接，新增用例 1/1 通过（`round2-async-results-regression.txt`）。最终定向 **10 文件 176/176**（`round2-focused-final.txt`）。C03/C05/C10/C11/C12/C14 改为通过仅代表前端自动化修复闭环，C14 不包含人工读屏；真实后端错误另见 §6.10，不以 mock 通过替代。

门禁：第一次全量 454 文件、3871/3871（`round2-test-run.txt`）；`round2-lint-check.txt`、`round2-typecheck.txt` 均退出码 0。该全量发生在新增共享历史页回归前；最终门禁另补记录。测试目录仍被 tsconfig 排除，typecheck 不能解释为测试文件全量静态类型验证。

### 6.9 round2 实机环境与主链路

所有运行环境/证据位于 `~/creation-studio/qa/`，不提交仓库；全程使用同一 ego-browser TaskSpace 7 / p1。合成账号 personal/member/empty@csqa.local，不使用真实厂商或真实用户数据。PostgreSQL16、Redis7、MinIO 官方固定版本源码 RELEASE.2025-04-22T22-12-26Z（镜像不可获取，官方 codeload 源码编译 Linux ARM64，alpine3.22 只作运行壳）；MinIO 真实 S3 读写，管理 API 保存配置并测试成功：`evidence/storage-admin-enable.json`、`evidence/MinIO-storage-test.json`。后端默认固定 `go build -o .../runtime/cs-qa-server ./cmd/server` 参数；启动配置仅指向隔离 DB/Redis/假上游。

假上游 `fake-upstream/server.mjs` 支持 OpenAI images generations/edits 的 n 张 b64 或 URL、Seedance 提交/查询和真实首帧 HTTP 拉取、Gemini predictLongRunning/operation、可控处理中/成功/失败。只记录本轮合成提示词，不记录 Authorization、上游凭据或图片字节。图片 PNG1024x1024、Gemini 本机 mp4 3秒；Seedance 公共下载受 HTTPS/public-host 安全规则限制，使用 orchestrator 批准的 MDN CC0 HTTPS flower.mp4（5.055秒）作为返回媒体，不弱化 SSRF。

| Q5 场景 | 真实结果 | 证据（~/creation-studio/qa/ 下） |
| --- | --- | --- |
| 个人文生图 n=2 | 提交→processing→completed，两张 MinIO 图片自然宽度1024均可加载 | `screenshots/Q5-image-n2-estimate-en-dark.png`、`screenshots/Q5-image-n2-completed-en-dark.png`、`evidence/Q5-result-urls-detail.json` |
| 个人单图编辑 | 真上传→/v1/images/edits（hasInlineEdit=true）→完成，源图/结果对照 | `fake-upstream/requests.jsonl` 的 QA_IMAGE_EDIT、`screenshots/Q5-image-edit-processing-en-dark.png`、`screenshots/Q5-image-edit-completed-en-dark.png` |
| 个人 Seedance 首帧 | 真实 MinIO 参考图由假上游拉取200/5371bytes，角色first_frame；完成后代理blob可播放 | `fake-upstream/requests.jsonl` 的 reference-fetched/QA_SEEDANCE_FIRST_FRAME、`screenshots/Q5-seedance-first-frame-completed-en-dark.png` |
| 个人 Gemini 文生视频 | 存储关闭时仍可提交，processing→完成、blob视频3秒 | `screenshots/Q5-gemini-completed-zh-light.png`、`evidence/Q5-gemini-async-tasks.json` |
| 共享任务目录 | 同一批任务可见，n=2 API result_urls 完整；初始页面只一条链接，frontend修复后新增回归已过，实机复验待补 | `evidence/Q5-personal-tasks.json`、`evidence/Q5-async-tasks-multimage-dom.json`、`screenshots/Q5-async-tasks-en-dark.png` |

账务：n=2 图像页面预估 $0.02，usage_logs.actual_cost=$0.02/image_size=1K，余额98.40→98.38；单图编辑另扣$0.01。Gemini预估/hold/actual均$1.60；Seedance真实处理中state=accepted/hold=1.60，成功settled/actual=1.60；可控失败任务released/actual=0，退款恢复。企业图片$0.02+视频$1.00从企业100扣到98.98，成员个人仍0。证据：`evidence/Q5-image-billing.txt`、`evidence/Q5-gemini-billing.txt`、`evidence/Q5-video-settled-released.txt`、`evidence/Q5-seedance-held.txt`、`evidence/Q5-Q6-final-billing-and-audit.txt`。没有以外部上游生成时长推断账务；输入时长8与花朵媒体实际5秒不同属于夹具，按协议返回duration核算。

**环境限制，按设计拦截（不算缺陷）**：Seedance初始本机HTTP结果由 `forwardPublicVideoURL` 的HTTPS/public-host校验拒绝，content代理502；真实证据 `evidence/Q5-seedance-content-failure.json`、`runtime/backend.log` 21:51:40 的content502。Gemini同源下载允许本机配置，不能将两者混为安全回归。经 orchestrator 批准改公共CC0媒体后，播放器已验证 duration5.055/readyState4。

### 6.10 round2 错误、企业与安全

| 项 | 当前结论 | 真实证据（~/creation-studio/qa/evidence/ 下） |
| --- | --- | --- |
| 余额0与正但不足 | HTTP403/402正确，Gemini reason均为INVALID_CREATION_REQUEST，UI错误提示参数非法；已转backend修，待通知后统一重编复测 | `Q6-video-balance-zero-confirmed.json`、`Q6-video-balance-positive-low-confirmed.json`、`../screenshots/Q6-balance-error-en-dark.png` |
| Key已用3.2/额度.1 | 返回400 CREATION_MODEL_NOT_AVAILABLE而非429；orchestrator决定计费错误优先，已转backend，待复测 | `Q6-key-exhausted-confirmed.json` |
| 企业足额 | 生图/Seedance完成、企业扣费、个人0 | `Q6-enterprise-image-submit.json`、`Q6-enterprise-video-submit.json`、`Q6-enterprise-tasks.json`、`Q5-Q6-final-billing-and-audit.txt` |
| 企业正余额不足 | **通过（以契约为准）**：0.001对所需0.02返回403 ENTERPRISE_BALANCE_INSUFFICIENT，不是402；orchestrator澄清原指令笔误，遵循§5.3表B；UI单独补证 | `Q6-enterprise-insufficient.json` |
| 存储关闭 | 生图404 ASYNC_IMAGE_DISABLED、上传404 CREATION_STORAGE_DISABLED；options禁图/上传但文本视频可用，Gemini已完成 | `Q6-storage-disabled-image.json`、`Q6-storage-disabled-upload.json`、`Q6-storage-disabled-options.json`、`options-storage-disabled.json` |
| GIF与大小 | 415 CREATION_UPLOAD_UNSUPPORTED_TYPE、10485761bytes→413 CREATION_UPLOAD_TOO_LARGE | `Q6-upload-gif.json`、`Q6-upload-large.json` |
| 参考图过期 | Redis记录expires_at设now+899（保留记录TTL1小时）→410 CREATION_UPLOAD_EXPIRED；页面真实提交410后标过期且阻断提交 | `Q6-reference-expired.json`、`Q6-expired-fixture.txt`、`../screenshots/Q7-expired-en-dark.png` |
| 审计正文 | 提交、上传、估算均为credential-bearing body omitted，无提示词、base64 | `security-audit-initial.txt`、`Q5-Q6-final-billing-and-audit.txt` |
| 创作响应/DOM/存储 | **通过（按契约 v1.2）**：options、estimate、task目录及历史响应未发现合成Key；DOM/localStorage也未发现；共享Key列表属既有暴露面，R2-04 已由用户接受现状 | `security-creation-responses.json`、`security-dom-localstorage.json`、`security-response-canary-scan.json` |

新发现清单（保留原始证据，不改业务）：

| ID / 当前状态 | 现象与复现 | 依据 / 归属 | 建议 |
| --- | --- | --- | --- |
| R2-01 高，修复中待复测 | 个人Gemini，管理API将余额先设98.4再subtract98.4；提交8秒720p→403 INVALID_CREATION_REQUEST；设0.001再提→402同reason，页面Invalid request | 契约§6.1/§6.2、§9；backend | Google错误体纳入计费reason归一，按4协议×边界覆盖；不得仅靠HTTP状态猜文案 |
| R2-02，修复中待复测 | 已用3.2的Key设置quota=.1，提交→400模型不可用 | 契约§6.1配额429；§5.2 V4与不可用catalog的优先顺序由orchestrator明确计费优先；backend | 在模型可用性前返回429 API_KEY_QUOTA_EXHAUSTED，保持options不可用状态 |
| R2-03，frontend已修，自动化通过/实机待复验 | n=2 task响应有result_urls2条，但/async-tasks仅render第一result_url | 契约§7.2/I6、本轮Q5清单1；frontend | 共享历史页安全遍历所有result_urls，旧记录回落result_url；已新增AsyncTasksResults.spec.ts回归 |
| R2-04 已决：用户接受现状（2026-11-08），契约 v1.2 已修正表述，列为后续可选收紧项 | 创作页真实请求GET /api/v1/keys?page=1&page_size=100&status=active，返回key字段含完整合成canary（DOM/UI仅masked，localStorage不含）；非跨用户越权，复用同一登录态既有列表，未扩大暴露面 | 契约 v1.2 §0.2、§10.3、§11.3；用户决策经 orchestrator 确认，spec 已修正文案 | 后续可选脱敏请求参数/精简picker接口，不作为本轮上线条件；现有行为未改。证据security-keys-response.json仅含本轮合成canary；创作接口自身与日志等安全证据见 §6.12 |

首次 `errors.mjs` 中余额0的管理mutation失败400（管理余额接口拒绝set0），其200提交不能算余额0放行缺陷；已用subtract构造真实0并生成confirmed证据，原始样本保留不删。所有直改库发生在初始seed阶段且已FLUSHALL+重启；后续余额/企业/Key走真实管理API缓存失效，不清空已有任务Redis；过期仅定点改合成上传记录。backend统一复测前不重启。

### 6.11 round2 截图索引

`~/creation-studio/qa/screenshots/Q7-<empty|uploading|submitting|failed|completed|expired>-<zh|en>-<light|dark>.png` 为四组合状态证据；上传中使用真实XHR+Network.emulateNetworkConditions（1KB/s），提交中使用假上游6秒提交响应，未改DOM伪造状态。中文和英文偏好只改本地测试origin。参考图本地快照expires_at构造配合真实后端410单独验证。移动宽度390截图与共享历史实机修复复验另补。截图不含Key明文或真实用户数据。

### 6.12 round2 最终修复复测与验收结论

以下结论覆盖 §6.9–§6.11 中“待复测”状态，不删除原始失败证据。最终 backend 使用原固定参数编译（`round2-rebuild.txt`、`round2-rebuild-final.txt`）并重启；最后一次编译后可用8.8Gi，过程中未低于5Gi，未自行清理外部其它数据。

| 项 | 最终结果 | 证据（~/creation-studio/qa/ 下） |
| --- | --- | --- |
| R2-01 Gemini余额归一 | **已修复通过**：真实0→403 INSUFFICIENT_BALANCE，0.001→402 insufficient_balance；真页面分别“余额过低，请先充值”/“余额不足，无法覆盖本次费用”，cost=insufficient | `evidence/R2-fixed-zero.json`、`evidence/R2-fixed-positive-low.json`、`evidence/R2-fixed-ui.json`、`screenshots/R2-fixed-balance-zero-zh-dark.png`、`screenshots/R2-fixed-balance-positive-low-zh-dark.png` |
| R2-02 Key额度 | **已修复通过**：quota=.1/used3.2，options保留模型，submit/estimate均429 API_KEY_QUOTA_EXHAUSTED；页面“该 Key 的额度已用完，请切换 Key”；disabled Key仍key_unavailable | `evidence/R2-fixed-quota-options.json`、`evidence/R2-fixed-quota-submit.json`、`evidence/R2-fixed-quota-estimate.json`、`evidence/R2-fixed-disabled-key-options.json`、`screenshots/R2-fixed-key-quota-zh-dark.png` |
| Seedance限流 | 5h=.01，已用超限→429 rate_limit_exceeded；恢复限额0 | `evidence/R2-fixed-seedance-rate-limit.json` |
| Seedance最低保留 | **最终通过**：minimum_balance_reserve=1，余额=.5>0，API→403 INSUFFICIENT_BALANCE；页面余额过低、费用insufficient；个人余额恢复80 | `evidence/R2-fixed-minimum-balance-seedance.json`、`evidence/R2-fixed-minimum-balance-ui.json`、`screenshots/R2-minimum-balance-seedance-zh-dark.png` |
| 订阅窗口归一 | **后端单测通过，未做真实订阅窗口构造**；日/周/月→USAGE_LIMIT_EXCEEDED，保留window_resets_at；不能冒充实机订阅额度验收 | `round2-subscription-error-unit.txt`：`go test -tags=unit ./internal/handler -run TestCreationStudioGoogleEnvelopeFallbacks -count=1 -v`，9子例通过 |
| 旧OpenAI账号Seedance过滤 | 混合images+seedance capability与gpt-4o mapping：不列文本模型，保留doubao-seedance、ep-csqa-alias、qa-seedance-alias、映射到ep的qa-video-alias | `evidence/R2-fixed-seedance-filter.json` |
| 免费图 | 显式image_price_1k=0：priced=true、估算0、API/页面均成功、usage_logs第14/15条actual_cost=0；测试完成后图价恢复.01 | `evidence/R2-fixed-free-estimate.json`、`evidence/R2-fixed-free-submit.json`、`evidence/R2-free-completed.json`、`evidence/round2-final-sql.txt`、`screenshots/R2-free-image-estimate-zh-dark.png`、`screenshots/R2-free-image-completed-zh-dark.png` |
| 企业403页面 | **通过（以冻结契约为准）**：企业余额不足，请联系企业管理员；data-state=insufficient、无个人充值入口；企业余额恢复98.98 | `evidence/Q6-enterprise-low-ui.json`、`screenshots/Q6-enterprise-low-zh-dark.png` |
| R2-03共享历史多图 | **已修复，实机通过**：n2记录结果1/结果2均为链接+图片，不仅首张；新增回归1/1过 | `evidence/Q5-async-tasks-fixed-dom.json`、`screenshots/Q5-async-tasks-fixed-zh-dark.png`、`frontend/src/__tests__/creation-studio/AsyncTasksResults.spec.ts` |
| Q7截图 | **24组合齐全**：六状态×zh/en×light/dark，无缺文件；上传/提交采用真实网络状态。移动冷加载390px文档宽390无横溢出，正常折叠侧栏 | `evidence/Q7-screenshot-manifest.json`、`screenshots/Q7-mobile-390-reload-zh-dark.png`、`screenshots/Q7-mobile-390-viewport-zh-dark.png` |

共享布局动态resize记录：桌面先打开再直接缩窄390不刷新，既有AppLayout侧栏仍占位挤压正文；手机390冷加载/刷新正常。orchestrator确认**既有问题，非本变更引入，列为后续改进，不算本轮缺陷**。对照 `screenshots/Q7-mobile-390-zh-dark.png` 与 `screenshots/Q7-mobile-390-reload-zh-dark.png`。前者不是手机冷加载通过证据。

**走查清单安全项 5：通过（按契约 v1.2）**。创作专用响应、DOM、localStorage无canary明文；审计72次estimate/11次upload/34次submit全部正文省略且sensitive_found=false（`evidence/round2-final-sql.txt`），日志扫描证据见下段。**R2-04 已决：用户接受现状（2026-11-08），契约 v1.2 已修正表述，列为后续可选收紧项**。创作页复用既有 `/api/v1/keys`，同一登录态原本即可取得所有者Key明文，未扩大暴露面；该响应与前端内存持有明文的既有行为未改变，不宣称已脱敏。按 v1.2 §0.2、§10.3、§11.3 界定创作接口自身的安全边界，§2 S16改为通过；§1 I7 的原始概括按该已确认边界解读。原始测试Key全部合成，仅隔离QA，截图不含完整Key。

补充所有者与日志验证：企业成员用个人用户的真实上传ID提交，返回404 CREATION_UPLOAD_NOT_FOUND（`evidence/Q6-other-owner-upload.json`），未因文件存在泄露其归属；本轮后端日志扫描未发现API Key canary、假上游凭据或图片data URL（`evidence/security-backend-log-scan.json`）。当前账务快照所有个人/企业余额非负，视频成功actual不大于hold，失败actual=0；这验证本轮串行成功/失败样本，不冒充任意高并发或所有协议的数学保证。

最终前端门禁：

| 命令 | 结果 | 日志（~/creation-studio/qa/ 下） |
| --- | --- | --- |
| QA全部定向 | 10文件、176/176 | `round2-focused-postfix.txt` |
| `pnpm test:run` | 455文件、3875/3875、0失败 | `round2-test-run-final.txt` |
| `pnpm lint:check` | 退出0，无error/warning | `round2-lint-check-final.txt` |
| `pnpm typecheck` | 退出0 | `round2-typecheck-final.txt` |
| `pnpm check:i18n` | 3/3，通过 | `round2-i18n-final.txt` |

测试文件共10个spec，本轮仅新增 `AsyncTasksResults.spec.ts`（1例）及在 `CreationStudioView.spec.ts` 增加 RATE_LIMITED（1例）；合计由174变176，不改原断言或他人测试。§2新增通过状态仅基于本轮实际命中范围，后端B号全套证据仍以backend/orchestrator自己的报告为准；S/U/M/E其它未命中条目不擅自代填，不能将本地Q5/Q6当作所有B单元测试已由QA重跑。

**整体验收：可上线**。用户已于2026-11-08接受R2-04现状，契约 v1.2 已修正既有Key列表暴露面的边界；脱敏Key picker列为后续可选收紧项，不再作为上线条件。未发现其它未闭环的本变更功能缺陷。订阅窗口仅单测、真实厂商未测（按任务批准用假上游）、人工读屏未做为明确残留边界。生产上线仍需按§11.2给creation-uploads配置生命周期并确保厂商可访问存储端点，本地MinIO不能证明生产公网联通。

运行环境保持供复核：`cs-qa-postgres`127.0.0.1:55437、`cs-qa-redis`127.0.0.1:56387、`cs-qa-minio`127.0.0.1:59000/59001；backend PID26133端口18880、Vite PID64354端口18881、fake上游PID77593端口18882。进程/容器清单：`evidence/round2-processes.txt`、`evidence/round2-containers.txt`。当前最低保留配置仍为1（供复核），个人余额80、企业98.98，1K价格已恢复.01，Key额度/5h限额均恢复0且active，混合模型映射保留供检查；无进程/容器被销毁。最后可用8.8Gi。

矩阵范围说明：S04/S05/S06 的协议 golden 全覆盖、S11 十条全覆盖、S13 全订阅/限额组合、U03 SVG/伪扩展名嗅探，并非本轮有限实机样本可以全部证明，故保留待实现，由 backend/orchestrator 已有单测报告补齐；本轮相应 OpenAI/Seedance/Gemini、余额、GIF样本通过仍完整记录。此为证据保守分层，不表示这些代码已确认失败。
