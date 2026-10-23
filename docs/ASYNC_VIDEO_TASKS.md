# 异步任务与视频平台

用户侧新增「异步任务」页面（`/async-tasks`），按任务 ID／模型搜索，并按图片、视频和处理状态筛选。页面仅显示当前登录用户的任务，默认每 10 秒刷新可见页；视频查询会请求原上游，图片任务由原有执行器更新。后台对账器独立于页面，每 15 秒扫描到期的持久预占任务；使用提交时绑定账号查询，有界并发为 4，数据库租约避免多实例重复领取。每次未观察到终态或查询失败后退避，首次为 30 秒，随后为 60／120 秒等，最长 30 分钟。列表记录保存于 Redis，通常保留 24 小时，每位用户最多索引 1000 条，结果链接有效期由上游或对象存储决定。

菜单入口：普通用户在左侧「使用记录」下方；管理员在左侧「我的账户 → 异步任务」。简易模式中，入口位于「API 密钥」下方，管理员仍可直接访问。

管理端另有「全局异步任务」入口，位于管理菜单「使用记录」下方，路径 `/admin/async-tasks`，普通用户无法访问。页面按创建时间倒序展示保留期内所有用户的图片与视频任务，支持用户 ID、API Key ID、分组 ID、任务类型、状态、协议和任务 ID／模型搜索，并提供任务详情、视频状态刷新和下载。列表显示所属用户、Key 名称和分组，已删除的关联数据保留 ID。管理员可查看原始结果链接，用户端仍按账号隐私开关输出。

全局索引随任务写入更新，首次管理端查询自动分批索引此前已保留的任务，不延长记录有效期。索引包含仍在保留期内但已被用户列表 1000 条上限移出的记录；每页最多 100 条，不把所有记录一次载入内存。自动刷新只查询当前页处理中视频，沿用原用户／Key 的账号绑定和去重计费，不创建新任务。账号连接测试仍不计入任务目录。

目录包含新增的异步图片任务，以及系统提交或查询过的 Seedance、Gemini、MiniMax 和 Grok 视频任务。已有任务不会批量回填；批量生图保留原有入口。列表展示首个生成结果；原生 API 保留全部结果。图片异步功能仍需启用对象存储，参见 [ASYNC_IMAGE_TASKS.md](ASYNC_IMAGE_TASKS.md)。视频任务目录无需开启图片对象存储。

## 配置视频平台

1. 在管理端创建平台为 `Video`（视频）的分组。
2. 创建「视频」平台的 API Key 账号，加入该分组，填写上游密钥及地址。
3. 选择上游协议，默认 `Seedance (Ark)`，也可选 `Google Gemini (Veo)` 或 `MiniMax (Hailuo)`。
4. 配置公开模型名与上游模型／部署 ID 的映射，并为用户提供绑定该分组的系统 API Key。

协议是账号凭据 `credentials.video_protocol` 的属性，允许值为 `seedance`、`gemini` 和 `minimax`；未填写时使用 Seedance。三种协议分别使用原生请求入口，按协议筛选可用账号。MiniMax v2 账号也接受旧客户端的 prompt／首尾帧字段，并转为 content；不在不同视频厂商之间转换请求。视频生成不依赖图片生成权限。原 OpenAI 平台中启用 Seedance 能力的账号继续可用，不会自动迁移到新平台。

## 上游账号能力配置

Video 平台账号可在 `credentials` 中声明实际上游支持的分辨率和时长，适用于 Seedance、Gemini 和 MiniMax v1/v2。三个字段均可省略；全部省略时保留既有型号能力表与默认值。Grok 账号和原 OpenAI 平台的 Seedance 账号不启用这些字段。

| 凭据字段 | 类型与取值 | 作用 |
| --- | --- | --- |
| `video_resolutions` | 非空字符串数组，只允许 `480p`、`720p`、`768p`、`1080p`、`2k`；忽略大小写和两端空白，保存时去重并按此顺序排列 | 替换该账号的型号分辨率列表，不与官方列表取交集 |
| `video_min_duration_seconds` | 1–60 的整数，不能是字符串 | 替换时长下界；未填写的上界继承型号默认值 |
| `video_max_duration_seconds` | 1–60 的整数，不能是字符串 | 替换时长上界；未填写的下界继承型号默认值 |

例如，秘塔的 MiniMax-H3 账号可配置以下凭据片段，声明支持 480p／768p／1080p／2K、4–30 秒：

```json
{
  "video_resolutions": ["480p", "768p", "1080p", "2k"],
  "video_min_duration_seconds": 4,
  "video_max_duration_seconds": 30
}
```

省略字段表示使用型号默认能力；清除配置应删除字段，空数组、空字符串和 `null` 均不表示清除。非法档位、错误类型、非整数、越界或显式最小时长大于最大时长，在保存时返回 400 `INVALID_VIDEO_ACCOUNT`。仅填写一侧时，须在最终模型映射后与继承的另一侧组成有效区间；例如只填最小 20 秒，而映射型号的默认最大值为 15 秒，该账号对此型号不兼容。运行时读取能力不会改写共享账号凭据。

