# 创作页（Creation Studio）视觉与交互规范

> 状态：设计稿 v1.2（2026-11-07：对齐冻结契约 `docs/CREATION_STUDIO.md` v1——组件名、`errors.*`、参数 label key 按契约；新增 §4.9 编辑模式「原图 → 结果」对照、§7.1 / §7.2 全部 reason 码的展示位置；v1.1：§12 决策已定、参考图可回填、上传「已过期 / 不可用」状态、MediaGrid 1–4 张排布）。配合 `openspec/changes/add-creation-studio/`、`docs/CREATION_STUDIO.md` 的接口契约
> 适用：用户侧「创作页」，支持生图（异步生图任务）与生视频
> 落地资源：`frontend/src/styles/creation-studio.css`、`frontend/src/assets/creation-studio/*.svg`
> 接口字段以 spec 角色的契约为准；本文只负责**区块结构、状态、视觉与无障碍**，参数项按「可容纳不同参数组合」的通用模型设计。

---

## 0. 设计目标与约束

1. **一个页面，两种产出**：生图与生视频共享「选 Key → 选模型 → 写提示词 → 调参数 → 看费用 → 提交 → 看进度 → 拿结果 → 复用参数」这条主链路，差异只在参数项和结果形态。
2. **参数区必须可扩展**：模型不同，可用参数项不同（尺寸/数量/质量/参考图 vs 时长/分辨率/比例/首帧）。表单按 **参数描述符（descriptor）驱动渲染**，不允许为某个模型写死一堆 `v-if`。
3. **与现有体系一致**：沿用 `src/style.css` 的 `.card / .btn / .input / .badge / .empty-state` 语言（teal 主色 + 深蓝灰辅助色），沿用 `AsyncTasksView` 的任务状态配色。新增加的能力（模式切换、参数胶囊、媒体网格、灯箱）用 `cs-` 前缀的样式类补齐，不另起一套视觉。
4. **长时任务优先**：页面要能同时回答「我刚提交的那个跑到哪了」「我上一批结果在哪」「为什么这次提交不了」。任务卡片必须在折叠状态下也能看出状态、进度、错误原因。
5. **移动端可用**：手机上参数区和结果区不能上下摞成一条无限长的页面，用「参数 / 结果」切换 + 常驻提交栏。

---

## 1. 栅格与间距

### 1.1 外部容器

沿用 `AppLayout`：内容区水平内边距 `px-4`（<md）/ `px-6`（md）/ `px-8`（lg+），顶部 `py-4 / py-6 / py-8`。页面**不**再套额外最大宽度，结果列随视口伸展。

| 断点 | 视口 | 侧栏 | 内容可用宽度（约） |
| --- | --- | --- | --- |
| `< md` | <768 | 抽屉（不占位） | 视口 − 32 |
| `md` | 768–1023 | 抽屉 | ≈ 720 |
| `lg` | 1024–1279 | 64 | ≈ 880（展开）/ 1070（收起） |
| `xl` | 1280–1535 | 64 | ≈ 1136 |
| `2xl` | ≥1536 | 64 | ≈ 1390 |

### 1.2 主栅格

```
.cs-layout  =  grid;  align-items: start
  < md   : 1 列（表单 / 结果用移动端切换条二选一）
  >= md  : [表单列 var(--cs-form-col)] + [结果列 minmax(0,1fr)]，gap 24px
```

| 断点 | 表单列 | 说明 |
| --- | --- | --- |
| `md` | 320px | 结果列仍约 380px，够放 1–2 列缩略图 |
| `lg` | 340px | |
| `xl` | 380px | 默认所见：表单 380 + 结果 ~730 |
| `2xl` | 420px | 结果区最多放宽到这里，再宽只加结果网格列数 |

- 表单列在 `md+` **粘性吸顶**（`top: var(--cs-sticky-top)`），内部独立滚动（`overflow-y: auto` + `overscroll-behavior: contain`），提交按钮在面板内常驻可见——用户滑到结果区也能一键再提交。
- 结果列自然滚动，不吸顶。
- 不允许横向滚动：所有 `min-width: 0` 的链路上都要保留（`.cs-layout`、两列、卡片内部），长 prompt / 长 URL 用 `break-words`。

### 1.3 间距尺度

只用 4 的倍数（Tailwind 默认尺度）：

| 用途 | 值 |
| --- | --- |
| 页面区块之间 | 20–24（`space-y-5` / `space-y-6`） |
| 卡片内边距 | 16（移动）/ 20（sm+） |
| 表单字段之间 | 16（`space-y-4`） |
| 字段标签与控件 | 6 |
| 控件高度 | 输入/选择/胶囊 36–40；主 CTA 44；图标按钮 32（触屏 40） |
| 圆角 | 卡片 16（`rounded-2xl`）；输入/按钮 12（`rounded-xl`）；结果单元 12；徽标 6–999 |

### 1.4 媒体网格

生图一次 1–4 张，**全部展示**。`cs-media-grid` 按 `data-count` 固定排布（骨架期用请求数量，结果期用实际返回数量）：

| `data-count` | 排布 | 最大宽度 | 说明 |
| --- | --- | --- | --- |
| `1` | 单列 | `min(28rem, 限高 × 宽/高)` | 限高 `--cs-tile-max-h`：桌面 32rem、移动 26rem；竖图自动收窄，不会撑出一屏 |
| `2` | 2 列 | 40rem | |
| `3` | 3 列 | 48rem | 移动端（约 343px）每格约 109px，仍保持 3 列，不换行成 2+1 |
| `4` | 2×2 | 40rem | |
| 缺省 / >4 | `repeat(auto-fill, minmax(var(--cs-tile-min), 1fr))` | — | 兜底；`--cs-tile-min` 桌面 9rem、移动 7rem |

- 间距 8px；单元圆角 `--cs-tile-radius`（12px）。
- 比例由**请求参数**决定，写在网格上、单元继承：`style="--cs-aspect-w: 2; --cs-aspect-h: 3"`（必须是数字，单图限高要用到）。个别单元需要不同比例时可在单元上写 `--cs-aspect: 3 / 2` 覆盖。骨架期就按目标比例占位，结果到位不跳动。
- 实际返回张数少于请求（上游只出了 3 张）：以实际数量重设 `data-count`，不留空位。
- 骨架单元写成 `class="cs-media-tile cs-media-skeleton"`（比例、圆角、底色来自 `cs-media-tile`，扫光来自 `cs-media-skeleton`），与结果单元同一网格，替换时不跳动。

---

## 2. 响应式行为

| 断点 | 布局 | 模式切换 | 表单 / 结果 |
| --- | --- | --- | --- |
| `< md` | 单列 | 顶部 `cs-mode-switch` 整行 | 用 `cs-view-switch-bar` 在「参数 / 结果」之间切换，切换条 sticky 在 AppHeader（`top-16`）下方；提交栏 `cs-form-footer` sticky 贴底（含安全区） |
| `md`–`lg` | 两列 | 顶部工具栏 | 表单 320–340px 吸顶独立滚动；结果列单栏 |
| `xl+` | 两列 | 顶部工具栏 | 表单 380–420px；结果网格 ≥2 列 |

移动端细则：

1. 「参数 / 结果」用 `role="tablist"`（`cs-segmented`），两个 tab：`form` / `results`。结果 tab 上带**进行中任务数角标**（`cs-segmented-badge`），让用户在填参数时知道后台有任务在跑。
2. 提交成功后**自动切到结果 tab** 并把焦点交给新任务卡片标题；桌面端不切换，新卡片插入结果区顶部，提交按钮旁出现「查看任务」次要链接。
3. 只在移动端生效：表单字段、结果区内容不重复渲染两份（同一份 DOM，靠 `hidden` / tab 显示）。
4. 粘性堆叠顺序：AppHeader（`z-30`）> 视图切换条（`z-20`）> 表单底部提交栏（`z-10`）> 任务卡片内容。灯箱/预览弹层 `z-50`，必须盖住以上全部。

---

## 3. 页面结构与分区

```
┌──────────────────────────────────────────────────────────────┐
│ A 页头：标题 + 说明 + 右侧（自动更新开关 / 刷新 / 使用文档）      │
│ B 模式切换：生图 | 生视频（+ 不可用角标）                        │
├────────────────────────┬─────────────────────────────────────┤
│ C 表单面板             │ D 结果列                            │
│  C1 Key 选择           │  D1 进行中（排队/处理中）分组         │
│  C2 模型选择           │  D2 最近结果 / 历史（筛选 + 加载更多）│
│  C3 提示词             │                                     │
│  C4 参数（描述符渲染） │                                     │
│  C5 预计费用 + 余额    │                                     │
│  C6 提交               │                                     │
└────────────────────────┴─────────────────────────────────────┘
```

- **A 页头**：`cs-title`（`text-xl` 半粗）+ `cs-description`。右侧为「自动更新」内联开关（默认开，与异步任务页习惯一致）、刷新按钮；如需跳转到 `/async-tasks` 看全量记录，放一个 `btn btn-ghost btn-sm` 链接。
- **B 模式切换**：`role="tablist"` 的 `cs-segmented cs-mode-switch`，选项 `image` / `video`，各带图标（`ImageIcon` / `Film`）。切换只改表单（C3–C6 与参数项），**不改**结果列的数据范围（D 始终展示全部类型，见 §12 决策 Q1）。
- **D1/D2 不分组为两个独立卡片**：进行中是「结果列顶部的一块」，完成后自动移入下方的「最近创作」流，视觉上避免「任务」和「历史」两套列表割裂。
- **空 / 不可用态**（§6）可以整块替代 C 或 D 的区域，而不是只弹一个 toast。

---

## 4. 组件与状态

所有用户可见文案只给语义与建议的 i18n key（前缀 `creationStudio.`）。现有通用组件能直接用的列在「复用」。**可复用清单见 §11.2。**

### 4.1 模式切换（ModeSwitch）

结构：`div.cs-segmented.cs-mode-switch[role=tablist]` > `button.cs-segmented-item[role=tab][aria-selected]`。

