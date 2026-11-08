# Asynchronous Image Tasks

Asynchronous image tasks let clients submit long-running OpenAI-compatible image requests without keeping one HTTP connection open. This avoids proxy/CDN response timeouts such as Cloudflare 524 while preserving the existing image routing, billing, moderation, concurrency, and failover behavior.

Users can also track new image tasks in the **Async tasks** page (`/async-tasks`), alongside video tasks. The page supports search and status/type filters and shows the first generated result. See [ASYNC_VIDEO_TASKS.md](ASYNC_VIDEO_TASKS.md) for the video platform and unified task catalog.

## Endpoints

The authenticated gateway exposes both `/v1` paths and their existing no-prefix aliases:

```text
POST /v1/images/generations/async
POST /v1/images/edits/async
GET  /v1/images/tasks/{task_id}
```

The aliases are `/images/generations/async`, `/images/edits/async`, and `/images/tasks/{task_id}`.

Only OpenAI and Grok groups are supported. Requests use the same JSON or multipart payload as the corresponding synchronous endpoint. Streaming image requests are rejected because a polled task returns one final JSON result.

## Enabling the feature (object storage)

Asynchronous image tasks are **disabled by default** and gated on object storage. When the switch is off — or the S3 credentials are incomplete — the async endpoints return `404` and never create a task or write to Redis. This is deliberate: without offloading, large `b64_json` results (several MB each, e.g. `gpt-image-1`) would accumulate in Redis and exhaust its memory.

### From the admin UI (recommended)

**Admin → Backup → Async image object storage.** Saving the form takes effect immediately — the object-storage client is rebuilt on the next request, so there is no container restart.

Because the async image storage and the database backup share one S3 client, the form defaults to **reusing the backup S3 configuration**: it borrows the endpoint, region and credentials already configured above and keeps only its own bucket and prefix, so backups stay under `backups/` while images go to `images/`. Leave the bucket empty to use the backup bucket as well. Untick the box to point images at a completely separate account.

Saving requires step-up 2FA when that gate is enabled, for the same reason the backup S3 form does: changing the target redirects generated content to another account.

Turning the switch off stops new submissions but keeps already-accepted tasks pollable, so nothing in flight is stranded.

### From the config file

The admin setting takes precedence. When nothing has ever been saved there, the `image_storage` block in `config.yaml` is used instead, so deployments that enabled the feature before the admin UI existed keep working untouched.

Configure an S3-compatible object store (AWS S3, Cloudflare R2, Aliyun OSS, MinIO, …) in `config.yaml` (all keys also accept the `IMAGE_STORAGE_*` environment overrides):

```yaml
image_storage:
  enabled: true
  endpoint: "https://<account_id>.r2.cloudflarestorage.com"  # AWS 官方可留空
  region: "auto"
  bucket: "my-images"
  access_key_id: "..."
  secret_access_key: "..."
  prefix: "images/"
  force_path_style: false          # MinIO/path-style buckets set true
  public_base_url: ""              # set to return public_base_url/key直链; empty → presigned URL
  presign_expiry_hours: 24         # presigned link TTL when public_base_url is empty
  max_download_bytes: 33554432     # cap when re-hosting an upstream image URL (32MB)
```

When a task completes, each generated image is uploaded to the bucket and the result is rewritten to a compact form: `data[].url` points at the stored object (a permanent `public_base_url/key` link, or a time-limited presigned URL) and `b64_json` is removed. Only this small JSON is stored in Redis. If an upload fails, the task is marked `failed` rather than persisting the raw base64.

To support a different vendor beyond the S3-compatible client, implement the `service.ImageStorage` interface (`Save(ctx, key, contentType, data) (url, error)`) and provide it in place of the S3 implementation.

### Troubleshooting: the endpoints return 404 after enabling

`404 async image tasks are not enabled` means `image_storage` did not resolve to a complete configuration, so the feature stayed off. The route exists either way — the 404 comes from the handler, not from an unregistered path, which makes it easy to mistake for a missing build.

Check the startup log for:

```text
WARN image_storage.enabled is true but object storage is not fully configured; async image tasks are disabled  missing_keys=[...]
```

