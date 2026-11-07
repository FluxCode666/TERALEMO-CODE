# 创作页接口契约（Creation Studio）

> **状态：已冻结 ｜ 冻结日期：2026-11-07 ｜ 版本：v1.2**（v1.1 改生图计价口径；v1.2 补记实现确认的事实，接口字段无变化。见文末变更记录）
>
> 适用：用户侧「创作页」`/creation`。生图走**异步生图**链路，生视频走 Seedance / Gemini(Veo) / MiniMax / Grok 四种既有原生链路。
>
> 本文是前后端唯一接口契约。路径、字段名、类型、错误码、参数映射**不得自由发挥**。实现中发现契约无法落地时，先停下来提问；任何改动必须追加到文末「变更记录」并升级版本号，不得直接改正文。
>
> 相关：[ASYNC_IMAGE_TASKS.md](ASYNC_IMAGE_TASKS.md)、[ASYNC_VIDEO_TASKS.md](ASYNC_VIDEO_TASKS.md)、`openspec/changes/add-creation-studio/`、`frontend/src/styles/creation-studio.spec.md`（视觉与交互）

---

## 0. 速览

### 0.1 接口清单（v1）

| 方法 | 路径 | 说明 | 新增/修改 |
| --- | --- | --- | --- |
| GET | `/api/v1/user/creation/options?api_key_id=` | 按所选 Key 的分组返回可用模式、模型、参数描述符、上传可用性 | 新增 |
| POST | `/api/v1/user/creation/estimate` | 提交前的预计费用与余额判断，无持久副作用 | 新增 |
| POST | `/api/v1/user/creation/uploads` | 上传一张参考图/首帧/编辑原图到对象存储，返回 `upload_id` 与链接 | 新增 |
| POST | `/api/v1/user/creation/tasks` | 统一提交生图/生视频；后端构造原生请求并在进程内重放到既有网关路由 | 新增 |
| GET | `/api/v1/user/async-tasks`、`/api/v1/user/async-tasks/{id}` | 历史与进度（复用）；图片任务新增可选字段 `result_urls` | 修改（只加字段） |
| GET | `/api/v1/admin/async-tasks`、`/api/v1/admin/async-tasks/{user_id}/{id}` | 管理端同上，自动带 `result_urls` | 修改（只加字段） |
| POST | `/api/v1/user/async-tasks/{id}/refresh` | 刷新处理中的视频（复用，不变） | 不变 |
| GET | `/api/v1/user/async-tasks/{id}/content` | 视频字节下载（复用，不变） | 不变 |

### 0.2 链路

```text
浏览器（登录态 JWT）
  ├─ GET  /creation/options?api_key_id=7
  ├─ POST /creation/uploads (multipart)          → 对象存储 <prefix>creation-uploads/<user_id>/...
  ├─ POST /creation/estimate
  ├─ POST /creation/tasks
  │     └─ 校验 Key 归属 → 解析上传 → 构造原生请求 → 进程内重放（Authorization: Bearer <该 Key>；重放用的明文由后端按 api_key_id 读取，只在后端内存）
  │          图片  POST /v1/images/generations/async | /v1/images/edits/async   （重放时额外执行「创作页生图预检」）
  │          视频  POST /v1/contents/generations/tasks                         （Seedance）
  │                POST /v1beta/models/{model}:predictLongRunning              （Gemini）
  │                POST /v2/video_generation | /v1/video_generation            （MiniMax）
  │                POST /v1/videos/generations                                 （Grok）
  └─ GET  /async-tasks?...                        → 历史、进度、结果（图片 result_urls 全量）
```

- 任务全部进入现有「异步任务」目录：图片 `imgtask_*`，视频 `vidtask_*`。`/async-tasks` 页可见。
- 面板接口**不接受**原生请求体、上游 URL、客户端指定的上游路径、data URL。
- Key 明文：创作接口（options / estimate / uploads / tasks）的请求只带 `api_key_id`，响应、日志、审计都不含 Key 明文；重放用的明文由后端读取，只在后端内存。Key 选择列表复用「API 密钥」页的既有接口 `GET /api/v1/keys`，其响应含明文，同一登录态本来就能拿到，创作页没有扩大暴露面（见 §10.3、§11.3）。
- v1 **不支持**合成分组（`platform=composite`）：options 对其返回两种模式均不可用（`platform_unsupported`）。

---

## 1. 通用约定

### 1.1 认证与中间件

- 四个新接口都注册在 `backend/internal/server/routes/user.go` 的 `user := authenticated.Group("/user")` 下，与 `/user/async-tasks` 共用 `jwtAuth` → `BackendModeUserGuard` → `panelRateLimiter.Global()` → `auditLog`。
- 注册写法（固定）：

```go
user.GET("/creation/options", h.CreationStudio.Options)
user.POST("/creation/estimate", h.CreationStudio.Estimate)
user.POST("/creation/uploads", middleware.RequestBodyLimit(11<<20), panelRateLimiter.Heavy(), h.CreationStudio.Upload)
user.POST("/creation/tasks", middleware.RequestBodyLimit(1<<20), h.CreationStudio.Submit)
```

- **审计正文省略（必须）**：`backend/internal/server/middleware/audit_log.go` 中
  - `auditBodyOmittedRoutes` 加入 `"POST /api/v1/user/creation/estimate"`、`"POST /api/v1/user/creation/uploads"`、`"POST /api/v1/user/creation/tasks"`；
  - `auditActionOverrides` 加入 `"POST /api/v1/user/creation/uploads": "user.creation.upload"`、`"POST /api/v1/user/creation/tasks": "user.creation.submit"`。
  - 这是 backend 对 middleware 目录**唯一**允许的改动。
- Key 只以 `api_key_id` 引用。归属校验：`apiKeyService.GetByID` 取到 Key 后要求 `key.UserID == subject.UserID`，否则 `404 CREATION_KEY_NOT_FOUND`（不区分「不存在」与「不属于我」）。之后用 `apiKeyService.GetByKey(ctx, key.Key)` 取鉴权同款水合对象（含 `User.Enterprise`、`Group`），供 options/estimate 使用。
- 企业成员可以使用创作页全部能力。路由**不加** `requiresPersonalWallet`；企业准入（企业/成员状态、可用分组）由内层重放的 `apiKeyAuth` 执行。

### 1.2 响应包络

沿用 `internal/pkg/response`：

```jsonc
{ "code": 0, "message": "success", "data": { /* ... */ } }                       // 成功
{ "code": 402, "message": "Available balance does not cover the video reservation",
  "reason": "insufficient_balance", "metadata": { "retry_after": "30" } }       // 失败
```

- 失败时 HTTP 状态码 = `code`。`reason` 是稳定机器码，前端只按 `reason` 分支；`message` 可直接展示（可能为网关原文，英文）。
- `metadata` 可选，值全部为字符串。约定键：`retry_after`（秒）、`window_resets_at`（RFC3339）、`max_bytes`、`max_side_px`。
- 唯一例外：面板通用限流返回的 429 没有 `reason`，见 §6.3。

### 1.3 标识、时间、金额

- 时间：Unix 秒（`int64`）。
- 金额：USD，`float64`，最多 8 位小数；前端展示保留 2–4 位。
- `upload_id`：`cupl_` + 32 位小写 hex。
- 目录 ID：见 §5.6。

---

## 2. `POST /api/v1/user/creation/uploads` —— 上传参考图

### 2.1 请求

- `Content-Type: multipart/form-data`，**只有一个**文件字段 `file`。其它字段忽略。
- 前端一次只传一张；多张参考图逐张上传。

### 2.2 校验（按顺序，任一失败即返回，不写对象存储、不写 Redis）

| 序号 | 规则 | 失败 |
| --- | --- | --- |
| U1 | 对象存储开关可用：`ImageTaskService.Enabled()` 为真（与异步生图同一开关） | 404 `CREATION_STORAGE_DISABLED` |
| U2 | 请求体 ≤ 11 MiB（路由级 `RequestBodyLimit`），且 `file` 字段存在、恰好一个 | 413 `CREATION_UPLOAD_TOO_LARGE` / 400 `INVALID_CREATION_REQUEST` |
| U3 | 文件字节数 ≤ 10 MiB（10485760） | 413 `CREATION_UPLOAD_TOO_LARGE`，`metadata.max_bytes="10485760"` |
| U4 | 用 `http.DetectContentType(前 512 字节)` 嗅探类型，必须为 `image/png`、`image/jpeg`、`image/webp`；**忽略**客户端声明的 Content-Type 与文件名 | 415 `CREATION_UPLOAD_UNSUPPORTED_TYPE` |
| U5 | `image.DecodeConfig` 能解出宽高（导入 `image/png`、`image/jpeg`、`golang.org/x/image/webp`），且 `16 ≤ 宽,高 ≤ 4096`、`宽×高 ≤ 16777216` | 422 `CREATION_UPLOAD_INVALID_IMAGE`，`metadata.max_side_px="4096"` |
| U6 | 该用户 24 小时内未过期的上传数 < 100 | 429 `CREATION_UPLOAD_LIMIT_EXCEEDED` |

### 2.3 存储

- 对象 key（相对 key，由 `ImageResultUploader` 拼上已配置的 `prefix`）：

```text
creation-uploads/<user_id>/<YYYYMMDD(UTC)>/<upload_id><ext>      ext ∈ .png | .jpg | .webp（按嗅探类型）
```

- 写入：`ImageResultUploader.SaveObject(ctx, relKey, contentType, data) (url string, err error)`，内部调用 `ImageStorage.Save(ctx, prefix+relKey, ...)`。返回值 `url` 由对象存储决定：配置了 `public_base_url` 为公开直链，否则为 presigned 链接（有效期 `presign_expiry_hours`，默认 24 小时）。
- 有效期：
  - `expires_at = created_at + min(24h, presign_expiry)`；配了 `public_base_url` 时为 `created_at + 24h`。
  - 获取链接策略：S3 存储实现新增 `LinkPolicy() (permanent bool, ttl time.Duration)`，`ImageResultUploader.LinkPolicy()` 透传；存储未实现该接口时按 presigned、24 小时处理。
- 元数据写 Redis（**不新增数据库表**），由现有 `imageTaskStore`（`backend/internal/repository/image_task_store.go`）实现新接口 `service.CreationUploadStore`：

```text
creation_upload:<upload_id>          STRING  JSON(CreationUploadRecord)  TTL = expires_at - now
creation_uploads:user:<user_id>      ZSET    member=upload_id, score=expires_at；写入前 ZREMRANGEBYSCORE 清掉过期项；key TTL 48h
```

- 写入失败（对象存储或 Redis）→ 502 `CREATION_UPLOAD_STORAGE_FAILED`。对象已写入但 Redis 失败时不回滚对象（依赖生命周期规则清理，见 §11）。

### 2.4 响应

