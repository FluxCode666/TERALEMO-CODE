# 任务清单：创作页（Creation Studio）

> 状态：已冻结（2026-11-07）。唯一依据：`docs/CREATION_STUDIO.md` v1.2（下文「契约 §x」指该文件章节；v1.1 改生图计价口径与 Grok 生图的 `size` 映射；v1.2 只补记实现事实，见契约变更记录）。
> 进度（2026-11-07）：backend 与 frontend 已按 v1.1 完成，门禁经 orchestrator 复核全绿；qa 进行中。
> backend 与 frontend **完全并行**：frontend 只依赖契约里的 TS 类型与 `fixtures.ts`，联调前用 mock；backend 不依赖前端。
> 契约写不清、写错或无法落地时：停下来，在 PR 或群里提问，由 spec 角色走契约「变更记录」，**不要自行改动接口**。

## 门禁（每个角色交付前自己跑，把输出贴进 PR 描述）

**backend**（在 `backend/` 下执行，编译参数固定，不要换 ldflags 或 tags 组合，避免编译缓存膨胀）：

```bash
go build ./...
go test -tags=unit ./internal/service/... ./internal/handler/... ./internal/repository/... ./internal/server/... -count=1
go test -tags=unit ./... -count=1          # 合并前跑一次全量
~/go/bin/golangci-lint run --timeout=30m --max-issues-per-linter=0 --max-same-issues=0 ./...   # v2.14，必须 0 issues
```

lint 常见坑：错误文案不能以单个大写的产品名开头（例如 `Gemini ...`、`Grok ...` 会触发 ST1005，写成 `gemini ...`）；`defer x.Close()` 要写成 `defer func() { _ = x.Close() }()`；测试里的类型断言要接 `ok`。

**frontend**（在 `frontend/` 下执行）：

```bash
pnpm lint:check        # 不要用 pnpm lint（它带 --fix 会改动他人文件）
pnpm typecheck         # vue-tsc --noEmit
pnpm test:run          # vitest run
pnpm check:i18n        # 中英 key 完整性
```

基线已有的 lint 或 typecheck 报错不算本次引入：开工前先跑一次，把基线输出存档，交付时证明没有新增。

## 文件归属（硬约束）

| 角色 | 可写 | 禁止 |
| --- | --- | --- |
| backend | `backend/internal/handler/creation_studio_*.go`（新增）、`backend/internal/service/creation_studio*.go`（新增）及对应 `_test.go`；下表「受限修改」中列出的既有文件，只能按指定改动点修改 | `backend/internal/server/middleware/**`（`audit_log.go` 的两个 map 除外）、`backend/cmd/**`（含 `wire_gen.go`）、`backend/migrations/**`（本次无迁移）、`backend/go.mod`、任何既有测试断言（只允许新增用例） |
| frontend | `frontend/src/views/user/CreationStudioView.vue`、`frontend/src/features/creation-studio/**`（`__tests__/` 除外）、`frontend/src/i18n/locales/{zh,en}/creationStudio.ts`；受限修改：`router/index.ts`、`components/layout/AppSidebar.vue`、`i18n/locales/{zh,en}/index.ts`、`i18n/locales/{zh,en}/common.ts`、`api/asyncTasks.ts` | `frontend/src/styles/**`、`frontend/src/assets/**`、`frontend/src/style.css`、其它 `api/*.ts` 与 `views/**` |
| design | `frontend/src/styles/creation-studio.css`、`frontend/src/styles/creation-studio.spec.md`、`frontend/src/assets/creation-studio/**`；受限修改：`frontend/src/style.css`（只加一行 `@import`） | 所有 `.vue`、`.ts`、`i18n`、`router` |
| qa | `frontend/src/features/creation-studio/__tests__/**`、`frontend/src/views/user/__tests__/CreationStudioView.spec.ts`、`frontend/src/__tests__/integration/creation-studio.spec.ts`、`openspec/changes/add-creation-studio/verification.md` 的 §6 证据章节、`docs/evidence/creation-studio/**`（本地，不提交含真实 Key 或提示词的截图） | 业务代码、`backend/**` |
| spec | `openspec/changes/add-creation-studio/**`、`docs/CREATION_STUDIO.md` | 代码 |

### backend 受限修改清单（只允许改这些点）

