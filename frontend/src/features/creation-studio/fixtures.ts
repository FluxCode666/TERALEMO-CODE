/**
 * 创作页 mock 数据（与契约示例一致）。
 *
 * 用途：后端联调前跑通页面、QA 用 `vi.mock('./api')` 或直接引用这里的数据做断言。
 * 页面代码**不引用**本文件；开发时临时把 `api.ts` 的实现换成 `mockCreationApi` 即可。
 */
import type {
  CreationEstimateResponse,
  CreationModelOption,
  CreationOptionsResponse,
  CreationParamDescriptor,
  CreationSubmitResponse,
  CreationUpload,
} from './types'
import type { CreationApiError, CreationStudioApi } from './api'

const ACCEPT = ['image/png', 'image/jpeg', 'image/webp']

function labelKey(key: string): string {
  return `creationStudio.params.${key}`
}

function imageDescriptor(
  key: 'source' | 'first_frame' | 'last_frame' | 'reference',
  maxCount: number,
  extra: { required?: boolean; disabled?: boolean } = {},
): CreationParamDescriptor {
  return {
    key,
    kind: 'image',
    labelKey: labelKey(key),
    maxCount,
    accept: [...ACCEPT],
    maxSizeMB: 10,
    required: extra.required ?? false,
    ...(extra.disabled ? { disabled: true, disabledReason: 'storage_disabled' as const } : {}),
  }
}

function openAIImageModel(disabled = false): CreationModelOption {
  return {
    id: 'image:openai:gpt-image-2',
    mode: 'image',
    protocol: 'openai',
    model: 'gpt-image-2',
    display_name: 'GPT Image 2',
    params: [
      {
        key: 'size',
        kind: 'select',
        labelKey: labelKey('size'),
        options: [
          { value: '1024x1024' },
          { value: '1536x1024' },
          { value: '1024x1536' },
          { value: 'auto' },
        ],
        default: '1024x1024',
      },
      {
        key: 'quality',
        kind: 'select',
        labelKey: labelKey('quality'),
        options: [{ value: 'auto' }, { value: 'low' }, { value: 'medium' }, { value: 'high' }],
        default: 'auto',
      },
      { key: 'n', kind: 'integer', labelKey: labelKey('n'), min: 1, max: 4, step: 1, default: 1 },
      imageDescriptor('source', 1, { disabled }),
    ],
    pricing: { priced: true, mode: 'image', unit_price: 0.04 },
    partial: false,
  }
}

function grokImageModel(disabled = false): CreationModelOption {
  return {
    id: 'image:grok:grok-imagine-image',
    mode: 'image',
    protocol: 'grok',
    model: 'grok-imagine-image',
    display_name: 'Grok Imagine',
    params: [
      {
        key: 'aspect_ratio',
        kind: 'ratio',
        labelKey: labelKey('aspect_ratio'),
        options: [
          { value: '1:1', w: 1, h: 1 },
          { value: '16:9', w: 16, h: 9 },
          { value: '9:16', w: 9, h: 16 },
          { value: '4:3', w: 4, h: 3 },
          { value: '3:4', w: 3, h: 4 },
        ],
        default: '1:1',
      },
      {
        key: 'resolution',
        kind: 'select',
        labelKey: labelKey('resolution'),
        options: [{ value: '1k' }, { value: '2k' }],
        default: '1k',
      },
      { key: 'n', kind: 'integer', labelKey: labelKey('n'), min: 1, max: 4, step: 1, default: 1 },
      imageDescriptor('source', 1, { disabled }),
    ],
    pricing: { priced: true, mode: 'image', unit_price: 0.02 },
    partial: false,
  }
}

function seedanceModel(disabled = false): CreationModelOption {
  return {
    id: 'video:seedance:doubao-seedance-1-0-pro',
    mode: 'video',
    protocol: 'seedance',
    model: 'doubao-seedance-1-0-pro',
    display_name: 'Seedance 1.0 Pro',
    params: [
      {
        key: 'resolution',
        kind: 'select',
        labelKey: labelKey('resolution'),
        options: [{ value: '480p' }, { value: '720p' }, { value: '1080p' }],
        default: '720p',
      },
      {
        key: 'duration',
        kind: 'integer',
        labelKey: labelKey('duration'),
        min: 4,
        max: 12,
        step: 1,
        unitKey: 'creationStudio.units.seconds',
        default: 5,
      },
      {
        key: 'ratio',
        kind: 'ratio',
        labelKey: labelKey('ratio'),
        options: [
          { value: '16:9', w: 16, h: 9 },
          { value: '9:16', w: 9, h: 16 },
          { value: '4:3', w: 4, h: 3 },
          { value: '3:4', w: 3, h: 4 },
          { value: '1:1', w: 1, h: 1 },
          { value: '21:9', w: 21, h: 9 },
          { value: 'adaptive', w: 1, h: 1 },
        ],
        default: '16:9',
      },
      {
        key: 'watermark',
        kind: 'select',
        labelKey: labelKey('watermark'),
        options: [{ value: 'false' }, { value: 'true' }],
        default: 'false',
      },
      imageDescriptor('first_frame', 1, { disabled }),
      imageDescriptor('last_frame', 1, { disabled }),
      imageDescriptor('reference', 4, { disabled }),
    ],
    pricing: { priced: true, mode: 'video', unit_price: 0.2 },
    partial: false,
  }
}