```ts
export interface CreationUpload {
  upload_id: string        // cupl_<32hex>
  url: string              // 公开直链或 presigned 链接；前端用于缩略图预览
  content_type: 'image/png' | 'image/jpeg' | 'image/webp'
  size_bytes: number
  width: number
  height: number
  created_at: number
  expires_at: number
}
```

Go：

```go
// backend/internal/service/creation_studio_upload.go
type CreationUploadRecord struct {
    UploadID    string `json:"upload_id"`
    UserID      int64  `json:"user_id"`
    ObjectKey   string `json:"object_key"`   // 相对 key（不含 prefix），不返回给前端
    URL         string `json:"url"`
    ContentType string `json:"content_type"`
    SizeBytes   int64  `json:"size_bytes"`
    Width       int    `json:"width"`
    Height      int    `json:"height"`
    SHA256      string `json:"sha256"`       // 不返回给前端
    CreatedAt   int64  `json:"created_at"`
    ExpiresAt   int64  `json:"expires_at"`
}

type CreationUploadStore interface {
    SaveCreationUpload(ctx context.Context, rec *CreationUploadRecord) error
    GetCreationUpload(ctx context.Context, uploadID string) (*CreationUploadRecord, error) // 不存在返回 ErrCreationUploadNotFound
    CountActiveCreationUploads(ctx context.Context, userID int64) (int64, error)
}
```

`ImageTaskService` 通过对 `s.store` 做类型断言取得 `CreationUploadStore`（与 `taskCatalog()` 同一写法），**不新增 Wire provider**。

### 2.5 使用期校验（提交时）

提交接口解析每个 `upload_id`：

| 情形 | 结果 |
| --- | --- |
| Redis 中无记录（从未存在或已过 TTL） | 404 `CREATION_UPLOAD_NOT_FOUND` |
| `record.UserID != subject.UserID` | 404 `CREATION_UPLOAD_NOT_FOUND`（不泄露存在性） |
| `record.ExpiresAt - now < 900`（不足 15 分钟） | 410 `CREATION_UPLOAD_EXPIRED` |
| 通过 | 按协议填入原生请求（§5.4） |

留 15 分钟余量是因为上游在创建后才会去拉取链接。

---

## 3. `GET /api/v1/user/creation/options?api_key_id=`

### 3.1 响应

```ts
export type CreationMode = 'image' | 'video'
export type VideoProtocol = 'seedance' | 'gemini' | 'minimax' | 'grok'
export type ImageProtocol = 'openai' | 'grok'
export type CreationUnavailableReason =
  | 'no_group'              // Key 未绑定分组
  | 'platform_unsupported'  // 分组平台不支持该模式（含合成分组）
  | 'storage_disabled'      // 对象存储未开启
  | 'permission_denied'     // 分组禁止生图（allow_image_generation=false）
  | 'no_accounts'           // 分组内无可调度的对应账号
  | 'key_unavailable'       // Key 状态非 active（含已被系统置为 quota_exhausted）或已过期（creationOptionsKeyUnavailable）；状态仍为 active、只是额度用完的 Key 不受影响

export interface CreationOptionsResponse {
  api_key: { id: number; name: string; group_id: number | null; group_name: string; platform: string }
  image: CreationAvailability
  video: CreationAvailability
  /** 参考图/首帧/编辑原图的上传能力；不可用时所有 kind=image 描述符 disabled */
  uploads: CreationAvailability
  upload_limits: {
    max_bytes: number        // 10485760
    accept: string[]         // ['image/png','image/jpeg','image/webp']
    max_side_px: number      // 4096
    max_pixels: number       // 16777216
  }
  /** 按 (mode, protocol, model) 去重；顺序即展示顺序 */
  models: CreationModelOption[]
}

export interface CreationAvailability {
  available: boolean
  reason?: CreationUnavailableReason
}

export interface CreationModelOption {
  id: string                         // `${mode}:${protocol}:${model}`
  mode: CreationMode
  protocol: ImageProtocol | VideoProtocol
  model: string                      // 提交时原样回传
  display_name: string
  params: CreationParamDescriptor[]  // 渲染顺序
  pricing: CreationPricingHint
  /** 多个账号能力不一致、只有部分账号支持某些取值时为 true */
  partial: boolean
}

export interface CreationPricingHint {
  priced: boolean
  mode?: 'video' | 'per_request' | 'token' | 'image'
  unit_price?: number | null         // 视频：USD/秒 或 USD/次（取默认分辨率档）；图片：USD/张（2K 档）
}
```

- 可用性判定（后端）：
  - `image`：分组平台 ∈ {openai, grok}，否则 `platform_unsupported`；`ImageTaskService.Enabled()` 为假 → `storage_disabled`；`GroupAllowsImageGeneration` 为假 → `permission_denied`；无图片模型 → `no_accounts`。
  - `video`：分组平台 ∈ {video, grok}，或 openai 且有启用 Seedance 能力的账号；否则 `platform_unsupported`；无对应账号 → `no_accounts`。
  - `uploads`：`ImageTaskService.Enabled()` 为假 → `storage_disabled`。
  - Key 非 active、已过期或额度用完 → 三者均为 `key_unavailable`。
- options 只是**提示**。最终以提交结果为准；两者不一致时前端按提交错误刷新状态。

### 3.2 参数描述符

`kind` 只有四种，与 `creation-studio.spec.md` §4.5 一致；布尔量用 `select`（`true`/`false`）表示，**不引入第五种**：

```ts
export type CreationParamDescriptor =
  | { key: string; kind: 'select'; labelKey: string; options: CreationSelectOption[]; default: string; hintKey?: string }
  | { key: string; kind: 'integer'; labelKey: string; min: number; max: number; step: number; unitKey?: string; default: number; hintKey?: string }
  | { key: string; kind: 'ratio'; labelKey: string; options: { value: string; w: number; h: number }[]; default: string; hintKey?: string }
  | {
      key: 'source' | 'first_frame' | 'last_frame' | 'reference'
      kind: 'image'
      labelKey: string
      maxCount: number
      accept: string[]
      maxSizeMB: number          // 10
      required: boolean
      hintKey?: string
      disabled?: boolean         // 上传不可用时为 true
      disabledReason?: 'storage_disabled'
    }

export interface CreationSelectOption {
  value: string
  labelKey?: string
  label?: string
  disabled?: boolean
  hintKey?: string
}
```

- `labelKey` 固定取 `creationStudio.params.<key>`；`unitKey` 时长取 `creationStudio.units.seconds`。
- `kind=image` 的 `key` 就是提交时 `images[].role` 的值。
- `kind=integer` 一律给出 `step`（默认 1）。

### 3.3 每个协议下发的描述符（v1 固定，不多不少）

#### 生图 · OpenAI（`protocol=openai`；模型：分组可用模型中 `IsGPTImageGenerationModel` 为真者）

| key | kind | 取值 | 默认 | 映射到 |
| --- | --- | --- | --- | --- |
| `size` | select | `1024x1024`、`1536x1024`、`1024x1536`、`auto` | `1024x1024` | `size` |
| `quality` | select | `auto`、`low`、`medium`、`high` | `auto` | `quality` |
| `n` | integer | 1–4，step 1 | 1 | `n` |
| `source` | image | maxCount 1，required=false | — | 有图 → 走编辑端点（§5.4） |

#### 生图 · Grok（`protocol=grok`；模型：`IsGrokImageGenerationModel` 为真者）

| key | kind | 取值 | 默认 | 映射到 |
| --- | --- | --- | --- | --- |
| `aspect_ratio` | ratio | `1:1`、`16:9`、`9:16`、`4:3`、`3:4` | `1:1` | `aspect_ratio` |
| `resolution` | select | `1k`、`2k` | `1k` | 原生 **`size`**（值原样为 `1k`/`2k`）。不写原生 `resolution`：Grok 的计费档位取自 `size`（`ParseGrokMediaRequest` → `SizeTier`/`ImageInputSize`），网关的 `applyGrokImagineImageGeometry` 会把 `size` 换算成上游的 `resolution` 并删除 `size`，上游请求不变 |
| `n` | integer | 1–4 | 1 | `n` |
| `source` | image | maxCount 1，required=false | — | 有图 → 走编辑端点 |

#### 生视频 · Seedance（`protocol=seedance`）

| key | kind | 取值 | 默认 | 映射到 |
| --- | --- | --- | --- | --- |
| `resolution` | select | 账号最终有效分辨率列表（`seedanceModelLimits` + 账号覆盖），顺序 480p/720p/768p/1080p/2k | 720p（不在列表时取最低档） | 顶层 `resolution` |
| `duration` | integer | `[MinDuration, MaxDuration]`，step 1 | 5（收敛到区间内） | 顶层 `duration` |
| `ratio` | ratio | `16:9`、`9:16`、`4:3`、`3:4`、`1:1`、`21:9`、`adaptive` | `16:9` | 顶层 `ratio` |
| `watermark` | select | `false`、`true` | `false` | 顶层 `watermark`（布尔） |
| `first_frame` | image | maxCount 1 | — | `content[]` role `first_frame` |
| `last_frame` | image | maxCount 1 | — | `content[]` role `last_frame` |
| `reference` | image | maxCount 4 | — | `content[]` role `reference_image` |

v1 不下发：智能时长 `-1`、`frames`、`draft`、`return_last_frame`、`seed`、`callback_url`。

#### 生视频 · Gemini（`protocol=gemini`）

| key | kind | 取值 | 默认 | 映射到 |
| --- | --- | --- | --- | --- |
| `resolution` | select | `geminiVideoModelLimits(mapped)` + 账号覆盖 | 720p | `parameters.resolution` |
| `duration` | integer | Veo 2：5–8 step 1；Veo 3.0：8–8；Veo 3.1：4–8 step 2；账号配了时长覆盖则为覆盖区间 step 1 | 8（收敛到区间内） | `parameters.durationSeconds` |
| `aspect_ratio` | ratio | `16:9`、`9:16` | `16:9` | `parameters.aspectRatio` |
| `first_frame` | image | maxCount 1 | — | `instances[0].image` |
| `reference` | image | maxCount 3；只对映射后为 `veo-3.1-*` 的模型下发 | — | `instances[0].referenceImages[]` |

`parameters.sampleCount` 固定为 1，不下发描述符。

#### 生视频 · MiniMax（`protocol=minimax`）

模型名以 `MiniMax-H3` 开头（不区分大小写，`creationStudioIsMiniMaxV2`）为 **v2 形态**，否则为 **v1 形态**。

> 判定分工（v1.2 确认）：创作层**只按模型名前缀**选择请求形态与路径（`/v2/video_generation` 或 `/v1/video_generation`）；网关按账号的 `base_url` 是否以 `/v2` 结尾判定账号版本（`miniMaxVideoV2`）。二者不一致时（例如非 H3 模型名映射到 v2 账号、或 H3 模型名落到只有 v1 账号的分组），由网关按既有规则拒绝或跳过不兼容账号，错误原文经 §6.2 归一返回。创作层不做额外转换。

