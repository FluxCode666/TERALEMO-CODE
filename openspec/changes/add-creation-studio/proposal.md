# add-creation-studio

> 状态：已冻结（2026-11-07）。接口契约：`docs/CREATION_STUDIO.md` v1.2。

在用户侧面板新增「创作页」（`/creation`）。一个页面走完整条链路：选 Key → 选模型 → 写提示词 → 调参数 → 上传参考图 → 看预计费用 → 提交 → 看进度 → 看全部结果 → 复用参数。

- 生图**只走现有异步生图链路**，即 `/v1/images/generations/async`、`/v1/images/edits/async` 及其执行器、计费、对象存储与任务目录。
- 生视频覆盖 Seedance、Gemini（Veo）、MiniMax、Grok 四种既有原生协议。
- 不新增同步生图通道，不改变网关任何既有接口对 API 用户的行为。

阅读顺序：`proposal.md` → `design.md` → `docs/CREATION_STUDIO.md`（实现以契约为准）→ `specs/*/spec.md` → `tasks.md` → `verification.md`。

## Why

1. 面板目前没有「给我一张图 / 一段视频」的入口。用户只能自己拼 API 请求，或者去用 Gemini 批量通道。
2. 异步生图、四种视频协议、持久预占和价格冻结都已经上线，但缺少统一的界面。用户必须自己理解四种请求体、时长和分辨率规则以及错误格式。
3. `/async-tasks` 页只能查看、不能提交；目录只显示第一张图，也不保存参数，没法一键复用上一次的设置。

## What Changes

- 新增页面 `/creation`（`CreationStudioView.vue`）。菜单入口放在「异步任务」之前，在普通用户菜单、管理员的「我的账户」区和简易模式中都显示。企业成员可用。
- 新增四个面板接口（JWT 鉴权，均在 `/api/v1/user/creation/` 下）：
  - `GET /options?api_key_id=`：按 Key 所在分组返回可用模式、模型、参数描述符，以及上传是否可用。
  - `POST /estimate`：返回预计费用与准入判断，不产生持久副作用。
  - `POST /uploads`：上传参考图、首帧或编辑原图到对象存储，返回 `upload_id` 和链接。
  - `POST /tasks`：统一提交入口。后端构造原生请求，在进程内重放到既有网关路由，完整复用鉴权、准入、计费、预占、价格冻结和目录登记。
- 生图在提交阶段硬拦截：创作页重放进入异步生图提交时，先执行一次计费预检，余额或配额不够就直接返回 403/402/429，不创建任务。直接调用 API 的用户行为不变。
- 异步任务目录新增可选字段 `result_urls`，图片任务返回全部结果链接，用户端和管理端都返回。`result_url` 保留，仍为第一张。
- 复用参数使用前端本地快照（localStorage），不落库。快照中的参考图在未过期时可以回填。
- 「API 返回 `403 billing_error`（余额低于最低保留）」只在创作层归一为 `INSUFFICIENT_BALANCE`，网关对外行为不改。
- 审计中间件省略三条创作 POST 接口的请求正文。

## Capabilities

### New Capabilities

- `creation-studio-submission`：统一提交、进程内重放、生图预检、原生请求映射、错误归一、目录 ID、预计费用。
- `creation-studio-uploads`：参考图上传、校验、存储、有效期、提交时的归属与过期校验、生命周期。
- `creation-studio-model-catalog`：按 Key/分组下发模式可用性、模型、四种参数描述符、上传可用性与价格摘要。
- `creation-studio-console`：页面路由与菜单、描述符渲染、费用展示、上传交互、任务流与轮询、全部结果展示、历史筛选、本地快照与复用参数、空态与错误态、国际化与无障碍。

### Modified Capabilities

无正式 capability。`openspec/specs/` 目前为空；异步任务目录增加可选字段 `result_urls`，作为向后兼容的扩展记录在 `creation-studio-submission` 中。

## Impact

- **后端**
  - 新增 `backend/internal/handler/creation_studio_*.go` 和 `backend/internal/service/creation_studio*.go`。
  - 对既有代码只做三类小改动：抽取、导出、加字段，每处都必须有回归测试：
    - 异步生图 `Submit` 中加一处受重放标记保护的预检调用；
    - `AsyncTask.ResultURLs` 及其写入；`AsyncVideoTaskCatalogID` 的抽取；
    - `ImageResultUploader` 的 `SaveObject`/`ReadObject`/`LinkPolicy`；S3 存储的 `Read`/`LinkPolicy`；
    - Gemini、MiniMax 能力表的抽取；两个判定函数的导出；
    - 新增四条路由和 `SetDispatcher`；
    - `audit_log.go` 两个 map 各加条目。
- **数据库**：无迁移、无新表。
- **Redis**：新增 `creation_upload:<upload_id>`（TTL ≤ 24h）和 `creation_uploads:user:<user_id>`（ZSET，48h）。
- **对象存储**：新增路径 `<prefix>creation-uploads/<user_id>/...`。部署方需要为该路径配置生命周期规则（建议 3 天），并保证存储端点能被视频厂商从公网访问。
- **Wire**：`backend/cmd/server/wire_gen.go` 不变（新 handler 在 `ProvideHandlers` 内用已注入依赖构造）。
- **前端**
  - 新增 `views/user/CreationStudioView.vue`、`features/creation-studio/**`、`i18n/locales/{zh,en}/creationStudio.ts`。
  - 修改 `router/index.ts`、`components/layout/AppSidebar.vue`、`i18n/locales/{zh,en}/index.ts`、`{zh,en}/common.ts`。
  - `api/asyncTasks.ts` 只新增 `result_urls?: string[]`。
- **视觉**：复用 design 角色产出的 `frontend/src/styles/creation-studio.css` 与 `frontend/src/assets/creation-studio/*.svg`；规范见 `frontend/src/styles/creation-studio.spec.md`。
- **兼容性**：没有破坏性变更。旧的目录记录没有 `result_urls`，前端回落到 `result_url`。直接调用 API 的用户在生图提交阶段不受新预检影响。
- **安全与隐私**：
  - 创作接口只传 `api_key_id`，自身的响应、日志、审计不含 Key 明文，重放用的明文只在后端内存；Key 选择列表复用既有的 `GET /api/v1/keys`（响应含明文，前端内存持有，DOM 与 localStorage 不含），未扩大既有暴露面。
  - 内层重放只允许白名单路径，标记不可伪造。
  - 上传按用户隔离，并靠内容嗅探判断类型。
  - 提示词只存在于请求内存和用户本地 localStorage，不写入审计和数据库。