| 状态 | 表现 |
| --- | --- |
| 默认 | 文字 `gray-600 / dark-400`，透明底；选中项白底 + 阴影（`dark:bg-dark-700`） |
| 悬停 | 文字变深（`gray-900 / white`） |
| 聚焦 | `ring-2 ring-primary-500/50`（键盘可见） |
| 禁用 | 整项 `opacity-50 cursor-not-allowed`，`title` / `aria-describedby` 指到不可用原因 |
| 不可用但可点（推荐） | 不真正禁用：加 `data-unavailable="true"`（右上角琥珀小圆点），点进去后在 C 区展示完整说明与处理入口（如「前往后台开启对象存储」）。禁用按钮无法告知原因，体验更差 |
| 切换中 | 表单内容 150ms 淡入（`fade-in`），不整块 loading；Key/模型选择按模式各自记忆 |

图标：生图 `ImageIcon`，生视频 `Film`。文案 key：`creationStudio.mode.image`、`creationStudio.mode.video`。

### 4.2 API Key 选择（KeyPicker）

复用 `common/Select.vue`（`searchable`、`clearable=false`），选项内容用 `cs-option` 结构：主行 Key 名称 + 分组徽标（`GroupBadge`），次行掩码 `sk-****1234`。

| 状态 | 表现 |
| --- | --- |
| 默认 | 显示最近使用的 Key（本地记忆），右侧显示所属分组 |
| 无可用 Key | Select 禁用 + `cs-callout--warning`：「还没有可用的 API Key」，操作「去创建 Key」→ `/keys` |
| 当前模式下 Key 的分组不支持 | 该 Key 仍可在列表里看到但 `disabled`，副文案给原因（`cs-option-reason`）：`creationStudio.key.reason.noImage` / `.noVideo` / `.noGroup`；选中旧值时自动降级为空 |
| 加载中 | Select `loading`（内部显示 `common.loading`） |
| 加载失败 | Select 禁用 + 行内 `cs-field-error` + 「重试」 |

### 4.3 模型选择（ModelPicker）

复用 `Select`，选项：模型图标（`ModelIcon` / `PlatformIcon`）+ 模型名 + 右侧说明（`cs-option-aside`，例如「异步任务」/「按时长计费」）。

| 状态 | 表现 |
| --- | --- |
| 默认 | 记住上次选择；模型名过长 `truncate` |
| 无可用模型 | 整块替换为 `cs-empty`（插画 `setup-required.svg`）+ 说明：「当前 Key 所在分组没有支持生图的模型」，操作「切换 Key」/「查看可用模型」 |
| 模型可用但部分参数不支持 | 参数区按新模型重建，不支持的参数项隐藏或禁用（见 4.5） |
| 模型下架 / 已失效（复用时） | `cs-callout--warning`：「该模型已不可用，参数已保留」，聚焦 ModelPicker |

切换模型时**保留**提示词，参数项按新模型的能力重置为默认值；已填但新模型不支持的值丢弃，并在对应字段上一次性的灰色提示里说明（不要弹窗打断）。

### 4.4 提示词（PromptField）

组合现有 `.input`：`class="input cs-prompt"`，外层用 `common/TextArea.vue` 亦可（自带 label / error / hint）。

| 状态 | 表现 |
| --- | --- |
| 默认 | 最小高 120px，可纵向拉伸到 352px；占位文案给 2 条示例（本地化） |
| 聚焦 | 沿用 `.input` 的 teal 边框 + ring |
| 计数 | 右下 `cs-field-counter`：`{n} / {max}`；超限 `data-over="true"` 变红，且提交按钮禁用 |
| 错误 | 空提示词 / 超限 → `cs-field-error` + `aria-invalid` + `aria-describedby` |
| 快捷键 | `⌘/Ctrl + Enter` 提交；右下角 `cs-kbd` 提示，`aria-keyshortcuts` 同步标注 |
| 只读（复用中） | 正常可编辑，复用只回填值 |

### 4.5 参数区（ParameterField / 描述符渲染）

参数区是一组「字段描述符」渲染出来的，形态由 `kind` 决定。**前端只消费描述符，不判断模型名**：

```ts
// 由 spec 契约给出，前端按此渲染；此处仅示意，最终以 docs/CREATION_STUDIO.md 为准
type ParamDescriptor =
  | { key: string; kind: 'select';  labelKey: string; options: { value: string; labelKey?: string; label?: string; disabled?: boolean; hintKey?: string }[]; default?: string }
  | { key: string; kind: 'integer'; labelKey: string; min: number; max: number; step?: number; unitKey?: string; default?: number }
  | { key: string; kind: 'ratio';   labelKey: string; options: { value: string; w: number; h: number }[]; default?: string }
  | { key: string; kind: 'image';   labelKey: string; maxCount: number; accept: string[]; maxSizeMB: number; required?: boolean; hintKey?: string }
```

| kind | 控件 | 轮换形态 |
| --- | --- | --- |
| `select`（≤4 且短） | `cs-chip-group` 单选胶囊（`cs-chip`，带 `cs-chip-sub` 副文案） | 选项多/长文案时退化为原生风格 `select.input` 或 `Select` 组件 |
| `select`（>4） | `Select` 下拉 | |
| `integer` | `cs-stepper`（−/输入/＋，单位后缀），或 `cs-chip-group` 胶囊当选项少 | 区间大（如时长 4–30）用步进器 |
| `ratio` | `cs-chip` + `cs-ratio-glyph`（`--cs-ratio-w/h` 驱动的小示意框） | 比例多时网格排布 `cs-chip-group--grid` |
| `image` | `cs-dropzone` + `cs-thumb-list`（生图参考图 / 编辑原图可多张，视频首帧 1 张） | 依赖对象存储：先上传到对象存储，再把链接随任务提交 |

参数项通用状态：

| 状态 | 表现 |
| --- | --- |
| 默认 / 悬停 / 聚焦 | 见 `.cs-chip`、`.cs-stepper`、`.cs-dropzone` 定义；聚焦一律 `ring-2 ring-primary-500/50` |
| 选中 | 胶囊：teal 边框 + `primary-50` 底 + 中等字重；步进器数值高亮 |
| 禁用（模型不支持 / 依赖未满足） | 50% 透明 + `cursor-not-allowed`；**不允许**静默隐藏已选值 |
| 错误 | 控件转红（`data-invalid` / `input-error`）+ `cs-field-error` |
| 依赖联动 | 例如选了某比例 → 尺寸列表收窄：先重置被影响字段，再给一行 `cs-field-hint` 说明「已按比例调整」 |
| 参数项随模型变化 | 内容变化用 150ms 淡入，不做高度动画（避免跳动） |

上传字段（`image`，组件 `ImageDropzone`）细则：

参考图 / 首帧 / 编辑原图都**先上传到对象存储，拿到链接后随任务提交**；缩略图展示的就是该链接。

| 状态 | 表现 |
| --- | --- |
| 空 | `cs-dropzone`：虚线框 + 上传图标 + `cs-dropzone-title`（「点击上传 / 拖拽到此」）+ `cs-dropzone-hint`（格式与大小上限） |
| 拖拽悬停 | `data-dragover="true"`：teal 边框 + teal 浅底 |
| 有图 | 上传入口缩成 `cs-dropzone--tile` 网格方格，已有图排成 `cs-thumb`；首帧图的缩略图左上角带 `cs-thumb-label`（「首帧」） |
| 上传中 | 缩略图（本地预览）+ `cs-thumb-progress`（`--cs-progress` 驱动）；上传未完成时提交按钮禁用，原因 `creationStudio.submit.blocked.refImageUploading` |
| 上传失败·可重试（502 `CREATION_UPLOAD_STORAGE_FAILED`、网络中断） | `cs-thumb[data-state=error]`，保留本地预览，覆盖层 `button.cs-thumb-error`（`RotateCcw` + `cs-thumb-error-text`）= 原位重试；可删除；有失败图时提交禁用（`submit.blocked.refImageFailed`） |
| 上传失败·不可重试（413 / 415 / 422 及前端预检） | **不保留**缩略图；`cs-dropzone[data-invalid=true]`（3 秒后自动复位）+ 字段下方 `cs-field-error` 逐个文件说明原因（§7.1） |
| 上传次数用完（429 `CREATION_UPLOAD_LIMIT_EXCEEDED`） | 已有缩略图保留；上传入口 `aria-disabled="true"`（本页会话内不再可点）+ `cs-field-error` |
| 数量上限 | 达到 `maxCount` 后只保留缩略图，上传入口隐藏 |
| 删除 | 悬停/聚焦显示 `cs-thumb-remove`（触屏常显） |
| **已过期，需要重新上传** | 见下方「已过期」 |
| **不可用（`uploads.available=false`）** | 见下方「不可用」 |

每个错误码的展示位置与文案 key 汇总在 **§7.1**。

**已过期（`cs-thumb[data-state=expired]`）**

判定口径与契约 §2.5 / §8.2 一致：`expires_at - now < 900`（剩余不足 15 分钟）即视为过期，不等真正到期。触发来源：①「复用参数」回填时已不足 15 分钟（契约 §8.2 第 4 条）；②已在表单里的图，计时到点（前端定时检查）；③回填后缩略图 `img` 加载失败（`error` 事件）；④提交返回 404 `CREATION_UPLOAD_NOT_FOUND` 或 410 `CREATION_UPLOAD_EXPIRED`（契约 §2.5；错误体不指明是哪一张：先标记本地判定已不足 15 分钟的；一张都没有时，把本次提交里的全部图片标为过期）。四种来源进同一个状态。

```html
<div class="cs-thumb" data-state="expired">
  <button type="button" class="cs-thumb-expired"
          :aria-label="t('creationStudio.upload.expiredAria', { index })">
    <Link2Off aria-hidden="true" />
    <span class="cs-thumb-expired-text">{{ t('creationStudio.upload.expired') }}</span>
  </button>
  <span class="cs-thumb-label">{{ 首帧等标签，照常显示 }}</span>
  <button type="button" class="cs-thumb-remove" :aria-label="t('common.remove')">…</button>
</div>
<!-- 字段下方一行汇总，紧跟 cs-thumb-list -->
<p class="cs-field-warning" :id="warningId">
  <TriangleAlert aria-hidden="true" />
  <span>{{ t('creationStudio.upload.expiredHint', { count }) }}
    <button type="button" class="cs-field-action">{{ t('creationStudio.upload.reupload') }}</button>
  </span>
</p>
```

