# 设计：创作页（Creation Studio）

> 状态：已冻结（2026-11-07，契约 `docs/CREATION_STUDIO.md` v1.2）。开放问题 O1–O6 已由用户拍板，结论见「决策记录」。后续变更走契约文末的「变更记录」。

## Context

### 现状（已核对代码，2026-11-07）

**异步生图**（`docs/ASYNC_IMAGE_TASKS.md`）

- 入口：`POST /v1/images/generations/async`、`POST /v1/images/edits/async`、`GET /v1/images/tasks/{task_id}`。handler 是 `backend/internal/handler/image_task_handler.go` 中的 `AsyncImageHandler`。
- 提交阶段同步完成的检查：对象存储开关（`ImageTaskService.Enabled()`）、分组平台只能是 OpenAI 或 Grok、`GroupAllowsImageGeneration`、请求解析、拒绝 `stream`、安全审计。之后写 Redis、返回 202，并起一个 goroutine 执行 `OpenAIGateway.Images` 或 `GrokImages`。
- **计费准入（`CheckBillingEligibility`）和在途预留（`reserveInflightBalance`）都在 goroutine 里执行**，所以 API 调用方在提交响应中看不到余额错误，只能看到任务失败。
- `reserveInflightBalance` 只有在「已有在途请求，且余额减去在途合计小于估算」时才拒绝（`billing_inflight_reservation.go` 中 `ReserveInflight` 的注释）。单个请求即使余额不足也会被放行，而且估算会被 `MaxReservationUSD` 封顶。
- 结果经 `ImageResultUploader.Rewrite` 转存对象存储。`image_task:<id>` 中保存完整结果（全部 `data[].url`），但目录记录只摘取了 `data.0.url`。

**异步视频**（`docs/ASYNC_VIDEO_TASKS.md`）

- 四种原生创建入口都汇聚到 `handleGrokMedia`（`backend/internal/handler/grok_media.go`）。顺序是：解析 → 安全审计 → 并发槽 → `CheckBillingEligibility` → 选账号并按账号规范化 → `BuildVideoPricingContract` 冻结价格并持久预占 → 调用上游 → 结算 → `ObserveVideoTask` 登记目录。
- MiniMax 走 v1 还是 v2 由入站路径决定：`NormalizeMiniMaxSubmission(body, account, strings.HasPrefix(path, "/v2/"))`。
- 余额判定有三种结果：
  - 余额 ≤ 0 或低于最低保留余额 → 403。来源有两处：`apiKeyAuth` 返回 `INSUFFICIENT_BALANCE`；`CheckBillingEligibility` 返回 `ErrInsufficientBalance`，经 `billingErrorDetails` 落到默认分支，变成 **`403 billing_error`**。
  - 余额为正但不足以覆盖冻结 → 402 `insufficient_balance`。
  - 配额 → 429。

**任务目录与面板**

- 已有接口：`GET /api/v1/user/async-tasks`、`/{id}`、`/{id}/refresh`、`/{id}/content`。目录记录 `service.AsyncTask` 中没有提示词和参数。
- 目录 ID 规则：图片的目录 ID 就是 `imgtask_*`；视频是 `vidtask_<sha256(userID:keyID:requestID)>`，其中 requestID 带协议前缀（Grok 无前缀）。
- Redis 记录保留 24 小时，每个用户最多 1000 条。
- `RefreshUserTask` 已经采用「JWT → GetByID 校验归属 → 构造上下文 → 调用网关 handler」的写法，可作为先例。

**对象存储**

- `ImageStorage` 接口只有 `Save`。S3 实现在 `backend/internal/repository/image_storage_s3.go`：配置了 `public_base_url` 时返回永久直链，否则返回 presigned 链接（`presign_expiry_hours`，默认 24 小时）。
- 现有的图片下载客户端会拒绝私网和回环地址，所以不能用来下载自建 MinIO 上的自家对象。

**Key 与面板**