| key | kind | 取值 | 默认 | 映射到（v1 / v2） |
| --- | --- | --- | --- | --- |
| `resolution` | select | `miniMaxVideoModelLimits(account, mapped)` + 账号覆盖 | 768p（不在列表时取最低档） | `resolution`，大写形式（`768P`） |
| `duration` | integer | v1 默认 6–10 step 4（即 6/10）；v2 4–15（H3-Max 5–15）step 1；账号覆盖后为覆盖区间 step 1 | 6 | `duration` |
| `ratio` | ratio | 仅 v2：`16:9`、`9:16`、`4:3`、`3:4`、`1:1`、`21:9`、`adaptive` | `16:9` | 仅 v2：`ratio` |
| `first_frame` | image | maxCount 1 | — | v1 `first_frame_image` / v2 `content[]` role `first_frame` |
| `last_frame` | image | maxCount 1 | — | v1 `last_frame_image` / v2 `content[]` role `last_frame` |
| `reference` | image | 仅 v2，maxCount 1 | — | v2 `content[]` role `reference_image` |

#### 生视频 · Grok（`protocol=grok`；模型：`grok-imagine-video*`）

| key | kind | 取值 | 默认 | 映射到 |
| --- | --- | --- | --- | --- |
| `duration` | integer | 1–15 | 8 | `duration` |
| `resolution` | select | `480p`、`720p`、`768p`、`1080p`、`2k` | `480p` | `resolution` |
| `first_frame` | image | maxCount 1 | — | `image: {"url": <链接>}` |

v1 不做 Grok 视频编辑/续写。

### 3.4 模型清单来源（以 `backend/internal/service/creation_studio_catalog.go` 为准）

只遍历分组内 `IsSchedulable()` 的账号，按账号平台分别枚举；`model_mapping` 中的通配项（含 `*` 或 `?`）一律跳过，结果按字典序排列。

| 模式 · 协议 | 账号条件 | 有 `model_mapping` 时 | 无 `model_mapping`（放行全部）时 |
| --- | --- | --- | --- |
| 生图 · openai | `platform=openai` | mapping 的 key 中 `IsGPTImageGenerationModel` 为真者 | 单一默认 `gpt-image-2` |
| 生图 · grok | `platform=grok` | mapping 的 key 中 `IsGrokImageGenerationModel` 为真者 | 单一默认 `grok-imagine-image` |
| 视频 · grok | `platform=grok` | mapping 的 key 中以 `grok-imagine-video` 开头者（不区分大小写） | 单一默认 `grok-imagine-video` |
| 视频 · seedance（旧 OpenAI 平台） | `platform=openai` 且开启 Seedance 能力 | mapping 的 key 同时满足两条才列出：① 映射目标经 `VideoTestModelMatchesProtocol(mapped, "seedance")` 不属于其它协议；② 公开别名或映射目标至少一个呈 Seedance 形态（不区分大小写包含 `seedance`，或以 `ep-` 开头）。第 ② 条必需，因为 `VideoTestModelMatchesProtocol` 对未知名字默认返回 true，单用它挡不住 `gpt-4o → gpt-4o` 这类文本模型。过滤后为空则该账号不贡献模型 | 单一默认 `doubao-seedance-1-0-pro` |
| 视频 · seedance / gemini / minimax（Video 平台） | `platform=video`，协议取 `VideoProtocol()` | `Account.VideoTestModelIDs()`：映射目标经 `VideoTestModelMatchesProtocol` 判定属于本协议的公开名，再加上协议默认清单中被账号支持、且映射后属于本协议的项 | 协议默认清单：seedance `doubao-seedance-1-0-pro`；gemini `veo-3.0-fast-generate-001`、`veo-3.0-generate-001`；minimax v1 `MiniMax-Hailuo-2.3`、`MiniMax-Hailuo-2.3-Fast`、`MiniMax-Hailuo-02`；minimax v2（`base_url` 以 `/v2` 结尾）`MiniMax-H3`、`MiniMax-H3-Max` |

- 视频候选还要通过 `creationStudioVideoLimits(account, protocol, model)` 解析能力表；解析失败（例如 MiniMax 型号与账号版本不兼容）的候选直接丢弃，不出现在 options 中。
- 有 mapping 但没有任何项满足条件时，该账号不贡献模型（不回落到默认名）。

### 3.5 描述符生成规则（后端）

- 能力表与提交期规范化**同源**：Seedance 用 `seedanceModelLimits`；Gemini 用抽出的 `geminiVideoModelLimits(mappedModel)`；MiniMax 用抽出的 `miniMaxVideoModelLimits(account, mappedModel)`；Grok 用 `grok_video_submission.go` 的固定规则。账号 `video_resolutions`、`video_min_duration_seconds`、`video_max_duration_seconds` 一律经 `resolveVideoAccountLimits` 计算。
- 同一 `(mode, protocol, model)` 由多个账号承载时：分辨率取并集，时长取 `[min(Min), max(Max)]`；只要并集比任一账号的能力大，就置 `partial=true`。
- 默认值必须落在最终取值范围内。
- 上传不可用（`uploads.available=false`）时，所有 `kind=image` 描述符置 `disabled=true, disabledReason='storage_disabled'`。生图模式本身此时也不可用（`image.reason=storage_disabled`）；纯文生视频不受影响。

---

## 4. `POST /api/v1/user/creation/estimate`

### 4.1 请求

```ts
export interface CreationEstimateRequest {
  api_key_id: number
  mode: CreationMode
  model: string
  protocol: ImageProtocol | VideoProtocol
  params: Record<string, string | number>
  /** 只用于判断走生成还是编辑端点、以及 token 兜底的体积估算；后端不读取对象 */
  images?: CreationImageRef[]
}
```

### 4.2 响应

```ts
export interface CreationEstimateResponse {
  priced: boolean
  /**
   * 本次提交的计费金额，也是提交时的准入金额（唯一金额字段）：
   * - image：= §5.3 预检金额 = 所选尺寸档位单价 × n × 图片倍率（EstimateCreationImageCost）
   * - video：= 提交时冻结额（BuildVideoPricingContract.ReservedAmount）
   */
  estimated_cost: number
  /**
   * image_tier：按所选档位计价（图片按次/按档计费）
   * image_token_estimate：渠道为 token 计费，金额为网关在途估算（token 只有生成后才知道）
   * video_reservation：视频冻结额
   */
  cost_basis: 'image_tier' | 'image_token_estimate' | 'video_reservation'
  /** 仅 cost_basis=image_tier：计价档位 */
  selected_tier?: '1K' | '2K' | '4K'
  pricing_mode: 'image' | 'video' | 'per_request' | 'token'
  pricing_source?: 'channel' | 'group' | 'fallback' | 'default'
  unit_price?: number | null
  detail: { label: 'count' | 'duration' | 'resolution' | 'unit_price' | 'multiplier'; value: string }[]
  balance_scope: 'personal' | 'enterprise' | 'subscription'
  /** 仅 personal 时给出 */
  available_balance: number | null
  minimum_reserve: number
  /** 仅 personal 时给出 true/false；enterprise、subscription 为 null（以提交结果为准） */
  sufficient: boolean | null
  currency: 'USD'
}
```

Go（`backend/internal/service/creation_studio_estimate.go`，json tag 与上面的 TS 一一对应）：

```go
type CreationEstimateResponse struct {
    Priced           bool                 `json:"priced"`
    EstimatedCost    float64              `json:"estimated_cost"`
    CostBasis        string               `json:"cost_basis"`              // image_tier | image_token_estimate | video_reservation
    SelectedTier     string               `json:"selected_tier,omitempty"` // 1K | 2K | 4K
    PricingMode      string               `json:"pricing_mode"`
    PricingSource    string               `json:"pricing_source,omitempty"`
    UnitPrice        *float64             `json:"unit_price"`
    Detail           []CreationCostDetail `json:"detail"`
    BalanceScope     string               `json:"balance_scope"`
    AvailableBalance *float64             `json:"available_balance"`
    MinimumReserve   float64              `json:"minimum_reserve"`
    Sufficient       *bool                `json:"sufficient"`
    Currency         string               `json:"currency"`
}

type CreationCostDetail struct {
    Label string `json:"label"` // count | duration | resolution | unit_price | multiplier
    Value string `json:"value"`
}
```

#### 4.2.1 图片计价函数（预检与估算共用，backend 必须复用现有函数，不得另写映射）

```go
// backend/internal/service/creation_studio_estimate.go
type CreationImageCostEstimate struct {
    Amount    float64        // 计费金额（USD，已乘倍率）
    Basis     string         // "image_tier" | "image_token_estimate"
    Tier      string         // Basis=image_tier 时为 1K/2K/4K
    Priced    bool
    Breakdown *CostBreakdown // Basis=image_tier 时非空
}

func (s *OpenAIGatewayService) EstimateCreationImageCost(
    ctx context.Context, apiKey *APIKey, userID int64,
    billingModel, size string, n, bodyBytes int,
) CreationImageCostEstimate
```

实现步骤（每一步都复用网关执行时 `RecordUsage` 走的同一函数）：

1. **档位**：`tier := ResolveImageBillingSize(size, nil).BillingSize`。这正是执行期 `ApplyOpenAIImageBillingResolution` 调用的函数（`backend/internal/service/image_billing_size.go`）；没有输出尺寸时它按 `ClassifyImageBillingTier(size)` 判档，`""`/`auto`/无法识别时默认 `2K`。
2. **倍率**（与 `OpenAIGatewayService.RecordUsage` 相同）：`base := s.cfg.Default.RateMultiplier`；有分组时 `base = s.ResolveUserGroupRateMultiplier(ctx, userID, *apiKey.GroupID, apiKey.Group.RateMultiplier)`（已含用户专属与企业专属倍率）；`_, imageMultiplier := computePeakAwareMultipliers(apiKey, base, time.Now())`（图片按次不叠加峰值，`ImageRateIndependent` 时用独立图片倍率）。
3. **分支**（与 `calculateOpenAIRecordUsageCost` 相同）：
   - `resolved := s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey)`；若 `resolved != nil && resolved.Mode == BillingModeToken`：执行期按 token 计费，生成前无法得知 token，返回 `Basis="image_token_estimate"`，`Amount, Priced = s.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{Model: billingModel, BodyBytes: bodyBytes, Kind: InflightEstimateImage, Units: n})`。
   - 否则：`cost := s.calculateOpenAIImageCost(ctx, billingModel, apiKey, &OpenAIForwardResult{ImageSize: tier, ImageInputSize: size, ImageCount: n}, imageMultiplier)`，返回 `Basis="image_tier"`、`Tier=tier`、`Amount=cost.ActualCost`、`Priced=cost != nil`。`calculateOpenAIImageCost` 即执行期的图片计价函数，覆盖分组价、渠道按次/按档价、`ImagePriceConfig`、默认价全部来源，**不新增任何价格查找逻辑**。