- 视觉：琥珀虚线边 + 琥珀浅底，**不显示旧图**（链接已失效会裂图），中间 `Link2Off` 图标 + 「已过期」小字；首帧标签保留，让用户知道过期的是哪一张。
- 交互：整块缩略图就是「原位替换」按钮，点击打开文件选择，新图上传后**占回原位置**（多图顺序、首帧位置不变）；右上角删除照常可用。字段下方 `cs-field-warning` 的「重新上传」等价于点第一张过期图。
- 阻断：只要有过期图，提交按钮禁用，`cs-submit-reason` 显示 `creationStudio.submit.blocked.refImageExpired`；字段标红不用（这不是用户输错），用琥珀警示。
- 只有部分过期：未过期的正常显示，过期的单独标记；汇总文案带数量。
- 无障碍：缩略图按钮 `aria-label` 说明「第 n 张参考图已过期，点击重新上传」；字段容器 `aria-describedby` 指向 `cs-field-warning`；状态出现时 live region 播报一次 `creationStudio.live.refImageExpired`。

**不可用（`cs-dropzone[data-state=unavailable]`）**

来源：options 的 `uploads.available=false`（契约 §3.1），此时所有 `kind=image` 描述符带 `disabled=true, disabledReason='storage_disabled'`；或者上传 / 带图提交时返回 404 `CREATION_STORAGE_DISABLED`（options 已过时，前端重新拉 options 后进入同一状态）。

| `uploads.reason` | 展示 |
| --- | --- |
| `storage_disabled` | 本节的字段级「不可用」 |
| `key_unavailable` | 不在字段上处理：此时 image / video 也都是 `key_unavailable`，整个表单进入 Key 不可用的页面级状态（§5），上传字段不渲染 |
| 其它 / 缺省 | 按 `storage_disabled` 处理（兜底） |

```html
<div class="cs-dropzone" data-state="unavailable" aria-disabled="true" :aria-describedby="hintId">
  <CloudOff class="cs-dropzone-icon" aria-hidden="true" />
  <span class="cs-dropzone-title">{{ t('creationStudio.unavailable.uploads.title') }}</span>
  <span class="cs-dropzone-hint" :id="hintId">{{ t('creationStudio.unavailable.uploads.description') }}</span>
</div>
```

- 不渲染 `<input type="file">`，不响应拖拽；实线边 + 中性底 + 琥珀 `CloudOff` 图标，**不降透明度**（原因文字要读得清）。
- **一个模型有多个图片字段**（如 Seedance 的首帧 / 尾帧 / 参考图）：不重复渲染 3 个不可用框，只在第一个图片字段的位置渲染**一个**不可用框，标题前用 `cs-field-label` 列出受影响的字段名（「首帧 / 尾帧 / 参考图」），其余图片字段不渲染。
- 字段均为**可选**（契约 v1 下发的图片描述符都是 `required=false`）：只禁用上传，其余表单照常提交；hint 用 `creationStudio.unavailable.uploads.description`（「站点未开启对象存储，暂不能上传参考图；不带图仍可生成」）。
- 字段为**必填**（契约预留 `required=true` 的情况）：hint 改用 `creationStudio.unavailable.uploads.required`，提交按钮禁用，`cs-submit-reason` 显示 `creationStudio.submit.blocked.refImageUnavailable`。
- 生图模式整体依赖对象存储（`image.reason=storage_disabled`），这时生图表单本身已是不可用态（§5），该字段不会单独出现；此状态主要出现在**生视频**的首帧 / 尾帧 / 参考图上。
- 表单里已有的缩略图（options 刷新后才发现不可用）：全部移除，字段下方 `cs-field-warning` 说明 `creationStudio.upload.removedUnavailable`。
- 复用参数遇到这种情况：参考图不回填（契约 §8.2 第 4 条），字段下方 `cs-field-warning` 说明 `creationStudio.upload.notRestored`。

### 4.6 预计费用与余额（CostSummary）

结构：`cs-cost`（左侧 `cs-cost-label`、右侧 `cs-cost-value` + `cs-cost-detail`），下方 `cs-balance`。

| 状态 | 表现 |
| --- | --- |
| 计算中 | `cs-cost-value` 显示骨架条（`skeleton`），不发抖动 |
| 可用 | `¥1.20`；明细「2 张 × ¥0.60」或「6 秒 × ¥0.20/秒」 |
| 价格缺失 | `data-state="unavailable"`，值显示 `—`，`cs-cost-detail` 提示「该模型未配置价格，无法提交」 |
| 余额不足 | `data-state="insufficient"`，值标红；下方提示「余额不足，还差 ¥x」+ 充值链接 |
| 余额低于最低保留额 | 同上，文案区分（`creationStudio.errors.balanceLow`） |
| 余额加载失败 | 只显示费用，不显示余额行 |

费用与余额都是**提交前的预估值**，文案里要体现「预计」，结果以任务完成后的实扣为准（与视频冻结/结算语义一致）。

### 4.7 提交按钮（SubmitAction）

`class="btn btn-primary cs-submit"`（44px 高、整宽），图标 `Sparkles` 或 `WandSparkles`。

| 状态 | 表现 |
| --- | --- |
| 默认 | 「生成图像」/「生成视频」随模式变化；右侧可带 `⌘↵` 提示 |
| 悬停 | 沿用 `.btn-primary` 的渐变加深 |
| 聚焦 | `focus:ring-2`（`.btn` 已内置） |
| 提交中 | `aria-busy="true"` + 图标换成旋转的 `LoaderCircle` + 文案「正在提交…」；**按钮保持原位尺寸不变**，禁用重复点击 |
| 禁用 | 下方 `cs-submit-reason` 用一句话说明**第一个**阻塞原因（缺 Key / 缺模型 / 提示词为空 / 余额不足 / 价格缺失 / 参数非法），不用 toast |
| 提交失败 | 按钮恢复可点；对应错误用 callout 显示在按钮**上方**（§7），并保留用户全部输入 |

### 4.8 任务卡片（TaskCard）

`class="card cs-task"` + `data-status`。结构：

```
header: [kind 图标] [模型名 / Key 名] ... [状态徽标]
prompt: 两行截断（点击展开）
params: 参数胶囊 cs-param-pill（尺寸、数量、时长、分辨率、比例…）
body:   按状态变化（骨架网格 / 结果网格 / 视频 / 错误框 / 取消说明）
footer: 时间（创建 / 耗时） + 操作区（下载、复制链接、复用参数、新窗口打开、刷新）
```

| 状态 | 卡片主体 | 状态徽标 | 操作 |
| --- | --- | --- | --- |
| `submitting`（本地，未拿到任务 ID） | 按请求数量渲染 `cs-media-skeleton` | 灰 · 旋转 | 无（可取消请求） |
| `queued` | 同上骨架 + 「前面还有 N 个任务」或「排队中」 | 蓝·描边 · 时钟 | 「查看全部任务」 |
| `processing` | 骨架 + `cs-progress`（有百分比则确定进度，否则不确定进度条）+ 「已用 00:42」 | 蓝·实底 · 旋转 | 手动刷新（视频任务） |
| `completed` | 图片网格 / 视频播放器 | 绿 · 对勾 | 下载、复制链接、复用参数、新窗口打开 |
| `failed` | `cs-task-error`（错误原文，可滚动） | 红 · 叉 | 复用参数重试、（生图可）复制原始错误 |
| `cancelled` | 一行中性说明「任务已取消，不计费」 | 灰 · 禁止 | 复用参数 |
| `expired`（派生） | 媒体位置的占位（`cs-media-placeholder[data-tone=warning]`）+ 「结果保留期已过」 | 琥珀 · 时钟 | 复用参数（不可下载） |

- 状态色条：只给需要关注的状态上 3px 左边条（queued 蓝-300 / processing 蓝-500 / failed 红-500 / expired 琥珀-400），completed 与 cancelled 不上色，避免整屏都是彩条。
- 只有 `processing` 的卡片会显示进度条；不确定进度时**不伪造百分比**。
- 卡片本身**不放**删除按钮（历史记录随保留期自动清理，不提供误删操作）。
- 新卡片入场：`cs-enter`（下移 6px + 淡入，280ms），仅对本次轮询新增的卡片播放一次；列表重排不播动画。
- 折叠提示词：`line-clamp-2`，点击整块展开（`data-expanded`），展开状态不记忆。

### 4.9 结果单元（MediaTile）与视频

图片：

- 结构：`a.cs-media-open`（打开灯箱，整块可点）+ `img` + 角标 `cs-media-index`（多张时）+ 悬浮操作条 `cs-media-overlay`。
- 操作条位置：右下角，图标按钮 32px（触屏 36–40）：下载 `Download`、复制链接 `Copy`（成功后 1.5s 变绿色对勾 `data-state=done`）、新窗口打开 `ExternalLink`、复用参数只在卡片 footer 提供，不进单元。
- 悬停/键盘聚焦才显示操作条；触屏常显（`@media (hover: none)`）。
- `object-cover` 裁切 + 灯箱看全图；若结果本身是透明 PNG，单元加 `cs-checker` 棋盘底。

视频：

- `cs-video`（`aspect-ratio` 由比例参数决定，`max-height: 70vh`）+ 原生 `<video controls preload="metadata" playsinline>`；竖屏加 `data-orientation="portrait"` 以高度为准。
- 播放器下方 `cs-video-actions`：下载、复制链接、新窗口打开。
- 非首个视频不自动预加载；`poster` 有则用，没有就黑底 + 播放按钮（原生控件负责）。

灯箱（Lightbox）：

