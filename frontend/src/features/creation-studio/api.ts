/**
 * 创作页 API 封装（契约 §0.1）。
 *
 * 全部走 `@/api/client` 的 `apiClient`：不出现 `fetch(`、`buildGatewayUrl`、`Authorization`。
 * 错误统一抛出 `CreationApiError`（带 `reason`、`status`、`metadata`），前端只按 `reason` 分支。
 */
import { apiClient } from '@/api/client'
import type {
  CreationEstimateRequest,
  CreationEstimateResponse,
  CreationOptionsResponse,
  CreationSubmitRequest,
  CreationSubmitResponse,
  CreationUpload,
} from './types'

/** 归一后的错误；`reason` 缺失时按契约 §7.2 的兜底规则推一个。 */
export interface CreationApiError {
  status: number
  reason: string
  message: string
  metadata: Record<string, string>
}

/** 契约 §5.6 的 `catalog_id` 可能为空串，调用方据此回落到 `/async-tasks?search=`。 */
export type CreationSubmitResult = CreationSubmitResponse

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

/**
 * 面板通用限流中间件（所有 /api/v1/user/* 共用）命中时只返回 429 + `{code:"RATE_LIMITED"}`，
 * 既没有 `reason`，`code` 还是字符串。前端在这里兜底成契约 §6.1 的 `rate_limit_exceeded`，
 * 不要求共用中间件为创作页改动。
 */
const RATE_LIMITED_CODES = new Set(['RATE_LIMITED', 'RATE_LIMIT_EXCEEDED'])

function fallbackReason(status: number): string {
  // 429 无 reason 时一律按限流处理，否则会落成 invalidRequest（“请求不合法”）误导用户。
  if (status === 429) return 'rate_limit_exceeded'
  if (status >= 400 && status < 500) return 'INVALID_CREATION_REQUEST'
  return 'upstream_error'
}

/** 优先用 `reason`；字符串 `code` 照旧透传，但限流码归一到 `rate_limit_exceeded`。 */
function resolveReason(status: number, rawReason: string, rawCode: string): string {
  if (rawReason) return rawReason
  if (RATE_LIMITED_CODES.has(rawCode)) return 'rate_limit_exceeded'
  if (rawCode) return rawCode
  return fallbackReason(status)
}

/** 把 axios 拦截器抛出的对象、以及任何意外错误，收敛成 `CreationApiError`。 */
export function toCreationApiError(error: unknown): CreationApiError {
  if (isRecord(error)) {
    const status = typeof error.status === 'number' ? error.status : 0
    const rawReason = typeof error.reason === 'string' && error.reason ? error.reason : ''
    const rawCode = typeof error.code === 'string' && error.code ? error.code : ''
    const reason = resolveReason(status, rawReason, rawCode)
    const message = typeof error.message === 'string' ? error.message : ''
    const metadata: Record<string, string> = {}
    if (isRecord(error.metadata)) {
      for (const [key, value] of Object.entries(error.metadata)) {
        if (value === null || value === undefined) continue
        metadata[key] = String(value)
      }
    }
    return { status, reason, message, metadata }
  }
  if (error instanceof Error) {
    return { status: 0, reason: fallbackReason(0), message: error.message, metadata: {} }
  }
  return { status: 0, reason: fallbackReason(0), message: '', metadata: {} }
}

/** 请求被主动取消（切换 Key、组件卸载）时不要当成失败展示。 */
export function isCreationRequestCanceled(error: unknown): boolean {
  if (!isRecord(error)) return false
  if (error.code === 'ERR_CANCELED') return true
  return error.name === 'CanceledError' || error.name === 'AbortError'
}

export interface CreationStudioApi {
  getOptions(apiKeyId: number, signal?: AbortSignal): Promise<CreationOptionsResponse>
  estimate(body: CreationEstimateRequest, signal?: AbortSignal): Promise<CreationEstimateResponse>
  upload(
    file: File,
    onProgress?: (percent: number) => void,
    signal?: AbortSignal,
  ): Promise<CreationUpload>
  submit(body: CreationSubmitRequest): Promise<CreationSubmitResult>
}

const BASE = '/user/creation'

async function getOptions(apiKeyId: number, signal?: AbortSignal): Promise<CreationOptionsResponse> {
  const { data } = await apiClient.get<CreationOptionsResponse>(`${BASE}/options`, {
    params: { api_key_id: apiKeyId },
    signal,
  })
  return data
}

async function estimate(
  body: CreationEstimateRequest,
  signal?: AbortSignal,
): Promise<CreationEstimateResponse> {
  const { data } = await apiClient.post<CreationEstimateResponse>(`${BASE}/estimate`, body, { signal })
  return data
}

async function upload(
  file: File,
  onProgress?: (percent: number) => void,
  signal?: AbortSignal,
): Promise<CreationUpload> {
  const form = new FormData()
  form.append('file', file)
  const { data } = await apiClient.post<CreationUpload>(`${BASE}/uploads`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 120000,
    signal,
    onUploadProgress: (event) => {
      if (!onProgress) return
      const total = event.total ?? 0
      onProgress(total > 0 ? Math.min(100, Math.round((event.loaded / total) * 100)) : 0)
    },
  })
  return data
}

async function submit(body: CreationSubmitRequest): Promise<CreationSubmitResult> {
  const { data } = await apiClient.post<CreationSubmitResult>(`${BASE}/tasks`, body)
  return data
}

/** 真实实现。开发/QA 需要 mock 时替换这一处导入即可（fixtures.ts 提供同形状的 mock）。 */
export const creationStudioApi: CreationStudioApi = { getOptions, estimate, upload, submit }