| 文件 | 允许的改动 |
| --- | --- |
| `backend/internal/server/middleware/audit_log.go` | 在 `auditBodyOmittedRoutes` 中加 3 条：`POST /api/v1/user/creation/{estimate,uploads,tasks}`；在 `auditActionOverrides` 中加 2 条：`uploads` → `user.creation.upload`、`tasks` → `user.creation.submit`。**其它一律不动** |
| `backend/internal/handler/image_task_handler.go` | `Submit` 中在 `checkSecurityAuditBeforeSubmit` 之后、`CreateWithMetadata` 之前加一段 `if service.IsCreationStudioReplay(ctx) { if !h.creationStudioImagePrecheck(...) { return } }` |
| `backend/internal/handler/handler.go`、`backend/internal/handler/wire.go` | `Handlers` 新增 `CreationStudio *CreationStudioHandler` 字段；`ProvideHandlers` 内用已有参数构造它（**不改函数签名**） |
| `backend/internal/service/async_task.go` | `AsyncTask` 加 `ResultURLs`；新增 `AsyncVideoTaskCatalogID` 并让 `ObserveVideoTask` 调用它 |
| `backend/internal/service/image_storage.go` | 新增可选接口 `ImageStorageReader`、`ImageStorageLinkPolicy`，以及 `ImageResultUploader.SaveObject`、`ReadObject`、`LinkPolicy` |
| `backend/internal/service/image_task.go` | 新增上传相关方法（类型断言取得 `CreationUploadStore`，暴露当前 uploader），不改既有方法 |
| `backend/internal/service/gemini_submission.go` | 把内联的 `official` 能力表抽成 `geminiVideoModelLimits(mappedModel)`，取值不变 |
| `backend/internal/service/minimax_submission.go` | 把 v2 分支的能力表抽成 `miniMaxVideoModelLimits(account, mappedModel)`，v1 分支同样抽出，取值不变 |
| `backend/internal/service/openai_images.go` | 新增导出的 `IsGrokImageGenerationModel`（原函数改为调用它） |
| `backend/internal/service/account_test_service_video.go` | 新增导出的 `VideoTestModelMatchesProtocol`（原函数改为调用它） |
| `backend/internal/repository/image_task_store.go` | `Save` 填充 `ResultURLs`；新增 `CreationUploadStore` 的三个方法 |
| `backend/internal/repository/image_storage_s3.go` | 新增 `Read(ctx, key, maxBytes)`（GetObject）与 `LinkPolicy()` |
| `backend/internal/server/routes/user.go` | 按契约 §1.1 新增 4 条路由 |
| `backend/internal/server/router.go` | `RegisterGatewayRoutes` 之后调用 `h.CreationStudio.SetDispatcher(...)` |

---

## backend（共 18 项）

> 同一个 Go 包内的任务按编号串行做；B1–B6 是抽取类改动，每项单独提交，便于审查。

- [ ] **B0 基线**：在开工 commit 上跑 `go test -tags=unit ./internal/service/... ./internal/handler/... ./internal/repository/... ./internal/server/... -count=1`，保存输出。
  验收：PR 描述附基线输出。

- [ ] **B1 目录 ID 抽取**：`backend/internal/service/async_task.go` 新增 `func AsyncVideoTaskCatalogID(userID, apiKeyID int64, requestID string) string`，`ObserveVideoTask` 改为调用它。
  验收：新增 `async_task_catalog_id_test.go`（`//go:build unit`），覆盖四种 requestID 前缀（`seedance:`、`gemini:`、`minimax:`、无前缀的 Grok），断言函数输出与 `ObserveVideoTask` 写入的 `ID` 一致；既有测试全绿。

- [ ] **B2 `result_urls`**：`AsyncTask` 加 `ResultURLs []string \`json:"result_urls,omitempty"\``；`repository/image_task_store.go` 的 `Save` 用 `gjson.GetBytes(task.Result, "data.#.url")` 填充，跳过空串，最多 10 个；`ResultURL` 不变。
  验收：`image_task_store_test.go` 新增用例，3 张图的结果对应 `result_urls` 长度 3 且顺序一致、`result_url` 为第一张；无结果时字段省略；`async_task_handler` 的用户列表和详情、`admin_async_task_handler` 的管理端列表和详情各有一个用例断言 JSON 中出现 `result_urls`。