- 基于 `reka-ui` 的 `Dialog` 原语（`@/components/ui/dialog`）自建一层全屏层，样式用 `cs-lightbox*`；必须 `role="dialog" aria-modal="true"` + 焦点陷阱 + 关闭后焦点回到触发单元。
- 工具栏：左侧「第 3 / 8 张」计数，右侧下载、复制、新窗口、关闭。
- 键盘：`←/→` 切换，`Esc` 关闭；触屏左右滑动切换（复用 `useSwipeSelect` 或原生 `touchstart/touchend`）。
- 图片在舞台上 `object-contain`，长图/宽图都不裁。

#### 编辑模式：原图 → 结果（EditCompare，D5）

生图带 `source`（契约 §3.3，maxCount 1）时走编辑端点，结果卡片用「原图 → 结果」对照，而不是只摆结果网格。

**何时显示对照**：异步任务目录不记录原图，原图只来自本地快照（契约 §8.1 的 `images[]` 中 `role='source'` 的一项）。

| 情形 | 展示 |
| --- | --- |
| 有快照且含 `source` | 对照布局（本节） |
| 没有快照（换设备、超过 7 天）或快照里没有 `source` | 普通结果网格（无法知道它是编辑任务，不显示「原图」位） |
| 有 `source` 但链接已过期（`expires_at < now`）或加载失败 | 对照布局保留，原图位换成 `cs-media-placeholder[data-tone=warning]`（`Link2Off` + `creationStudio.compare.sourceExpired`），结果照常展示 |

注意：结果卡片里原图的过期口径是**真正到期**（`expires_at < now`），和表单里「不足 15 分钟就算过期」不同——卡片只用于查看，到期前都能看。

**结构**（类名见 css 注释）：

```html
<div class="cs-compare">
  <div class="cs-compare-body">
    <figure class="cs-compare-source">
      <div class="cs-media-tile" :style="{ '--cs-aspect-w': source.width, '--cs-aspect-h': source.height }">
        <img :src="safeURL(source.url)" :alt="t('creationStudio.compare.sourceAlt')" />
        <button class="cs-media-open" :aria-label="t('creationStudio.compare.openSource')" />
        <span class="cs-media-tag">{{ t('creationStudio.compare.source') }}</span>
      </div>
      <figcaption class="cs-compare-label">{{ t('creationStudio.compare.sourceMeta', { w, h }) }}</figcaption>
    </figure>
    <span class="cs-compare-arrow" aria-hidden="true"><ArrowRight /></span>
    <div class="cs-compare-results">
      <p class="cs-compare-label">{{ t('creationStudio.compare.results', { count }) }}</p>
      <div class="cs-media-grid" :data-count="count" :style="结果比例">…结果单元 / 骨架…</div>
    </div>
  </div>
</div>
```

**布局**（按卡片自身宽度切换，用 container query，不按视口）：

| 卡片内宽 | 典型场景 | 排布 |
| --- | --- | --- |
| < 30rem | 移动端、`md`、`lg` 侧栏展开 | **上下**：第一行是 4.5rem 原图缩略图 + 「原图 · 宽×高」；向下箭头；下面是结果网格（`data-count` 1–4 规则同 §1.4） |
| ≥ 30rem | `xl+`、`lg` 侧栏收起 | **左右**：左列原图（宽 26%，限 6–11rem），说明在图上方；中间向右箭头（垂直居中）；右侧结果网格 |

- 原图在两种排布里都比结果小，视觉重心始终在结果上。
- 原图单元的比例取快照的 `width` / `height`，与结果比例可以不同（例如 `size=auto`）。
- 不支持 container query 的浏览器停在上下排布，仍然可用。

**各任务状态下的对照**：

| 状态 | 原图位 | 结果位 |
| --- | --- | --- |
| `submitting` / `processing` | 原图（让用户确认改的是哪张） | `cs-media-tile cs-media-skeleton` × n + 不确定进度条 |
| `completed` | 原图 | 1–4 张结果 |
| `failed` | 原图 | `cs-task-error` 替代结果网格 |
| `expired`（结果过期） | 原图（若也已过期则占位） | `cs-media-placeholder[data-tone=warning]` |

**交互**：

- 原图和结果都能点开灯箱；灯箱序列为 `[原图, 结果 1…n]`。当前项是原图时，计数位置显示 `cs-lightbox-tag`（`creationStudio.compare.source`），否则显示「2 / 4」（只计结果）。
- 原图单元**不**放下载 / 复制链接操作：那是用户自己上传的图，不是产出。
- 卡片 footer 的「复用参数」会把原图一起回填（§4.11，契约 §8.2 第 4 条）。

**无障碍**：原图 `alt` 用 `creationStudio.compare.sourceAlt`（「编辑前的原图」）；结果 `alt` 沿用「生图结果 n」；`figure` + `figcaption` 提供分组语义；箭头纯装饰 `aria-hidden`。

### 4.10 历史与筛选（HistoryFeed）

- 顶部 `cs-section-head`：标题 + `cs-section-count` + 右侧筛选（`cs-segmented`：全部 / 图片 / 视频，以及可选的状态筛选；两者收进一行，移动端可换行）。
- 列表形态：**单列卡片流**（不单独做表格；`/async-tasks` 已经是表格视图，创作页以结果为中心）。
- 分页策略：**加载更多**（`btn btn-secondary`，`cs-load-more`），到底显示 `cs-feed-end`（「没有更多了」）。不改页码不清空列表（见 §12 决策 Q3）。
- 加载更多失败：保留已有列表，按钮下方 `cs-feed-end` 位置换成错误文案 + 重试。
- 自动轮询只作用于 `queued/processing` 的任务；`cs-sync` 指示器（`data-state=live/paused/error`）说明当前是否在自动更新，页面隐藏 / 切换 tab 时暂停，回来时补一次。

### 4.11 「复用参数」（ReuseParams）

- 入口：任务卡片 footer 的 `btn btn-ghost btn-sm`，图标 `RotateCcw`。
- 行为：
  1. 切到对应模式（图片 → 生图，视频 → 生视频），保持当前 Key（若原 Key 已失效则保留当前 Key 并提示）。
  2. 回填模型、提示词、全部可用参数；**不回填**没有任何意义的字段（任务 ID、结果链接）。
  3. 参考图 / 首帧 / 尾帧 / 编辑原图**回填**（§12 Q6 已决定，契约 §8.2 第 4 条）：`expires_at - now ≥ 900` → 原样回填为缩略图（缩略图用快照 `url`，提交用 `upload_id`，不重新上传）；不足 15 分钟 → 该位置进入「已过期」状态（§4.5），提示重新上传；上传不可用 → 一律不回填并给字段级说明。
  4. 被回填的字段逐个加 `cs-flash`（一次性 teal 外圈闪动 1.2s，前端到时移除该类），让用户看到「哪些变了」；`prefers-reduced-motion` 下改为静态描边（样式已处理）。
  5. 模型已下架 / 参数已不被支持：能回填的回填，其余在对应字段下方给一行 `cs-field-hint`，聚焦第一个问题字段。
  6. 桌面端表单已在左侧：直接回填并滚动表单到顶部；移动端：切回「参数」tab。
  7. 成功后轻提示（`useAppStore().showToast('success', …)`）：`creationStudio.reuse.applied`；部分失败（含参考图过期）用 `creationStudio.reuse.partial`，参考图过期另用 `creationStudio.reuse.refImagesExpired`（带数量）。

---

## 5. 状态矩阵（页面级）

| 场景 | 形态 | 变量 / 类 | 文案 key（建议） |
| --- | --- | --- | --- |
| 首次进入、无任何记录 | 结果区 `cs-empty` + `empty-studio.svg`（`cs-illu`）+ 标题/说明 + 「试试这些提示词」胶囊（点击填入提示词） | `.cs-empty` `.cs-suggestion` | `creationStudio.empty.title` / `.description` / `.suggestions.*` |
| 首次进入、表单从未提交 | 表单默认值齐全，不需要空态；若缺 Key/模型走不可用态 | — | — |
| 无可用 Key | 表单整块 `cs-empty`（`setup-required.svg`）+ 「去创建 Key」 | `.cs-empty` `.cs-empty--compact` | `creationStudio.unavailable.noKey*` |
| 无可用模型（该 Key 分组） | 模型字段处 `cs-empty--compact` 或 `cs-callout--warning` + 「切换 Key」 | | `creationStudio.unavailable.noModel*` |
| 对象存储未开启 → 生图不可用 | 模式切换「生图」保留并带 `data-unavailable` 角标（§12 Q4 已决定）；切到生图后表单位置显示 `cs-callout--warning`（面向非管理员只写「请联系管理员」）+ 插画 `setup-required.svg`；**生视频仍可用** | `.cs-segmented-item[data-unavailable]` `.cs-callout--warning` | `creationStudio.unavailable.imageStorage*` |
| 对象存储未开启 → 视频首帧 / 参考图不可上传 | 该上传字段进入「不可用」（§4.5）；可选字段只禁用自身，必填字段同时禁用提交 | `.cs-dropzone[data-state=unavailable]` | `creationStudio.unavailable.uploads.*` |
| 复用参数时参考图已过期 | 对应缩略图进入「已过期」，字段下方琥珀汇总 + 「重新上传」，提交禁用直到处理完 | `.cs-thumb[data-state=expired]` `.cs-thumb-expired` `.cs-field-warning` `.cs-field-action` | `creationStudio.upload.expired*` / `creationStudio.reuse.refImagesExpired` |
| 余额不足（402 / 403） | 提交按钮上方 `cs-callout--danger`（图标 `Wallet`）+ 费用 `data-state=insufficient` + 「去充值」（仅个人钱包） | | `creationStudio.errors.balanceInsufficient`（402）/ `.balanceLow`（403）/ `.enterpriseBalance` |
| 频率 / 额度（429） | `cs-callout--warning`（图标 `Timer`），`rate_limit_exceeded` 带 `metadata.retry_after` 倒计时，到点后显示「重试」 | | `creationStudio.errors.rateLimited` / `.quotaExceeded` / `.keyQuota` / `.enterpriseQuota` |
| 模型未配置价格 | `cs-callout--warning` + 费用 `data-state=unavailable`，提交禁用 | | `creationStudio.errors.pricingMissing` |
| 其它提交失败 | 按 §7.2 映射；正文展示后端 `message`（截断到 4 行，可展开）+ 「重试」；输入不丢 | | `creationStudio.errors.*`（§7.2） |
| 列表加载中 | 4 张卡片骨架（`card` + `skeleton` 块），`aria-busy` | `.cs-media-skeleton` | 由 `common.loading` 提供 |
| 列表加载失败 | 结果区 `cs-empty--compact`（`TriangleAlert` 图标）+ 「重试」；顶部若有旧数据则保留旧数据并只显示行内错误 | | `creationStudio.feed.loadFailed` |
| 轮询中断 | `cs-sync[data-state=error]` 一行小字「自动更新已暂停，点击刷新」，不清空已有卡片 | | `creationStudio.feed.syncError` |
| 结果链接过期 | 该任务卡片转 `expired` 形态（§4.8） | | `creationStudio.task.expired*` |