4. `billingModel` 取与执行器在途预留相同的 `routingModel`（见 §5.3）。

#### 4.2.2 规则

- **无持久副作用**：不冻结、不登记在途预留、不写 Redis、不登记任务、不访问上游、不读取上传对象。
- image：`estimated_cost` 与 §5.3 预检调用**同一个函数** `EstimateCreationImageCost`，`size`、`n` 都取表单值。
  - **按档位计费（主口径，`cost_basis=image_tier`）**：金额只取决于档位、`n`、倍率，与请求体大小无关，估算与预检**恒等**。
  - **token 计费渠道（`cost_basis=image_token_estimate`）**：金额依赖 `bodyBytes`。`/estimate` **不读取上传**（既不读对象，也不读元数据），`bodyBytes = len(原生请求体，不含图片)`；而预检用的是重放请求体的实际长度（编辑时含原图的 data URL）。因此 token 计费渠道下的**图片编辑**，估算金额会低于预检金额，可能出现「估算显示足够、提交返回 402」。
- video：`BuildVideoPricingContract` 的入参与 `handleGrokMedia` 冻结合同时一致（Model、Resolution、MaxDurationSeconds、MaxVideoCount=1、AllowTokenFallback=true）。实现者必须对照 `backend/internal/handler/grok_media.go` 中的调用处取值，并由单测 E02 锁定「估算 = 冻结额」。峰值倍率按估算时刻计算；提交时刻跨越峰值边界时两者可能不同，以提交时冻结为准。
- 免费模型：计价结果为 0（显式配置 0 元）时返回 `priced=true`、`estimated_cost=0`；只有计价失败或找不到任何价格时才返回 `priced=false`。生图预检对金额为 0 的请求跳过覆盖检查，直接放行（资格检查照常执行）。
- `sufficient`（personal）：`available_balance > 0` 且不低于 `minimum_reserve` 且 `≥ estimated_cost`。视频不扣除既有冻结（仅供提示），真实判定在提交。
- 前端只在 `sufficient === false` 或 `priced === false` 时禁用提交。

### 4.3 错误

| HTTP | reason | 说明 |
| --- | --- | --- |
| 400 | `INVALID_CREATION_REQUEST` | 参数不合法 |
| 400 | `CREATION_MODEL_NOT_AVAILABLE` | 模型不在 options 结果中 |
| 404 | `CREATION_KEY_NOT_FOUND` | Key 不存在或不属于我 |
| 429 | `API_KEY_QUOTA_EXHAUSTED` | Key 额度用完；与 §5.2 V2a 相同，在解析 Key 之后、判断模型可用性之前检查 |
| 503 | `CREATION_ESTIMATE_UNAVAILABLE` | 计费服务不可用 |

`priced=false` 不是错误，返回 200。

---

## 5. `POST /api/v1/user/creation/tasks`

### 5.1 请求

```ts
export interface CreationImageRef {
  role: 'source' | 'first_frame' | 'last_frame' | 'reference'
  upload_id: string
}

export interface CreationSubmitRequest {
  api_key_id: number
  mode: CreationMode
  model: string
  protocol: ImageProtocol | VideoProtocol     // 必填，取 options 中该行的 protocol
  prompt: string
  params: Record<string, string | number>     // 只含非 image 描述符；缺省项由后端补描述符默认值
  images?: CreationImageRef[]
}
```

```go
// backend/internal/service/creation_studio.go
type CreationSubmitRequest struct {
    APIKeyID int64             `json:"api_key_id"`
    Mode     string            `json:"mode"`
    Model    string            `json:"model"`
    Protocol string            `json:"protocol"`
    Prompt   string            `json:"prompt"`
    Params   map[string]any    `json:"params"`
    Images   []CreationImageRef `json:"images,omitempty"`
}

type CreationImageRef struct {
    Role     string `json:"role"`
    UploadID string `json:"upload_id"`
}
```

`params` 值：select/ratio 传字符串；integer 传 JSON 数字（后端也接受纯数字字符串）；布尔 select 传 `"true"`/`"false"`。

### 5.2 校验（重放前，失败不产生任何冻结或任务）

| 序号 | 规则 | 失败 |
| --- | --- | --- |
| V1 | 请求体 ≤ 1 MiB | 413 `CREATION_BODY_TOO_LARGE` |
| V2 | Key 属于当前用户 | 404 `CREATION_KEY_NOT_FOUND` |
| V2a | Key 额度未用完：`key.IsQuotaExhausted()` 为假（`quota > 0 且 quota_used ≥ quota` 即为用完）。**此项先于 V3、V4 检查**：额度用完时直接返回，不再判断模型可用性；禁用或过期的 Key 仍按 V4 返回 400 `CREATION_MODEL_NOT_AVAILABLE` | 429 `API_KEY_QUOTA_EXHAUSTED` |
| V3 | `mode` 合法；`prompt` 去空白后非空且 ≤ 8000 字符（按 rune 计） | 400 `INVALID_CREATION_REQUEST` |
| V4 | `(mode, protocol, model)` 在该 Key 的 options 结果中且对应模式 `available=true` | 400 `CREATION_MODEL_NOT_AVAILABLE`；image 且原因为 `storage_disabled` → 404 `ASYNC_IMAGE_DISABLED` |
| V5 | `params` 的 key 都属于该模型的非 image 描述符；值满足描述符（select/ratio 在 options 内且未 disabled；integer 在 [min,max] 且符合 step） | 400 `INVALID_CREATION_REQUEST` |
| V6 | `images[].role` 是该模型某个 image 描述符的 key，且每个 role 的数量 ≤ `maxCount`；不允许重复 `upload_id` | 400 `INVALID_CREATION_REQUEST` |
| V7 | 有 `images` 时上传可用；否则 | 404 `CREATION_STORAGE_DISABLED` |
| V8 | 每个 `upload_id` 通过 §2.5 | 404 `CREATION_UPLOAD_NOT_FOUND` / 410 `CREATION_UPLOAD_EXPIRED` |
| V9 | 互斥规则：Seedance 与 MiniMax v2 的 `reference` 不能与 `first_frame`/`last_frame` 同时出现 | 400 `INVALID_CREATION_REQUEST` |

跨字段的协议规则（例如 Veo 3.1 的 1080p 或参考图必须 8 秒、Veo 3.0 的 1080p 只支持 16:9）不在创作层实现，由网关规范化拒绝，原文返回。

### 5.3 生图提交预检（硬拦截）

**位置**：在内层重放进入 `AsyncImageHandler.Submit` 后执行，不在面板 handler 里执行。理由：内层链路上 `apiKeyAuth` 已经加载了订阅、企业上下文和分组，预检直接复用这些上下文，不必另写一份。

**触发条件**：只有当请求 context 带创作页重放标记 `service.IsCreationStudioReplay(ctx)` 时才执行。这个标记只能由面板 handler 在进程内设置（未导出的 context key），外部 HTTP 无法伪造。**API 用户直接调用 `/v1/images/*/async` 的行为不变。**

**执行时机**：`checkSecurityAuditBeforeSubmit` 通过之后、`CreateWithMetadata` 之前。预检失败则不创建任务、不写 Redis、不启动 goroutine。

**步骤**：

1. `subscription, _ := middleware.GetSubscriptionFromContext(c)`
2. 资格：`billingCacheService.CheckBillingEligibility(ctx, apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(ctx, apiKey))`，按下表 A 映射。
3. 金额：`e := gatewayService.EstimateCreationImageCost(ctx, apiKey, apiKey.User.ID, routingModel, size, n, len(body))`，`est := e.Amount`（§4.2.1）。`routingModel`、`size`、`n` 的取法与执行器一致：OpenAI 取 `ParseOpenAIImagesRequest` 的 `Model`（经 `ResolvedUpstreamModelFromContext` 修正，同 `openai_images.go` 第 79–82 行）、`Size`、`N`；Grok 取 `ParseGrokMediaRequest` 的 `Model`、`Size`、`N`。`!e.Priced && billingCacheService.InflightReservationFailClosedOnUnpriced()` 时按 402 拒绝。
4. 覆盖：`est > 0` 时调用 `res, err := billingCacheService.ReserveFixedCharge(ctx, apiKey.User, apiKey, apiKey.Group, subscription, est)`，**无论成败都立即 `res.Release()`**（nil 安全），按下表 B 映射。
5. 通过后继续原有流程。

**预检金额口径（v1.1）**：按所选尺寸档位计算，即「档位单价 × n × 图片倍率」，与执行完成后实际扣费使用同一套函数（§4.2.1），而不是网关在途预留使用的 1K/2K/4K 最高档上界。

**为什么覆盖检查用 `ReserveFixedCharge`，而不是直接调用 `reserveInflightBalance`**：`reserveInflightBalance` 在单个请求时会放行（只有「已有在途请求且余额减在途小于估算」才拒绝），还受 `MaxReservationUSD` 封顶，做不到硬拦截。`ReserveFixedCharge` 对给定金额检查：余额模式要求余额覆盖金额，企业要求钱包和限额覆盖，订阅要求日/周/月剩余额度覆盖，同时叠加 Key 额度与 Key 限速窗口。

**预检只是准入，不代替真实预留**：goroutine 内的 `CheckBillingEligibility` 与 `reserveInflightBalance` 照常执行。并发提交时各请求的预检互不可见，以 goroutine 内的结果为准，后到的任务可能仍以 `failed` 结束并带余额错误原文。创作页按任务失败展示。

表 A（资格阶段）：

| 错误 | HTTP | 错误体 `error.code` |
| --- | --- | --- |
| `ErrInsufficientBalance`（余额 ≤ 0 或低于最低保留） | 403 | `INSUFFICIENT_BALANCE` |
| 企业类（`ENTERPRISE_*`、`DEPARTMENT_QUOTA_EXCEEDED`、`MEMBER_QUOTA_EXCEEDED`） | 按 `billingErrorDetailsForContext` | 原 reason |
| `ErrDailyLimitExceeded` / `ErrWeeklyLimitExceeded` / `ErrMonthlyLimitExceeded` | 429 | `USAGE_LIMIT_EXCEEDED` |
| Key 限速、RPM、用户平台配额 | 429 | `rate_limit_exceeded` |
| `ErrBillingServiceUnavailable` | 503 | `billing_service_error` |
| 其它 | 按 `billingErrorDetailsForContext` | 原 code |

表 B（覆盖阶段）：

| 错误 | HTTP | 错误体 `error.code` |
| --- | --- | --- |
| `ErrInsufficientBalance`（余额为正但不足以覆盖） | 402 | `insufficient_balance` |
| `ErrEnterpriseBillingBalanceInsufficient` | 403 | `ENTERPRISE_BALANCE_INSUFFICIENT` |
| 订阅日/周/月窗口 | 429 | `USAGE_LIMIT_EXCEEDED` |
| `ErrAPIKeyQuotaExhausted` | 429 | `API_KEY_QUOTA_EXHAUSTED` |
| Key 5h/1d/7d 限速 | 429 | `rate_limit_exceeded` |
| `ErrBillingServiceUnavailable` | 503 | `billing_service_error` |