- [ ] **B3 导出**：`IsGrokImageGenerationModel`、`VideoTestModelMatchesProtocol`。
  验收：调用方行为不变；`go vet` 与 lint 通过。

- [ ] **B4 Gemini 能力表抽取**：`geminiVideoModelLimits(mappedModel string) videoModelLimits`，`NormalizeGeminiVideoSubmission` 改为调用它。
  验收：表驱动测试锁定 `veo-2.0-*`（720p，5–8）、`veo-3.0-*`（720p/1080p，8）、`veo-3.1-*`（720p/1080p，4/6/8）、未知型号（720p，8）四组；`go test -tags=unit ./internal/service -run 'Gemini' -count=1` 全绿。

- [ ] **B5 MiniMax 能力表抽取**：`miniMaxVideoModelLimits(account *Account, mappedModel string) videoModelLimits`，覆盖 v2 默认、`MiniMax-H3`、`MiniMax-H3-Max`、v1 四组，`NormalizeMiniMaxSubmission` 改为调用它。
  验收：四组取值单测；`-run 'MiniMax'` 全绿。

- [ ] **B6 对象存储读写**：`service/image_storage.go` 新增
  `type ImageStorageReader interface { Read(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) }`、
  `type ImageStorageLinkPolicy interface { LinkPolicy() (permanent bool, ttl time.Duration) }`，以及 `ImageResultUploader` 的 `SaveObject(ctx, relKey, contentType, data)`（拼接 prefix）、`ReadObject(ctx, relKey, maxBytes)`、`LinkPolicy()`（存储未实现接口时返回 `false, 24h`）；`repository/image_storage_s3.go` 实现 `Read` 和 `LinkPolicy`。
  验收：fake storage 单测覆盖 prefix 拼接、超过 maxBytes 报错、未实现 Reader 时返回明确错误；S3 的 `Read` 有 `defer func() { _ = out.Body.Close() }()`。

- [ ] **B7 上传服务**：新增 `service/creation_studio_upload.go`，包含 `CreationUploadRecord`、`CreationUploadStore`、`ErrCreationUpload*`（各码与契约 §6.1 一致）、`ValidateCreationUploadBytes`（契约 §2.2 的 U3–U5，用 `http.DetectContentType` 嗅探，用 `image.DecodeConfig` 解码并导入 webp）、`creationUploadObjectKey(userID, uploadID, contentType, now)`，以及 `ImageTaskService` 上的 `CreateCreationUpload(ctx, userID, data)`、`ResolveCreationUpload(ctx, userID, uploadID, now)`（契约 §2.5）。
  验收：单测覆盖 png、jpeg、webp 正常；gif/svg/伪造扩展名 → 415；11 MiB → 413；5000×10 像素 → 422；第 101 张 → 429；他人上传 → NotFound；剩余 14 分钟 → Expired；对象 key 形如 `creation-uploads/42/20261107/cupl_<32hex>.png`。

- [ ] **B8 上传元数据存储**：`repository/image_task_store.go` 实现 `SaveCreationUpload`、`GetCreationUpload`、`CountActiveCreationUploads`，key 与 TTL 见契约 §2.3。
  验收：miniredis 测试覆盖 TTL 生效、ZSET 清理过期项、跨用户隔离。

- [ ] **B9 表单与重放标记**：新增 `service/creation_studio.go`，包含 `CreationSubmitRequest`、`CreationImageRef`、`CreationEstimateRequest`、模式与协议常量、`WithCreationStudioReplay(ctx)`、`IsCreationStudioReplay(ctx)`（未导出的 context key 类型），以及 `ValidateCreationSubmit(req, option CreationModelOption)`（契约 §5.2 的 V3、V5、V6、V9）。
  验收：表驱动单测覆盖每条规则的通过与失败各一例。

