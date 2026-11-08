## ADDED Requirements

### Requirement: 系统必须按所选 Key 的分组下发可用模式与模型

系统 SHALL 提供 `GET /api/v1/user/creation/options?api_key_id=`（JWT 鉴权），按该 Key 当前分组返回生图与生视频两种模式的可用性、可选模型列表以及每个模型的参数描述符。返回的模型 MUST 只来自该分组内可调度的账号，MUST NOT 混入其它分组的模型。

#### Scenario: 选择 OpenAI 分组的 Key
- **WHEN** 用户以 OpenAI 平台分组的 Key 请求 options，且对象存储已开启、分组允许生图
- **THEN** `image.available` MUST 为 `true`
- **THEN** `models` MUST 包含该分组可用的 `gpt-image-*` 模型行，`mode=image`、`protocol=openai`

#### Scenario: 选择 Video 分组的 Key
- **WHEN** 用户以 Video 平台分组的 Key 请求 options
- **THEN** `image.available` MUST 为 `false` 且 `reason=platform_unsupported`
- **THEN** `models` MUST 按账号的 `video_protocol` 列出 Seedance、Gemini、MiniMax 的模型行

#### Scenario: 账号未配置模型映射
- **WHEN** 分组内账号未配置 `model_mapping`
- **THEN** 该账号 MUST 只贡献契约 §3.4 规定的默认模型名（例如 OpenAI `gpt-image-2`、Grok 生图 `grok-imagine-image`、Grok 视频 `grok-imagine-video`）

#### Scenario: 映射中没有符合条件的模型
- **WHEN** 账号配置了 `model_mapping`，但没有任何 key 满足该模式与协议的条件
- **THEN** 该账号 MUST NOT 贡献模型，MUST NOT 回落到默认名

#### Scenario: Key 不属于当前用户
- **WHEN** `api_key_id` 不存在或不属于当前用户
- **THEN** 系统 MUST 返回 `404 CREATION_KEY_NOT_FOUND`

#### Scenario: 合成分组
- **WHEN** Key 的分组平台为 composite
- **THEN** `image` 与 `video` MUST 均为 `available=false` 且 `reason=platform_unsupported`

#### Scenario: Key 不可用
- **WHEN** Key 非 active、已过期或额度用完
- **THEN** `image`、`video`、`uploads` MUST 均为 `available=false` 且 `reason=key_unavailable`

#### Scenario: 视频分组无可用账号
- **WHEN** 分组内没有任何可调度的视频账号
- **THEN** `video.available` MUST 为 `false` 且 `reason=no_accounts`
- **THEN** `models` MUST NOT 包含任何 `mode=video` 的行

### Requirement: 模型行必须以（模式、协议、模型）三元组唯一标识

同一公开模型名可能由多个协议的账号承载。系统 SHALL 以 `(mode, protocol, model)` 三元组作为模型行的唯一标识，MUST 为每种承载协议各输出一行，并 MUST 在提交时要求客户端带上所选行的 `protocol`。

#### Scenario: 同一模型被两种协议承载
- **WHEN** 分组内同时存在把同一公开名映射到 Seedance 与 MiniMax 的账号
- **THEN** `models` MUST 输出两行，`protocol` 不同、`id` 不同
- **THEN** 前端 MUST 以「模型名 · 协议」区分展示

#### Scenario: 多账号能力不一致
- **WHEN** 同一模型行由能力不同的多个账号承载
- **THEN** 描述符 MUST 取能力并集
- **THEN** 该行 MUST 标注 `partial=true`

### Requirement: 参数描述符的 kind 必须限定为四种

每个模型行的 `params` SHALL 是参数描述符数组，`kind` MUST 只取 `select`、`integer`、`ratio`、`image` 四值，与 `frontend/src/styles/creation-studio.spec.md` §4.5 一致。布尔量 MUST 表示为 `select`（`true`/`false` 两项），MUST NOT 引入第五种 kind。