**空态文案要求**：标题一句话说「现在是什么情况」，说明一句话说「下一步该做什么 / 为什么」，按钮最多 2 个（主 + 次）。空态必须带插画或图标，不允许只有一行文字。

**插画资源**（`frontend/src/assets/creation-studio/`）：

| 文件 | 用途 | 尺寸 |
| --- | --- | --- |
| `empty-studio.svg` | 首次空态（还没有作品） | 160×112，`class="cs-illu"` |
| `setup-required.svg` | 缺 Key / 缺模型 / 能力未开启 | 160×112，`cs-illu` |
| `results-expired.svg` | 结果过期（卡片内或区块级） | 160×112，`cs-illu cs-illu--sm` |

三张图都用中性灰（`#64748b` ~ `#94a3b8`）+ 一处 teal / 琥珀强调，灰阶取中段，浅色与深色卡片都能读，**不需要双份资源**。装饰性图片一律 `alt="" aria-hidden="true"`。

---

## 6. 任务状态 → 颜色 / 图标 / 文案 映射

与 `AsyncTasksView.statusClass` 保持同一套色，新增「排队」与「结果过期」两个派生状态：

| 状态 | 颜色（浅色 / 深色） | 图标（lucide） | 文案 key | aria |
| --- | --- | --- | --- | --- |
| `submitting`（本地） | `gray-100 / gray-600` · `dark-700 / gray-300` | `LoaderCircle`（旋转） | `creationStudio.task.submitting` | `aria-busy="true"` |
| `queued` | `white / blue-700` + `ring-blue-200` · 同色系 `blue-300` + `ring-blue-800` | `Clock` | `creationStudio.task.queued` | 状态文本进 live region |
| `processing` | `blue-50 / blue-700` · `blue-950 / blue-300` | `LoaderCircle`（旋转） | `creationStudio.task.processing` | 同上；进度条带 `role="progressbar"` |
| `completed` | `green-50 / green-700` · `green-950 / green-300` | `CircleCheck` | `creationStudio.task.completed` | 完成时在 live region 播报一次 |
| `failed` | `red-50 / red-700` · `red-950 / red-300` | `CircleX` | `creationStudio.task.failed` | 错误信息关联 `aria-describedby` |
| `cancelled` | `gray-100 / gray-600` · `dark-700 / gray-400` | `Ban` | `creationStudio.task.cancelled` | — |
| `expired`（派生） | `amber-50 / amber-700` · `amber-950/60 / amber-300` | `Clock` + `Link2Off` 角标 | `creationStudio.task.expired` | — |

规则：

- **颜色不是唯一信息**：每种状态都同时给图标 + 文本，不依赖色觉。
- 状态徽标文字始终是本地化短词（2–4 字），不加 emoji。
- 排队 vs 处理中同属蓝色系，用「描边 vs 实底」和「时钟 vs 旋转」区分，不引入第三种蓝。
- 状态变化时卡片不闪烁、不位移：只更新徽标与主体区域，`cs-enter` 只用于新卡片。

---

## 7. 错误码与提示条（Callout）

`cs-callout` 四种色调按语义选，不按 HTTP 状态机械映射：

| 色调 | 何时用 | 图标 |
| --- | --- | --- |
| `--info` | 中性说明（例如「生成通常需要 30–90 秒」「结果保留 24 小时」） | `Info` |
| `--warning` | 可自愈或需用户决策（429、未开对象存储、价格缺失、模型下架、结果即将过期） | `TriangleAlert` |
| `--danger` | 提交/加载失败（402、403、400、5xx、网络） | `CircleX` / `Wallet` |
| `--neutral` | 只读/暂停状态说明 | `Info` |

结构：图标 + `cs-callout-title`（一句话）+ `cs-callout-body`（原因与下一步，最多 2 行，超出可展开）+ `cs-callout-actions`（最多 2 个操作，文字链接样式 `cs-callout-link`）。

后端错误文案处理：优先展示后端返回的 `message`；映射不到或为空时用本地兜底文案 + 保留「复制错误信息」入口（`useClipboard`），方便用户反馈。

前端**只按 `reason` 分支**（契约 §1.2）。下文「文案 key」一律是 `creationStudio.errors.*`，名字取自契约 §6.1，不另起名字；`unavailable.*` 是契约指定的不可用态。

展示位置代号：

| 代号 | 位置 | 形态 |
| --- | --- | --- |
| **F** | 上传字段下方 | `cs-field-error`（红，用户需要换文件）或 `cs-field-warning`（琥珀，需要重新上传 / 已被移除） |
| **T** | 具体那张缩略图 | `cs-thumb[data-state=error]`（可重试）/ `cs-thumb[data-state=expired]` |
| **U** | 上传字段整体 | `cs-dropzone[data-state=unavailable]`（§4.5「不可用」） |
| **S** | 提交按钮上方 | `cs-callout`（色调见各行），标题 = 文案 key，正文 = 后端 `message`（英文原文也照常展示） |
| **C** | 费用区 | `cs-cost[data-state=…]` |
| **P** | 页面 / 表单级 | 表单整块不可用态、模式切换 `data-unavailable` 角标（§5） |

### 7.1 上传相关（`POST /creation/uploads` 与带图提交）

| 来源 | HTTP / reason | 位置 | 形态与行为 | 文案 key |
| --- | --- | --- | --- | --- |
| options | `uploads.available=false`，`reason=storage_disabled` | **U** | 图片字段不可用；多个图片字段合并为一个框（§4.5） | `unavailable.uploads.title` / `.description` / `.required` |
| options | `uploads.reason=key_unavailable` | **P** | Key 不可用的表单级状态，上传字段不渲染 | `errors.keyUnavailable` |
| 前端预检 | 超过 `upload_limits.max_bytes` | **F** 红 | 不发请求、不生成缩略图；上传框 `data-invalid` 3 秒 | `errors.uploadTooLarge`（`{max}` = `max_bytes` 换算成 MB） |
| 前端预检 | 类型不在 `upload_limits.accept` | **F** 红 | 同上 | `errors.uploadUnsupportedType`（`{types}` = accept 转成「PNG、JPEG、WebP」） |
| 上传 | 413 `CREATION_UPLOAD_TOO_LARGE` | **F** 红 | 移除该文件的占位缩略图 | `errors.uploadTooLarge`（`{max}` 优先取 `metadata.max_bytes`） |
| 上传 | 415 `CREATION_UPLOAD_UNSUPPORTED_TYPE` | **F** 红 | 同上（后端按文件内容嗅探，改扩展名也会被拒） | `errors.uploadUnsupportedType` |
| 上传 | 422 `CREATION_UPLOAD_INVALID_IMAGE` | **F** 红 | 同上；说明「无法识别，或边长需在 16–`{maxSide}` 像素、总像素不超过 `{maxPixels}`」 | `errors.uploadInvalidImage`（`{maxSide}` 取 `metadata.max_side_px`，`{maxPixels}` 取 `upload_limits.max_pixels`） |
| 上传 | 429 `CREATION_UPLOAD_LIMIT_EXCEEDED` | **F** 红 | 已有缩略图保留；上传框 `aria-disabled`（本页会话内） | `errors.uploadLimit` |
| 上传 | 502 `CREATION_UPLOAD_STORAGE_FAILED` | **T** | 缩略图 `data-state=error`，`button.cs-thumb-error` 原位重试；提交禁用 | `errors.uploadFailed` |
| 上传 | 网络中断 / 超时（无响应体） | **T** | 同上 | `errors.uploadFailed` |
| 上传 | 400 `INVALID_CREATION_REQUEST` | **F** 红 | 正常不会出现（缺 `file` 字段等），按通用失败展示 | `errors.invalidRequest` |
| 上传 / 提交 | 404 `CREATION_STORAGE_DISABLED` | **U**（+ **S** warning，仅提交时） | options 已过时：重新拉 options → 进入 U；表单里已有缩略图全部移除（F 琥珀 `upload.removedUnavailable`）。提交时额外给 S：说明参考图已移除，不带图可直接重新提交 | `unavailable.uploads.*` |
| 提交 | 404 `CREATION_UPLOAD_NOT_FOUND` | **T** + **F** 琥珀 + **S** warning | 缩略图转「已过期」（判定规则见 §4.5）；S 带操作「定位到参考图」（滚动并聚焦第一张过期图；移动端在「参数」标签内） | `errors.uploadExpired`（S）、`upload.expiredHint`（F） |
| 提交 | 410 `CREATION_UPLOAD_EXPIRED` | 同上 | 同上 | `errors.uploadExpired` |
| 提交 | 413 `CREATION_BODY_TOO_LARGE` | **S** danger | 正常不会出现（提示词上限 8000 字），按通用失败展示 | `errors.bodyTooLarge` |

字段内错误（F 红）的文案格式：每个文件一行 `creationStudio.upload.fileError`（`{name}：{reason}`，`{reason}` 为上表 errors.* 的译文），最多显示 3 行，其余合并为 `creationStudio.upload.moreFileErrors`（`{count}`）。新的一批文件开始上传时清空旧的字段错误。字段错误通过 `aria-describedby` 关联上传框，并由 live region 播报一次。