预检错误体用 `imageTaskJSONError(c, status, code, message)` 输出（`{"error":{"type":code,"code":code,"message":...}}`）；带重置时间的写 `Retry-After`。

### 5.4 统一表单 → 原生请求（纯函数 `BuildCreationNativeRequest`）

- 输出：`NativeCreationRequest{Path string; Body []byte; Protocol string; RequestIDPrefix string}`。`Body` 是序列化好的 JSON，`Content-Type: application/json`。
- 路径白名单（`creationStudioAllowedPaths`，写死，不可配置）：

```text
/v1/images/generations/async
/v1/images/edits/async
/v1/contents/generations/tasks
/v1beta/models/<model>:predictLongRunning      （<model> 必须通过 validateUpstreamPathSegment）
/v2/video_generation
/v1/video_generation
/v1/videos/generations
```

- 所有描述符参数**显式写入**（含默认值），不依赖网关补默认值。
- 不构造任何跨协议的数量字段（Seedance/MiniMax/Grok 不出现 `n`、`sampleCount`；Gemini 固定 `sampleCount=1`）。

图片字节的取得：需要内联字节时，用 `ImageResultUploader.ReadObject(ctx, record.ObjectKey, 10<<20)` 从对象存储读回（S3 实现新增 `Read`，走 `GetObject`）。**不通过 HTTP 下载自己的链接**：现有下载客户端会拒绝私网地址，自建的 MinIO 会失败。

| mode / protocol | 端点 | 请求体 | 图片 |
| --- | --- | --- | --- |
| image / openai，无 `source` | `/v1/images/generations/async` | `{"model","prompt","size","quality","n"}` | — |
| image / openai，有 `source` | `/v1/images/edits/async` | 同上 + `"images":[{"image_url":"<data URL>"}]` | 读回字节，转为 `data:<type>;base64,...` |
| image / grok，无 `source` | `/v1/images/generations/async` | `{"model","prompt","aspect_ratio","size","n"}`，`size` 为 `1k`/`2k`（来自描述符 `resolution`，见 §3.3） | — |
| image / grok，有 `source` | `/v1/images/edits/async` | 同上 + `"image":{"url":"<data URL>"}` | 读回字节，转为 data URL |
| video / seedance | `/v1/contents/generations/tasks` | `{"model","content":[{"type":"text","text":prompt}, ...图片项],"resolution","duration","ratio","watermark"}` | 图片项 `{"type":"image_url","image_url":{"url":"<上传链接>"},"role":"first_frame"\|"last_frame"\|"reference_image"}`，顺序为 first_frame、last_frame、reference 依次排列 |
| video / gemini | `/v1beta/models/<model>:predictLongRunning` | `{"instances":[{"prompt":prompt, "image":{...}?, "referenceImages":[...]?}],"parameters":{"durationSeconds","resolution","aspectRatio","sampleCount":1}}` | 读回字节：`image={"bytesBase64Encoded":"<b64>","mimeType":"<type>"}`；参考图项为 `{"image":{"bytesBase64Encoded","mimeType"},"referenceType":"asset"}` |
| video / minimax v1 | `/v1/video_generation` | `{"model","prompt","duration","resolution","first_frame_image"?,"last_frame_image"?}` | 上传链接（字符串） |
| video / minimax v2（H3） | `/v2/video_generation` | `{"model","content":[{"type":"text","text":prompt}, ...],"duration","resolution","ratio"}` | `{"type":"image_url","image_url":{"url":"<上传链接>"},"role":"first_frame"\|"last_frame"\|"reference_image"}` |
| video / grok | `/v1/videos/generations` | `{"model","prompt","duration","resolution","image"?}` | `"image":{"url":"<上传链接>"}` |

生图编辑为什么用 JSON 加 data URL，而不是组装 multipart：

1. `ParseOpenAIImagesRequest`（`images[].image_url`）和 `ParseGrokMediaRequest`（`image.url`）都原生支持 JSON 编辑请求；
2. 网关自己处理 multipart 时也是先转成 data URL 再转发（`ModerationDataURL`、`openAIImageUploadToDataURL`），两种写法对网关等价；
3. 不要求上游能访问我方存储桶（OpenAI OAuth 账号走 Responses 转换，不保证会拉取外部 URL）；
4. 省掉 multipart 边界构造与二次解析，纯函数便于测试。

视频的 Seedance、MiniMax、Grok 按用户决定传上传链接，由上游拉取。Gemini 原生只接受内联字节或 GCS URI，因此读回字节内联。

### 5.5 进程内重放

- 面板 handler 持有 `StudioDispatcher`：

```go
type StudioDispatcher interface {
    Dispatch(w http.ResponseWriter, r *http.Request)
}
```

由 `server.registerRoutes` 在 `routes.RegisterGatewayRoutes(...)` 之后调用 `h.CreationStudio.SetDispatcher(r)` 注入（`*gin.Engine` 实现了 `ServeHTTP`，用一个适配类型包一层）。未注入时返回 503 `CREATION_DISPATCHER_UNAVAILABLE`。
- 内层请求：
  - `Method=POST`，`URL.Path=<白名单路径>`，Body 为 `NativeCreationRequest.Body`；
  - Header 只设置 `Authorization: Bearer <key.Key>`、`Content-Type: application/json`，以及透传外层的 `User-Agent`、`X-Forwarded-For`、`X-Real-IP`、`X-Request-ID`；**不透传** `Cookie`、`Origin`、`Referer`、外层 `Authorization`；
  - `RemoteAddr` 复制外层，保证 Key 的 IP 白/黑名单看到真实客户端；
  - context：`service.WithCreationStudioReplay(context.WithoutCancel(outer))`，再套 90 秒超时；
  - 响应写入 `httptest.ResponseRecorder`，最多读 1 MiB。
- 递归防护：面板 handler 入口检查到重放标记时直接返回 500 `CREATION_INTERNAL_ERROR`。
- 内层 2xx 时按协议解析任务标识（找不到视为 502 `upstream_error`）：

| 协议 | 字段 | requestID（用于目录 ID） |
| --- | --- | --- |
| image | `id` | 不需要（目录 ID = `id`） |
| seedance | 顶层 `id` | `"seedance:" + id` |
| gemini | 顶层 `name` | `"gemini:" + name` |
| minimax | `task_id`，没有则 `task.id` | `"minimax:" + id` |
| grok | 顶层 `request_id`，没有则 `id` | `id`（无前缀） |

- 内层非 2xx：按 §6.2 归一后返回。

### 5.6 响应

```ts
export interface CreationSubmitResponse {
  task_id: string           // 原生任务标识（图片为 imgtask_*；Gemini 为 operation name）
  catalog_id: string        // 异步任务目录 ID；极端竞态下可能为空字符串
  mode: CreationMode
  protocol: ImageProtocol | VideoProtocol
  model: string
  status: 'processing'
  estimated_cost: number    // image：预检金额（= /estimate 的 estimated_cost）；video：本次冻结额
  created_at: number
  expires_at: number        // 目录记录到期时间
}
```

- `catalog_id`：
  - 图片：等于 `task_id`（`image_task_store.Save` 同步写入目录）。
  - 视频：`service.AsyncVideoTaskCatalogID(userID, apiKeyID, requestID)` = `"vidtask_" + hex(sha256(fmt.Sprintf("%d:%d:%s", userID, apiKeyID, requestID)))`，与 `ObserveVideoTask` **共用同一函数**。后端再用 `GetUserTask` 确认目录记录已存在；不存在则返回 `""`，前端回落为 `GET /async-tasks?kind=video&search=<task_id>`。
- 视频 `estimated_cost` 的取值：后端提交成功后用 `FindVideoTaskReservation(protocol, requestID)` 读取冻结额；读取失败时退回 `/estimate` 同口径计算的值。

---

## 6. 错误码

### 6.1 总表（前端只按 `reason` 分支）

| HTTP | reason | 场景 | 前端 i18n key（`creationStudio.errors.*`） |
| --- | --- | --- | --- |
| 400 | `INVALID_CREATION_REQUEST` | 表单不合法、互斥规则、网关参数规范化拒绝 | `invalidRequest`（同时展示 message） |
| 400 | `CREATION_MODEL_NOT_AVAILABLE` | 模型不在该 Key 的可用列表 | `modelUnavailable` |
| 400 | `video_pricing_not_configured` | 视频模型无有效价格 | `pricingMissing` |
| 402 | `insufficient_balance` | 余额为正，但扣除既有冻结或在途后不足以覆盖本次金额 | `balanceInsufficient` |
| 403 | `INSUFFICIENT_BALANCE` | 余额 ≤ 0 或低于最低保留余额 | `balanceLow` |
| 403 | `ENTERPRISE_BALANCE_INSUFFICIENT` | 企业钱包不足 | `enterpriseBalance` |
| 403 | `IMAGE_GENERATION_FORBIDDEN` | 分组禁止生图 | `imageForbidden` |
| 403 | `GROUP_DISABLED` / `GROUP_DELETED` / `GROUP_NOT_ALLOWED` | 分组不可用 | `groupUnavailable` |
| 403 | `ENTERPRISE_DISABLED` / `ENTERPRISE_MEMBER_DISABLED` / `ENTERPRISE_GROUP_NOT_ALLOWED` | 企业准入 | `enterpriseBlocked` |
| 403 | `API_KEY_EXPIRED` / `API_KEY_DISABLED` | Key 不可用 | `keyUnavailable` |
| 403 | `SUBSCRIPTION_NOT_FOUND` | 订阅分组无有效订阅 | `subscriptionMissing` |
| 403 | `ACCESS_DENIED` | Key 的 IP 限制 | `ipRestricted` |
| 404 | `CREATION_KEY_NOT_FOUND` | Key 不存在或不属于我 | `keyNotFound` |
| 404 | `ASYNC_IMAGE_DISABLED` | 异步生图未开启（对象存储） | 走 `unavailable.imageStorage` 不可用态 |
| 404 | `CREATION_STORAGE_DISABLED` | 上传/带图提交时对象存储未开启 | 走 `unavailable.uploads` 不可用态 |
| 404 | `CREATION_UPLOAD_NOT_FOUND` | 上传不存在、已过期清理或不属于我 | `uploadExpired`（提示重新上传） |
| 410 | `CREATION_UPLOAD_EXPIRED` | 上传剩余有效期不足 15 分钟 | `uploadExpired` |
| 413 | `CREATION_UPLOAD_TOO_LARGE` | 文件超过 10 MiB | `uploadTooLarge` |
| 413 | `CREATION_BODY_TOO_LARGE` | 提交体超过 1 MiB | `bodyTooLarge` |
| 415 | `CREATION_UPLOAD_UNSUPPORTED_TYPE` | 不是 png/jpeg/webp | `uploadUnsupportedType` |
| 422 | `CREATION_UPLOAD_INVALID_IMAGE` | 无法解码，或尺寸/像素超限 | `uploadInvalidImage` |
| 429 | `rate_limit_exceeded` | RPM、Key 限速、用户平台配额；面板通用限流（无 `reason`，见 §6.3）也按此处理 | `rateLimited`（倒计时 `metadata.retry_after`，§6.3 情形取 `Retry-After` 头） |
| 429 | `USAGE_LIMIT_EXCEEDED` | 订阅日/周/月额度 | `quotaExceeded` |
| 429 | `API_KEY_QUOTA_EXHAUSTED` | Key 额度用完 | `keyQuota` |
| 429 | `VIDEO_RESERVATION_QUOTA_EXCEEDED` | 视频预占触发配额 | `quotaExceeded` |
| 429 | `DEPARTMENT_QUOTA_EXCEEDED` / `MEMBER_QUOTA_EXCEEDED` / 其它 `ENTERPRISE_*` 429 | 企业部门或员工限额 | `enterpriseQuota` |
| 429 | `CREATION_UPLOAD_LIMIT_EXCEEDED` | 24 小时内有效上传达到 100 张 | `uploadLimit` |
| 502 | `upstream_error` | 上游失败；视频结果不确定时预占保留待人工核实 | `upstream` |
| 502 | `CREATION_UPLOAD_STORAGE_FAILED` | 写对象存储或 Redis 失败 | `uploadFailed` |
| 503 | `seedance_no_eligible_account` / `gemini_video_no_eligible_account` / `minimax_video_no_eligible_account` / `grok_media_no_eligible_account` | 分组内无该协议可用账号 | `noCapacity` |
| 503 | `billing_service_error` / `CREATION_ESTIMATE_UNAVAILABLE` | 计费服务不可用 | `billingUnavailable` |
| 503 | `CREATION_DISPATCHER_UNAVAILABLE` | 服务装配异常 | `internal` |
| 500 | `CREATION_INTERNAL_ERROR` | 其它内部错误 | `internal` |

