## ADDED Requirements

### Requirement: 用户侧必须提供创作页路由与菜单入口

前端 SHALL 新增路由 `/creation`（name `CreationStudio`，组件 `@/views/user/CreationStudioView.vue`），`meta` MUST 为 `requiresAuth: true`、`requiresAdmin: false`、`titleKey: 'creationStudio.title'`、`descriptionKey: 'creationStudio.description'`。路由 MUST NOT 设置 `requiresPersonalWallet`。侧栏 MUST 在「异步任务」之前提供入口。

#### Scenario: 未登录访问
- **WHEN** 未登录用户访问 `/creation`
- **THEN** 系统 MUST 按现有鉴权守卫跳转登录

#### Scenario: 企业成员访问
- **WHEN** 企业成员登录后访问 `/creation`
- **THEN** 页面 MUST 正常打开，MUST NOT 被个人钱包门禁拦截

#### Scenario: 三处菜单
- **WHEN** 普通用户、管理员（「我的账户」区）或简单模式下的管理员查看侧栏
- **THEN** 三种情形 MUST 都能看到「创作」入口，位置在「异步任务」之前

### Requirement: 页面必须由描述符驱动渲染参数区

参数区 SHALL 只消费后端 options 返回的描述符，按 `kind` 分发到对应控件。前端 MUST NOT 根据模型名或协议名编写参数分支。

#### Scenario: 切换模型
- **WHEN** 用户在同一模式下切换模型
- **THEN** 参数区 MUST 按新模型的描述符重建并填入其默认值
- **THEN** 提示词 MUST 保留

#### Scenario: 切换模式
- **WHEN** 用户在「生图」与「生视频」间切换
- **THEN** 只有表单区域变化
- **THEN** 历史列表的筛选 MUST NOT 跟随变化

#### Scenario: 切换 Key
- **WHEN** 用户切换 API Key
- **THEN** 页面 MUST 重新加载 options，清空不再可用的模型与参数

### Requirement: 页面必须在提交前展示预计费用与余额

页面 SHALL 在参数变化后以防抖方式调用估算接口，展示「预计」费用、明细与可用余额，并在不足时禁用提交并说明原因。

#### Scenario: 余额不足
- **WHEN** 估算返回 `sufficient=false`
- **THEN** 费用区 MUST 进入 `insufficient` 状态并展示缺口与充值入口
- **THEN** 生视频的提交按钮 MUST 禁用；生图 MUST 以软提示呈现（执行期仍可能失败）

#### Scenario: 无法定价
- **WHEN** 估算返回 `priced=false`
- **THEN** 费用 MUST 显示 `—`，提交按钮 MUST 禁用

#### Scenario: 余额读取失败
- **WHEN** `balance_available=false`
- **THEN** 页面 MUST 只展示费用、隐藏余额行，MUST NOT 阻止提交

### Requirement: 提交、轮询与结果展示必须与异步任务目录一致

页面 SHALL 通过 `POST /api/v1/user/creation/tasks` 提交，用响应中的 `catalog_id` 跟踪任务，通过 `GET /api/v1/user/async-tasks` 读取历史与进度，不新增列表接口。

#### Scenario: 提交互斥
- **WHEN** 一个提交仍在进行中
- **THEN** 提交按钮 MUST 处于 `aria-busy` 且禁用，重复点击 MUST NOT 产生第二个请求

#### Scenario: 轮询启停
- **WHEN** 列表中存在 `processing` 任务
- **THEN** 页面 MUST 每 10 秒刷新当前已加载的列表；页面不可见时 MUST 暂停，恢复可见时 MUST 补一次
- **THEN** 视频任务的 refresh 调用 MUST 对每个任务节流为 30 秒一次

#### Scenario: 多图结果
- **WHEN** 图片任务完成且 `result_urls` 有多个链接
- **THEN** 页面 MUST 展示全部图片；没有 `result_urls` 时 MUST 回落为 `[result_url]`

#### Scenario: 进度展示
- **WHEN** 任务处于 `processing`
- **THEN** 页面 MUST 显示不确定进度条与已用时长（`now - created_at`）

#### Scenario: 视频结果播放
- **WHEN** 视频任务完成
- **THEN** 页面 MUST 通过 `GET /api/v1/user/async-tasks/{id}/content` 获取字节并以 `blob:` URL 播放
- **THEN** 页面 MUST NOT 直接把上游链接写入 `<video src>`

#### Scenario: 不安全链接
- **WHEN** `result_url` 不是 `http(s)` 或站内相对路径
- **THEN** 页面 MUST NOT 渲染为链接或图片源

#### Scenario: 结果过期
- **WHEN** 任务已超过 `expires_at` 或结果资源不可访问
- **THEN** 卡片 MUST 进入 `expired` 形态并说明原因

### Requirement: 页面必须支持参考图上传