### 7.2 其余 reason → 展示位置

| HTTP / reason | 位置 | 色调 / 行为 | 文案 key |
| --- | --- | --- | --- |
| 400 `INVALID_CREATION_REQUEST` | S | danger；正文展示 `message`（常见于网关参数规范化拒绝） | `errors.invalidRequest` |
| 400 `CREATION_MODEL_NOT_AVAILABLE` | S + P | warning；重新拉 options，模型选择器回落 | `errors.modelUnavailable` |
| 400 `video_pricing_not_configured` | S + C | warning；费用 `data-state=unavailable`，提交禁用 | `errors.pricingMissing` |
| 402 `insufficient_balance` | S + C | danger（`Wallet`）；费用 `data-state=insufficient`；个人钱包给「去充值」 | `errors.balanceInsufficient` |
| 403 `INSUFFICIENT_BALANCE` | S + C | 同上 | `errors.balanceLow` |
| 403 `ENTERPRISE_BALANCE_INSUFFICIENT` | S | danger；不给充值链接，提示联系企业管理员 | `errors.enterpriseBalance` |
| 403 `IMAGE_GENERATION_FORBIDDEN` | S + P | warning；生图选项标不可用 | `errors.imageForbidden` |
| 403 `GROUP_DISABLED` / `GROUP_DELETED` / `GROUP_NOT_ALLOWED` | S | warning；操作「切换 Key」 | `errors.groupUnavailable` |
| 403 `ENTERPRISE_DISABLED` / `ENTERPRISE_MEMBER_DISABLED` / `ENTERPRISE_GROUP_NOT_ALLOWED` | S | danger | `errors.enterpriseBlocked` |
| 403 `API_KEY_EXPIRED` / `API_KEY_DISABLED` | S | warning；重新拉 Key 列表，操作「切换 Key」 | `errors.keyUnavailable` |
| 403 `SUBSCRIPTION_NOT_FOUND` | S | warning；操作跳转订阅页 | `errors.subscriptionMissing` |
| 403 `ACCESS_DENIED` | S | warning | `errors.ipRestricted` |
| 404 `CREATION_KEY_NOT_FOUND` | S | warning；重新拉 Key 列表 | `errors.keyNotFound` |
| 404 `ASYNC_IMAGE_DISABLED` | P | 不出 callout；生图进入不可用态 | `unavailable.imageStorage.*` |
| 429 `rate_limit_exceeded` | S | warning（`Timer`）；按 `metadata.retry_after` 倒计时，结束后显示「重试」 | `errors.rateLimited` |
| 429 `USAGE_LIMIT_EXCEEDED` / `VIDEO_RESERVATION_QUOTA_EXCEEDED` | S | warning；有 `metadata.window_resets_at` 时显示重置时间 | `errors.quotaExceeded` |
| 429 `API_KEY_QUOTA_EXHAUSTED` | S | warning；操作「切换 Key」 | `errors.keyQuota` |
| 429 `DEPARTMENT_QUOTA_EXCEEDED` / `MEMBER_QUOTA_EXCEEDED` / 其它企业 429 | S | warning | `errors.enterpriseQuota` |
| 502 `upstream_error` | S | danger；正文展示 `message` | `errors.upstream` |
| 503 `*_no_eligible_account` | S | warning | `errors.noCapacity` |
| 503 `billing_service_error` | S | danger；「重试」 | `errors.billingUnavailable` |
| 503 `CREATION_ESTIMATE_UNAVAILABLE`（估算接口） | C | 不出 callout；费用显示「暂无法估算」并隐藏余额行，**不禁用提交**（契约 §4.2：只有 `priced=false` / `sufficient=false` 才禁用） | `errors.billingUnavailable` |
| 503 `CREATION_DISPATCHER_UNAVAILABLE` / 500 `CREATION_INTERNAL_ERROR` | S | danger | `errors.internal` |
| 未列出的 reason（契约 §6.2 第 7 条会透传原 code） | S | 4xx 按 `errors.invalidRequest`、5xx 按 `errors.upstream`，正文展示 `message` | 同左 |

估算接口（`/creation/estimate`）的 400 / 404 只用于静默刷新 options / Key 列表，不在 S 位置弹 callout；真正的阻断以提交结果为准。

---

## 8. 交互与反馈细则

| 交互 | 规则 |
| --- | --- |
| 提交 | 一次只允许一个提交在途；提交中按钮 `aria-busy`、禁用；成功后清空「提交策略」由用户决定（默认保留提示词与参数，方便连续出图） |
| 轮询 | 间隔参考异步任务页（10s），页面隐藏时暂停，`document.hidden` 恢复时补拉；单次最多并发 3 个上游刷新（与现有实现一致） |
| 自动更新开关 | 页头开关，默认开；关闭后 `cs-sync[data-state=paused]` 显示「已暂停自动更新」 |
| 复制链接 | 复制成功后按钮 1.5s 绿色对勾 + `sr-only` 播报；失败时 toast 错误 |
| 下载 | 走 blob 下载（参考 `AsyncTasksView.download`），文件名用「类型-任务ID」；下载中按钮禁用 + spinner |
| 新窗口打开 | `target="_blank" rel="noopener noreferrer"`；URL 必须经协议白名单校验（`http(s):` / 站内相对路径），与现有 `safeURL` 一致 |
| 结果即将过期 | 剩余 < 2 小时时，卡片 footers 出现 `cs-param-pill` 样式的琥珀提示「2 小时后过期」+ 下载引导 |
| 键盘 | `Tab` 顺序：模式切换 → Key → 模型 → 提示词 → 参数 → 提交 → 结果区；灯箱内焦点受限，`Esc` 退出 |
| 减少动态 | 所有骨架扫光、脉冲点、进度条动画、入场动画在 `prefers-reduced-motion` 下关闭（样式内已处理） |

---

## 9. 暗色模式

- 全部按现有写法：`dark:` 变体 + `html.dark` 下的 CSS 变量（`--cs-*`），不引入新的主题机制。
- 卡片：`bg-white dark:bg-dark-800/50`（沿用 `.card`）；媒体底 `--cs-media-bg` 浅 `gray-100` / 深 `dark-900`，保证黑边视频在两种模式下都看得清边界。
- 图片不带白色描边；玻璃/模糊只用在 AppHeader、视图切换条、表单底部栏，结果单元不模糊（避免图片发灰）。
- 悬浮操作条在两种模式下都用黑色半透明遮罩 + 白色图标（与图片内容无关，始终可读）。
- 选中态（teal）在深色下降低透明度到 `/30` 并提亮文字到 `primary-300`，保证 4.5:1。

---

## 10. 无障碍（A11y）

| 项 | 要求 |
| --- | --- |
| 焦点可见 | 所有可交互元素键盘聚焦时有 `ring-2 ring-primary-500/50`（深色下 `ring-offset-dark-900`）；不要 `outline: none` 无替代 |
| 对比度 | 正文 ≥ 4.5:1；次要文字（`gray-500 / dark-400`）≥ 4.5:1；状态徽标与图标 ≥ 3:1；焦点环 ≥ 3:1 |
| 触控目标 | ≥ 40×40（`pointer: coarse` 下图标按钮升到 40，步进器按钮 44 宽） |
| 语义 | 模式切换 / 移动端视图切换 / 历史筛选用 `role="tablist"` + `aria-selected`，筛选也可用 `radiogroup`；参数胶囊用原生 `radio`（视觉隐藏，`label` 包裹），保证方向键与读屏原生行为 |
| 表单 | 每个字段有 `<label for>`；错误用 `aria-invalid` + `aria-describedby` 指向 `cs-field-error`；必填 `required` / `aria-required` |
| 进度 | 确定进度：`role="progressbar" aria-valuemin/max/now`；不确定进度：`role="progressbar"` 不给 `aria-valuenow`，并配文字「处理中」 |
| 播报 | 页面内一个 `aria-live="polite"` 区域，仅在**状态变化**时播报一句话（「任务已完成」「任务失败：xxx」「余额不足，无法提交」）；轮询心跳不播报 |
| 键盘快捷键 | `⌘/Ctrl+Enter` 提交必须同时有可见的按钮路径；`aria-keyshortcuts` 标注 |
| 图片 | 生成结果 `alt` 用「模式 + 模型 + 序号」（如「生图结果 2，模型 xxx」）；装饰图 `alt=""` |
| 灯箱 | `role="dialog" aria-modal="true"`，`aria-label` 为「预览 2 / 8」；打开时锁 body 滚动，关闭恢复焦点 |
| 语言 | 文档和 aria-label 都走 i18n，不硬编码中文 |

---

## 11. 资源与接入

### 11.1 设计 token / 类名清单

**CSS 变量**（`creation-studio.css` 的 `@layer base`）

| 变量 | 浅色 | 深色 | 用途 |
| --- | --- | --- | --- |
| `--cs-form-col` | 断点决定 | 同 | 表单列宽 |
| `--cs-sticky-top` | 5rem → 6rem | 同 | 表单吸顶偏移（AppHeader 高 + 内边距） |
| `--cs-tile-min` | 9rem（移动 7rem） | 同 | 结果网格兜底排布的单元最小宽 |
| `--cs-tile-max-h` | 32rem（移动 26rem） | 同 | 单张结果限高（定义在 `.cs-media-grid` 上） |
| `--cs-tile-radius` | .75rem | 同 | 单元圆角 |
| `--cs-media-bg` | gray-100 | dark-900 | 媒体底 |
| `--cs-checker-a/b` | white / gray-200 | dark-800 / dark-700 | 透明图棋盘格 |
| `--cs-overlay` | 黑 55% | 黑 65% | 悬浮条渐变 |
| `--cs-shimmer` | 白 65% | 白 7% | 骨架扫光 |