`missing_keys` names exactly which credentials were empty when the config was loaded.

Note that releases **before v0.1.161 silently dropped `IMAGE_STORAGE_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY_ID`, `_SECRET_ACCESS_KEY` and `_PUBLIC_BASE_URL`** when they were supplied only through the environment: those keys had no registered default, and viper cannot see an environment variable for a key it does not already know about. Deployments driven purely by `environment:` — which is what `deploy/docker-compose.yml` does by default — therefore reported `enabled: true` with empty credentials and 404'd on every async call. On an affected release the workaround is to also place the `image_storage` block in `/app/data/config.yaml` (copy it from `deploy/config.example.yaml`); once the keys exist in the file, the environment overrides apply normally.

Two further causes of a 404 that are unrelated to storage: the API key's group must be on the **OpenAI or Grok** platform (any other platform, or a key with no group at all, yields `Images API is not supported for this platform`), and a task may only be polled with the **same API key that submitted it** — polling with a different key of the same user returns `image task not found` by design.

## Submit a task

```bash
curl -i https://api.example.com/v1/images/generations/async \
  -H 'Authorization: Bearer sk-...' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-image-1",
    "prompt": "A lighthouse during a winter storm",
    "size": "1536x1024"
  }'
```

The server stores the initial task in Redis and responds with `202 Accepted`:

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "status": "processing",
  "created_at": 1784092800,
  "expires_at": 1784179200,
  "poll_url": "/v1/images/tasks/imgtask_0123456789abcdef"
}
```

`Location` contains the polling path and `Retry-After: 3` provides the recommended polling interval.

## Poll a task

Use the same API key that submitted the task:

```bash
curl https://api.example.com/v1/images/tasks/imgtask_0123456789abcdef \
  -H 'Authorization: Bearer sk-...'
```

While work is in progress:

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "status": "processing",
  "created_at": 1784092800,
  "expires_at": 1784179200
}
```

On success, `result` mirrors the synchronous image API body, except each image has been offloaded to object storage: `data[].url` points at the stored object and `b64_json` is stripped (so both URL and base64 upstream formats end up as compact stored links):

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "status": "completed",
  "http_status": 200,
  "image_url": "https://...",
  "result": {
    "created": 1784092923,
    "data": [{"url": "https://..."}]
  },
  "created_at": 1784092800,
  "completed_at": 1784092923,
  "expires_at": 1784179323
}
```

For URL responses, `image_url` mirrors the first `data[].url` for simple clients. On failure, the task reaches `failed` and exposes the original OpenAI-compatible error object where available:

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "status": "failed",
  "http_status": 502,
  "error": {
    "type": "api_error",
    "message": "Upstream request failed"
  },
  "created_at": 1784092800,
  "completed_at": 1784092923,
  "expires_at": 1784179323
}
```

All submit and poll responses include `Cache-Control: no-store`, preventing a CDN from caching the `processing` state. Tasks and results expire 24 hours after their latest state update. A task executes for at most 30 minutes.

Task ownership is scoped to both user and API key. Unknown task IDs and IDs owned by another key both return `404`, avoiding task-existence disclosure. Polling remains available when the completed generation used the key's remaining balance; normal authentication, disabled-key, user, IP, and group checks still apply.

## 上游为默认异步的 sub2api 系统

部分 sub2api 衍生上游在普通 `POST /v1/images/generations` 或 `/v1/images/edits` 中默认返回 `202` 任务对象，而不是最终图片。网关会替客户端轮询这类上游，并将最终 `result` 接入原有生图结果处理流程。此适配仅作用于 **OpenAI 平台的 API Key 账号**，不改变 OAuth 或 Grok 路径，也不改变普通同步图片响应。

这与本文前面的**本网关异步任务接口**是两层机制：