- `GET /api/v1/keys` 会把 Key 明文返回给浏览器，批量生图页已经在浏览器里直连网关。创作页的 Key 选择列表复用这个既有接口，所以前端内存中会持有它返回的明文（DOM 与 localStorage 不含）；但创作接口自身的请求只带 `api_key_id`，响应、日志、审计都不含明文，重放用的明文只在后端内存。同一登录态本来就能拿到这些明文，本变更没有扩大暴露面。

### 约束

- 既定产品规则（2026-10-22）：
  - 企业成员可以使用全部视频能力。
  - 视频按提交时的价格冻结。
  - 不允许扣成负数。
- 异步生图只支持 OpenAI 与 Grok 分组，且需要开启对象存储。
- 本次无数据库迁移。backend 只改 `backend/internal`，middleware 只允许改 `audit_log.go` 中的两个 map。
- 视觉与交互规范见 `frontend/src/styles/creation-studio.spec.md`，本文不重复。

## 决策记录（2026-11-07 用户拍板）

| 编号 | 问题 | 结论 |
| --- | --- | --- |
| O1 | 提交通道 | **C：进程内重放**到既有网关路由 |
| O2 | 生图是否在提交阶段硬拦截 | **硬拦截**：只在创作页重放时执行预检，API 用户行为不变 |
| O3 | 是否服务端保存提示词与参数 | **不落库**：历史用 `/async-tasks`，复用参数用前端本地快照 |
| O4 | 生图范围 | **文生图 + 单图编辑，n 为 1–4，展示全部结果**（目录新增 `result_urls`） |
| O5 | 视频参考图/首帧 | **先上传到对象存储**，再把链接传给上游；新增上传接口 |
| O6 | 企业成员 | **可用**，不套用批量生图的「仅个人钱包」门禁 |
| 风险 2 | 低于最低保留余额时返回 `403 billing_error` | **只在创作层归一**为 `INSUFFICIENT_BALANCE`，不改网关对外行为 |
| 交互 | 历史筛选、移动端、存储未开、进度 | 全部采纳 design 的默认值：历史不跟随模式切换；移动端用标签切换并吸底提交；对象存储未开时选项保留并标「不可用」；进度用不确定进度条加已用时长 |

## Goals / Non-Goals

**Goals**

1. 一个页面完成生图与生视频；参数面板由后端下发的描述符驱动。
2. 生图只走异步生图链路，生视频只走四种原生链路；计费判据零复制。
3. 提交前给出费用与准入判断；生图余额不足时在提交阶段直接拒绝。
4. 任务进入 `/async-tasks` 目录；多张图片全部可见。
5. 可上传参考图、首帧、编辑原图，复用参数时可以回填未过期的图片。

**Non-Goals**

- 同步生图、批量生图、任务取消与删除。
- 服务端保存提示词或参数；新增数据库表。
- 合成分组（composite）；Grok 视频编辑与续写；Seedance 智能时长、样片、帧数；Gemini 多结果。
- 上传图片去除 EXIF；应用内删除对象（由存储桶生命周期规则负责）。

## 关键设计

### D1 进程内重放

面板 handler 校验 JWT、Key 归属与上传后，构造原生请求，并以该 Key 的身份（`Authorization: Bearer <Key>`，只存在于后端内存）重放到同一 gin engine 上的白名单路由。这样鉴权、分组准入、模型白名单、计费准入、持久预占、价格冻结、目录登记、Ops 指标全部复用现有实现。

- engine 由 `server.registerRoutes` 在注册网关路由之后通过 `SetDispatcher` 注入。handler 在 `ProvideHandlers` 里用已注入的依赖构造，**不改 Wire 生成文件**。
- 白名单共 7 条路径，写死在 `creationStudioAllowedPaths`。
- 内层请求保留 `RemoteAddr`、`X-Forwarded-For`、`X-Real-IP`，以保证 Key 的 IP 规则看到真实客户端；清空 `Cookie`、`Origin`、`Referer`。
- context 使用 `WithoutCancel` 加 90 秒超时：用户关闭页面不应让已冻结的创建变成「结果不确定」。
- context 中带不可伪造的重放标记 `service.WithCreationStudioReplay`。它有两个用途：防递归，以及触发 D3 的生图预检。