function geminiModel(disabled = false): CreationModelOption {
  return {
    id: 'video:gemini:veo-3.1-generate-001',
    mode: 'video',
    protocol: 'gemini',
    model: 'veo-3.1-generate-001',
    display_name: 'Veo 3.1',
    params: [
      {
        key: 'resolution',
        kind: 'select',
        labelKey: labelKey('resolution'),
        options: [{ value: '720p' }, { value: '1080p' }],
        default: '720p',
      },
      {
        key: 'duration',
        kind: 'integer',
        labelKey: labelKey('duration'),
        min: 4,
        max: 8,
        step: 2,
        unitKey: 'creationStudio.units.seconds',
        default: 8,
      },
      {
        key: 'aspect_ratio',
        kind: 'ratio',
        labelKey: labelKey('aspect_ratio'),
        options: [
          { value: '16:9', w: 16, h: 9 },
          { value: '9:16', w: 9, h: 16 },
        ],
        default: '16:9',
      },
      imageDescriptor('first_frame', 1, { disabled }),
      imageDescriptor('reference', 3, { disabled }),
    ],
    pricing: { priced: true, mode: 'video', unit_price: 0.4 },
    partial: true,
  }
}

function minimaxModel(disabled = false): CreationModelOption {
  return {
    id: 'video:minimax:MiniMax-Hailuo-2.3',
    mode: 'video',
    protocol: 'minimax',
    model: 'MiniMax-Hailuo-2.3',
    display_name: 'Hailuo 2.3',
    params: [
      {
        key: 'resolution',
        kind: 'select',
        labelKey: labelKey('resolution'),
        options: [{ value: '768p' }, { value: '1080p' }],
        default: '768p',
      },
      {
        key: 'duration',
        kind: 'integer',
        labelKey: labelKey('duration'),
        min: 6,
        max: 10,
        step: 4,
        unitKey: 'creationStudio.units.seconds',
        default: 6,
      },
      imageDescriptor('first_frame', 1, { disabled }),
      imageDescriptor('last_frame', 1, { disabled }),
    ],
    pricing: { priced: true, mode: 'video', unit_price: 0.15 },
    partial: false,
  }
}

function grokVideoModel(disabled = false): CreationModelOption {
  return {
    id: 'video:grok:grok-imagine-video',
    mode: 'video',
    protocol: 'grok',
    model: 'grok-imagine-video',
    display_name: 'Grok Imagine Video',
    params: [
      {
        key: 'duration',
        kind: 'integer',
        labelKey: labelKey('duration'),
        min: 1,
        max: 15,
        step: 1,
        unitKey: 'creationStudio.units.seconds',
        default: 8,
      },
      {
        key: 'resolution',
        kind: 'select',
        labelKey: labelKey('resolution'),
        options: [
          { value: '480p' },
          { value: '720p' },
          { value: '768p' },
          { value: '1080p' },
          { value: '2k' },
        ],
        default: '480p',
      },
      imageDescriptor('first_frame', 1, { disabled }),
    ],
    pricing: { priced: true, mode: 'video', unit_price: 0.1 },
    partial: false,
  }
}

const UPLOAD_LIMITS = {
  max_bytes: 10485760,
  accept: [...ACCEPT],
  max_side_px: 4096,
  max_pixels: 16777216,
}

/** openai Key：可生图、无视频账号、存储可用。 */
export const optionsOpenAI: CreationOptionsResponse = {
  api_key: { id: 7, name: 'Studio key', group_id: 3, group_name: 'OpenAI Standard', platform: 'openai' },
  image: { available: true },
  video: { available: false, reason: 'no_accounts' },
  uploads: { available: true },
  upload_limits: { ...UPLOAD_LIMITS },
  models: [openAIImageModel(), grokImageModel()],
}

/** video Key：可生视频（四种协议），生图平台不支持。 */
export const optionsVideo: CreationOptionsResponse = {
  api_key: { id: 8, name: 'Video key', group_id: 5, group_name: 'Video Pro', platform: 'video' },
  image: { available: false, reason: 'platform_unsupported' },
  video: { available: true },
  uploads: { available: true },
  upload_limits: { ...UPLOAD_LIMITS },
  models: [seedanceModel(), geminiModel(), minimaxModel(), grokVideoModel()],
}

/** 对象存储关闭：生图与上传不可用，纯文生视频仍可用。 */
export const optionsStorageDisabled: CreationOptionsResponse = {
  api_key: { id: 9, name: 'No storage key', group_id: 6, group_name: 'Video Basic', platform: 'video' },
  image: { available: false, reason: 'storage_disabled' },
  video: { available: true },
  uploads: { available: false, reason: 'storage_disabled' },
  upload_limits: { ...UPLOAD_LIMITS },
  models: [seedanceModel(true), geminiModel(true)],
}