**403 / 402 / 429 的口径**与网关和视频既定规则一致，创作层不另定规则：

- **403 `INSUFFICIENT_BALANCE`**：余额 ≤ 0，或低于 `billing.minimum_balance_reserve`。
- **402 `insufficient_balance`**：余额为正且过了上述门槛，但不足以覆盖本次冻结（视频）或预检金额（生图）。
- **429**：配额或频率限制，与余额无关。

### 6.2 内层错误归一（`creationStudioNormalizeError(status int, header http.Header, body []byte)`）

实现：`backend/internal/handler/creation_studio_errors.go`。识别四种内层错误体：

- 中间件：`{"code":"<字符串>","message"}`
- OpenAI：`{"error":{"type","code"?,"message","reason"?,"metadata"?}}`。注意：`handleGrokMedia` 与 `errorResponse` 把本地错误码写在 **`type`** 而不是 `code`；上游错误的 `type` 则是泛型分类（`api_error`、`invalid_request_error` 等）
- Google：`{"error":{"code":<HTTP 状态数字>,"message","status","details"?:[{"reason"}]}}`。没有字符串机器码
- MiniMax：`{"base_resp":{"status_code","status_msg"}}`

按下表顺序匹配，**命中即停**（「规则」列与代码注释中的编号一致）：

| 规则 | 条件 | 归一结果 |
| --- | --- | --- |
| 1 | 403，且 code 或 OpenAI 体 `type` 为 `billing_error`，且 message 精确等于 `insufficient balance` | 403 `INSUFFICIENT_BALANCE` |
| 2 | 403，且 code 为 `permission_error`，且 message 等于 `service.ImageGenerationPermissionMessage()` | 403 `IMAGE_GENERATION_FORBIDDEN` |
| 3 | 404，且 message 为 `async image tasks are not enabled` | 404 `ASYNC_IMAGE_DISABLED` |
| 4 | 404，且 message 为 `Images API is not supported for this platform` 或 `Videos API is not supported for this platform` | 400 `CREATION_MODEL_NOT_AVAILABLE` |
| 5 | 429，且 OpenAI 体 `type` 为 `insufficient_quota` | 429 `API_KEY_QUOTA_EXHAUSTED` |
| 5b | **显式机器码**：`error.reason`（企业计费元数据桥接写入），或 `error.details[].reason`（Google 鉴权中间件写入）中第一个非空值。Google 体与非 Google 体都认 | 原 status，`reason` = 该值 |
| 5c | Google 体且 HTTP 402（只可能来自视频覆盖检查） | 402 `insufficient_balance` |
| 5d | Google 体且 message 命中下方「Google 本地错误消息表」 | 原 status，`reason` = 表中值 |
| 6 | 400，且 OpenAI 体 `type` 为 `invalid_request_error`，或为 Google 体 | 400 `INVALID_CREATION_REQUEST`，message 透传 |
| 7 | 中间件体的顶层 `code`，或 OpenAI 体的字符串 `error.code` 非空 | 原 status，`reason` = 该 code |
| 7b | OpenAI 体 `type` **属于 §6.1 码表**（或以 `ENTERPRISE_` 开头），即本地网关错误；上游的泛型 `type`（如 `api_error`）**不提升** | 原 status，`reason` = `type` |
| 8 | MiniMax `base_resp.status_code ≠ 0` | 502 `upstream_error`，message 取 `status_msg` |
| 9 | 其它 4xx | 原 status，`INVALID_CREATION_REQUEST` |
| 10 | 其它 5xx | 原 status（503 保持 503，其它一律 502），`upstream_error` |

**Google 本地错误消息表**（`creationStudioGoogleLocalReasons`，规则 5d）。Google 体没有机器码，本地网关错误只能按消息识别；先精确匹配，再匹配动态形态：

| 来源 | 消息 | reason |
| --- | --- | --- |
| 用错误常量现算：`infraerrors.Message(service.ErrX)` | `ErrInsufficientBalance` | `INSUFFICIENT_BALANCE` |
| 同上 | `ErrAPIKeyQuotaExhausted` | `API_KEY_QUOTA_EXHAUSTED` |
| 同上 | `ErrAPIKeyRateLimit5hExceeded`、`ErrAPIKeyRateLimit1dExceeded`、`ErrAPIKeyRateLimit7dExceeded`、`ErrGroupRPMExceeded`、`ErrUserRPMExceeded`、`ErrUserPlatformDailyQuotaExhausted`、`ErrUserPlatformWeeklyQuotaExhausted`、`ErrUserPlatformMonthlyQuotaExhausted` | `rate_limit_exceeded` |
| 同上 | `ErrDailyLimitExceeded`、`ErrWeeklyLimitExceeded`、`ErrMonthlyLimitExceeded` | `USAGE_LIMIT_EXCEEDED` |
| 同上 | `ErrVideoReservationQuotaExceeded` | `VIDEO_RESERVATION_QUOTA_EXCEEDED` |
| 同上 | `ErrBillingServiceUnavailable` | `billing_service_error` |
| Google 鉴权中间件字面量 | `Insufficient account balance` | `INSUFFICIENT_BALANCE` |
| 同上 | `API key 额度已用完` | `API_KEY_QUOTA_EXHAUSTED` |
| 同上 | `API key is disabled` | `API_KEY_DISABLED` |
| 同上 | `API key 已过期` | `API_KEY_EXPIRED` |
| 同上 | `No active subscription found for this group` | `SUBSCRIPTION_NOT_FOUND` |
| `handleGrokMedia` 覆盖检查字面量 | `Available balance does not cover the video reservation` | `insufficient_balance` |
| 动态：前缀匹配 | `Access denied. Your IP is …` | `ACCESS_DENIED` |
| 动态：订阅窗口（中间件写入整段 ApplicationError 文本） | 消息中含 `reason="X"`（正则 `reason="([A-Z][A-Z0-9_]*)"`） | 提取出的 `X`，再按 §6.2.1 第 2 条归为 `USAGE_LIMIT_EXCEEDED` |

- 内层 `Retry-After` 写入 `metadata.retry_after`；`metadata` 对象（顶层或 `error.metadata`，如企业计费的 `window_resets_at`）原样复制。
- 规则 1 是对「低于最低保留余额时网关返回 `403 billing_error`」的处理：**只在创作层归一，不改网关对外行为**。生图预检（§5.3）直接输出 `INSUFFICIENT_BALANCE`，不经过这条。视频路径中，Seedance、MiniMax、Grok 的错误体把 `billing_error` 写在 `error.type`，经规则 1 归一；Gemini 的错误体是 Google 格式，经规则 5d 由消息 `insufficient balance` 归一。

#### 6.2.1 实现偏差（2026-11-07 核对代码发现，待 backend 修正）

以下两条为本契约的目标行为，当前实现尚未满足：

1. **规则 1 只检查了 `code`**：实现条件为 `envelope.code == "billing_error"`，而 Seedance、MiniMax、Grok 视频的余额错误体是 `{"error":{"type":"billing_error","message":"insufficient balance"}}`（`code` 为空）。结果这类错误落到规则 7b，`reason` 为 `billing_error`（不在 §6.1 码表）。修正：规则 1 同时检查 `type`。
2. **订阅窗口 `reason="X"` 未归一**：实现把提取出的 `DAILY_LIMIT_EXCEEDED` / `WEEKLY_LIMIT_EXCEEDED` / `MONTHLY_LIMIT_EXCEEDED` 原样作为 `reason`（单测 `creation_studio_errors_test.go` 也是这样期望的），它们不在 §6.1 码表。修正：这三个值归为 `USAGE_LIMIT_EXCEEDED`。

### 6.3 包络例外：面板通用限流的 429

所有 `/api/v1/user/*` 共用的面板限流中间件（`backend/internal/server/middleware/panel_rate_limit.go`，含路由上的 `panelRateLimiter.Global()` 与 `Heavy()`）命中时，在请求进入创作 handler **之前**就返回：

```http
HTTP/1.1 429 Too Many Requests
Retry-After: <秒>

{"code":"RATE_LIMITED","message":"Too many requests, please slow down and try again later"}
```

- 这是创作接口**唯一**不满足 `{code, message, reason, metadata}` 包络的响应：`code` 是字符串，没有 `reason` 与 `metadata`。
- 决定：不改共用中间件（会影响全部面板接口）。前端统一规则：**HTTP 429 且响应体没有 `reason` 时，按 `reason = "rate_limit_exceeded"` 处理**，倒计时取响应头 `Retry-After`（缺失时 60 秒）。

---

## 7. 进度、历史与结果

### 7.1 复用接口