被否决的方案：
- 浏览器直连网关：参数规范化需要在前端复制一份；Key 明文进入页面；视频目录 ID 前端算不出来。
- 面板直接调 handler：需要复制整条 `apiKeyAuth` 准入链，任何遗漏都是计费或安全漏洞。

### D2 统一表单与描述符

- 描述符的 `kind` 只有 `select`、`integer`、`ratio`、`image` 四种（与 design 规范一致），布尔量用 `select` 表示。每个协议的键集合在契约 §3.3 写死。
- 视频能力表与提交期规范化同源：Gemini、MiniMax 原本内联在函数里的官方能力表抽成函数复用，取值逐项不变，并用单测锁定。
- 统一表单到原生请求的映射是纯函数 `BuildCreationNativeRequest`，按协议做表驱动测试。所有参数都显式写入，不依赖网关补默认值。

### D3 生图提交预检（O2）

- 预检放在内层重放进入 `AsyncImageHandler.Submit` 之后执行，受重放标记保护。好处是直接复用内层 `apiKeyAuth` 已加载的订阅、企业上下文和分组，不重复实现。
- 三步：
  1. 资格：`CheckBillingEligibility`。
  2. 金额（v1.1）：按所选尺寸档位计价，调用 `EstimateCreationImageCost`。它只组合执行期计费已有的函数：`ResolveImageBillingSize(size, nil)` 判档 → `ResolveUserGroupRateMultiplier` + `computePeakAwareMultipliers` 取图片倍率 → `calculateOpenAIImageCost` 计价；渠道为 token 计费时退回 `EstimateInflightReservation`（token 只能事后得知）。
  3. 覆盖：`ReserveFixedCharge(金额)`，随后立即 `Release`。
- 覆盖检查不直接用 `reserveInflightBalance`，因为它对单个请求会放行，且有封顶，做不到「不够就拒绝」。`ReserveFixedCharge` 用同一金额检查余额、企业钱包与限额、订阅窗口、Key 额度与限速，比执行期**更严格**。
- 预检只是准入，不替代 goroutine 内的真实预留。并发提交时以 goroutine 为准，后到者可能以任务失败结束。
- 错误映射：资格阶段的余额错误 → 403 `INSUFFICIENT_BALANCE`；覆盖阶段的余额错误 → 402 `insufficient_balance`；配额 → 429。与视频口径一致（契约 §5.3 表 A/B）。

### D4 上传（O5）

- 接口：`POST /api/v1/user/creation/uploads`，multipart 单文件。
- 限制：
  - 类型靠嗅探判定，只接受 png、jpeg、webp。
  - 大小 ≤ 10 MiB。
  - 每边 16–4096 像素，总像素 ≤ 16M。
  - 每个用户 24 小时内最多 100 张有效上传。
- 复用异步生图的对象存储（同一开关、同一客户端、同一 prefix）。对象路径为 `creation-uploads/<user_id>/<日期>/<upload_id><ext>`，按用户隔离。
- 元数据存 Redis（`creation_upload:<id>`），TTL 等于有效期 `min(24h, presign 有效期)`。这些方法由现有 `imageTaskStore` 实现，通过类型断言取得，不新增 Wire provider。
- 提交时校验上传属于本人，且剩余有效期 ≥ 15 分钟；不满足返回 404 或 410。
- 不同协议的用法：
  - Seedance、MiniMax、Grok：直接传上传链接，由上游拉取。
  - Gemini：原生只收内联字节，由后端经 S3 `GetObject` 读回后内联。
  - 生图编辑：也是读回字节，转成 data URL 放进 JSON `images[].image_url`（Grok 为 `image.url`）。不组装 multipart。理由见契约 §5.4：JSON 编辑路径网关已原生支持；网关处理 multipart 时本身也是转成 data URL；不要求上游能访问我方存储桶；纯函数便于测试。
- 读回字节不走 HTTP：现有下载客户端拒绝私网地址，会让自建 MinIO 失效。因此给 S3 存储新增 `Read`。
- 对象生命周期：应用不删除对象，由存储桶生命周期规则清理（建议 3 天），写入发布说明。
- 上传不可用时，所有图片类描述符置为 `disabled`。生图模式本身此时也不可用；纯文生视频不受影响。