/** Key 不可用：三者同时给出 `key_unavailable`。 */
export const optionsKeyUnavailable: CreationOptionsResponse = {
  api_key: { id: 10, name: 'Expired key', group_id: 3, group_name: 'OpenAI Standard', platform: 'openai' },
  image: { available: false, reason: 'key_unavailable' },
  video: { available: false, reason: 'key_unavailable' },
  uploads: { available: false, reason: 'key_unavailable' },
  upload_limits: { ...UPLOAD_LIMITS },
  models: [],
}

export const estimateSufficient: CreationEstimateResponse = {
  priced: true,
  estimated_cost: 0.08,
  cost_basis: 'image_tier',
  selected_tier: '2K',
  pricing_mode: 'image',
  pricing_source: 'group',
  unit_price: 0.04,
  detail: [
    { label: 'count', value: '2' },
    { label: 'unit_price', value: '0.04' },
    { label: 'multiplier', value: '1' },
  ],
  balance_scope: 'personal',
  available_balance: 12.5,
  minimum_reserve: 0.1,
  sufficient: true,
  currency: 'USD',
}

export const estimateInsufficient: CreationEstimateResponse = {
  ...estimateSufficient,
  estimated_cost: 24,
  available_balance: 3.2,
  sufficient: false,
}

export const estimateUnpriced: CreationEstimateResponse = {
  priced: false,
  estimated_cost: 0,
  cost_basis: 'video_reservation',
  pricing_mode: 'video',
  unit_price: null,
  detail: [],
  balance_scope: 'enterprise',
  available_balance: null,
  minimum_reserve: 0,
  sufficient: null,
  currency: 'USD',
}

export const estimateVideo: CreationEstimateResponse = {
  priced: true,
  estimated_cost: 1.2,
  cost_basis: 'video_reservation',
  pricing_mode: 'video',
  pricing_source: 'channel',
  unit_price: 0.2,
  detail: [
    { label: 'duration', value: '6' },
    { label: 'resolution', value: '720p' },
    { label: 'unit_price', value: '0.2' },
  ],
  balance_scope: 'personal',
  available_balance: 12.5,
  minimum_reserve: 0.1,
  sufficient: true,
  currency: 'USD',
}

export const uploadFixture: CreationUpload = {
  upload_id: `cupl_${'a1b2c3d4e5f60718293a4b5c6d7e8f90'}`,
  url: 'https://img.example.com/images/creation-uploads/42/20261107/cupl_a1b2c3d4.png',
  content_type: 'image/png',
  size_bytes: 204800,
  width: 1024,
  height: 1024,
  created_at: 1794038400,
  expires_at: 1794124800,
}

export const submitImageFixture: CreationSubmitResponse = {
  task_id: 'imgtask_01hzy3',
  catalog_id: 'imgtask_01hzy3',
  mode: 'image',
  protocol: 'openai',
  model: 'gpt-image-2',
  status: 'processing',
  estimated_cost: 0.08,
  created_at: 1794038400,
  expires_at: 1794124800,
}

export const submitVideoFixture: CreationSubmitResponse = {
  task_id: 'seedance:01hzy4',
  catalog_id: `vidtask_${'4f3c2b1a'.repeat(8)}`,
  mode: 'video',
  protocol: 'seedance',
  model: 'doubao-seedance-1-0-pro',
  status: 'processing',
  estimated_cost: 1.2,
  created_at: 1794038400,
  expires_at: 1794124800,
}

/** 契约 §6.1 的错误响应样例（与 `CreationApiError` 同形状）。 */
export const errors: Record<'402' | '403' | '429' | '410', CreationApiError> = {
  '402': {
    status: 402,
    reason: 'insufficient_balance',
    message: 'Available balance does not cover the video reservation',
    metadata: {},
  },
  '403': {
    status: 403,
    reason: 'INSUFFICIENT_BALANCE',
    message: 'insufficient balance',
    metadata: {},
  },
  '429': {
    status: 429,
    reason: 'rate_limit_exceeded',
    message: 'Too many requests',
    metadata: { retry_after: '30' },
  },
  '410': {
    status: 410,
    reason: 'CREATION_UPLOAD_EXPIRED',
    message: 'creation upload expired',
    metadata: {},
  },
}

/** 开发/QA 用的假实现：替换 `api.ts` 的 `creationStudioApi` 即可离线跑页面。 */
export const mockCreationApi: CreationStudioApi = {
  async getOptions(apiKeyId: number) {
    if (apiKeyId === 8) return optionsVideo
    if (apiKeyId === 9) return optionsStorageDisabled
    if (apiKeyId === 10) return optionsKeyUnavailable
    return optionsOpenAI
  },
  async estimate(body) {
    if (body.mode === 'video') return estimateVideo
    return estimateSufficient
  },
  async upload(file: File) {
    return { ...uploadFixture, size_bytes: file.size }
  },
  async submit(body) {
    return body.mode === 'video' ? submitVideoFixture : submitImageFixture
  },
}