- [ ] **B10 原生请求构造**：新增 `service/creation_studio_request.go`，包含 `BuildCreationNativeRequest(...) (*NativeCreationRequest, error)` 和 `creationStudioAllowedPaths`（契约 §5.4）。
  验收：`creation_studio_request_test.go` 用 golden JSON 覆盖 9 种情形：OpenAI 生成、OpenAI 编辑、Grok 生成、Grok 编辑、Seedance（首帧+尾帧）、Seedance（参考图）、Gemini（首帧）、MiniMax v1、MiniMax v2，外加 Grok 视频；断言 Seedance/MiniMax/Grok 不含 `n` 和 `sampleCount`；断言参数默认值都显式写入；断言白名单外路径报错；断言 Gemini 的 `<model>` 含 `/` 时报错；断言 Grok 生图把描述符 `resolution`（`1k`/`2k`）写入原生 `size`、请求体不含 `resolution`（契约 §3.3、§5.4）。

- [ ] **B11 options**：新增 `service/creation_studio_catalog.go`，在 `OpenAIGatewayService` 上实现 `CreationOptions(ctx, key, imageTasks *ImageTaskService) (*CreationOptionsResponse, error)`，按契约 §3 生成可用性、模型、描述符、`upload_limits`、价格摘要。
  验收：快照测试覆盖 openai、grok、video（Seedance/Gemini/MiniMax 三种协议各有账号）、composite、无分组 5 种 Key；断言 `kind` 只出现四种值；账号配置 `video_resolutions:["480p","1080p"]` 后描述符只剩这两档；对象存储关闭时 `image.reason=storage_disabled`、`uploads.reason=storage_disabled`、所有 image 描述符为 `disabled`，而 `video.available=true`；多账号能力不一致时 `partial=true`。

- [ ] **B12 estimate**：新增 `service/creation_studio_estimate.go`，按契约 §4 实现，其中包含预检与估算共用的 `func (s *OpenAIGatewayService) EstimateCreationImageCost(ctx, apiKey, userID, billingModel, size string, n, bodyBytes int) CreationImageCostEstimate`（契约 §4.2.1）。该函数只能组合现有函数：判档用 `ResolveImageBillingSize(size, nil).BillingSize`，倍率用 `ResolveUserGroupRateMultiplier` + `computePeakAwareMultipliers`，计价用 `calculateOpenAIImageCost`，渠道为 token 计费时用 `EstimateInflightReservation`。**不得新写 size → 档位映射或价格查找。**
  验收：用 fake 断言不调用任何预留或写入接口（`ReserveFixedCharge`、`ReserveInflight` 及 repo 写方法调用次数为 0）；`EstimateCreationImageCost` 表驱动测试：`1024x1024`→1K、`1536x1024`→2K、`auto`/空→2K、`4k`→4K，Grok `1k`→1K、`2k`→2K；同一组入参下，其 `Amount` 与把同样的 `OpenAIForwardResult{ImageSize, ImageInputSize, ImageCount}` 和 `imageMultiplier` 交给 `calculateOpenAIImageCost` 得到的 `ActualCost` 相等（含分组价、渠道按次价、用户专属倍率、`ImageRateIndependent` 四种配置）；渠道为 token 计费时 `cost_basis=image_token_estimate` 且金额等于 `EstimateInflightReservation`；响应中没有 `selected_tier_cost` 字段；视频的 `estimated_cost` 与按同参数调用 `BuildVideoPricingContract` 得到的 `ReservedAmount` 一致；`priced=false` 时返回 200；`sufficient` 覆盖个人（余额足/不足/低于保留）、企业（null）、订阅（null）。

- [ ] **B13 生图预检**：新增 `handler/creation_studio_image_precheck.go`，实现 `(h *AsyncImageHandler) creationStudioImagePrecheck(c, apiKey, platform, body) bool`（契约 §5.3 的步骤与表 A、表 B），并在 `image_task_handler.go` 的 `Submit` 中挂上。金额**必须**调用 B12 的 `EstimateCreationImageCost`，不得直接调用 `EstimateInflightReservation` 作为金额（token 计费分支除外，由该函数内部处理）。
  验收：同一表单下预检金额与 `/estimate` 的 `estimated_cost` 相等；所选 1K 档余额够、但按 4K 计不够的用户**能通过**预检；单测证明不带重放标记时**完全不执行**（API 行为不变，用 fake billing 断言调用次数为 0）；带标记时：余额 0 → 403 `INSUFFICIENT_BALANCE`；余额为正但低于估算 → 402 `insufficient_balance`，且未创建任务（store 无 `image_task:*`）；订阅窗口超限 → 429 `USAGE_LIMIT_EXCEEDED`；Key 额度用完 → 429 `API_KEY_QUOTA_EXHAUSTED`；企业钱包不足 → 403 `ENTERPRISE_BALANCE_INSUFFICIENT`；通过时 `ReserveFixedCharge` 返回的预留立即释放（fake 断言 Release 被调用）。

  > v1.2 补记：「订阅窗口超限」与「企业钱包不足」两个分支目前只有错误映射级单测，端到端由 qa 第二轮覆盖（见 design「实现确认」第 6 条）。

