/**
 * 创作页（Creation Studio）接口类型。
 *
 * 唯一依据：docs/CREATION_STUDIO.md v1.1 §2–§8。字段名与可选性必须与该契约逐字一致，
 * 前端不得自行增删字段或改可选性；契约未写的字段一律不要加。
 */

// ==================== 枚举（§3.1） ====================

export type CreationMode = 'image' | 'video'
export type ImageProtocol = 'openai' | 'grok'
export type VideoProtocol = 'seedance' | 'gemini' | 'minimax' | 'grok'
export type CreationProtocol = ImageProtocol | VideoProtocol

/** `kind=image` 描述符的 key 就是提交时 `images[].role` 的取值（§3.2）。 */
export type CreationImageRole = 'source' | 'first_frame' | 'last_frame' | 'reference'

export type CreationUnavailableReason =
  | 'no_group'
  | 'platform_unsupported'
  | 'storage_disabled'
  | 'permission_denied'
  | 'no_accounts'
  | 'key_unavailable'

/** 契约 §6.1 的前端 i18n key 后缀（`creationStudio.errors.*`）。 */
export type CreationErrorKey =
  | 'invalidRequest'
  | 'modelUnavailable'
  | 'pricingMissing'
  | 'balanceInsufficient'
  | 'balanceLow'
  | 'enterpriseBalance'
  | 'imageForbidden'
  | 'groupUnavailable'
  | 'enterpriseBlocked'
  | 'keyUnavailable'
  | 'subscriptionMissing'
  | 'ipRestricted'
  | 'keyNotFound'
  | 'uploadExpired'
  | 'uploadTooLarge'
  | 'bodyTooLarge'
  | 'uploadUnsupportedType'
  | 'uploadInvalidImage'
  | 'rateLimited'
  | 'quotaExceeded'
  | 'keyQuota'
  | 'enterpriseQuota'
  | 'uploadLimit'
  | 'upstream'
  | 'uploadFailed'
  | 'noCapacity'
  | 'billingUnavailable'
  | 'internal'

// ==================== §2 上传 ====================

export interface CreationUpload {
  upload_id: string
  url: string
  content_type: 'image/png' | 'image/jpeg' | 'image/webp'
  size_bytes: number
  width: number
  height: number
  created_at: number
  expires_at: number
}

// ==================== §3 options ====================

export interface CreationAvailability {
  available: boolean
  reason?: CreationUnavailableReason
}

export interface CreationUploadLimits {
  max_bytes: number
  accept: string[]
  max_side_px: number
  max_pixels: number
}

export interface CreationSelectOption {
  value: string
  labelKey?: string
  label?: string
  disabled?: boolean
  hintKey?: string
}

export interface CreationParamSelectDescriptor {
  key: string
  kind: 'select'
  labelKey: string
  options: CreationSelectOption[]
  default: string
  hintKey?: string
}

export interface CreationParamIntegerDescriptor {
  key: string
  kind: 'integer'
  labelKey: string
  min: number
  max: number
  step: number
  unitKey?: string
  default: number
  hintKey?: string
}

export interface CreationParamRatioOption {
  value: string
  w: number
  h: number
}

export interface CreationParamRatioDescriptor {
  key: string
  kind: 'ratio'
  labelKey: string
  options: CreationParamRatioOption[]
  default: string
  hintKey?: string
}

export interface CreationParamImageDescriptor {
  key: CreationImageRole
  kind: 'image'
  labelKey: string
  maxCount: number
  accept: string[]
  maxSizeMB: number
  required: boolean
  hintKey?: string
  disabled?: boolean
  disabledReason?: 'storage_disabled'
}

export type CreationParamDescriptor =
  | CreationParamSelectDescriptor
  | CreationParamIntegerDescriptor
  | CreationParamRatioDescriptor
  | CreationParamImageDescriptor

/** 前端按 `kind` 分发时用的判别集合；未知 kind 只告警并跳过（F7）。 */
export type CreationParamKind = CreationParamDescriptor['kind']

export interface CreationPricingHint {
  priced: boolean
  mode?: 'video' | 'per_request' | 'token' | 'image'
  unit_price?: number | null
}

