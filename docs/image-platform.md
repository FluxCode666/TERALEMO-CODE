# Image 平台

Image 是独立的生图平台，账号仅支持 API Key。客户端统一使用 OpenAI Images API，账号的上游协议由凭证选择：

| 凭证字段 | 说明 |
| --- | --- |
| `image_protocol` | `openai`（默认）、`gemini`、`grok`，忽略大小写及两侧空白 |
| `api_key` | 必填 |
| `base_url` | 可选；分别默认为 `https://api.openai.com`、`https://generativelanguage.googleapis.com`、`https://api.x.ai` |
| `model_mapping` | 沿用账号模型映射；键是客户端模型名，值是上游模型名 |

无需数据库迁移；协议存于现有 credentials JSON。地址格式在账号保存时校验，出站请求仍受部署的上游 URL 策略约束。

## 接口

- `POST /v1/images/generations`
- `POST /v1/images/edits`（JSON `images[].image_url` 或 multipart `image`/`image[]`）
- 上述路径追加 `/async` 可提交异步任务，使用 `GET /v1/images/tasks/:task_id` 查询；需要启用现有图片任务对象存储。
- 同时支持不带 `/v1` 的根路径别名。批量 Images API 暂不支持 Image 分组。

OpenAI 协议复用 API Key Images 转发；Gemini 中 `imagen-` 开头的映射模型走 `:predict`，其他走 `:generateContent`；Grok 走 xAI Images API。创作页对 Image 平台使用 `protocol=openai` 请求格式，模型来自账号映射或协议默认目录。组合分组可显式将 Images 路由指向 `image`；异步提交和创作页当前要求具体的 Image 分组。

## 同步上游模型

管理端创建或编辑 Image 账号时，可以使用“从上游同步模型”获取真实模型列表。已保存账号调用 `POST /api/v1/admin/accounts/:id/models/sync-upstream`；创建预览调用 `POST /api/v1/admin/accounts/models/sync-upstream-preview`，请求体沿用 `platform`、`type`、`base_url`、`api_key`、`model_mapping`，新增可选 `image_protocol`，为空按 openai 处理。响应仍为 `models`、可选 `metadata` 和 `warnings`；同步只查询模型目录，不调用生图接口，也不会自动修改模型映射。

- openai：Bearer 鉴权请求 `/v1/models`，保留名称含 `image` 或以 `dall-e-` 开头的模型（包括 `gpt-image-*`、`chatgpt-image*`）。
- gemini：`x-goog-api-key` 鉴权请求 `/v1beta/models`，最多读取 20 页；去掉 `models/` 前缀，保留 `imagen-*` 和名称含 `image` 的模型。
- grok：Bearer 鉴权优先请求 `/v1/image-generation-models`，保留其中全部模型；仅在该接口返回 404/405 时回退 `/v1/models`，再保留名称含 `image` 或 `imagine` 的模型。

地址取自账号协议对应的默认地址或 `base_url`，沿用现有同步功能的上游地址校验、代理和响应大小限制。过滤后没有识别出图片模型但原列表非空时，返回完整列表并附 `image_models_not_identified` 警告，管理员需手动挑选；上游本身返回空列表则报错。Image 同步跳过 Codex 能力元数据补全，避免自定义图片模型因缺少文本上下文信息而同步失败；上游错误不向前端返回凭证或原始响应体。

## 参数与限制

Gemini/Imagen 默认返回 `b64_json`，显式 `response_format=url` 返回 400。转换协议 Grok 默认也请求 `b64_json`，上游仅返回 URL 时会安全下载并回填，下载失败则返回错误且不计费；请求 `url` 但上游只给 base64 时，转换为 `url: "data:<mime>;base64,<data>"`，按实际图片数计费，无法识别 MIME 时使用 `image/png`。OpenAI 透传协议不改变上游默认格式，建议显式传 `response_format`。Gemini generateContent 的 `n` 为 1–4，最多 4 路并发调用，结果顺序和调用顺序一致；部分失败不会取消其他调用。Imagen 为 1–4，Grok 为 1–10。Imagen 不支持 edits；Gemini 支持内联参考图，Grok 支持 URL 或 data URL。Image URL 下载在连接时解析、验证并固定公网 IP，逐跳校验重定向，每图最大 20 MiB；账号配置代理时拒绝需要本地下载的参考图或回填，避免代理出口绕过校验。

转换协议对不支持的字段或组合返回 400 `invalid_request_error`，例如 stream、mask、非默认 background/style 等。size 必须精确匹配上游比例，不会选择近似比例。Gemini 2.5 Flash Image 仅 1K，Gemini 3 Pro 支持 1K/2K/4K，Imagen 4 标准/ultra 支持 1K/2K，Grok 支持 1K/2K。`WxH` 超过模型最高档位时保留比例并降到最高支持档，例如 Gemini 2.5 收到 `1536x1024` 按 3:2 出 1K；显式 `2K`/`4K` 超出能力时仍返回 400。创作页展示能降档的尺寸，并按实际档位估价。

## 计费与错误

按成功响应实际返回的图片数乘以现有按张单价；部分成功只收取实际出图数，零张、失败及安全拦截不收费。上游 token 用量可记入日志，但不改变 Image 平台按张计费口径；日志保留账号平台、图片数量、实际尺寸档位和客户端输入尺寸。转换器选定的档位优先用于结算；账号未选定前，预占和创作估价按可选账号的实际档位取最高费用。余额和平台额度预检沿用现有 Images 链路。

仅转换器标记 `Retryable` 的错误允许切换账号，包括 429、5xx，以及账号鉴权/额度故障。普通参数错误及内容安全拦截不重试。重试耗尽保留最后一次转换后的状态、type、code、param，429 传递经过安全校验的 Retry-After。首条 SSE 错误尚未出图时也适用换号；已出图后发送脱敏终止事件，按成功图数计费。

错误消息及账号状态会按所有非空真实 API Key、上游地址和主机名脱敏；少于 6 字符的 Key 使用固定错误消息，避免在正常文字中替换造成误读。客户端与日志不包含原始堆栈；异步失败保留同步错误的 type/code/param。明确的 401/无效 Key 停用账号，普通 403、额度及地区问题使用 5 分钟冷却；参数错误和安全拦截不改变账号状态。一次多调用请求至多触发一次副作用，优先级为凭证、额度、限流、服务故障。本地校验 400 不计入账号调度失败。管理端“测试账号”当前明确返回暂不支持，可使用 Images API 验证账号。