- [ ] **B14 错误归一**：新增 `handler/creation_studio_errors.go`，按契约 §6.2 的 10 条规则实现。
  验收：表驱动单测每条规则至少一例；特别覆盖「403 + `billing_error` + `insufficient balance`」→ `INSUFFICIENT_BALANCE`；Google 格式和 MiniMax `base_resp` 各一例；`Retry-After` 进入 `metadata.retry_after`。

- [ ] **B15 面板 handler**：新增 `handler/creation_studio_handler.go`，包含 `CreationStudioHandler`、`StudioDispatcher`、`SetDispatcher`、`Options`、`Estimate`、`Upload`、`Submit`（契约 §2、§3、§4、§5.5、§5.6）。
  验收：`creation_studio_handler_test.go` 用 fake dispatcher 断言：内层 `Authorization` 是该 Key、外层 Cookie 和 Authorization 未透传、`RemoteAddr` 与 `X-Forwarded-For` 透传、context 带重放标记；他人 Key → 404；白名单外路径 → 400；内层 202（图片）、200（四种视频协议）→ 面板 200，`catalog_id` 正确；内层 402/403/429 → 归一后的 reason；未注入 dispatcher → 503；带重放标记再次进入 → 500；canary Key `sk-canary-creation` 不出现在任何响应体或日志中。

- [ ] **B16 装配与路由**：`handler.go`/`wire.go` 加字段并构造；`routes/user.go` 加 4 条路由（含 body limit 和 Heavy 限流）；`router.go` 调用 `SetDispatcher`。
  验收：`backend/cmd/server/wire_gen.go` 无 diff；路由测试未登录访问 4 条路由均为 401；`/creation/tasks` 提交 2 MiB 正文 → 413 `CREATION_BODY_TOO_LARGE`；`go build ./...` 通过。

- [ ] **B17 审计正文省略**：`audit_log.go` 两个 map 加条目（见受限修改清单）。
  验收：审计中间件测试断言三条路由记录的正文为 `<credential-bearing body omitted>`，且不含 canary 提示词；`uploads` 和 `tasks` 的 action 名正确。

- [ ] **B18 门禁**：跑完上文 backend 门禁全部命令。
  验收：输出贴进 PR；golangci-lint 为 0 issues。

---

## frontend（共 18 项）

> 只依赖契约中的 TS 类型。联调前通过 `fixtures.ts` 加 `vi.mock`，或在开发模式下临时替换 `api.ts` 实现（不提交 mock 开关）来跑通页面。

- [ ] **F1 类型**：`frontend/src/features/creation-studio/types.ts`，照抄契约 §2–§8 的全部 TS 类型（字段名与可选性一致）。
  验收：`pnpm typecheck` 通过；与契约逐字段核对，PR 中附对照表。

- [ ] **F2 fixtures**：`features/creation-studio/fixtures.ts`，提供 options（openai Key、video Key、存储关闭）、estimate（足/不足/未定价）、upload、submit（图片、视频）、错误响应（402/403/429/410）各一份，与契约示例一致。
  验收：类型检查通过；QA 可直接 import 使用。

- [ ] **F3 API**：`features/creation-studio/api.ts`，提供 `getOptions(apiKeyId, signal?)`、`estimate(body, signal?)`、`upload(file, onProgress?, signal?)`、`submit(body)`，全部走 `apiClient`；`upload` 使用 multipart，超时 120 秒。
  验收：不出现 `fetch(`、`buildGatewayUrl`、`Authorization`；错误统一抛出带 `reason`、`status`、`metadata` 的对象。

- [ ] **F4 `result_urls` 类型**：`frontend/src/api/asyncTasks.ts` 的 `AsyncTask` 新增 `result_urls?: string[]`，其它不动。
  验收：diff 只有这一行；`AdminAsyncTask` 自动继承。