```text
GET  /api/v1/user/async-tasks?page=1&page_size=20&kind=image|video&status=processing|completed|failed|cancelled&search=
GET  /api/v1/user/async-tasks/{catalog_id}
POST /api/v1/user/async-tasks/{catalog_id}/refresh       仅 processing 的视频
GET  /api/v1/user/async-tasks/{catalog_id}/content       视频字节（登录态）
```

### 7.2 目录记录新增 `result_urls`（向后兼容）

```go
// backend/internal/service/async_task.go
type AsyncTask struct {
    // ...原字段不变
    ResultURL  string   `json:"result_url,omitempty"`  // 保留：首个结果
    ResultURLs []string `json:"result_urls,omitempty"` // 新增：图片任务的全部结果链接
    // ...
}
```

```ts
// frontend/src/api/asyncTasks.ts —— 只加这一行，其余不动
result_urls?: string[]
```

- 写入：`repository/image_task_store.go` 的 `Save` 构造目录记录时，`ResultURLs` 由 `imageTaskResultURLs` 用 `gjson.GetBytes(task.Result, "data.#.url")` 遍历取值，**跳过空串**，最多 10 个。
- `ResultURL` **保持原行为不变**：仍为 `gjson.GetBytes(task.Result, "data.0.url")`，不跳过空串。因此当第一项没有 `url` 时，`result_url` 为空而 `result_urls` 可能非空；前端按下一条规则取用即可。
- 视频任务不填 `result_urls`（保持只有 `result_url`，隐藏上游链接逻辑不变）。
- 用户列表、用户详情、管理端列表、管理端详情都会自动带上该字段（`AdminAsyncTask` 内嵌 `AsyncTask`；前端 `AdminAsyncTask extends AsyncTask`）。
- 旧记录没有该字段：前端统一用 `task.result_urls?.length ? task.result_urls : (task.result_url ? [task.result_url] : [])`。

### 7.3 轮询（前端）

- 已加载的列表里存在 `processing` 时，每 10 秒刷新已加载的页；页面不可见时暂停，回到前台补刷一次。
- 视频 `processing` 调 `refresh`，每个任务节流 30 秒；图片不调 `refresh`。
- 进度：不确定进度条加「已用时长」（`now - created_at`）。

### 7.4 结果展示

- 图片：按 7.2 的规则取全部结果链接，用 `cs-media-grid[data-count]` 展示，打开大图走灯箱。链接先用 `safeURL()` 校验（只允许 `https:`、`http:`、站内相对路径）。
- 视频：一律调 `asyncTasksAPI.download(catalog_id)` 取字节，转成 `blob:` URL 播放；不直接把上游链接写进 `<video src>`。
- 过期：`expires_at < now`，或资源加载返回 403/404 → 卡片进入 `expired` 形态。

---

## 8. 复用参数（前端本地快照，不落库）

### 8.1 存储

| 项 | 值 |
| --- | --- |
| localStorage key | `creationStudio:snapshots:v1:<user_id>` |
| 最近使用的 Key | `creationStudio:lastKeyId:v1:<user_id>`（值为数字字符串） |
| 结构 | `{ "v": 1, "items": CreationSnapshot[] }`，按 `created_at` 倒序 |
| 条数上限 | 100；写入后超过就删最旧的 |
| 过期 | `created_at` 早于 7 天前的条目在每次读和写时删除 |
| 写入失败（`QuotaExceededError` 等） | 删掉最旧的一半后重试一次；仍失败就放弃写入，只 `console.warn`，**不影响提交** |
| 读失败、JSON 损坏、`v` 不为 1 | 当作空，并覆盖写回 `{ v:1, items:[] }` |

```ts
export interface CreationSnapshot {
  catalog_id: string           // 可能为空串（见 §5.6）
  task_id: string
  mode: CreationMode
  api_key_id: number
  protocol: ImageProtocol | VideoProtocol
  model: string
  prompt: string
  params: Record<string, string | number>
  images: {
    role: 'source' | 'first_frame' | 'last_frame' | 'reference'
    upload_id: string
    url: string
    expires_at: number
    width: number
    height: number
  }[]
  created_at: number           // 提交成功时刻（Unix 秒）
}
```

- 只在**提交成功**后写入一条。
- 查找：先按 `catalog_id`，找不到再按 `task_id`。

### 8.2 回填规则

1. 切换到快照的 `mode`；`api_key_id` 仍在 active Key 列表里就切过去，否则保留当前 Key 并提示。
2. 重新拉 options。模型行（`mode, protocol, model`）存在就选中，不存在就提示「模型已不可用」，参数仍尽量保留。
3. 回填 `prompt`；`params` 里在新描述符中合法的值回填，不合法的丢弃，并在对应字段下给一行提示。
4. 图片：`expires_at - now ≥ 900` 的回填（缩略图用 `url`，提交时用 `upload_id`）；不满足的在对应位置显示「参考图已过期，请重新上传」（design 补的状态）。上传不可用时一律不回填。
5. 没有快照的历史任务（换了设备，或超过 7 天）：只回填 `model` 和描述符默认值，提示词留空，提示「该任务没有本地记录，提示词无法复用」。
6. 隐私：快照按用户隔离，7 天过期；退出登录时不清除（已知限制，见 §11）。

---

## 9. 计费口径（不可违反）

1. 视频：提交被接受时冻结价格合同；之后改价、改倍率、改 Key 分组、删 Key 都不影响已接受任务。成功按实际计费、封顶预占；失败或取消释放。
2. 不扣成负数：视频由持久预占保证。生图在提交前按所选档位做硬拦截（§5.3），执行器内的准入与在途预留照常执行；但生成完成后按实际结果扣费，**现有扣费逻辑在余额不足时允许透支**（个人余额 `deductUsageBillingBalance` 先尝试 `balance >= amount`，失败则无条件扣减并标记 `BalanceOverdrafted`；企业钱包 `deductUsageBillingEnterpriseBalance` 无下限）。因此生图在 §11.3 所列情形下可能出现小额负余额——这是网关现有行为，本变更不改变。
3. 403 / 402 / 429 口径见 §6.1。
4. 企业成员：可用全部能力；订阅制分组订阅优先；非订阅走企业钱包与公司/部门/员工限额。
5. 创作层不新增任何计费判据；生图预检用的是网关现有函数（`CheckBillingEligibility`、`EstimateInflightReservation`、`ReserveFixedCharge`），只是提前到创建任务之前执行。

---

## 10. 前端落点

### 10.1 路由（`frontend/src/router/index.ts`，插在 `/async-tasks` 路由之前）

```ts
{
  path: '/creation',
  name: 'CreationStudio',
  component: () => import('@/views/user/CreationStudioView.vue'),
  meta: {
    requiresAuth: true,
    requiresAdmin: false,
    title: 'Creation studio',
    titleKey: 'creationStudio.title',
    descriptionKey: 'creationStudio.description',
  },
},
```

不加 `requiresPersonalWallet`，不加 `hideInSimpleMode`。

### 10.2 菜单（`frontend/src/components/layout/AppSidebar.vue`）

- 在 `BatchImageIcon` 定义之后，照同样的写法新增 `const CreationIcon`（heroicons outline sparkles，path 见下）。
- `buildSelfNavItems` 中插在 `{ path: '/async-tasks', ... }` 之前：`{ path: '/creation', label: t('nav.creationStudio'), icon: CreationIcon }`。
- 简单模式分支 `filtered.push({ path: '/async-tasks', ... })` 之前同样插入一条。

```text
M9.813 15.904 9 18.75l-.813-2.846a4.5 4.5 0 0 0-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 0 0 3.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 0 0 3.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 0 0-3.09 3.09ZM18.259 8.715 18 9.75l-.259-1.035a3.375 3.375 0 0 0-2.455-2.456L14.25 6l1.036-.259a3.375 3.375 0 0 0 2.455-2.456L18 2.25l.259 1.035a3.375 3.375 0 0 0 2.456 2.456L21.75 6l-1.035.259a3.375 3.375 0 0 0-2.456 2.456Z
```

### 10.3 文件

```text
frontend/src/views/user/CreationStudioView.vue
frontend/src/features/creation-studio/
  types.ts                 本契约 §2–§8 的全部 TS 类型（字段名、可选性不得改）
  api.ts                   getOptions / estimate / upload / submit，全部用 apiClient（/user/creation/...）
  fixtures.ts              与本契约示例一致的 mock 数据，供联调前开发与测试使用
  useCreationKeys.ts       keysAPI.list(1,100,{status:'active'})，过滤掉无 group 的 Key；记忆 lastKeyId。注意：该接口（GET /api/v1/keys）响应含 Key 明文；创作页只使用 id、name、group 等字段，不在 DOM 中展示明文，不写入 localStorage（lastKeyId 只存数字 id），也不在任何请求中回传明文
  useCreationOptions.ts    按 api_key_id 加载并缓存 options
  useCreationForm.ts       模式、模型、提示词、参数（按描述符默认值初始化）、图片引用
  useCreationUploads.ts    逐张上传、进度、失败重试、过期判断（expires_at - now < 900）
  useCreationEstimate.ts   300ms 防抖
  useCreationSubmit.ts     提交互斥、成功后写快照并在任务流顶部插入「刚提交」卡片
  useCreationFeed.ts       加载更多、10 秒轮询、视频 refresh 每任务 30 秒节流
  useCreationSnapshots.ts  §8 的 localStorage 读写
  components/ModeSwitch.vue  KeyPicker.vue  ModelPicker.vue  PromptField.vue
  components/ParamField.vue（按 kind 分发）  ChipGroup.vue  RatioGroup.vue  IntegerStepper.vue  ImageDropzone.vue
  components/CostSummary.vue  SubmitAction.vue  Callout.vue
  components/TaskCard.vue  MediaGrid.vue  VideoPlayer.vue  Lightbox.vue  HistoryFeed.vue
frontend/src/api/asyncTasks.ts           只新增 result_urls?: string[]
frontend/src/i18n/locales/{zh,en}/creationStudio.ts（新增）、index.ts（展开）、common.ts（nav.creationStudio）
```

- 上传调用：`apiClient.post('/user/creation/uploads', formData, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 120000, onUploadProgress })`。
- 前端先按 `upload_limits` 检查大小和类型，再上传；后端仍会校验。
- 样式只用 `cs-*` 类和现有 `.card`、`.btn`、`.input`；不在 `.vue` 里 import css。
- 布局：桌面双栏；移动端「参数 / 结果」标签切换，提交栏吸底；历史筛选（全部 / 图片 / 视频）不跟随模式切换；对象存储未开启时生图选项保留，标「不可用」并给原因。
- i18n 命名空间 `creationStudio`；`errors.*` 的 key 名见 §6.1；导航名 `nav.creationStudio`（中文「创作」，英文「Create」）。`pnpm check:i18n` 必须通过。

---

## 11. 后端落点与运维

### 11.1 文件