参考图、首帧、尾帧、编辑原图 SHALL 通过 `POST /api/v1/user/creation/uploads` 逐张上传，提交时只传 `upload_id`。上传前页面 MUST 按 `upload_limits` 校验大小与类型。

#### Scenario: 上传中与失败
- **WHEN** 用户选择图片
- **THEN** 页面 MUST 显示上传进度；失败时 MUST 显示原因并允许重试或删除

#### Scenario: 上传不可用
- **WHEN** `uploads.available=false`
- **THEN** 所有图片字段 MUST 禁用并显示原因，文生视频 MUST 仍可提交

### Requirement: 历史列表必须采用加载更多并自带类型筛选

历史区 SHALL 以单列卡片流展示，提供「全部 / 图片 / 视频」筛选，采用「加载更多」追加分页。

#### Scenario: 加载更多
- **WHEN** 用户点击加载更多
- **THEN** 新数据 MUST 追加到列表尾部，已有卡片 MUST NOT 被清空或重排

#### Scenario: 加载失败
- **WHEN** 加载更多失败
- **THEN** 已有列表 MUST 保留，并在列表底部给出错误与重试

### Requirement: 复用参数必须基于本地快照且只回填未过期的参考图

页面 SHALL 在每次提交成功后，把快照写入 localStorage `creationStudio:snapshots:v1:<user_id>`（结构 `{v:1, items:[...]}`，最多 100 条，7 天过期），MUST NOT 把提示词或参数写入服务端。写入失败 MUST NOT 影响提交。「复用参数」SHALL 回填模式、Key、模型、提示词、参数与未过期的参考图。

#### Scenario: 有本地快照的任务
- **WHEN** 用户对有本地快照的任务点击复用
- **THEN** 页面 MUST 回填模型、提示词与在当前描述符下合法的参数
- **THEN** 剩余有效期不少于 15 分钟的图片 MUST 回填（缩略图用快照中的 `url`，提交用 `upload_id`）

#### Scenario: 参考图已过期
- **WHEN** 快照中的图片剩余有效期少于 15 分钟
- **THEN** 页面 MUST 在对应位置显示「参考图已过期，请重新上传」，MUST NOT 提交该 `upload_id`

#### Scenario: 无本地快照的历史任务
- **WHEN** 任务没有本地快照（换了设备或已超过 7 天）
- **THEN** 页面 MUST 只回填模型与描述符默认值，提示词留空，并提示无法复用提示词

#### Scenario: 存储空间不足
- **WHEN** 写入 localStorage 抛出配额错误
- **THEN** 页面 MUST 删掉最旧的一半后重试一次，再失败则放弃写入，提交流程不受影响

#### Scenario: 模型已不可用
- **WHEN** 被复用任务的模型不在当前 Key 的 options 中
- **THEN** 页面 MUST 保留其余参数并提示模型不可用

### Requirement: 页面必须覆盖空态、不可用态与错误态

页面 SHALL 依据 options 的 `reason` 与提交错误的 `reason` 呈现空态、不可用态与错误提示，文案 MUST 来自 `creationStudio` 命名空间。

#### Scenario: 没有可用 Key
- **WHEN** 用户没有任何 active 且绑定分组的 Key
- **THEN** 表单区 MUST 显示空态与「去创建 Key」入口

#### Scenario: 生图不可用
- **WHEN** `image.available=false`
- **THEN** 模式切换的「生图」MUST 保留并带不可用标记，选中后展示原因说明；生视频 MUST 仍可使用

#### Scenario: 面板通用限流
- **WHEN** 任一创作接口返回 HTTP 429 且响应体没有 `reason`
- **THEN** 页面 MUST 按 `rate_limit_exceeded` 处理，倒计时取响应头 `Retry-After`（缺失时 60 秒）

#### Scenario: 错误映射
- **WHEN** 提交返回 402、403、429、404、502 或 503
- **THEN** 页面 MUST 按 `docs/CREATION_STUDIO.md` §8.5 的映射展示对应提示，并保留用户输入

### Requirement: 页面必须满足国际化、暗色模式与无障碍要求

#### Scenario: 国际化完整
- **WHEN** 运行 `pnpm check:i18n`
- **THEN** `creationStudio` 中英文 key 集合 MUST 完全一致，页面 MUST NOT 含硬编码中文

#### Scenario: 键盘与读屏
- **WHEN** 用户仅用键盘操作页面
- **THEN** 模式切换与筛选 MUST 使用 `role="tablist"` 与 `aria-selected`，参数胶囊 MUST 使用原生 radio，进度条 MUST 使用 `role="progressbar"`
- **THEN** 任务状态变化 MUST 通过 live region 播报

#### Scenario: 暗色模式
- **WHEN** 用户切换到暗色模式
- **THEN** 所有区块 MUST 使用 `creation-studio.css` 定义的暗色变量，不出现对比度不足的文本