能力按选定账号和映射后的上游型号计算，同一账号的所有模型共享这套配置。只配置分辨率时，型号时长规则继续生效；只配置时长时，型号分辨率规则继续生效。Gemini 一旦配置任意时长边界，就按最终区间接受其中的整数秒，不再使用型号原有的 4／6／8 秒等离散集合。与被覆盖维度关联的型号组合限制按该维度的新能力判断，管理员应以实际上游支持情况填写。

请求未明确指定分辨率时，协议默认档若在账号有效列表中则继续使用；否则按 `480p`、`720p`、`768p`、`1080p`、`2k` 顺序选择支持的最低档。未明确指定普通时长时，将协议默认秒数收敛到最终区间内，例如默认 8 秒、账号最大 6 秒时提交 6 秒。客户端明确指定的值不会被静默换档或截短。Seedance 智能时长按该账号最终有效最大时长预占；未知型号／部署 ID 只有显式配置 `video_max_duration_seconds` 才可使用 `-1`，只配置最小时长不能提供可信上界。

这些配置只覆盖对应型号的分辨率、时长能力，不增加 4K 等计费档，也不放宽协议结构与硬规则：数量上限、单个 Gemini instance、原生字段类型、Seedance `frames` 的适用型号及范围／步进、draft 规则、MiniMax content role、ratio 和非空文本要求仍须满足。Seedance 固定帧数仍按原生 24fps 与步进规则换算；账号填写了哪一侧秒数边界，就额外检查对应精确秒数边界，未填写的一侧沿用原生帧数边界，不把整数 duration 的 2–12 秒误套到 29–289 帧。

混合账号池会在本地跳过能力不匹配的账号，尝试其它可用账号，不向不兼容账号试探创建，也不为其建立资金预占。最终使用的分辨率和秒数会明确写入上游请求，再按最终档位命中的价格冻结合同；未指定参数时的最低档回退或时长收敛也遵循此规则。账号能力声明不会补价格或借用其它档位价格。

账号连接测试仍使用固定测试参数：Seedance 5 秒／720p、Gemini 8 秒／720p／单结果、MiniMax 6 秒／768P（v2 的文生 ratio 为 `16:9`、首帧图生为 `adaptive`）。能力配置不会自动改写这些显式测试值；不兼容的测试组合会在本地失败，需结合账号实际能力判断。

## 隐藏上游视频链接

在「账号管理 → 添加／编辑视频账号」中设置「隐藏上游视频链接」，保存在布尔凭据 `credentials.hide_video_upstream_url`。通过页面创建的新账号默认开启；未配置该字段的旧账号保持原行为，按账号单独开启。开关适用于 Seedance、Gemini、MiniMax v1/v2。

开启后，原生响应保持字段结构，但视频 URL 替换为本站相对路径，例如：

```json
{"task":{"id":"TASK_ID","status":"succeeded","content":{"url":"/v1/video-tasks/RESOURCE/content?index=0"}}}
```

下游将该路径拼接至系统 API 的域名，并用**创建任务的原系统 API Key**请求：

```bash
curl 'https://api.example.com/v1/video-tasks/RESOURCE/content?index=0' \
  -H 'Authorization: Bearer sk-...' \
  -o generated-video.mp4
```

本站校验原用户、Key、分组与任务绑定账号，查询最新任务结果并直接返回视频字节，不重定向到上游。链接本身不含上游地址、签名或密钥；跨用户、跨 Key 和跨分组访问返回 404。多视频输出按 `index` 下载。余额耗尽后仍可读取已接受的任务，重复下载不重复计费。MiniMax 与 Seedance 的公开视频链接仅由服务端访问，下载不携带上游 Key；Gemini 仅在配置的同源地址鉴权下载。

异步任务列表、详情和刷新接口也按账号**当前设置**隐藏 `result_url`，包括此前已经完成的任务。用户在面板点击「下载视频」使用登录态鉴权。关闭开关后，原生接口和面板恢复返回原始上游链接。此前已发出的链接无法撤回。

开启隐藏时，不支持直接把 `callback_url` 传给上游（上游回调可能绕过网关并暴露原始链接），提交前返回 400；请改用轮询查询。响应中的其它 URL 字段（如首尾帧图片或诊断链接）会隐藏，不通过视频下载接口代理非视频资源。账号连接测试仍由服务端下载并预览视频。

Seedance 默认上游地址为 `https://ark.cn-beijing.volces.com/api/v3`，接受 origin、`/v1` 基址或完整任务 endpoint，避免重复拼接 `/v1/api/v3`。Gemini 默认地址为 `https://generativelanguage.googleapis.com`，也支持以 `/v1beta` 结尾的基址。自定义地址可指向兼容对应原生协议的中转。

