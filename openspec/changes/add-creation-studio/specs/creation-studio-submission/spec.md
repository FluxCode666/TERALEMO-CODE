## ADDED Requirements

### Requirement: 面板必须提供统一的创作提交接口且只复用既有生成链路

系统 SHALL 在 `POST /api/v1/user/creation/tasks`（JWT 鉴权）接受生图或生视频的统一表单。生图 MUST 只路由到 `POST /v1/images/generations/async` 或 `POST /v1/images/edits/async`；生视频 MUST 只路由到既有的 Seedance、Gemini、MiniMax、Grok 原生创建端点。系统 MUST NOT 提供同步生图路径。

#### Scenario: 提交文生图
- **WHEN** 登录用户以 `mode=image` 提交合法表单且没有 `source` 图片
- **THEN** 系统 MUST 重放 `POST /v1/images/generations/async`
- **THEN** 响应 MUST 包含 `task_id`、`catalog_id`、`mode`、`protocol`、`model`、`status`、`estimated_cost`、`created_at`、`expires_at`

#### Scenario: 提交单图编辑
- **WHEN** 表单带一个 `role=source` 的有效上传
- **THEN** 系统 MUST 从对象存储读回该图字节，以 data URL 放进 JSON 请求体（OpenAI 为 `images[].image_url`，Grok 为 `image.url`），并重放 `POST /v1/images/edits/async`

#### Scenario: 提交生视频
- **WHEN** 登录用户以 `mode=video` 提交合法表单
- **THEN** 系统 MUST 重放对应协议的原生创建端点
- **THEN** 视频 MUST 沿用提交时冻结价格、持久预占、成功按实际封顶、失败或取消释放的既有语义

### Requirement: 提交必须通过进程内重放复用网关准入

系统 SHALL 以所选 Key 的身份，在进程内把原生请求重放到同一 gin engine 的白名单路径（契约 §5.4，共 7 条）。重放 MUST 保留真实客户端地址（`RemoteAddr`、`X-Forwarded-For`、`X-Real-IP`），MUST NOT 透传外层的 Cookie、Origin、Referer 与 Authorization。重放 context MUST 带有不可从外部伪造的创作页重放标记。

#### Scenario: Key 不属于当前用户
- **WHEN** `api_key_id` 不存在或不属于当前用户
- **THEN** 系统 MUST 返回 `404 CREATION_KEY_NOT_FOUND`，MUST NOT 发起内层请求

#### Scenario: 白名单之外的路径
- **WHEN** 映射结果不在白名单内
- **THEN** 系统 MUST 返回 `400 INVALID_CREATION_REQUEST`，MUST NOT 发起内层请求

#### Scenario: Key 明文不出后端
- **WHEN** 任意面板接口返回成功或失败
- **THEN** 响应与日志 MUST NOT 包含 API Key 明文

#### Scenario: 递归进入
- **WHEN** 带重放标记的请求再次进入创作接口
- **THEN** 系统 MUST 返回 `500 CREATION_INTERNAL_ERROR`，MUST NOT 循环

### Requirement: 生图必须在创建任务前硬拦截余额与配额不足

当且仅当请求带有创作页重放标记时，异步生图提交 SHALL 在安全审计之后、创建任务之前执行预检：资格（`CheckBillingEligibility`）→ 金额（`EstimateCreationImageCost`：按所选尺寸档位，复用执行期的 `ResolveImageBillingSize`、`calculateOpenAIImageCost` 与图片倍率；渠道为 token 计费时使用网关在途估算）→ 覆盖（`ReserveFixedCharge` 后立即释放）。预检失败时 MUST NOT 创建任务、MUST NOT 写 Redis、MUST NOT 启动执行器。

#### Scenario: 余额为零或低于最低保留
- **WHEN** 资格阶段返回余额不足
- **THEN** 系统 MUST 返回 `403 INSUFFICIENT_BALANCE`

#### Scenario: 余额为正但不足以覆盖
- **WHEN** 覆盖阶段返回余额不足
- **THEN** 系统 MUST 返回 `402 insufficient_balance`

#### Scenario: 配额不足
- **WHEN** 订阅窗口、Key 额度或限速不足
- **THEN** 系统 MUST 返回 `429`，`reason` 分别为 `USAGE_LIMIT_EXCEEDED`、`API_KEY_QUOTA_EXHAUSTED`、`rate_limit_exceeded`

#### Scenario: 企业钱包不足
- **WHEN** 企业成员的企业钱包或限额不足以覆盖
- **THEN** 系统 MUST 返回企业计费错误（例如 `403 ENTERPRISE_BALANCE_INSUFFICIENT`）

#### Scenario: API 用户不受影响
- **WHEN** 客户端直接用 API Key 调用 `/v1/images/generations/async`，未经创作页重放
- **THEN** 系统 MUST NOT 执行预检，行为 MUST 与升级前一致

#### Scenario: 预检只是准入
- **WHEN** 多个提交并发通过预检
- **THEN** 执行器内的计费准入与在途预留 MUST 照常执行并以其结果为准
- **THEN** 后到的任务 MAY 以 `failed` 结束并携带余额错误原文