**局部变量**（元素内联）：`--cs-progress`（进度）、`--cs-aspect-w` / `--cs-aspect-h`（媒体比例，数字，写在 `.cs-media-grid` 上由单元继承）、`--cs-aspect`（单元级比例覆盖，如 `3 / 2`）、`--cs-ratio-w/h`（比例示意框）、`--cs-task-accent`（卡片状态色条，由 `data-status` 自动设置）。

**类名分组**（全部 `cs-` 前缀，模板工具类可覆盖）

- 骨架：`cs-page` `cs-header` `cs-title` `cs-description` `cs-layout` `cs-view-switch-bar`
- 分段：`cs-segmented` `cs-segmented-item`（`[data-unavailable]`）`cs-segmented-badge` `cs-mode-switch`
- 表单：`cs-form-panel` `cs-form-body` `cs-form-footer` `cs-group` `cs-group-title` `cs-disclosure`
- 字段：`cs-field` `cs-field-head` `cs-field-label` `cs-field-optional` `cs-field-hint` `cs-field-error` `cs-field-warning` `cs-field-action` `cs-field-counter` `cs-prompt` `cs-prompt-footer` `cs-kbd`
- 下拉项：`cs-option` `cs-option-main` `cs-option-name` `cs-option-sub` `cs-option-aside` `cs-option-reason`
- 参数：`cs-chip-group` `cs-chip-group--grid` `cs-chip-input` `cs-chip` `cs-chip-stack` `cs-chip-sub` `cs-ratio-glyph` `cs-stepper` `cs-stepper-btn` `cs-stepper-input` `cs-stepper-unit`
- 上传：`cs-dropzone`（`[data-dragover]` `[data-invalid]` `[data-state=unavailable]` `[aria-disabled]`）`cs-dropzone--tile` `cs-dropzone-icon` `cs-dropzone-title` `cs-dropzone-hint` `cs-thumb-list` `cs-thumb`（`[data-state=error|expired]`）`cs-thumb-label` `cs-thumb-remove` `cs-thumb-progress` `cs-thumb-error` `cs-thumb-error-text` `cs-thumb-expired` `cs-thumb-expired-text`
- 费用/提交：`cs-cost` `cs-cost-label` `cs-cost-value` `cs-cost-detail` `cs-balance` `cs-submit` `cs-submit-reason`
- 提示条：`cs-callout` `cs-callout--info` `cs-callout--warning` `cs-callout--danger` `cs-callout--neutral` `cs-callout-icon` `cs-callout-content` `cs-callout-title` `cs-callout-body` `cs-callout-actions` `cs-callout-link`
- 空态：`cs-empty` `cs-empty--compact` `cs-illu` `cs-illu--sm` `cs-empty-title` `cs-empty-desc` `cs-empty-actions` `cs-suggestion`
- 结果列：`cs-results` `cs-section` `cs-section-head` `cs-section-title` `cs-section-count` `cs-sync` `cs-sync-dot`
- 任务卡片：`cs-task`（`[data-status]`）`cs-task-header` `cs-task-kind` `cs-task-title` `cs-task-meta` `cs-dot-sep` `cs-task-prompt` `cs-task-params` `cs-param-pill` `cs-task-body` `cs-task-error` `cs-task-footer` `cs-task-actions`
- 状态：`cs-status` `cs-status--submitting` `cs-status--queued` `cs-status--processing` `cs-status--completed` `cs-status--failed` `cs-status--cancelled` `cs-status--expired`
- 进度：`cs-progress`（`[data-indeterminate]`）`cs-progress-bar` `cs-progress-meta`
- 媒体：`cs-media-grid`（`[data-count]`）`cs-media-tile`（`[data-fit]`）`cs-media-img` `cs-checker` `cs-media-open` `cs-media-index` `cs-media-tag` `cs-media-overlay` `cs-media-action` `cs-media-skeleton` `cs-media-placeholder` `cs-video` `cs-video-actions`
- 编辑对照：`cs-compare` `cs-compare-body` `cs-compare-source` `cs-compare-arrow` `cs-compare-results` `cs-compare-label`
- 通用：`cs-icon-btn`（`[data-state=done]`）
- 灯箱：`cs-lightbox-overlay` `cs-lightbox` `cs-lightbox-toolbar` `cs-lightbox-counter` `cs-lightbox-tag` `cs-lightbox-btn` `cs-lightbox-btn--icon` `cs-lightbox-stage` `cs-lightbox-nav` `cs-lightbox-nav--prev` `cs-lightbox-nav--next` `cs-lightbox-caption`
- 历史：`cs-feed` `cs-feed-divider` `cs-load-more` `cs-feed-end`
- 动效：`cs-enter` `cs-flash`

### 11.2 可直接复用的现有组件

| 需求 | 组件 | 备注 |
| --- | --- | --- |
| 卡片 / 按钮 / 输入 / 徽标 / 分隔线 / Tabs / 进度 | `style.css` 的 `.card .btn .btn-primary .btn-secondary .btn-ghost .btn-sm .input .input-error .badge .divider .skeleton .spinner .progress` | 创作页主视觉直接用这套 |
| 下拉选择 | `@/components/common/Select.vue` | 支持搜索、远程搜索、`loading`、`disabled`、`error`、`clearable`；选项插槽 |
| 多行提示词 | `@/components/common/TextArea.vue` | label/error/hint/rows 已内置 |
| 空态 | `@/components/common/EmptyState.vue` | 自带 icon 槽；但创作页空态需要插画 + 多个按钮，建议用 `cs-empty` 自建 |
| 分页 | `@/components/common/Pagination.vue` | 历史用「加载更多」时不需要；若 spec 最终选页码式可直接用 |
| 加载 / 骨架 | `LoadingSpinner.vue` `Skeleton.vue` | |
| 状态点 | `StatusBadge.vue` | 只有「点 + 文字」，任务状态需要带背景色的徽标 → 用 `cs-status` |
| 图片上传 | `ImageUpload.vue` | **只支持单张、无拖拽、无缩略图网格**，参考图/首帧建议新建 `ImageDropzone`（见 11.3） |
| 弹窗 | `BaseDialog.vue` / `ui/dialog` | 灯箱建议用 `ui/dialog`（reka-ui，自带焦点陷阱） |
| 图标 | `@lucide/vue` | `ImageIcon` `Film` `Clock` `LoaderCircle` `CircleCheck` `CircleX` `Ban` `Wallet` `Timer` `TriangleAlert` `Link2Off` `Download` `Copy` `ExternalLink` `RotateCcw` `Sparkles` `Upload` `X` `ChevronLeft/Right` `Info` `KeyRound` `CloudOff` `Maximize2` `Play` 均已确认存在 |
| 复制 / 提示 | `useClipboard()`（`@/composables/useClipboard`）、`useAppStore().showToast(type, message)`（`@/stores/app`） | |
| 设置开关 | `common/Toggle.vue` | 页头「自动更新」 |
| 模型图标 | `common/ModelIcon.vue`、`PlatformIcon.vue` | 模型下拉、任务卡片 |

### 11.3 需要新建的组件（结构 + 状态，交给 frontend 实现）

组件名以契约 §10.3 / tasks F13 为准（本规范早期稿里的 RefImageField / NumberStepper / SubmitBar / VideoResult 分别对应 `ImageDropzone` / `IntegerStepper` / `SubmitAction` / `VideoPlayer`）。

| 组件 | 结构要点 | 必须覆盖的状态 |
| --- | --- | --- |
| `CreationStudioView`（页面壳） | 页头 + 模式切换 + `cs-layout`；移动端视图切换；组合下面所有块 | 首屏加载、无 Key、无模型、生图不可用 |
| `ParamField`（描述符渲染器） | 按 `kind` 分发到 ChipGroup / IntegerStepper / Select / RatioGroup / ImageDropzone | 默认、选中、禁用+原因、错误、依赖联动 |
| `ChipGroup` / `RatioGroup` | 原生 `radio` + `cs-chip`（可加副文案 `cs-chip-sub`、比例示意 `cs-ratio-glyph`） | 默认/悬停/聚焦/选中/禁用 |
| `IntegerStepper` | −/输入/＋ + 单位，输入非法时 `data-invalid` | 默认/聚焦/越界/禁用 |
| `ImageDropzone` | `cs-dropzone` + `cs-thumb-list`，先传对象存储再回链接；支持拖拽、多图、首帧标签、上传进度、单张删除、过期图原位替换、失败图原位重试 | 空/拖拽/上传中/成功/可重试失败/不可重试失败（字段内错误）/上传次数用完/达到上限/**已过期需重传**/**不可用（合并多字段，区分可选与必填）**（§4.5、§7.1） |
| `CostSummary` | 费用 + 明细 + 余额 | 计算中/可用/价格缺失/余额不足/估算服务不可用 |
| `SubmitAction` | 提交按钮 + 阻塞原因；移动端 sticky | 默认/悬停/聚焦/提交中/禁用+原因（参考图上传中、上传失败、已过期、必填参考图不可用）/失败 |
| `TaskCard` | §4.8 的结构，`data-status` 驱动主体；编辑任务内含 §4.9「原图 → 结果」对照 | 7 种状态 + 提示词展开 + 结果链接过期 + 编辑对照（原图可用/原图过期/无快照） |
| `MediaGrid` / `VideoPlayer` | `data-count` 1/2/3/4 固定排布（§1.4）；骨架数量与比例来自请求参数；单元操作条；视频 blob 播放 | 骨架/加载失败/过期/多图角标/复制成功 |
| `Lightbox` | 全屏层 + 工具栏 + 键盘/滑动切换；编辑任务序列为 `[原图, 结果…]` | 打开/切换/禁用首尾方向/原图标签/关闭回焦 |
| `HistoryFeed` | 筛选 + 卡片流 + 加载更多 | 加载中/空/失败/加载更多中/到底 |
| `Callout` | §7 结构 | 4 种色调 × 可选操作 × 倒计时（429） |

### 11.4 入口引入（交给 frontend 角色）

`src/styles/creation-studio.css` **必须**在 `src/style.css` 里引入才能用 `@layer` + `@apply`（单独在 `.vue` 里 `import` 会因缺少 `@tailwind` 指令而失败）：