- [ ] **F5 `useCreationKeys.ts`**：调用 `keysAPI.list(1, 100, { status: 'active' })`，过滤掉无 `group` 的 Key；按 `creationStudio:lastKeyId:v1:<user_id>` 记忆上次选择。
  验收：上次选中的 Key 已失效时回落到第一个 Key。

- [ ] **F6 `useCreationOptions.ts`**：按 `api_key_id` 缓存；切换 Key 时取消旧请求、清空模型。
  验收：快速切换两个 Key 时只采用最后一次的响应。

- [ ] **F7 `useCreationForm.ts`**：维护模式、模型（按 `id` 选中）、提示词、`params`（用描述符 `default` 初始化）、`images`。切换模型时保留提示词并重建参数；切换模式只换表单，不影响历史筛选。
  验收：提示词超过 8000 字符或为空时提交按钮禁用；未知 `kind` 只告警并跳过，不报错。

- [ ] **F8 `useCreationUploads.ts`**：上传前按 `upload_limits` 校验大小和类型；逐张上传并显示进度；失败可重试；剩余有效期不足 900 秒视为过期。
  验收：415/413/422/429/404 各对应字段内错误文案；上传不可用时入口禁用并显示原因。

- [ ] **F9 `useCreationEstimate.ts`**：300ms 防抖；任一入参变化就重新估算；失败时只隐藏余额行。
  验收：`priced=false` 或 `sufficient=false` 时 `canSubmit=false`；`sufficient=null` 时不禁用提交。

- [ ] **F10 `useCreationSubmit.ts`**：同一时间只允许一个提交在途；成功后写本地快照并在任务流顶部插入卡片；失败按契约 §6.1 把 `reason` 映射到 `errors.*`，用户输入保留；HTTP 429 且无 `reason` 时按 `rate_limit_exceeded` 处理，倒计时取 `Retry-After` 头（契约 §6.3）。
  验收：连续点击只发出一次请求；410/404 上传错误会把对应缩略图标为过期。

- [ ] **F11 `useCreationFeed.ts`**：分页使用 `asyncTasksAPI.list` 并以「加载更多」追加；存在 `processing` 时每 10 秒刷新已加载的页；页面隐藏时暂停；视频 `refresh` 每个任务 30 秒节流；筛选项为全部/图片/视频。
  验收：加载更多不清空已有列表；失败时保留旧数据。

- [ ] **F12 `useCreationSnapshots.ts`**：按契约 §8 实现 key、结构、100 条上限、7 天过期、配额溢出时的处理、JSON 损坏时的恢复。
  验收：模拟 `QuotaExceededError` 时删掉最旧的一半后重试一次，再失败也不抛错。

- [ ] **F13 组件**：在 `features/creation-studio/components/` 下实现 `ModeSwitch`、`KeyPicker`、`ModelPicker`、`PromptField`、`ParamField`（按 kind 分发）、`ChipGroup`、`RatioGroup`、`IntegerStepper`、`ImageDropzone`（含上传中、失败、过期、禁用状态）、`CostSummary`、`SubmitAction`、`Callout`、`TaskCard`（不确定进度条加已用时长）、`MediaGrid`（按 `result_urls` 展示全部图片）、`VideoPlayer`（blob 播放）、`Lightbox`、`HistoryFeed`。
  验收：只使用 `cs-*` 类和现有基础类；状态通过 `data-*` 和 `aria-*` 表达；`safeURL` 拦截 `javascript:`。

- [ ] **F14 页面**：`frontend/src/views/user/CreationStudioView.vue`，`AppLayout` + `cs-page`；桌面为双栏，移动端为「参数/结果」标签切换并吸底提交栏；覆盖空态和不可用态（无 Key、无模型、存储关闭、未定价、无视频账号）。
  验收：对照 `creation-studio.spec.md` §3、§5 逐项自查。

- [ ] **F15 路由**：在 `router/index.ts` 的 `/async-tasks` 之前插入 `/creation`（契约 §10.1）。
  验收：meta 与契约一致；没有 `requiresPersonalWallet`。

- [ ] **F16 菜单**：`AppSidebar.vue` 新增 `CreationIcon`，在 `buildSelfNavItems` 和简易模式分支中各插入一条（契约 §10.2）。
  验收：普通用户、管理员「我的账户」、简易模式三处可见；企业成员可见。