### Requirement: 视频余额口径必须与网关一致

视频提交 SHALL 沿用 `handleGrokMedia` 的准入与持久预占。创作层 MUST 把「403 + `billing_error` + 消息为 `insufficient balance`」归一为 `403 INSUFFICIENT_BALANCE`，MUST NOT 修改网关对外行为。

#### Scenario: 余额低于最低保留
- **WHEN** 用户余额为正但低于最低保留余额，提交视频
- **THEN** 创作接口 MUST 返回 `403 INSUFFICIENT_BALANCE`

#### Scenario: 余额不足以冻结
- **WHEN** 余额为正但不足以覆盖本次冻结
- **THEN** 创作接口 MUST 返回 `402 insufficient_balance`

#### Scenario: 企业成员
- **WHEN** 企业成员提交视频
- **THEN** 系统 MUST 允许全部四种协议；订阅制分组 MUST 优先使用订阅，非订阅 MUST 使用企业钱包与公司、部门、员工限额

### Requirement: 统一表单到原生请求的映射必须是纯函数

映射 SHALL 实现为 `BuildCreationNativeRequest` 纯函数（图片字节由调用方传入），MUST 显式写入所有描述符参数，MUST NOT 构造跨协议数量字段（Seedance、MiniMax、Grok 不出现 `n`、`sampleCount`；Gemini 固定 `sampleCount=1`）。MiniMax 模型名以 `MiniMax-H3` 开头时 MUST 使用 v2 的 content 形态与 `/v2/video_generation`，否则 MUST 使用 v1 legacy 形态与 `/v1/video_generation`。

#### Scenario: Gemini 内联图片
- **WHEN** Gemini 请求带 `first_frame`
- **THEN** `instances[0].image` MUST 为 `{bytesBase64Encoded, mimeType}`，字节来自对象存储读回

#### Scenario: 链接型协议
- **WHEN** Seedance、MiniMax 或 Grok 请求带图片
- **THEN** 原生请求 MUST 使用上传返回的链接

#### Scenario: 互斥规则
- **WHEN** Seedance 或 MiniMax v2 请求同时带 `reference` 与 `first_frame` 或 `last_frame`
- **THEN** 系统 MUST 返回 `400 INVALID_CREATION_REQUEST`

### Requirement: 提交响应必须给出可查询的目录标识

系统 SHALL 在响应中返回 `catalog_id`。图片 MUST 等于任务 ID；视频 MUST 由 `service.AsyncVideoTaskCatalogID` 计算，该函数 MUST 与 `ObserveVideoTask` 共用。

#### Scenario: 视频目录 ID 一致
- **WHEN** 视频任务创建成功
- **THEN** `catalog_id` MUST 等于 `/api/v1/user/async-tasks` 中该任务的 `id`

#### Scenario: 目录记录尚未写入
- **WHEN** 后端未查到目录记录
- **THEN** 系统 MAY 返回空的 `catalog_id`，前端 MUST 回落为按 `task_id` 搜索

### Requirement: 异步任务目录必须返回图片的全部结果链接

目录记录 SHALL 新增可选字段 `result_urls`。图片任务保存时 MUST 从结果中取出全部 `data[].url`（最多 10 个）；`result_url` MUST 保留为第一个。用户端与管理端的列表、详情 MUST 返回该字段。

#### Scenario: 多图任务
- **WHEN** n=3 的图片任务完成
- **THEN** 详情 MUST 返回长度为 3、顺序与结果一致的 `result_urls`

#### Scenario: 旧记录
- **WHEN** 读取升级前写入的记录
- **THEN** 响应 MAY 不含 `result_urls`，前端 MUST 回落为 `[result_url]`

### Requirement: 预计费用必须无持久副作用且与提交同源

`POST /api/v1/user/creation/estimate` SHALL 返回准入金额与余额判断。它 MUST NOT 冻结、预留、写 Redis 或登记任务。图片的 `estimated_cost` MUST 与生图预检调用同一函数 `EstimateCreationImageCost`，二者 MUST 相等；视频的 `estimated_cost` MUST 与提交时冻结额使用同一价格合同函数。

#### Scenario: 视频估算与冻结一致
- **WHEN** 同一组参数先估算、后提交
- **THEN** 两者的 `estimated_cost` MUST 相等（跨越峰值时段边界的情形除外）

#### Scenario: 未配置价格
- **WHEN** 模型无有效价格
- **THEN** 估算 MUST 返回 `200` 且 `priced=false`，提交 MUST 返回 `400 video_pricing_not_configured`

### Requirement: 创作接口的请求正文不得进入审计表

`POST /api/v1/user/creation/estimate`、`/uploads`、`/tasks` SHALL 加入审计中间件的正文省略列表。

#### Scenario: 审计记录
- **WHEN** 用户提交创作任务
- **THEN** 审计记录的请求正文 MUST 为省略占位符，MUST NOT 包含提示词或图片数据