#### Scenario: 遍历全部描述符
- **WHEN** 对任意 Key 的 options 结果遍历所有描述符
- **THEN** 每个描述符的 `kind` MUST 属于上述四值

#### Scenario: 前端遇到未知 kind
- **WHEN** 前端收到不在四值之内的 `kind`
- **THEN** 前端 MUST 跳过该字段并记录告警
- **THEN** 页面 MUST NOT 崩溃

### Requirement: 描述符的键集与取值必须与提交期的协议规范化同源

描述符 SHALL 按 `docs/CREATION_STUDIO.md` §4.3 的表格生成。视频的分辨率与时长取值 MUST 使用与提交期规范化相同的能力表与账号覆盖规则（`video_resolutions`、`video_min_duration_seconds`、`video_max_duration_seconds`），系统 MUST NOT 维护第二份能力表。

#### Scenario: 账号覆盖分辨率
- **WHEN** 视频账号配置 `video_resolutions: ["480p", "1080p"]`
- **THEN** 该账号承载的模型行的 `resolution` 描述符 MUST 只包含 `480p` 与 `1080p`

#### Scenario: 能力表抽取不改变行为
- **WHEN** 把 Gemini、MiniMax 内联能力表抽取为共享函数
- **THEN** 抽取前后的分辨率、时长下界、时长上界、离散时长集合 MUST 逐项相等
- **THEN** 现有提交路径的单测 MUST 全部保持通过

#### Scenario: 键集合固定
- **WHEN** 生成任一协议的描述符
- **THEN** 键集合与默认值 MUST 与 `docs/CREATION_STUDIO.md` §3.3 一致，不多不少
- **THEN** `kind=image` 描述符的 key MUST 为 `source`、`first_frame`、`last_frame`、`reference` 之一

#### Scenario: 不下发跨协议数量字段
- **WHEN** 生成 Seedance、MiniMax 或 Grok 的描述符
- **THEN** 描述符 MUST NOT 包含 `n`、`sample_count` 等数量参数

### Requirement: 模式可用性必须给出稳定原因码

`image` 与 `video` 两个可用性对象 SHALL 在不可用时返回稳定的 `reason`：`no_group`、`platform_unsupported`、`storage_disabled`、`permission_denied`、`no_accounts`、`key_unavailable`。options 的可用性只作提示，最终裁决 MUST 以提交接口的结果为准。另需返回 `uploads` 可用性与 `upload_limits`（`max_bytes=10485760`、`accept`、`max_side_px=4096`、`max_pixels=16777216`）。

#### Scenario: 对象存储未开启
- **WHEN** 异步生图功能未启用
- **THEN** `image.available` MUST 为 `false` 且 `reason=storage_disabled`
- **THEN** `uploads.available` MUST 为 `false` 且 `reason=storage_disabled`
- **THEN** 所有 `kind=image` 的描述符 MUST 带 `disabled=true` 与 `disabledReason='storage_disabled'`
- **THEN** `video` 的可用性 MUST NOT 受影响，纯文生视频仍可提交

#### Scenario: 分组禁止生图
- **WHEN** Key 分组 `allow_image_generation=false`
- **THEN** `image.available` MUST 为 `false` 且 `reason=permission_denied`

#### Scenario: 提示与提交结果不一致
- **WHEN** options 报告生图可用，但提交时对象存储已被关闭
- **THEN** 提交 MUST 返回 `404 ASYNC_IMAGE_DISABLED`
- **THEN** 前端 MUST 以提交结果刷新可用态

### Requirement: 价格摘要必须只读且不得替代估算

每个模型行 SHALL 带只读的 `pricing` 摘要，仅用于列表展示。摘要计算 MUST NOT 冻结资金或写入任何存储；真实费用 MUST 以 `POST /api/v1/user/creation/estimate` 为准。

#### Scenario: 模型未配置价格
- **WHEN** 视频模型没有任何可用价格
- **THEN** `pricing.priced` MUST 为 `false`
- **THEN** 该模型行 MAY 仍然展示，但前端 MUST 在估算后禁用提交