```text
backend/internal/handler/creation_studio_handler.go       Options / Estimate / Upload / Submit / SetDispatcher
backend/internal/handler/creation_studio_errors.go        §6.2 归一
backend/internal/handler/creation_studio_image_precheck.go §5.3（AsyncImageHandler 的方法）
backend/internal/handler/image_task_handler.go            Submit 中加一处受重放标记保护的预检调用
backend/internal/handler/handler.go / wire.go             Handlers 新增 CreationStudio 字段；在 ProvideHandlers 内用已注入的 asyncImageHandler、openaiGatewayHandler 构造，不改 ProvideHandlers 签名
backend/internal/service/creation_studio.go               类型、校验、重放标记
backend/internal/service/creation_studio_request.go       §5.4 纯函数与白名单
backend/internal/service/creation_studio_catalog.go       §3（OpenAIGatewayService 的方法，以便使用 accountRepo）
backend/internal/service/creation_studio_estimate.go      §4；含 EstimateCreationImageCost（§4.2.1，预检与估算共用）
backend/internal/service/creation_studio_upload.go        §2：类型嗅探、解码、key 生成、记录、CreationUploadStore
backend/internal/service/async_task.go                    ResultURLs 字段；AsyncVideoTaskCatalogID
backend/internal/service/image_storage.go                 SaveObject / ReadObject / LinkPolicy 与可选接口 ImageStorageReader、ImageStorageLinkPolicy
backend/internal/service/gemini_submission.go             抽出 geminiVideoModelLimits（取值不变）
backend/internal/service/minimax_submission.go            抽出 miniMaxVideoModelLimits（取值不变）
backend/internal/service/openai_images.go                 导出 IsGrokImageGenerationModel
backend/internal/service/account_test_service_video.go    导出 VideoTestModelMatchesProtocol
backend/internal/repository/image_task_store.go           ResultURLs 填充；CreationUploadStore 实现
backend/internal/repository/image_storage_s3.go           Read（GetObject）与 LinkPolicy
backend/internal/server/routes/user.go                    §1.1 四条路由
backend/internal/server/router.go                         SetDispatcher 注入
backend/internal/server/middleware/audit_log.go           只在 auditBodyOmittedRoutes、auditActionOverrides 加条目
```

- **无数据库迁移，无新表。** 新 Redis key 只有 `creation_upload:*` 和 `creation_uploads:user:*`。
- **`backend/cmd/server/wire_gen.go` 不应变化**；如果实现中发现必须改，先停下来提问。
- `golang.org/x/image` 已在 `go.mod` 的直接依赖里，导入 `golang.org/x/image/webp` 不需要改 `go.mod`。

### 11.2 运维要求（写入发布说明）

1. **对象生命周期**：应用不会删除上传对象（`ImageStorage` 没有删除能力，生图结果同样不删）。部署方必须在存储桶上为 `<prefix>creation-uploads/` 配置生命周期规则，**建议 3 天后过期删除**。Redis 元数据到期后系统不再接受该上传，即使对象还没被删。
2. **链接可达性**：Seedance、MiniMax、Grok 的参考图由上游通过链接拉取。存储端点（presigned 链接的 host 或 `public_base_url`）必须能从公网访问；内网 MinIO 会导致这些协议上游报错（原文透传给用户）。Gemini 和生图编辑由后端读回字节内联，不受影响。
3. **出站白名单**：启用 `security.url_allowlist` 时，不影响本功能（上游拉取发生在厂商侧）。

### 11.3 已知限制

- 上传图片不去除 EXIF 元数据，原样存储并提供给上游。
- **未配置 `model_mapping` 的分组只会展示默认模型**（§3.4）：每类只出现一个默认名（Video 平台为协议默认清单）。用户需要其它模型时，由管理员在账号上配置 mapping。
- token 计费渠道下的图片编辑，`/estimate` 金额低于预检金额（§4.2.2）。
- 创作页复用 `/api/v1/keys`，前端内存持有 Key 明文（既有暴露面，未扩大）；如需收紧，后续可新增不含明文的选择列表接口。
- **Gemini 视频路径的错误识别依赖文案**（§6.2 规则 5d）：Google 错误体没有字符串机器码，网关改动错误文案时可能退化为 `INVALID_CREATION_REQUEST` 或 `upstream_error`。消息表已尽量用错误常量现算，但中间件字面量仍需人工同步。后续改进项：代发 Gemini 请求时让错误体带出字符串 code（例如写入 `error.details[].reason`），需要改网关 Gemini handler，本次不做。
- 旧 OpenAI 平台 Seedance 账号中，公开别名和映射目标都不含 `seedance`、也不以 `ep-` 开头的自定义中转名，不会出现在创作页（§3.4）；这类模型仍可通过原生 API 调用。
- 本地快照退出登录不清除，同一浏览器的同一用户在 7 天内可见。
- 生图预检与执行器之间存在竞态窗口，并发提交时后到者可能以任务失败结束。
- **生图实际计费可能高于预检金额**，出现在以下情形：
  1. 上游返回的图片尺寸高于所选档位（执行期 `ResolveImageBillingSize` 优先按输出尺寸判档，例如选 `auto` 按 2K 预检、实际输出 4K）；
  2. 渠道为 token 计费，实际 token 超过网关估算；
  3. 执行时选中账号的模型映射或渠道映射使计费模型与预检时的 `routingModel` 不同。

  这些情形下，若余额在执行时已不足，执行器内的准入（`CheckBillingEligibility` 返回 403、并发时在途预留返回 402）会让任务以 `failed` 结束；若准入通过、生成完成后扣费超过余额，现有扣费逻辑会**透支**（见 §9 第 2 条），余额可能出现小额负数。本变更不改变扣费逻辑。

---

## 12. 原生请求示例（后端构造，前端不可见）

```http
POST /v1/images/generations/async
{"model":"gpt-image-2","prompt":"雪夜灯塔","size":"1536x1024","quality":"high","n":2}

POST /v1/images/edits/async
{"model":"gpt-image-2","prompt":"把天空换成极光","size":"1024x1024","quality":"auto","n":1,
 "images":[{"image_url":"data:image/png;base64,iVBORw0..."}]}

POST /v1/contents/generations/tasks
{"model":"doubao-seedance-1-0-pro","content":[
  {"type":"text","text":"夕阳下的海浪"},
  {"type":"image_url","image_url":{"url":"https://img.example.com/images/creation-uploads/42/20261107/cupl_ab..png"},"role":"first_frame"}
 ],"resolution":"720p","duration":5,"ratio":"16:9","watermark":false}

POST /v1beta/models/veo-3.1-generate-001:predictLongRunning
{"instances":[{"prompt":"夕阳下的海浪","image":{"bytesBase64Encoded":"iVBORw0...","mimeType":"image/png"}}],
 "parameters":{"durationSeconds":8,"resolution":"1080p","aspectRatio":"16:9","sampleCount":1}}

POST /v1/video_generation
{"model":"MiniMax-Hailuo-2.3","prompt":"夕阳下的海浪","duration":6,"resolution":"768P",
 "first_frame_image":"https://img.example.com/images/creation-uploads/42/20261107/cupl_cd..jpg"}

POST /v2/video_generation
{"model":"MiniMax-H3","content":[
  {"type":"text","text":"夕阳下的海浪"},
  {"type":"image_url","image_url":{"url":"https://..."},"role":"first_frame"}
 ],"duration":6,"resolution":"768P","ratio":"adaptive"}

POST /v1/videos/generations
{"model":"grok-imagine-video","prompt":"夕阳下的海浪","duration":8,"resolution":"480p","image":{"url":"https://..."}}
```

---

## 变更记录

| 版本 | 日期 | 内容 | 决策来源 |
| --- | --- | --- | --- |
| v0.1 | 2026-11-07 | 草案 | spec |
| v1 | 2026-11-07 | 冻结：O1 进程内重放；O2 生图提交前硬拦截预检（只在创作页重放时生效）；O3 不落库，复用参数用本地快照；O4 文生图加单图编辑、n 1–4、全部结果（目录新增 `result_urls`）；O5 参考图先上传对象存储（新增 uploads 接口）；O6 企业成员可用；「低于最低保留余额返回 403 billing_error」只在创作层归一 | 用户 2026-11-07 拍板 |
| v1.1 | 2026-11-07 | 生图预检与 `/estimate` 金额改为按所选尺寸档位计价（§4.2、§4.2.1、§5.3）：复用 `ResolveImageBillingSize` 判档、`calculateOpenAIImageCost` 计价、`ResolveUserGroupRateMultiplier` + `computePeakAwareMultipliers` 取倍率，新增共用函数 `EstimateCreationImageCost`；渠道为 token 计费时保留网关在途估算。估算响应删除 `selected_tier_cost`，`cost_basis` 取值改为 `image_tier` / `image_token_estimate` / `video_reservation`，`estimated_cost` 为唯一金额字段。Grok 生图的描述符 `resolution` 改为写入原生 `size`（§3.3、§5.4），使计费档位与所选值一致。§9、§11.3 如实写明生图执行后扣费可能透支 | 用户 2026-11-07 拍板（遗留问题 1） |
| v1.2 | 2026-11-07 | 补记实现确认的事实，接口与字段无变化：§6.3 面板通用限流 429 无 `reason` 的包络例外及前端处理规则（§1.2、§6.1 同步）；§3.4 新增模型清单来源表（未配 mapping 时的默认模型名与 Video 平台默认清单，原 §3.4 顺延为 §3.5）；§4.2.2 token 计费图片编辑的估算与预检差异；§3.3 MiniMax 前缀选路与网关按 base_url 判定的分工；§7.2 `result_url` 保持 `data.0.url`、仅 `result_urls` 跳过空串；§11.3 补充已知限制。同版补充（按契约改代码）：旧 OpenAI 平台 Seedance 模型清单改为经 `VideoTestModelMatchesProtocol` 过滤，并要求公开别名或映射目标至少一个呈 Seedance 形态（含 `seedance` 或以 `ep-` 开头）（§3.4，删除原已知限制、新增「自定义中转名不展示」限制）；0 元模型按已定价处理（`priced=true`、`estimated_cost=0`），生图预检对免费请求放行（§4.2.2）。再补（qa 实机缺陷修复后）：§6.2 按实现重写为 14 条规则（新增 5b 显式 reason、5c Google 402、5d Google 本地错误消息表、7b 仅提升 §6.1 码表内的 type），新增 §6.2.1 两条实现偏差待修；§5.2 新增 V2a（额度用完先于 V4，429），§4.3 同步；§3.1 `key_unavailable` 口径改为只看状态与过期；§11.3 补 Gemini 依赖文案识别及后续改进项；Key 明文表述按实际修正（§0.2、§10.3、§11.3：前端复用 `/api/v1/keys` 会在内存持有明文，创作接口自身不含明文） | backend/frontend 实现完成后由 orchestrator 确认 |

后续任何改动：在本表追加一行（版本号 +0.1 或 +1），写清改动的章节与原因，并同步 `openspec/changes/add-creation-studio/`。
