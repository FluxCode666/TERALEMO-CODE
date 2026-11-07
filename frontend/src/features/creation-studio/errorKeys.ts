/**
 * 契约 §6.1 的 `reason` → `creationStudio.errors.*` 映射。
 *
 * 前端只按 `reason` 分支；未列出的 reason 按契约 §6.2 第 7 条的透传规则兜底：
 * 4xx → `invalidRequest`，5xx → `upstream`。
 */
import type { CreationErrorKey } from './types'

const REASON_TO_KEY: Record<string, CreationErrorKey> = {
  INVALID_CREATION_REQUEST: 'invalidRequest',
  CREATION_MODEL_NOT_AVAILABLE: 'modelUnavailable',
  video_pricing_not_configured: 'pricingMissing',
  insufficient_balance: 'balanceInsufficient',
  INSUFFICIENT_BALANCE: 'balanceLow',
  ENTERPRISE_BALANCE_INSUFFICIENT: 'enterpriseBalance',
  IMAGE_GENERATION_FORBIDDEN: 'imageForbidden',
  GROUP_DISABLED: 'groupUnavailable',
  GROUP_DELETED: 'groupUnavailable',
  GROUP_NOT_ALLOWED: 'groupUnavailable',
  ENTERPRISE_DISABLED: 'enterpriseBlocked',
  ENTERPRISE_MEMBER_DISABLED: 'enterpriseBlocked',
  ENTERPRISE_GROUP_NOT_ALLOWED: 'enterpriseBlocked',
  API_KEY_EXPIRED: 'keyUnavailable',
  API_KEY_DISABLED: 'keyUnavailable',
  SUBSCRIPTION_NOT_FOUND: 'subscriptionMissing',
  ACCESS_DENIED: 'ipRestricted',
  CREATION_KEY_NOT_FOUND: 'keyNotFound',
  CREATION_UPLOAD_NOT_FOUND: 'uploadExpired',
  CREATION_UPLOAD_EXPIRED: 'uploadExpired',
  CREATION_UPLOAD_TOO_LARGE: 'uploadTooLarge',
  CREATION_BODY_TOO_LARGE: 'bodyTooLarge',
  CREATION_UPLOAD_UNSUPPORTED_TYPE: 'uploadUnsupportedType',
  CREATION_UPLOAD_INVALID_IMAGE: 'uploadInvalidImage',
  rate_limit_exceeded: 'rateLimited',
  USAGE_LIMIT_EXCEEDED: 'quotaExceeded',
  VIDEO_RESERVATION_QUOTA_EXCEEDED: 'quotaExceeded',
  API_KEY_QUOTA_EXHAUSTED: 'keyQuota',
  DEPARTMENT_QUOTA_EXCEEDED: 'enterpriseQuota',
  MEMBER_QUOTA_EXCEEDED: 'enterpriseQuota',
  CREATION_UPLOAD_LIMIT_EXCEEDED: 'uploadLimit',
  upstream_error: 'upstream',
  CREATION_UPLOAD_STORAGE_FAILED: 'uploadFailed',
  seedance_no_eligible_account: 'noCapacity',
  gemini_video_no_eligible_account: 'noCapacity',
  minimax_video_no_eligible_account: 'noCapacity',
  grok_media_no_eligible_account: 'noCapacity',
  billing_service_error: 'billingUnavailable',
  CREATION_ESTIMATE_UNAVAILABLE: 'billingUnavailable',
  CREATION_DISPATCHER_UNAVAILABLE: 'internal',
  CREATION_INTERNAL_ERROR: 'internal',
}

export function errorKeyForReason(reason: string, status = 0): CreationErrorKey {
  const mapped = REASON_TO_KEY[reason]
  if (mapped) return mapped
  if (reason.startsWith('ENTERPRISE_')) {
    return status === 429 ? 'enterpriseQuota' : 'enterpriseBlocked'
  }
  if (reason.endsWith('_no_eligible_account')) return 'noCapacity'
  return status >= 400 && status < 500 ? 'invalidRequest' : 'upstream'
}

/**
 * 这些 reason 不弹 callout，而是把页面切成对应的不可用态（规范 §7.1 / §7.2 的 **P** / **U** 位置）。
 */
const STATE_REASONS = new Set([
  'ASYNC_IMAGE_DISABLED',
  'CREATION_STORAGE_DISABLED',
])

export function isUnavailableStateReason(reason: string): boolean {
  return STATE_REASONS.has(reason)
}

/** 提交时这两类错误要把对应缩略图标成「已过期」（规范 §4.5、契约 §2.5）。 */
export function isUploadExpiredReason(reason: string): boolean {
  return reason === 'CREATION_UPLOAD_NOT_FOUND' || reason === 'CREATION_UPLOAD_EXPIRED'
}

/** 可原位重试的上传失败（502 写存储失败 / 网络中断），规范 §4.5。 */
export function isRetryableUploadReason(reason: string, status: number): boolean {
  if (reason === 'CREATION_UPLOAD_STORAGE_FAILED') return true
  return status === 0
}
