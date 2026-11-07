## ADDED Requirements

### Requirement: 面板必须提供参考图上传接口并复用异步生图的对象存储

系统 SHALL 提供 `POST /api/v1/user/creation/uploads`（JWT 鉴权，multipart，单文件字段 `file`），使用与异步生图相同的对象存储开关、客户端与 prefix。对象路径 MUST 为 `creation-uploads/<user_id>/<YYYYMMDD>/<upload_id><ext>`，按用户隔离。

#### Scenario: 上传成功
- **WHEN** 用户上传一张合法的 png、jpeg 或 webp 图片
- **THEN** 系统 MUST 返回 `upload_id`、`url`、`content_type`、`size_bytes`、`width`、`height`、`created_at`、`expires_at`
- **THEN** 响应 MUST NOT 包含对象 key

#### Scenario: 对象存储未开启
- **WHEN** 异步生图的对象存储开关不可用
- **THEN** 系统 MUST 返回 `404 CREATION_STORAGE_DISABLED`，MUST NOT 写对象存储或 Redis

### Requirement: 上传必须校验类型、大小与像素

系统 SHALL 依据文件内容嗅探类型，MUST 忽略客户端声明的 Content-Type 与文件名。

#### Scenario: 类型不支持
- **WHEN** 文件内容不是 png、jpeg、webp（包括把 gif 或 svg 改成 `.png`）
- **THEN** 系统 MUST 返回 `415 CREATION_UPLOAD_UNSUPPORTED_TYPE`

#### Scenario: 文件过大
- **WHEN** 文件超过 10 MiB
- **THEN** 系统 MUST 返回 `413 CREATION_UPLOAD_TOO_LARGE`，`metadata.max_bytes` 为 `10485760`

#### Scenario: 像素超限或无法解码
- **WHEN** 宽或高不在 16–4096 之间、总像素超过 16777216，或无法解码
- **THEN** 系统 MUST 返回 `422 CREATION_UPLOAD_INVALID_IMAGE`

#### Scenario: 上传数量上限
- **WHEN** 用户 24 小时内的有效上传已达 100 张
- **THEN** 系统 MUST 返回 `429 CREATION_UPLOAD_LIMIT_EXCEEDED`

### Requirement: 上传必须有明确的有效期并在提交时校验归属

上传元数据 SHALL 存放在 Redis 的 `creation_upload:<upload_id>`，TTL 等于有效期。有效期 MUST 为 `min(24 小时, presigned 链接有效期)`；配置公开直链时为 24 小时。提交时系统 MUST 校验上传属于当前用户，且剩余有效期不少于 15 分钟。

#### Scenario: 使用他人或不存在的上传
- **WHEN** 提交引用的 `upload_id` 不存在、已过 TTL 或不属于当前用户
- **THEN** 系统 MUST 返回 `404 CREATION_UPLOAD_NOT_FOUND`，MUST NOT 泄露其是否存在

#### Scenario: 即将过期
- **WHEN** 上传剩余有效期少于 15 分钟
- **THEN** 系统 MUST 返回 `410 CREATION_UPLOAD_EXPIRED`

### Requirement: 需要内联字节时必须从对象存储直接读回

Gemini 视频与生图编辑需要图片字节。系统 SHALL 通过对象存储客户端（S3 `GetObject`）读回字节，MUST NOT 经 HTTP 下载自身的链接。

#### Scenario: 自建内网对象存储
- **WHEN** 对象存储端点位于私网
- **THEN** Gemini 首帧与生图编辑 MUST 仍能取得字节并成功提交

### Requirement: 上传对象的生命周期必须由存储桶规则负责

应用 MUST NOT 依赖自身删除上传对象。Redis 元数据过期后，系统 MUST 拒绝使用该上传。部署说明 MUST 要求为 `<prefix>creation-uploads/` 配置生命周期规则（建议 3 天）。

#### Scenario: 元数据过期但对象仍在
- **WHEN** 对象尚未被生命周期规则删除，但 Redis 元数据已过期
- **THEN** 提交 MUST 返回 `404 CREATION_UPLOAD_NOT_FOUND`

### Requirement: 上传接口的正文不得进入审计表

`POST /api/v1/user/creation/uploads` SHALL 加入审计中间件的正文省略列表，动作名为 `user.creation.upload`。

#### Scenario: 审计记录
- **WHEN** 用户上传图片
- **THEN** 审计记录的正文 MUST 为省略占位符