export interface CreationModelOption {
  /** `${mode}:${protocol}:${model}` */
  id: string
  mode: CreationMode
  protocol: CreationProtocol
  model: string
  display_name: string
  params: CreationParamDescriptor[]
  pricing: CreationPricingHint
  partial: boolean
}

export interface CreationOptionsResponse {
  api_key: {
    id: number
    name: string
    group_id: number | null
    group_name: string
    platform: string
  }
  image: CreationAvailability
  video: CreationAvailability
  uploads: CreationAvailability
  upload_limits: CreationUploadLimits
  models: CreationModelOption[]
}

// ==================== §4 estimate ====================

export type CreationParamValues = Record<string, string | number>

export interface CreationImageRef {
  role: CreationImageRole
  upload_id: string
}

export interface CreationEstimateRequest {
  api_key_id: number
  mode: CreationMode
  model: string
  protocol: CreationProtocol
  params: CreationParamValues
  images?: CreationImageRef[]
}

export interface CreationCostDetail {
  label: 'count' | 'duration' | 'resolution' | 'unit_price' | 'multiplier'
  value: string
}

export interface CreationEstimateResponse {
  priced: boolean
  estimated_cost: number
  cost_basis: 'image_tier' | 'image_token_estimate' | 'video_reservation'
  selected_tier?: '1K' | '2K' | '4K'
  pricing_mode: 'image' | 'video' | 'per_request' | 'token'
  pricing_source?: 'channel' | 'group' | 'fallback' | 'default'
  unit_price?: number | null
  detail: CreationCostDetail[]
  balance_scope: 'personal' | 'enterprise' | 'subscription'
  available_balance: number | null
  minimum_reserve: number
  sufficient: boolean | null
  currency: 'USD'
}

// ==================== §5 submit ====================

export interface CreationSubmitRequest {
  api_key_id: number
  mode: CreationMode
  model: string
  protocol: CreationProtocol
  prompt: string
  params: CreationParamValues
  images?: CreationImageRef[]
}

export interface CreationSubmitResponse {
  task_id: string
  catalog_id: string
  mode: CreationMode
  protocol: CreationProtocol
  model: string
  status: 'processing'
  estimated_cost: number
  created_at: number
  expires_at: number
}

// ==================== §8 本地快照 ====================

export interface CreationSnapshotImage {
  role: CreationImageRole
  upload_id: string
  url: string
  expires_at: number
  width: number
  height: number
}

export interface CreationSnapshot {
  catalog_id: string
  task_id: string
  mode: CreationMode
  api_key_id: number
  protocol: CreationProtocol
  model: string
  prompt: string
  params: CreationParamValues
  images: CreationSnapshotImage[]
  created_at: number
}

// ==================== 前端本地模型（不属于契约） ====================

/** `creationStudio.errors.*` 的映射目标（契约 §6.1）。 */
export interface CreationErrorDescriptor {
  key: CreationErrorKey
  /** 后端 `message`，原样展示（英文原文也照常展示）。 */
  message?: string
  status?: number
  metadata?: Record<string, string>
}

/** 任务卡片与历史流的统一视图模型。 */
export type CreationTaskStatus =
  | 'submitting'
  | 'queued'
  | 'processing'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'expired'

export interface CreationTaskView {
  /** 异步任务目录 ID；极端竞态下可能为空字符串（契约 §5.6）。 */
  catalogId: string
  taskId: string
  kind: CreationMode
  protocol: string
  model: string
  status: CreationTaskStatus
  prompt: string
  params: CreationParamValues
  /** 请求的图片数量（骨架期用来占位）。 */
  imageCount: number
  /** 全部结果链接（图片用 `result_urls`，视频用 `result_url`）。 */
  urls: string[]
  error: string
  createdAt: number
  completedAt: number
  expiresAt: number
  /** 本地快照；没有快照时为空，编辑对照因此不可用（规范 §4.9）。 */
  snapshot: CreationSnapshot | null
  /** 本次会话刚提交的卡片，入场时播一次 `cs-enter`。 */
  fresh?: boolean
}

/** 灯箱条目（前端本地，不属于契约）。 */
export interface CreationLightboxItem {
  url: string
  kind: 'image' | 'video'
  tag?: string
  alt?: string
  caption?: string
}