视频计费统一支持 `480p`、`720p`、`768p`、`1080p`、`2K` 独立档位。渠道选择「视频」模式，为模型填写对应分辨率的每秒单价；标签不区分大小写。视频分组提供「按秒」和「按次」两种兜底计费方式。创建或编辑视频分组时，按秒须填写全部五个分辨率的每秒价，按次须填写每次成功任务的价格，0 为显式免费；不会自动复制或补填实际价格。视频分组优先使用渠道中匹配模型和分辨率的显式价格（包括默认价和 0 元），未命中才使用分组逐模型价格、分组兜底价。现有 Grok / OpenAI 分组保留分组覆盖优先的规则。未命中分辨率时仅使用显式默认视频价，不按其它分辨率或 token 区间取价；0 表示免费。768p、2K 不复制或沿用 720p 价格，提交前须配置对应档或默认视频价。按秒费用 = 视频数 × 时长 × 每秒单价 × 有效视频倍率；按次费用 = 单次价格 × 有效视频倍率，每个成功任务只收一次，不乘时长或输出视频数。失败任务不计费，重复查询不重复扣费。

Seedance 配置视频价格时也按所选按秒或按次规则结算，时长优先采用成功任务实际返回值，适配动态时长；未配置视频价格时保留逐模型 token 定价，按成功任务返回的 `usage.completion_tokens` 计费。Gemini 和 MiniMax 提交前须配置有效的按秒或按次视频价格；历史视频分组在下次保存时必须补齐所选方式的兜底价格。任务被上游接受时，完整价格合同与任务绑定：模型、分辨率、计价方式、命中单价及来源、适用用户／企业分组倍率、独立视频倍率、峰值及渠道分时和推理倍率、token fallback 价卡、付款身份和原生创建端点均冻结。之后修改价格、倍率、Key 分组或删除 Key 不改变已接受任务的结算；原有个人任务的创建者后来加入企业，仍从原个人付款方结算。

### 持久预占、企业与结算

企业成员可使用 Seedance、Gemini、MiniMax v1/v2 和 Grok 视频的创建、查询、取消与下载。计费与普通企业请求一致：有有效订阅且分组为订阅制时优先使用订阅，预占与实际费用累计订阅日／周／月窗口，保留企业身份用于用量归属，不扣个人或企业钱包，也不累计企业钱包限额和用户平台配额；企业／成员状态与分组准入、Key 配额仍生效。非订阅请求使用企业钱包、企业分组倍率及公司／部门／员工限额，并叠加用户平台与 Key 配额。其它企业禁用媒体功能的规则保持不变。

创建请求发往上游前，在 PostgreSQL 事务中冻结预计费用上限：按秒使用最终规范化的请求时长 × 数量，智能时长使用该账号最终有效最大时长（未配置时为已识别模型上界），按次只使用单价；token fallback 使用可计算的保守输出上界（4096×4096 像素、60fps、最终有效时长上界／1024）。固定帧数以 frames/24 换算，预占向上覆盖至整秒。可用余额已扣除现有冻结金额；余额、企业钱包及限额、Key 配额、用户平台日／周／月配额和适用订阅限额均须覆盖新增上限，否则请求在上游调用前被拒绝。预占与 HTTP 并发槽分离，不随请求结束释放，不依赖 Redis TTL，进程重启后仍保留。

余额准入沿用网关通用规则：余额 ≤ 0，或低于配置的最低保留余额，返回 403；余额为正且通过通用门槛，但扣除已有冻结后不足以覆盖本次冻结金额，返回 402。配额不足按对应限额返回 429。Gemini 原生入口使用 Google `error.code/message/status` 格式；Seedance、MiniMax v1/v2 使用 `error.type/message` 格式。这些错误均在创建发往上游前返回。

成功时按冻结合同与实际返回时长、数量、tokens 计算实际费，客户实扣金额封顶为预占金额；超限产生告警，差额退回可用余额。失败或确认取消全额退回。扣款、配额累计、任务去重和用量日志在同一个数据库事务提交，失败可安全重试。平台一旦有持久视频预占，其平台配额用量以数据库为准，避免 Redis 刷盘覆盖结算。查询、取消、下载在余额不足后仍可用，首次观察终态的原生查询、用户／管理员刷新、内容下载、MiniMax 文件查询以及后台对账均可结算。

MiniMax v1 查询确认 `Success` 后先完成持久结算，再获取文件下载信息；文件查询失败不撤销已确认的成功任务费用。该错误仍保留 HTTP 状态和合法的 Retry-After，并返回安全的通用 JSON；后续重查可以重新获取文件，不重复扣费。