- 调用本网关普通同步生图接口时，网关在当前请求中等待上游任务结束，成功后返回 `200 application/json`，响应体是最终生图结果（含 `data[]` 和上游提供的 `usage`），不是上游任务外壳。提交响应里的 `Location`、`Retry-After` 不会透传，上游任务 ID 也不会作为本网关任务 ID 暴露。
- 调用本网关 `/async` 接口或使用创作页的异步任务流程时，客户端仍提交、查询**本网关自己的任务**；后台执行会先等待上游终态，再按现有逻辑转存图片、保存结果，而不是将上游任务外壳误判为已完成。
- 上游适配本身不要求启用本网关的 `image_storage`；但使用本网关 `/async` 接口仍需满足前文的对象存储配置和任务归属校验，最终图片仍按本网关配置转存。

### 识别、轮询和终态行为

为避免将普通响应误判为任务，仅识别成功的 `2xx` 合法 JSON 对象，且对象**没有 `data` 字段**，并满足以下任一条件：

- `object` 为 `image.generation.task`；
- HTTP 状态为 `202`，存在非空 `task_id`（缺省回落 `id`），且 `status` 为 `processing`、`queued`、`pending` 或 `in_progress`。

其他形状保留原有行为。提交时已经是 `completed` 或 `failed` 的任务直接按终态处理，无需额外查询。

轮询使用与提交相同的上游 API Key 和账号请求头设置、User-Agent、代理及 OpenAI HTTP 上游配置。轮询间隔读取上游 `Retry-After` 的秒数，限制在 **1–10 秒**之间；缺省为 **3 秒**。网络错误、`429` 和 `5xx` 视为暂时故障，按轮询节奏重试，直到成功、明确失败或超过总时限。

| 上游结果或轮询情况 | 本网关结果（尚未发送 JSON 保活时） | 客户端计费 |
| --- | --- | --- |
| `completed` 且 `result` 中识别到至少一张图片 | `200`，返回最终 `result` | 按实际结果计费 |
| `completed` 但没有可识别图片 | `502 upstream_error`，提示 `Upstream async image task completed without images` | 不计费 |
| `failed` | 使用 `http_status`（仅接受 `400–599`，否则 `502`），按同步错误链脱敏并应用管理员透传规则，再返回标准 `error`；缺失时使用 `Upstream async image task failed` | 不计费 |
| 连续三次轮询 `404` | `502`，提示 `Upstream async image task not found or expired` | 不计费 |
| 轮询 `401`、`403` 或其他不可重试的 `4xx`（不含上述 `404`、`429`） | `502`，日志记录上游 HTTP 状态 | 不计费 |
| 超过轮询总时限 | `504 timeout_error` | 不计费 |
| 任务 ID 不合法 | `502` | 不计费 |

上游接受任务后，后续任何失败都**不会切换账号重新提交**，避免同一请求重复产生上游成本；也不会因任务内层的 `http_status` 对账号触发限流、禁用等副作用，因为它可能表示上游所用供应商的状态，而非本网关账号的状态。这些失败还会跳过账号调度反馈，既不记录账号失败，也不以成功反馈清除账号临时状态；异步任务成功完成时仍按普通成功请求上报调度结果。

失败任务的 `error` 不原样透传：先移除账号身份敏感内容，再应用管理员错误透传规则；未命中规则时仅保留标准 `type`、`code`、`param`、`message`，对消息中 URL 查询参数的密钥脱敏。管理员规则可改写返回状态码和文案，规则透传的上游消息同样脱敏；上游扩展调试字段不暴露。网关新增的通用错误文案统一为英文。

### 配置

```yaml
gateway:
  image_async_upstream_poll_timeout: 1800
  image_nonstream_keepalive_interval: 0
```

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `gateway.image_async_upstream_poll_timeout` | `1800` | 上游异步任务轮询总时限，单位秒（默认 30 分钟）；`<=0` 仍使用默认值 `1800`，不表示无限等待或关闭适配。配置加载保留非正值，由 service 在使用时执行默认回退。 |
| `gateway.image_nonstream_keepalive_interval` | `0` | 非流式图片 JSON 保活间隔，单位秒；`0` 为关闭，启用时必须在 `5–60` 之间。可缓解轮询等待期间反向代理/CDN 的空闲超时，但不能保证绕过代理的硬性时限。 |