### D5 目录与结果（O4）

- `service.AsyncTask` 新增可选字段 `ResultURLs []string`（json 名 `result_urls`）。`imageTaskStore.Save` 用 `gjson` 取 `data.#.url`，最多 10 个；`result_url` 保留为第一张。
- 用户端和管理端的列表、详情都会自动带上该字段（管理端结构内嵌 `AsyncTask`）。前端只在 `AsyncTask` 类型上加一个可选字段；旧记录回落到 `[result_url]`。
- 视频目录 ID 的算法抽成 `service.AsyncVideoTaskCatalogID`，与 `ObserveVideoTask` 共用，提交响应里直接给出 `catalog_id`。

### D6 复用参数（O3）

- 不落库。提交成功后把快照写入 localStorage：
  - key：`creationStudio:snapshots:v1:<user_id>`
  - 上限 100 条，保留 7 天
  - 写入失败时删掉最旧的一半重试一次，再失败就放弃，不影响提交
- 回填图片：剩余有效期 ≥ 15 分钟的回填；不足的显示「参考图已过期，请重新上传」。
- 没有本地快照的历史任务，只回填模型和参数默认值。

### D7 预计费用

- `POST /creation/estimate` 不产生任何持久副作用，也不写 Redis。
  - 图片：`estimated_cost` 就是预检金额（同一函数、同一入参），另给 `selected_tier` 供明细展示；不再单独返回所选档位费用。
  - 视频：返回与提交时冻结额同源的 `ReservedAmount`。
- 个人钱包给出 `sufficient`。企业和订阅给 `null`，以提交结果为准。

### D8 错误归一

- 内层有四种错误体格式：中间件、OpenAI、Google、MiniMax。按契约 §6.2 的 10 条规则依次匹配，归一为稳定的 `reason`。
- 第 1 条把「403 + `billing_error` + `insufficient balance`」归为 `INSUFFICIENT_BALANCE`，这是风险 2 的处理方式。**只在创作层处理，不改网关对外行为。**

### D9 审计与隐私

- `estimate`、`uploads`、`tasks` 三条 POST 加入 `auditBodyOmittedRoutes`，防止提示词、图片、multipart 正文写进审计表。`uploads` 与 `tasks` 另登记动作名。
- 创作接口（options / estimate / uploads / tasks）自身的响应、日志、审计不含 Key 明文（canary 测试）。前端内存中会持有既有 `/api/v1/keys` 接口返回的明文，DOM 与 localStorage 不含；如需收紧，后续可新增不含明文的选择列表接口（契约 §11.3）。

## 实现确认（v1.2，backend 与 frontend 完成后补记）