明确拒绝须来自真实上游 HTTP ≥ 400 的完整 JSON 响应，并含 `error` 对象（包括 Google `error.code`），或 MiniMax 非零 `base_resp.status_code`，同时没有对应协议的任务身份。任务身份按提交端点判断：Seedance 为顶层 `id`，Gemini 为 `name`（operation），MiniMax v1/v2 为 `task_id` 或 `task.id`，Grok 为顶层 `request_id` 或 `id`。除 Grok 顶层 `request_id` 外，`request_id`、`error.request_id`、`trace_id` 等仅作请求追踪，不表示任务已创建；例如 MiniMax 返回含追踪号的 402 余额不足错误时，立即释放预占。判定采用改写或隐私过滤前的完整上游错误原文；截断、超出响应读取上限、非 JSON 或没有明确错误对象的响应继续视为不确定，客户端的通用 JSON 错误不能替代上游证据。

明确拒绝立即释放预占，同时沿用各协议既有的安全错误响应状态映射与 Retry-After 规则（MiniMax 原生 402 等状态保持透传）。本地凭据或 URL 校验失败，以及可确认创建请求未发出的 DNS 失败、连接被拒、TLS 握手／证书验证失败、代理 CONNECT 失败或写出前取消，同样立即释放，释放原因记录为 `upstream request not sent`；传输故障对客户端返回安全的 502 错误。

创建请求通过 `httptrace` 跟踪实际连接及写出，包括 TLS 指纹和 HTTP/2 路径；只有已确认支持跟踪的传输、且尚未取得可供该请求使用的连接时，才认定未发出。取得连接后可能已有部分写出，不能仅凭缺少成功的 `WroteRequest` 回调断言未发出。请求可能或已经写出后超时／连接中断、2xx 无法解析或缺任务 ID，以及无法证明是否发出的自定义传输，仍视为结果不确定。网关本地包装的 502 不是上游明确拒绝，不据此解冻。结果不确定、或超过 7 天仍无可确认终态的任务继续保留预占，并写入 `video_billing.manual_review_required` 告警以待人工处理，不自动释放或扣款。

上游创建请求不会因 HTTP 错误、超时或断连自动重试；仅在尚未发送任何创建请求就发现本地账号凭据无效时可安全改选账号。用量日志始终记录合同冻结的原生创建入站模板及上游模板，不使用触发结算的面板 URL 或任务 ID；现有测试数据不回填。

`manual` 表示停止后台轮询、等待核实；若用户／管理员查询或内容下载随后观察到真实成功、失败或取消终态，仍可自动结算或释放。真实终态优先于尚未执行的人工估值；与人工处理并发时由同一预占行锁和终态幂等检查串行化，已结算或释放的记录不再扣款。

预占快照同时冻结账号的上游 origin（规范化 scheme、host、port，省略默认端口），不保存 userinfo、中转路径或查询串。后台对账每次比较当前账号 origin：变更或无效时不发上游请求，转为人工处理并保留冻结，告警原因包含“上游地址已变更”。账号修改或批量修改 base_url 且存在 preparing／accepted／manual 预占时另写告警；所有对账请求禁止重定向。旧预占没有 origin 时，在首次对账前以数据库原子操作绑定当前 origin，记录兼容告警；旧快照无法还原历史地址，因此这一兼容仅信任升级时的现有配置，升级前应核对账号地址，异常任务先人工核实。

### Seedance 时长规则

