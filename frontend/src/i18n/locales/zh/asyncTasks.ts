export default {
  "adminAsyncTasks": {
    "title": "全局异步任务",
    "description": "查看所有用户的图片与视频任务，追踪处理状态、归属和生成结果。",
    "owner": "所属用户",
    "source": "API Key / 分组",
    "group": "分组",
    "user_id": "用户 ID",
    "api_key_id": "API Key ID",
    "group_id": "分组 ID",
    "protocol": "协议 / 平台",
    "invalidIdFilter": "ID 筛选必须是正整数。",
    "details": "任务详情",
    "error": "错误信息",
    "resultURL": "原始结果链接",
    "emptyHint": "当前没有匹配的任务，请调整筛选条件；用户提交任务后将在此显示。",
    "retention": "展示保留期内所有用户的任务（通常为 24 小时）。自动刷新仅查询当前页的处理中视频；账号连接测试不计入此列表。",
    "tabs": {
      "tasks": "任务列表",
      "reservations": "人工处理预占"
    },
    "detail": {
      "task_id": "任务 ID", "id": "目录 ID", "model": "模型", "status": "状态",
      "user_id": "用户 ID", "user_email": "用户邮箱", "api_key_id": "API Key ID", "group_id": "分组 ID",
      "protocol": "协议 / 平台", "created_at": "创建时间", "completed_at": "完成时间", "expires_at": "记录到期时间"
    }
  },
  "asyncTasks": {
    "title": "异步任务",
    "description": "查看图片与视频任务的处理进度和生成结果。",
    "autoRefresh": "自动刷新",
    "search": "查找任务",
    "searchPlaceholder": "任务 ID 或模型名称",
    "kind": "任务类型",
    "image": "图片",
    "video": "视频",
    "task": "任务 / 模型",
    "processing": "处理中",
    "completed": "已完成",
    "failed": "失败",
    "cancelled": "已取消",
    "createdAt": "创建时间",
    "finishedAt": "完成于",
    "empty": "暂无异步任务",
    "emptyHint": "通过 API 提交异步图片或视频任务后，可在此查看；也可调整筛选条件。",
    "retention": "任务有效期通常为 24 小时，每个账号最多展示 1000 条。结果链接可能过期，请及时保存。",
    "refreshFailed": "部分任务暂时无法从上游刷新，请稍后重试。",
    "loadFailed": "任务加载失败，请重试。",
    "viewResult": "查看结果",
    "download": "下载视频",
    "downloadFailed": "视频下载失败，请稍后重试。"
  },
  "adminVideoReservations": {
    "state": "状态",
    "states": {
      "preparing": "准备中",
      "accepted": "已受理",
      "manual": "待人工处理",
      "settled": "已结算",
      "released": "已释放"
    },
    "empty": "暂无预占记录",
    "emptyHint": "该状态下没有视频计费预占。上游结果不确定或 7 天仍无终态的预占会进入「待人工处理」。",
    "updatedAt": "更新于",
    "noTaskId": "无上游任务 ID",
    "billingModes": {
      "video": "按秒计费",
      "token": "按 Token",
      "per_request": "按次计费"
    },
    "columns": {
      "id": "预占 ID",
      "userId": "用户 ID",
      "apiKeyId": "Key ID",
      "groupId": "分组 ID",
      "protocol": "协议 / 平台",
      "model": "模型 / 分辨率",
      "billingMode": "计费方式",
      "holdAmount": "预占金额",
      "actualAmount": "实收金额",
      "multiplier": "有效倍率",
      "attempts": "尝试次数",
      "reviewNote": "复核说明",
      "timestamps": "创建 / 更新时间"
    },
    "actions": {
      "settle": "结算",
      "release": "释放"
    },
    "release": {
      "title": "释放预占",
      "warning": "释放后不向用户收费，预占金额将退回用户可用余额。",
      "confirmHint": "确认释放该预占？此操作无法在本页面撤销。"
    },
    "settle": {
      "title": "结算预占",
      "cap": "实际收费不超过预占金额 {amount}。",
      "confirmHint": "确认按填写的实际用量结算该预占？",
      "durationSeconds": "时长（秒）",
      "videoCount": "视频数",
      "completionTokens": "Completion Tokens",
      "usage": "实际用量",
      "noUsage": "按次计费无需填写用量。"
    },
    "form": {
      "reason": "原因",
      "reasonPlaceholder": "说明上游账单核对结果，1–1000 字符。",
      "reasonRequired": "必须填写原因。",
      "reasonTooLong": "原因不能超过 1000 字符。",
      "durationInvalid": "时长必须为大于 0 且不超过 2147483647 的数字。",
      "videoCountInvalid": "视频数必须为 1–2147483647 的整数。",
      "tokensInvalid": "Completion tokens 必须为 0–2147483647 的整数。",
      "continue": "继续",
      "back": "返回",
      "confirm": "确认"
    },
    "errors": {
      "invalid": "输入无效，请检查原因与用量字段。",
      "unauthorized": "登录状态已失效，请重新登录。",
      "forbidden": "只有完整管理员可以处理视频计费预占。",
      "notFound": "该预占不存在，请刷新列表。",
      "conflict": "该预占状态已变化，请刷新列表后重试。",
      "unavailable": "当前部署未启用持久计费。",
      "generic": "操作失败，请重试。"
    },
    "released": "预占已释放。",
    "settled": "预占已结算。"
  },
  "videoAccounts": {
    "platform": "视频",
    "protocol": "上游视频协议",
    "hideUpstreamURL": "隐藏上游视频链接",
    "hideUpstreamURLHint": "开启后，API 响应和异步任务页仅返回本站下载地址，用户需使用原 API Key 下载视频。关闭后返回上游原始链接。此设置对该账号的已有任务也生效；开启时请使用轮询查询，不支持上游回调。",
    "seedanceHint": "默认协议。使用 Seedance / Ark 原生任务接口提交和查询；支持配置按秒或按次视频计费，历史未配置视频价格的分组保留 completion tokens 计费。",
    "geminiHint": "使用 Google Gemini predictLongRunning / Operation 协议，支持 Veo 视频模型；费用采用匹配的渠道价格或分组按秒／按次兜底价格。",
    "minimaxHint": "支持 Hailuo v1 与 H3 v2。H3 的上游地址需包含 /v2，例如 https://metaso.cn/api/minimax/v2；连接测试自动使用 content 与 ratio。默认 768P，支持独立分辨率按秒价或按次价格。",
    "pricingHint": "视频分组可选按秒或按次收费，并须填写相应兜底价格。渠道中匹配的显式定价优先；按次每个成功任务只收一次。按秒支持 480p、720p、768p、1080p、2K 独立档位，768p、2K 不沿用 720p；MiniMax 512P 使用 480p 档。",
    "usageHint": "请使用分组内账号所选的协议，并将 YOUR_VIDEO_MODEL 替换为管理员提供的模型名。提交后可用返回的任务 ID／operation name 查询，也可在「异步任务」页查看进度和结果。",
    "capability": {
      "title": "上游能力（可选）",
      "resolutions": "支持的分辨率",
      "resolutionsHint": "全不选表示按官方模型规则。",
      "minDuration": "最短时长（秒）",
      "maxDuration": "最长时长（秒）",
      "durationPlaceholder": "按官方规则",
      "hint": "未配置时按协议和模型的官方规则校验，例如 MiniMax-H3 为 768P/2K、4–15 秒；仅当上游明确支持更大范围时才配置。配置后，网关会把超出范围的请求在提交前拒绝，并按配置在分组内挑选支持的账号；对应分辨率需要在分组或渠道里配置价格。"
    }
  }
}
