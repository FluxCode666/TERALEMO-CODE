export default {
  "adminAsyncTasks": {
    "title": "Global async tasks",
    "description": "View image and video tasks across all users, including status, ownership and results.",
    "owner": "User",
    "source": "API key / Group",
    "group": "Group",
    "user_id": "User ID",
    "api_key_id": "API key ID",
    "group_id": "Group ID",
    "protocol": "Protocol / Platform",
    "invalidIdFilter": "ID filters must be positive integers.",
    "details": "Task details",
    "error": "Error",
    "resultURL": "Original result URL",
    "emptyHint": "No matching tasks. Adjust the filters; tasks submitted by users will appear here.",
    "retention": "Shows retained tasks across all users, usually for 24 hours. Auto-refresh polls pending videos on the current page only. Account connection tests are excluded.",
    "tabs": {
      "tasks": "Task list",
      "reservations": "Manual reservations"
    },
    "detail": {
      "task_id": "Task ID", "id": "Catalog ID", "model": "Model", "status": "Status",
      "user_id": "User ID", "user_email": "User email", "api_key_id": "API key ID", "group_id": "Group ID",
      "protocol": "Protocol / Platform", "created_at": "Created", "completed_at": "Completed", "expires_at": "Record expires"
    }
  },
  "asyncTasks": {
    "title": "Async tasks",
    "description": "Track image and video generation progress and results.",
    "autoRefresh": "Auto refresh",
    "search": "Find tasks",
    "searchPlaceholder": "Task ID or model name",
    "kind": "Task type",
    "image": "Image",
    "video": "Video",
    "task": "Task / model",
    "processing": "Processing",
    "completed": "Completed",
    "failed": "Failed",
    "cancelled": "Cancelled",
    "createdAt": "Created",
    "finishedAt": "Finished",
    "empty": "No async tasks",
    "emptyHint": "Submit an async image or video request through the API to see it here, or adjust your filters.",
    "retention": "Tasks usually expire after 24 hours; up to 1,000 records per user are shown. Result links may expire, so save files promptly.",
    "refreshFailed": "Some tasks could not be refreshed from the upstream. Try again later.",
    "loadFailed": "Failed to load tasks. Please retry.",
    "viewResult": "View result",
    "download": "Download video",
    "downloadFailed": "Failed to download the video. Please retry later."
  },
  "adminVideoReservations": {
    "state": "State",
    "states": {
      "preparing": "Preparing",
      "accepted": "Accepted",
      "manual": "Awaiting review",
      "settled": "Settled",
      "released": "Released"
    },
    "empty": "No reservations",
    "emptyHint": "No video billing reservations in this state. Reservations that upstream never confirmed or that stayed unresolved for 7 days appear under Awaiting review.",
    "updatedAt": "Updated",
    "noTaskId": "No upstream task ID",
    "billingModes": {
      "video": "Per second",
      "token": "Tokens",
      "per_request": "Per request"
    },
    "columns": {
      "id": "Reservation ID",
      "userId": "User ID",
      "apiKeyId": "Key ID",
      "groupId": "Group ID",
      "protocol": "Protocol / Platform",
      "model": "Model / Resolution",
      "billingMode": "Billing mode",
      "holdAmount": "Hold amount",
      "actualAmount": "Charged",
      "multiplier": "Multiplier",
      "attempts": "Attempts",
      "reviewNote": "Review note",
      "timestamps": "Created / Updated"
    },
    "actions": {
      "settle": "Settle",
      "release": "Release"
    },
    "release": {
      "title": "Release reservation",
      "warning": "The user is not charged after release. The held amount returns to the available balance.",
      "confirmHint": "Confirm releasing this reservation? This cannot be undone from this page."
    },
    "settle": {
      "title": "Settle reservation",
      "cap": "The actual charge never exceeds the hold amount {amount}.",
      "confirmHint": "Confirm settling this reservation with the entered actual usage?",
      "durationSeconds": "Duration (seconds)",
      "videoCount": "Video count",
      "completionTokens": "Completion tokens",
      "usage": "Actual usage",
      "noUsage": "No usage required for per-request billing."
    },
    "form": {
      "reason": "Reason",
      "reasonPlaceholder": "Describe the upstream reconciliation result, 1-1000 characters.",
      "reasonRequired": "A reason is required.",
      "reasonTooLong": "The reason must be at most 1000 characters.",
      "durationInvalid": "Duration must be a positive number no greater than 2147483647.",
      "videoCountInvalid": "Video count must be a positive integer no greater than 2147483647.",
      "tokensInvalid": "Completion tokens must be an integer between 0 and 2147483647.",
      "continue": "Continue",
      "back": "Back",
      "confirm": "Confirm"
    },
    "errors": {
      "invalid": "Invalid input. Check the reason and usage fields.",
      "unauthorized": "Your session has expired. Sign in again.",
      "forbidden": "Only full administrators can manage video billing reservations.",
      "notFound": "The reservation no longer exists. Refresh the list.",
      "conflict": "The reservation state has changed. Refresh the list and try again.",
      "unavailable": "Persistent video billing is not enabled on this deployment.",
      "generic": "Operation failed. Please retry."
    },
    "released": "Reservation released.",
    "settled": "Reservation settled."
  },
  "videoAccounts": {
    "platform": "Video",
    "protocol": "Upstream video protocol",
    "hideUpstreamURL": "Hide upstream video URLs",
    "hideUpstreamURLHint": "When enabled, API responses and async tasks expose only local download endpoints. API downloads require the original API key. When disabled, upstream URLs are returned. Applies to existing tasks on this account. Use polling; upstream callbacks are unavailable when enabled.",
    "seedanceHint": "Default protocol. Submit and query native Seedance / Ark tasks. Supports configured per-second or per-request video billing; legacy groups without video pricing retain completion-token billing.",
    "geminiHint": "Uses Google Gemini predictLongRunning / Operation with Veo models. Matching channel pricing takes priority over the group per-second or per-request fallback.",
    "minimaxHint": "Supports Hailuo v1 and H3 v2. H3 upstream URLs must include /v2, for example https://metaso.cn/api/minimax/v2; connection tests use content and ratio. Defaults to 768P and supports independent resolution rates or per-request pricing.",
    "pricingHint": "Video groups support per-second or per-request billing and require the corresponding fallback prices. Matching explicit channel pricing takes priority; per-request billing charges once per successful task. Per-second billing uses independent 480p, 720p, 768p, 1080p and 2K tiers. 768p and 2K do not use 720p rates; MiniMax 512P uses the 480p tier.",
    "usageHint": "Use the protocol selected for the group’s accounts and replace YOUR_VIDEO_MODEL with the model provided by your administrator. Poll using the returned task ID or operation name, or view progress and results on the Async tasks page.",
    "capability": {
      "title": "Upstream capabilities (optional)",
      "resolutions": "Supported resolutions",
      "resolutionsHint": "Leave all unchecked to follow the official model rules.",
      "minDuration": "Min duration (seconds)",
      "maxDuration": "Max duration (seconds)",
      "durationPlaceholder": "Official rules",
      "hint": "When unset, requests are validated against the official rules of the protocol and model (e.g. MiniMax-H3: 768P/2K, 4-15s). Configure only when the upstream explicitly supports a wider range. Once configured, the gateway rejects out-of-range requests before submission and selects a supporting account within the group; the configured resolutions also need matching prices in the group or channel."
    }
  }
}