以 [Ark 创建任务文档](https://www.volcengine.com/docs/82379/1520757) 及 [BytePlus 同源官方参考](https://docs.byteplus.com/en/docs/ModelArk/1520757) 为准，未通过顶层 `duration` 或文本 `--duration`／`--dur` 指定时长时，网关显式补上默认值再提交：Seedance 1.0／1.5／2.0 为 5 秒，2.5 为 `-1` 智能时长。文本参数会验证并转为顶层强参数，不再静默按 5 秒预占。

未配置账号时长能力时，已识别 Seedance 1.0 pro／pro-fast 支持 2–12 秒；1.5 pro 支持 4–12 秒或 `-1`；2.0 支持 4–15 秒或 `-1`；2.5 支持 4–30 秒或 `-1`。智能时长分别按模型最大值 12／15／30 秒预占，成功按实际时长结算并封顶；配置账号能力后，使用该账号最终有效最大时长预占。无法识别具体模型的别名或部署 ID 默认不接受智能时长；可把账号模型映射配置为官方模型名，或在 Video 账号显式填写 `video_max_duration_seconds` 提供可信上界。只配置最小时长不能解除这一限制。

`frames` 仅对已识别 Seedance 1.0 pro／pro-fast 接受：范围 29–289，须满足 `25+4n`，按 24fps 换算，优先于 duration。实际成功响应如有 frames，结算优先使用 frames/24，避免官方整数 duration 向下取整造成漏计；例如 289 帧为 12.041666… 秒。日志整数时长向上覆盖，精确秒数另记计费档说明。不能确认的时长字段、弱参数语法、冲突表达与未知型号的 frames 会在提交前返回 400。

### Seedance 分辨率与样片

未指定分辨率且未配置账号能力时，网关显式向上游提交 720p，与预占档位一致（Seedance 1.0 上游自身默认 1080p，不再出现按 720p 计费却生成 1080p 的偏差）；账号有效列表不含 720p 时，按前述能力配置规则选择最低支持档。文本 `--resolution`／`--rs` 与顶层 `resolution` 冲突或格式不合法时返回 400。每个任务固定生成 1 个视频，其它协议的数量字段不透传。

样片（`draft: true`）须为 480p。正式片通过 `content` 中的 `draft_task` 引用样片，仅允许引用同一用户、同一 Key、同一分组经本网关创建的样片；网关固定使用样片的上游账号与冻结模型映射，先查询源任务确认其为已成功的 480p 样片并读取实际时长，再按官方规定的 1080p 建立新的价格合同与预占。继承的时长、比例等参数不会重传给 Ark。引用未知任务或越权引用统一返回 400，且不调用上游。


## 账号连接测试

「账号管理 → 更多 → 测试连接」按视频账号所选协议调用原生视频提交和查询接口。模型下拉框展示对应协议的模型和公开映射名；支持视频提示词与可选首帧图片。Seedance 使用 5 秒／720P，Gemini 使用 8 秒／720P／单个结果，MiniMax 使用 6 秒／768P；模型或部署的具体支持情况由上游校验。

测试窗口显示任务 ID，每 5 秒轮询，最多等待 10 分钟，成功后预览视频。MiniMax v1 会进一步获取文件链接，v2 直接读取任务中的视频链接；Gemini 视频由服务端同源鉴权下载后预览；MiniMax 由服务端下载公开签名链接后预览，不向 CDN 发送密钥。上游 Key 不发送到浏览器。提交失败不自动重试，生成失败不会显示测试成功。测试会实际生成视频，上游可能产生费用；关闭窗口只停止查询，已提交的上游任务可能继续。管理员测试直接针对该账号执行，不计入用户 API Key 的异步任务目录或用户用量。

## Seedance 原生接口

请求使用系统 API Key，而非上游密钥：

```bash
curl https://api.example.com/api/v3/contents/generations/tasks \
  -H 'Authorization: Bearer sk-...' \
  -H 'Content-Type: application/json' \
  -d '{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"夕阳下的海浪"}]}'

curl https://api.example.com/api/v3/contents/generations/tasks/TASK_ID \
  -H 'Authorization: Bearer sk-...'
```

`DELETE /api/v3/contents/generations/tasks/{task_id}` 取消任务。兼容已有的 `/v3`、`/v1` 和无前缀任务入口。请求与上游响应保留 Ark 原生格式，只根据账号配置改写模型名。

## Gemini（Veo）原生接口

```bash
curl https://api.example.com/v1beta/models/YOUR_VIDEO_MODEL:predictLongRunning \
  -H 'Authorization: Bearer sk-...' \
  -H 'Content-Type: application/json' \
  -d '{"instances":[{"prompt":"夕阳下的海浪"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}'

# operation_name 为提交响应中的 name，例如 models/veo-3.0-generate-001/operations/xxx。
curl https://api.example.com/v1beta/OPERATION_NAME \
  -H 'Authorization: Bearer sk-...'
```

模型以 URL 中的模型名为准；模型映射后返回的 operation name 用于后续查询。上游鉴权使用 `x-goog-api-key`。Gemini 完成结果通常需要鉴权下载，用户可直接在「异步任务」页点击「下载视频」，无需接触上游密钥。服务端只向已配置上游的同 origin 地址发送该密钥，并拒绝下载跳转。

`instances` 只能包含一个对象，生成数量使用 `parameters.sampleCount`。网关在选定账号并完成模型映射后校验数量、时长和分辨率；未配置账号能力时使用以下型号表，配置后仅替换对应的分辨率／时长维度：

| 映射后模型 | 数量 | 时长 | 分辨率 |
| --- | --- | --- | --- |
| `veo-2.0-*` | 1–2（保守上限） | 5–8 秒 | 720p |
| `veo-3.0-*` | 1 | 8 秒 | 720p；1080p 仅 16:9 |
| `veo-3.1-*` | 1 | 4／6／8 秒 | 720p、1080p；1080p 或使用参考图时须 8 秒 |
| 其它模型 | 1 | 8 秒 | 720p |

未填写时，网关以 1 个／8 秒／720p（及 16:9 比例）为默认值，再按账号能力选择有效档位与收敛时长，并显式写入上游请求，保证上游实际生成与价格合同一致。超出账号最终有效范围时在本地过滤账号；多个 instance、4K 和视频扩展仍在提交前返回 400 且不调用上游。本站接入的是 Gemini API（generativelanguage），不适用 Vertex AI 的 1–4 个结果上限。

## MiniMax（Hailuo）原生接口

账号协议选择 `minimax`。地址中的 `/v2` 决定使用 H3 v2；origin 或不含版本的基址默认使用 Hailuo v1。支持完整生成地址，并保留中转前缀。两版均用 Bearer 鉴权，使用系统 Key 调用下游接口。

### MiniMax H3 v2

秘塔上游 Base URL 填 `https://metaso.cn/api/minimax/v2` 或 `https://metaso.cn/api/minimax/v2/video_generation`，模型填 `MiniMax-H3` 或 `MiniMax-H3-Max`。系统连接测试自动提交 `content` 数组、6 秒、768P 和 `ratio`（文生为 `16:9`，首帧图生为 `adaptive`）。默认模型列表按配置版本切换。

```bash
curl https://api.example.com/v2/video_generation \
  -H 'Authorization: Bearer sk-...' \
  -H 'Content-Type: application/json' \
  -d '{"model":"MiniMax-H3","content":[{"type":"text","text":"夕阳下的海浪"}],"duration":6,"resolution":"768P","ratio":"16:9"}'

curl https://api.example.com/v2/query/video_generation/TASK_ID \
  -H 'Authorization: Bearer sk-...'
```

v2 成功提交仅返回 `task_id`，没有 `base_resp`。查询使用 **路径中的任务 ID**，返回 `task.id`／`task.status`。`GET /v2/query/video_generation?task_id=...` 是上游任务列表，不能用于单任务查询；网关不开放该列表。`queued`／`running` 显示处理中，`succeeded` 显示已完成，`failed`／`cancelled` 显示对应终态。结果来自 `task.content.url`，不调用 v1 的文件接口。

原生 `content`、`ratio`、`extra` 等字段透传。每个请求须有非空文本；图生用 `image_url` 和 `first_frame`／`last_frame` role，参考图用 `reference_image` role。官方 H3 能力表为整数 4–15 秒、768P／2K；H3-Max 为整数 5–15 秒、480P／768P，不支持 2K。未配置账号能力时，按映射后的上游型号使用这张官方表；兼容中转可通过前述账号字段替换分辨率和时长能力，content／role／ratio 等协议硬规则保持不变。原生协议限制仍由上游进一步校验。

### MiniMax Hailuo v1

默认上游 `https://api.minimax.io`，国内可填 `https://api.minimaxi.com`，也可填写带 `/v1` 的基址或完整生成地址。状态和文件查询使用同一前缀下的 `/query/video_generation?task_id=...` 与 `/files/retrieve?file_id=...`。

```bash
curl https://api.example.com/v1/video_generation \
  -H 'Authorization: Bearer sk-...' \
  -H 'Content-Type: application/json' \
  -d '{"model":"YOUR_VIDEO_MODEL","prompt":"夕阳下的海浪","duration":6,"resolution":"1080P"}'

curl 'https://api.example.com/v1/query/video_generation?task_id=TASK_ID' \
  -H 'Authorization: Bearer sk-...'

# file_id 从成功的任务状态响应中获取。
curl 'https://api.example.com/v1/files/retrieve?file_id=FILE_ID' \
  -H 'Authorization: Bearer sk-...'
```

文生、图生视频共用提交入口；`first_frame_image`、`last_frame_image`、`subject_reference`、`prompt_optimizer` 等原生字段透传，仅按账号映射改写模型。未配置账号能力时，时长沿用 6／10 秒规则（各模型和分辨率的具体限制由上游校验）；默认 6 秒，网关未指定分辨率时明确提交 768P，旧模型请显式指定其支持的分辨率。配置账号能力后，默认档回退与普通时长收敛按前述规则处理。768P、720P、1080P、2K 各自使用对应计费档，512P 保持使用 480P 档，须事先配置价格。

Preparing／Queueing／Processing 在任务页显示「处理中」，Success 为「已完成」，Fail 为「失败」。成功状态查询后，服务端用相同账号获取文件链接，并绑定文件归属；原生状态响应保持 MiniMax 格式。文件查询仅允许查询过成功状态的原用户、Key 和分组访问。HTTP 200 中非零 `base_resp.status_code` 作为上游错误处理，不登记成功任务、不重试提交、不计费。失败的生成任务不计费。

MiniMax 下载链接有效期通常为 1 小时；任务页下载时重新获取链接并代理下载，不向文件 CDN 发送上游 Key，也不跟随重定向。启用了出站域名白名单时，需要允许上游返回的视频 CDN 域名。任务查询与文件读取在余额不足后仍可进行，费用在首次观察到成功任务时去重记录。

官方协议参考：[H3 v2 提交](https://platform.minimax.io/docs/api-reference/video-generation-v2-create)、[H3 v2 查询](https://platform.minimax.io/docs/api-reference/video-generation-v2-query)、[v1 视频提交](https://platform.minimax.io/docs/api-reference/video-generation-t2v)、[状态查询](https://platform.minimax.io/docs/api-reference/video-generation-query)、[文件查询](https://platform.minimax.io/docs/api-reference/video-generation-download)。视频任务复用原 Redis 目录和归属绑定；独立分辨率价格由迁移 `255_video_resolution_768p_2k.sql` 添加，启动服务时自动应用，不回填旧 720p 价格。回退旧版本前应完成或保留 MiniMax 任务记录，旧版本不会识别该协议。

## 请求参数规范化

各协议创建请求在发往上游前统一规范化：影响输出数量、时长或分辨率的字段都会解析进价格合同，并以同样的值写入上游请求，未填写的字段显式补默认值。

- JSON 请求体不得包含重复键（包括转义后同名的键）或多个 JSON 值，嵌套不超过 128 层，避免网关与上游对同一请求解读不同。
- 不支持的跨协议数量或用量字段（例如在 Seedance 中传 `n`、`sampleCount`）在上游调用前返回 400。Seedance、MiniMax、Grok 每个任务固定生成 1 个视频。
- Grok 普通生成的 `duration`／`seconds` 接受数字或整数字符串，未填写时显式提交 8 秒；`resolution` 未填写时显式提交 480p。重复或冲突的字段、非原生的数量字段和提示词中的输出控制命令返回 400，普通自然语言描述不受影响。Grok 视频编辑与续写暂保持原有计费方式。
- 实际费用始终不超过预占金额；若上游返回的用量超出合同，按预占金额结算并记录告警日志 `video_billing_actual_cost_exceeds_reservation`。这类任务结算后为已结算状态，不进入人工处理列表。

## 归属与面板接口

API 查询继续要求原始系统 Key、用户和分组，并绑定创建任务的上游账号。查询和取消已接受的任务不受当前余额不足阻断。面板通过用户登录态调用以下接口，无需把系统 Key 的密钥交给页面：

```text
GET  /api/v1/user/async-tasks?page=1&page_size=20&kind=video&status=processing&search=veo
GET  /api/v1/user/async-tasks/{id}
POST /api/v1/user/async-tasks/{id}/refresh
GET  /api/v1/user/async-tasks/{id}/content
```

面板 `id` 是目录内部 ID，与原生任务 ID／operation name 不同。内容下载适用于 Seedance、Gemini、MiniMax 和 Grok；图片通过结果链接访问。刷新和下载仍检查原 Key／分组可用性，用户接口的跨用户任务访问返回 404。

企业或成员被禁用后，新请求仍受企业准入限制；成员可通过面板登录态查看、刷新及下载自己已有且原 Key／分组仍可访问的任务。既有任务的冻结付款义务继续结算，不因禁用而转给他人或免除；后台对账也不因账号禁用／改组而取消既有义务。

全局管理接口仅限管理员登录态或管理员 API Key，使用任务记录中的用户 ID 和目录 ID：

```text
GET  /api/v1/admin/async-tasks?page=1&page_size=20&user_id=USER_ID&kind=video&status=processing&protocol=minimax&search=MODEL
GET  /api/v1/admin/async-tasks/{user_id}/{id}
POST /api/v1/admin/async-tasks/{user_id}/{id}/refresh
GET  /api/v1/admin/async-tasks/{user_id}/{id}/content
```

详情及操作检查用户与任务的组合身份，不接受任意上游任务 ID 或 URL。响应只包含任务元数据与必要的名称，不包含系统 Key、上游 Key 或内部路由字段。

## 原生协议与管理端安全

原生视频提交、查询及取消请求不跟随上游 HTTP 重定向。Gemini 的 `x-goog-api-key` 和 Seedance 的 Bearer Key 只发送给配置的上游目标，不因 302／307 转发到其它站点。异步创建失败不会自动重试。

视频账号保存时拒绝 base_url 中的 userinfo（例如 `https://user:password@host`）。对账传输错误不记录 `url.Error` 的原始 URL，避免中转路径、查询签名或多层转义凭据进入日志。origin 冻结不替代网络访问控制，也不能阻止同一域名的 DNS 地址变化。建议启用 `security.url_allowlist.enabled`，将 `upstream_hosts` 收窄为实际厂商及可信中转域名，并把视频 CDN 加入必要的允许范围；公网部署建议关闭 `allow_private_hosts`，按需关闭 `allow_insecure_http`。内网自建中转可以显式保留私网访问，本轮不统一启用 PublicHostsOnly，以免破坏兼容。

Gemini 关闭隐藏上游链接开关时，保留既有原生错误透传行为：上游错误体和响应白名单内的 `x-request-id` 可能返回客户端。需要隐藏这些诊断信息时应开启该账号的隐私开关；本地错误始终使用安全的 Google 格式。

MiniMax 上游以非 JSON 返回 4xx／5xx 时，网关保留 HTTP 状态与合法的 Retry-After，返回通用 JSON 错误，不回显上游诊断页面或签名 URL；v1 查询成功后获取文件链接失败也遵循该规则。

三种视频账号连接测试均由服务端下载后返回预览数据，浏览器不获得上游原始链接。Seedance／MiniMax 的公开视频下载不携带上游 Key，仅接受公网 HTTPS、禁止重定向，预览最大 32 MiB。上游失败诊断中的 URL、查询串、签名及已知凭据在响应和日志中脱敏；响应超限或下载失败发送明确失败终态。

管理端列表批量补齐当前页的用户、Key、分组显示信息；关联信息不可用时保留任务 ID。管理员查看详情和下载内容写入敏感读取审计，归属操作管理员，不记录视频内容或原始结果响应体。

## MiniMax 选号、价格与错误格式

MiniMax 在选择账号之后，根据该账号 API 版本和映射后的上游模型精确校验与规范化。公开别名和 legacy prompt／首尾帧字段可由 v2 处理；legacy 字段转为原生 content，以 6 秒／768P 为默认值，按账号有效能力收敛后明确写入请求。模型只映射一次，其它原生字段透传，时长和分辨率以映射后型号的默认能力及账号覆盖配置为准。

`/v2/video_generation` 和必须使用 v2 的 content／H3 请求在本地跳过不兼容 v1 账号，不发送试探创建、不建立资金预占。查询继续用创建绑定账号；无兼容账号返回 503，参数错误在创建前返回 400。实际创建失败不自动重试。

价格合同使用最终规范化时长、数量和分辨率，在上游创建前冻结并预占。Gemini／MiniMax 缺有效视频价返回 400 `video_pricing_not_configured`，零上游创建且不留下预占、pending 或目录。旧 OpenAI／Grok 分组保留既有 token 覆盖价，包括 768P／2K。

Gemini 本地错误（权限、价格、并发、依赖、安全审计、查询归属等）采用 Google `error.code/message/status` 格式，保留 HTTP 状态及内部 Ops 分类；其它协议保留原格式。

## 人工冻结处理 API

仅完整管理员角色可访问，使用现有管理员认证、合规校验和重操作限流；不对普通用户或子管理员开放，响应禁止缓存。所有读取与处理记录原操作管理员审计，付款身份仍来自持久合同。

| 方法与路径 | 参数 | 审计动作 |
|---|---|---|
| `GET /api/v1/admin/video-billing-reservations` | `state` 默认 manual，可选 preparing/accepted/manual/settled/released；`page` 默认1，`page_size` 默认20、最大100 | `admin.video_billing_reservations.list` |
| `POST /api/v1/admin/video-billing-reservations/{id}/release` | JSON `reason` 必填，1–1000字符；仅manual，可幂等读取已released | `admin.video_billing_reservations.release` |
| `POST /api/v1/admin/video-billing-reservations/{id}/settle` | JSON `reason` 必填；按秒须 `duration_seconds` 与 `video_count`；token须 `completion_tokens`；按次无需数量/时长 | `admin.video_billing_reservations.settle` |

结算仅接收核实后的实际用量，不接受单价、扣款金额、付款人或操作人覆盖字段；按冻结合同计价且封顶预占。无上游任务ID时使用独立 reservation 计费去重标识，不伪造上游任务绑定；有任务ID保持原去重键。列表只返回安全元数据、冻结/实际金额、合同摘要和处理说明，不返回完整合同、路由快照或密钥。运营人员应先与上游核对是否创建和实际用量，再填写原因释放或结算。

包括认证中间件返回的 401／403 在内，上述预占接口所有响应均带 `Cache-Control: no-store`。视频内容下载路径和未知路径内的长 base64url／hex／UUID 等 ID 段归一为占位符，避免用量端点和视频下载 Ops 请求路径按任务展开。

## 迁移 258 发布说明

`258_video_usage_request_id.sql` 将 usage_logs、usage_billing_dedup、usage_billing_dedup_archive 和 ops_system_logs 的 request_id 从 VARCHAR 扩为 TEXT，不改变已有 ID 或唯一键。独立 PostgreSQL 16.14 使用真实前链结构、四表各 10 万行实测：heap 与全部 44 个索引物理文件复用，无全表重写；其中四个 request_id 索引的目录 OID 更新，不代表物理索引全量重建。

DDL 仍申请 ACCESS EXCLUSIVE 并持有到迁移事务提交。无争用时本地四条 ALTER 合计 4.786ms，开始标记到提交后标记 5.184ms；受长 SELECT 事务阻挡时实测等锁约 1.973s，不能把等待误认为表重写。生产应选择低峰、检查长事务与阻塞锁，按部署策略设置较短锁等待超时并在失败后重试；迁移事务不附加无关慢操作。本地时延不构成生产承诺，生产若有额外表达式／自定义 opclass 索引或不同 PostgreSQL／排序规则，应先在结构副本确认。扩容后不要直接回退到 VARCHAR，否则长 ID 可能阻断回退；回滚应用时保留兼容的 TEXT 字段。