两个配置项分别支持环境变量 `GATEWAY_IMAGE_ASYNC_UPSTREAM_POLL_TIMEOUT` 和 `GATEWAY_IMAGE_NONSTREAM_KEEPALIVE_INTERVAL`，环境变量优先于配置文件。部署示例见 `deploy/config.example.yaml`。

JSON 保活发出首个心跳后，HTTP 状态已经固定为 `200`；后续失败仍通过响应体的 `error` 表达，无法再把 HTTP 状态改为表中的 `4xx/5xx`。需要可靠获取最终状态、避免长连接的客户端应优先使用本网关 `/async` 接口。

本网关 `/async` 的后台执行会读取上游异步失败的独立内部信号，不以保活提交的 `200` 判断成功。因此，即使后台执行已经发送保活，任务失败或轮询超时仍保存为 `failed`，任务的 `http_status` 保留真实错误状态，结果不会被误存为无图的 `completed`。

### 计费与断开连接

- 成功时按最终 `result` 中识别到的**实际图片张数**、`usage` 和尺寸进入现有计费流程，绝不回落到请求的 `n`。例如请求 `n=4`、实际完成两张，只按两张结果处理。
- 异步结果的图片字段必须为字符串（缺失或 `null` 视为空）；`url: false`、数字或对象类型的 `b64_json` 不会被认作实际图片，此类异常结果返回无图错误而不计费。普通同步上游的兼容行为不受影响。
- 不会因接收到了任务对象就计费；完成但无图、任务失败、轮询失败或超时均不向客户端计费。
- **客户端断开不会停止上游轮询。** 上游一旦接受生成就可能向本网关收取费用，因此网关在独立于客户端取消的上下文中继续等待；总时限内成功的结果仍按正常流程计费，不以客户端是否接收到响应为准。
- 超过本网关轮询时限后，上游任务不一定已停止，仍可能产生上游成本；本网关此时未获得成功图片，不向客户端计费。可结合账号和上游任务 ID 日志进行对账，避免立即重复提交造成额外成本。

### 安全与可观测性

- **不使用上游响应的 `poll_url` 或 `Location` 构造请求**，避免跳转到任意主机造成 SSRF 或凭据泄露。网关只使用经过校验的账号 base URL，拼接 `/v1/images/tasks/{task_id}`；兼容 base URL 已含或不含 `/v1`。
- 任务 ID 必须符合 `^[A-Za-z0-9_.:-]{1,128}$`，并作为路径段转义；额外拒绝路径语义特殊的 `.` 和 `..`，非法 ID 直接拒绝。轮询禁用 HTTP 重定向，只携带所需上游鉴权和账号请求头设置，不复制客户端请求体或无关头；轮询响应体沿用网关已有大小上限。
- 本网关自己的任务查询继续要求提交时的同一用户、同一 API Key；上游任务不会作为本地任务直接供客户端查询。上游 `404` 也可能表示任务过期、提交与查询的 API Key 不一致，或上游关闭了异步功能。
- 日志事件使用 `openai.images.async_upstream.` 前缀，包括 `detected`、`poll_retry`、`completed`、`failed`、`timeout`，带账号 ID、上游任务 ID、轮询次数与耗时；不记录 prompt、图片内容或鉴权头。超时记录 WARN，任务/轮询失败写入上游错误记录；上游延迟统计包含提交和轮询的总耗时。

### 已知限制

- 等待上游终态期间，账号和用户并发名额持续占用；本适配不引入单独的持久化轮询队列，也不是进程重启后的任务恢复机制。
- 同步请求仍可能被 nginx、CDN 或客户端自身的超时中断。JSON 保活只能缓解部分空闲超时；需要长时间等待时，使用本网关异步任务接口。
- 普通同步接口即使客户端传入 `stream=true`，上游返回任务 JSON 时也只返回**最终 JSON，不转换为 SSE**；本网关 `/async` 接口仍拒绝流式图片请求。
- 若上游最终结果只有 URL、客户端需要 `b64_json`，仍依赖账号的 `images_url_to_b64_json` 开关，经现有回填流程转换；适配不会强制打开该开关。