- [ ] **F17 i18n**：新增 `i18n/locales/{zh,en}/creationStudio.ts`，在 `index.ts` 中展开，`common.ts` 加 `nav.creationStudio`；`errors.*` 的 key 名按契约 §6.1，其余 key 按 `creation-studio.spec.md` §11.5。
  验收：`pnpm check:i18n` 通过；`.vue` 中无硬编码的中文或英文文案。

- [ ] **F18 门禁**：跑完上文 frontend 门禁的四条命令。
  验收：输出贴进 PR，相对基线无新增 lint 或 typecheck 错误。

---

## design（共 6 项）

- [x] **D1** `frontend/src/styles/creation-studio.spec.md`、`creation-studio.css`、`frontend/src/assets/creation-studio/*.svg` 已交付。
- [ ] **D2** 在 `frontend/src/style.css` 的 `@import './styles/shadcn.css';` 之后追加 `@import './styles/creation-studio.css';`。
  验收：`pnpm typecheck` 与页面加载正常，`cs-*` 类生效。
- [ ] **D3** 补齐缺失的 `cs-flash`、`cs-load-more`、`cs-feed-end`（已由 orchestrator 转交 design）。
  验收：类名与 spec §11.1 清单一致。
- [ ] **D4** 新增「参考图已过期」缩略图状态（建议 `cs-thumb[data-state='expired']`），以及上传中、上传失败、描述符 `disabled`（存储未开）的视觉样式。
  验收：spec 与 css 同步更新，类名提供给 frontend。
- [ ] **D5** 多图结果网格适配 n=1–4（`cs-media-grid[data-count='1'..'4']` 已有的，核对一遍）以及编辑模式下的「原图 → 结果」对照展示。
- [ ] **D6** 补充 `uploads` 不可用、`CREATION_UPLOAD_*` 错误的 callout 与字段内错误形态。

---

## qa（共 8 项）

- [ ] **Q1** `features/creation-studio/__tests__/useCreationForm.spec.ts`、`useCreationEstimate.spec.ts`、`useCreationSubmit.spec.ts`、`useCreationFeed.spec.ts`、`useCreationUploads.spec.ts`、`useCreationSnapshots.spec.ts`，覆盖 F7–F12 的验收点（用 fake timer 与 `fixtures.ts`）。
- [ ] **Q2** `features/creation-studio/__tests__/ParamField.spec.ts`：四种 kind 渲染正确；未知 kind 被跳过；`disabled` 状态正确。
- [ ] **Q3** `views/user/__tests__/CreationStudioView.spec.ts`：空态、不可用态、提交成功插入卡片、402/403/429 callout、上传过期、全部结果网格、`javascript:` 链接被拦截。
- [ ] **Q4** `__tests__/integration/creation-studio.spec.ts`：未登录访问 `/creation` 跳转登录；三处菜单入口（参考 `navigation.spec.ts`）。
- [ ] **Q5** ego-browser 实机走查（真后端）：个人钱包用户完成「文生图 n=2」「单图编辑」「Seedance 首帧图生视频」「Gemini 文生视频」各一条，并在 `/async-tasks` 中看到同一批任务和全部图片。
- [ ] **Q6** 错误分支实机验证：余额 0（403）、余额不足（402）、Key 额度用完（429）、对象存储关闭（生图与上传不可用，文生视频可用）、上传 gif（415）。
- [ ] **Q7** 中英文、亮/暗色截图：空态、上传中、提交中、失败、完成、参考图已过期。
- [ ] **Q8** 把证据写入 `verification.md` §6，并更新 §2 矩阵的状态。

---

## 依赖关系

```text
backend: B0 → B1…B6（抽取，串行） → B7 → B8 → B9 → B10 → B11 → B12 → B13 → B14 → B15 → B16 → B17 → B18
frontend: F1 → F2 → F3/F4 → F5…F12（可并行） → F13 → F14 → F15/F16/F17 → F18
design: D2 尽早完成（frontend 需要用到类名）；D3–D6 与 frontend 并行
qa: Q1–Q4 跟随 frontend 进度；Q5–Q7 需要 B18 与 F18 都完成
```

backend 与 frontend 之间没有代码依赖，只在 Q5 联调时会合。