```css
/* src/style.css，第 1 行之后 */
@import './styles/shadcn.css';
@import './styles/creation-studio.css';
```

Vite 会先把 `@import` 内联，再交给 Tailwind 处理；该文件在页面未使用任何 `cs-*` 类时会被 purge（关键帧始终保留，体积可忽略）。不要改 `main.ts`。

插画按 URL 引入（与 `assets/icons/*.svg` 一致）：

```ts
import emptyStudio from '@/assets/creation-studio/empty-studio.svg'
```

### 11.5 i18n key 建议（扁平表）

| 分组 | key |
| --- | --- |
| 页面 | `creationStudio.title` `creationStudio.description` |
| 模式 | `creationStudio.mode.image` `creationStudio.mode.video` |
| 视图切换 | `creationStudio.view.form` `creationStudio.view.results` `creationStudio.view.inProgressCount` |
| 表单 | `creationStudio.form.apiKey` `creationStudio.form.model` `creationStudio.form.prompt` `creationStudio.form.promptPlaceholder` `creationStudio.form.promptCount` `creationStudio.form.advanced` `creationStudio.form.optional` `creationStudio.form.required` |
| 参数 | 描述符 label 固定为 `creationStudio.params.<key>`（契约 §3.2），v1 的 key 全集：`creationStudio.params.size` `.quality` `.n` `.aspect_ratio` `.resolution` `.duration` `.ratio` `.watermark` `.source` `.first_frame` `.last_frame` `.reference`；单位 `creationStudio.units.seconds`；上传框辅助文案 `creationStudio.upload.dropzone` `creationStudio.upload.dragHint` `creationStudio.upload.limitHint`（`{types}` `{max}`）`creationStudio.upload.maxFiles`（`{count}`） |
| 上传 | `creationStudio.upload.uploading` `creationStudio.upload.retry`（失败缩略图上的「重试」）`creationStudio.upload.retryAria`（`{index}`）`creationStudio.upload.expired`（缩略图上的短词「已过期」）`creationStudio.upload.expiredAria`（`{index}`：「第 {index} 张参考图已过期，点击重新上传」）`creationStudio.upload.expiredHint`（`{count}`：「{count} 张参考图已过期，需要重新上传」）`creationStudio.upload.reupload`（「重新上传」）`creationStudio.upload.locate`（「定位到参考图」）`creationStudio.upload.fileError`（`{name}` `{reason}`）`creationStudio.upload.moreFileErrors`（`{count}`）`creationStudio.upload.removedUnavailable`（「站点暂不支持上传，已移除参考图」）`creationStudio.upload.notRestored`（「参考图未带入：站点暂不支持上传」） |
| 编辑对照 | `creationStudio.compare.source`（「原图」）`creationStudio.compare.sourceMeta`（`{w}` `{h}`：「原图 · {w}×{h}」）`creationStudio.compare.results`（`{count}`：「结果 · {count} 张」）`creationStudio.compare.sourceAlt`（「编辑前的原图」）`creationStudio.compare.openSource`（「查看原图」）`creationStudio.compare.sourceExpired`（「原图链接已过期」） |
| 费用 | `creationStudio.cost.estimate` `creationStudio.cost.perUnit` `creationStudio.cost.balance` `creationStudio.cost.calculating` `creationStudio.cost.unavailable` |
| 提交 | `creationStudio.submit.image` `creationStudio.submit.video` `creationStudio.submit.submitting` `creationStudio.submit.shortcutHint` `creationStudio.submit.blocked.refImageUploading` `creationStudio.submit.blocked.refImageFailed` `creationStudio.submit.blocked.refImageExpired` `creationStudio.submit.blocked.refImageUnavailable` |
| 任务 | `creationStudio.task.submitting` `.queued` `.processing` `.completed` `.failed` `.cancelled` `.expired` `.elapsed` `.queuePosition` `.expiredHint` `.cancelledHint` `.retentionWarning` |
| 操作 | `creationStudio.action.reuse` `creationStudio.action.download` `creationStudio.action.copyLink` `creationStudio.action.copied` `creationStudio.action.openInNewTab` `creationStudio.action.preview` `creationStudio.action.previewCount` `creationStudio.action.retry` `creationStudio.action.openTaskList` `creationStudio.action.goRecharge` `creationStudio.action.createKey` `creationStudio.action.switchKey` |
| 结果列 | `creationStudio.feed.title` `creationStudio.feed.inProgress` `creationStudio.feed.recent` `creationStudio.feed.filterAll` `creationStudio.feed.loadMore` `creationStudio.feed.noMore` `creationStudio.feed.loadFailed` `creationStudio.feed.syncLive` `creationStudio.feed.syncPaused` `creationStudio.feed.syncError` |
| 空态 / 不可用 | `creationStudio.empty.title` `creationStudio.empty.description` `creationStudio.empty.suggestions` `creationStudio.unavailable.noKey.title` `.description` `creationStudio.unavailable.noModel.title` `.description` `creationStudio.unavailable.imageStorage.title` `.description` `.contactAdmin` `creationStudio.unavailable.uploads.title`（「暂不能上传参考图」）`.description`（「站点未开启对象存储，暂不能上传参考图；不带图仍可生成」）`.required`（「该模型需要参考图，站点开启对象存储后才能使用」）（`unavailable.imageStorage` / `unavailable.uploads` 两个名字由契约 §6.1 指定） |
| 错误（`errors.*`，**以契约 §6.1 为准，不另起名字**） | `creationStudio.errors.invalidRequest` `.modelUnavailable` `.pricingMissing` `.balanceInsufficient` `.balanceLow` `.enterpriseBalance` `.imageForbidden` `.groupUnavailable` `.enterpriseBlocked` `.keyUnavailable` `.subscriptionMissing` `.ipRestricted` `.keyNotFound` `.uploadExpired` `.uploadTooLarge` `.bodyTooLarge` `.uploadUnsupportedType` `.uploadInvalidImage` `.rateLimited` `.quotaExceeded` `.keyQuota` `.enterpriseQuota` `.uploadLimit` `.upstream` `.uploadFailed` `.noCapacity` `.billingUnavailable` `.internal`（共 28 个；每个 reason 的展示位置见 §7.1 / §7.2） |
| 非 reason 的前端失败 | `creationStudio.feed.loadFailed`（列表加载失败）`creationStudio.action.copyFailed` `creationStudio.action.downloadFailed`（不放进 `errors.*`，`errors.*` 只对应契约 reason 码） |
| 复用 | `creationStudio.reuse.applied` `creationStudio.reuse.partial` `creationStudio.reuse.modelUnavailable` `creationStudio.reuse.keyMissing` `creationStudio.reuse.refImagesExpired`（`{count}`） |
| 播报 | `creationStudio.live.taskCompleted` `creationStudio.live.taskFailed` `creationStudio.live.balanceInsufficient` `creationStudio.live.copied` `creationStudio.live.refImageExpired` |
| 灯箱 | `creationStudio.lightbox.preview` `creationStudio.lightbox.prev` `creationStudio.lightbox.next` `creationStudio.lightbox.close` |

---

## 12. 设计决策记录

2026-11-07 用户拍板：Q1、Q2、Q4、Q5 采纳推荐，Q6 改为回填参考图；Q3、Q7、Q8 随「其余交互默认值全部采纳」按推荐执行。

| # | 问题 | 选项 | 结论 | 状态 |
| --- | --- | --- | --- | --- |
| Q1 | 历史列表是否跟随顶部模式切换（生图模式下只看图片） | A 跟随（列表随模式过滤，切换即换列表）· B 不跟随，只有手动筛选 | **B**：结果列始终展示全部类型，自带 `全部 / 图片 / 视频` 筛选；正在跑的视频任务不会因为切到生图而被藏起来 | 已决定 |
| Q2 | 移动端参数与结果的关系 | A 标签切换 · B 上下堆叠整页滚动 · C 底部抽屉弹出参数 | **A**：「参数 / 结果」标签切换 + 吸底提交栏，提交成功自动切到结果（§2）；C 留作二期 | 已决定 |
| Q3 | 历史记录的分页 | A 「加载更多」追加 · B 页码分页 | **A**：创作页是可滚动的流，轮询会插入新卡片，页码会跳动 | 已决定（按推荐） |
| Q4 | 对象存储未开启时「生图」入口 | A 隐藏生图选项 · B 保留但标不可用并给出原因 | **B**：保留选项 + `data-unavailable` 角标，切进去显示原因（§5）；同理视频首帧 / 参考图字段显示「不可用」而不是消失（§4.5） | 已决定 |
| Q5 | 进度展示 | A 百分比进度条 · B 不确定进度条 + 已用时长 · C 只给「排队第 N 位」 | **B**；后端将来给出真实百分比时直接切到 A（`--cs-progress` 已支持） | 已决定 |
| Q6 | 「复用参数」是否带回参考图 / 首帧 / 编辑原图 | A 不回填 · B 回填 | **B**：参考图先上传到对象存储再传链接，链接未过期就原样回填；过期则该图进入「已过期，需要重新上传」状态并阻断提交（§4.5、§4.11） | 已决定（改为 B） |
| Q7 | 用户能否取消进行中的任务 | A 一期不提供 · B 仅对明确支持取消的协议开放 | **A**：上游取消能力参差不齐；`cancelled` 状态只来自服务端 | 已决定（按推荐） |
| Q8 | 提交成功后表单是否清空 | A 保留提示词与参数 · B 清空提示词 | **A**：便于连续生成；另给一个「清空」次级按钮 | 已决定（按推荐） |

---

## 13. 文案语气

- 简体中文界面短句、动宾结构；技术错误原文放进 callout 正文，不替换成「出错了」。
- 不在文案里承诺时间精确值（「约 1 分钟」可以，「60 秒内」不行）。
- 面向非管理员用户，提到对象存储 / 分组 / 配额时只写「当前 Key 无法生图，请联系管理员或更换 Key」，不暴露后台配置细节。
- 英文本地化按现有 `en` 目录风格，句子首字母大写、不加句号结尾（短句）。