| # | 事实 | 处理 |
| --- | --- | --- |
| 1 | 面板通用限流 `middleware/panel_rate_limit.go`（所有 `/api/v1/user/*` 共用）命中时返回 429 `{"code":"RATE_LIMITED","message":...}` 与 `Retry-After` 头，没有 `reason` | 不改共用中间件；前端把「429 且无 `reason`」按 `rate_limit_exceeded` 处理，倒计时取 `Retry-After`。契约 §6.3 记为唯一的包络例外 |
| 2 | 模型清单按账号 `model_mapping` 中满足家族谓词的 key 枚举；未配置 mapping 时回落为默认名：OpenAI `gpt-image-2`、Grok 生图 `grok-imagine-image`、Grok 视频 `grok-imagine-video`、旧 OpenAI 平台 Seedance `doubao-seedance-1-0-pro`；Video 平台走 `Account.VideoTestModelIDs()` 的协议默认清单（Gemini 两个、MiniMax v1 三个、v2 两个）。旧 OpenAI 平台 Seedance 有 mapping 时按 `VideoTestModelMatchesProtocol` 排除其它协议，并要求公开别名或映射目标至少一个含 `seedance` 或以 `ep-` 开头（该函数对未知名字默认返回 true，单用它挡不住 `gpt-4o → gpt-4o`）；过滤后为空不贡献模型（按契约改代码） | 契约 §3.4 写明来源表；§11.3 写明「未配 mapping 只展示默认模型」与「不含 seedance、非 ep- 的自定义中转名不展示」 |
| 7 | 0 元（免费）模型按已定价处理：`priced=true`、`estimated_cost=0`；只有计价失败或无价格才 `priced=false`；生图预检对免费请求放行（按契约改代码） | 契约 §4.2.2 |
| 3 | `/estimate` 不读取上传（对象和元数据都不读），token 计费渠道的图片编辑估算金额会低于预检金额；按档位计费二者恒等 | 契约 §4.2.2、§11.3 |
| 4 | MiniMax：创作层按模型名前缀（`MiniMax-H3`）选 v1/v2 路径，网关按账号 `base_url` 判定版本，不一致时由网关按既有规则拒绝或跳过 | 契约 §3.3 |
| 5 | `result_url` 保持原行为（`data.0.url`，不跳过空串），只有 `result_urls` 跳过空串 | 契约 §7.2 |
| 6 | B13 生图预检对「订阅窗口超限」与「企业钱包不足」只有错误映射级单测，没有经过真实计费服务的端到端用例 | 端到端由 qa 第二轮覆盖；上线前必须补齐证据 |
| 8 | 错误归一按实现为 14 条规则：显式 `error.reason` / `error.details[].reason` 优先于形态规则；Google 体按消息识别本地网关错误（消息由 `infraerrors.Message(service.ErrX)` 现算，加中间件字面量），Google 402 归为 `insufficient_balance`，订阅窗口从消息中的 `reason="X"` 提取；OpenAI 体 `type` 只有属于 §6.1 码表才提升为 reason | 契约 §6.2。核对代码发现两处偏差（规则 1 未检查 `type`、订阅窗口提取值未归为 `USAGE_LIMIT_EXCEEDED`），记入 §6.2.1 待 backend 修正 |
| 9 | Key 额度用完的检查先于模型可用性：options 的 `key_unavailable` 只看「状态非 active 或已过期」；Submit 与 Estimate 解析 Key 后先查 `IsQuotaExhausted()`，用完直接返回 429 `API_KEY_QUOTA_EXHAUSTED`；禁用或过期仍返回 400 `CREATION_MODEL_NOT_AVAILABLE` | 契约 §3.1、§4.3、§5.2 V2a |

## 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 生图实际计费可能高于按所选档位算出的预检金额（上游输出更大尺寸、token 计费、执行时模型映射不同） | v1.1 已按用户决定改为所选档位；执行器内准入照常拦截。生成完成后的扣费沿用现有逻辑，余额不足时会透支，可能出现小额负余额——契约 §9、§11.3 如实记录，本变更不改扣费逻辑 |
| 未配置 mapping 的分组只展示默认模型 | 契约 §3.4、§11.3 记录；需要更多模型时由管理员配置 mapping |
| Gemini 视频路径的错误识别依赖文案，网关改文案会退化为通用错误 | 消息表尽量用错误常量现算；后续改进：让 Gemini 错误体带出字符串 code（需改网关 handler），契约 §11.3 记录 |
| 生图预检的订阅窗口、企业钱包分支缺端到端验证 | qa 第二轮覆盖（实现确认第 6 条） |
| Grok 生图若写原生 `resolution`，计费档会落到默认 2K | v1.1 改为把 `1k`/`2k` 写入原生 `size`，由网关换算为上游 `resolution` 并删除 `size` |
| 预检与执行器之间存在竞态 | 契约写明以执行器为准；后到的任务以失败结束并展示原文 |
| 上传链接对上游不可达（内网 MinIO） | 发布说明要求存储端点可从公网访问；Gemini 与生图编辑因为读回字节，不受影响 |
| 上传对象不会被应用删除 | 存储桶生命周期规则（发布说明必配项） |
| 能力表抽取改变行为 | 逐项等价的表驱动单测；原有测试必须全绿 |
| 内层重放导致 Ops 双记录 | 接受，可通过 request id 关联；审计只记录外层一次 |
| 视频估算与冻结额因峰值边界不同 | 文案标注「预计」；以提交时冻结为准 |
| 本地快照在共享电脑上残留 | 按用户隔离、7 天过期；作为已知限制记录 |
