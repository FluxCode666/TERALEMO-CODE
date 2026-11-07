/**
 * F8：参考图 / 首帧 / 尾帧 / 编辑原图的上传。
 *
 * - 上传前按 `upload_limits` 做前端预检（后端仍会校验），不通过不发起请求。
 * - 契约 §2.1：一次只传一张，多文件按队列**逐张**上传，每张有自己的进度、取消与重试。
 * - 所有异步回调都通过 id 从 `entries.value` 取回响应式代理再修改，否则 computed 不更新。
 * - 剩余有效期不足 900 秒视为过期（契约 §2.5、§8.2 第 4 条），与真正到期区分。
 */
import { computed, onUnmounted, ref, type ComputedRef } from 'vue'
import { creationStudioApi, isCreationRequestCanceled, toCreationApiError } from './api'
import { errorKeyForReason, isRetryableUploadReason } from './errorKeys'
import type {
  CreationErrorKey,
  CreationImageRef,
  CreationImageRole,
  CreationSnapshotImage,
  CreationUpload,
  CreationUploadLimits,
} from './types'

/** 契约 §2.5：剩余有效期不足 15 分钟即视为过期。 */
export const UPLOAD_EXPIRY_MARGIN_SECONDS = 900

const ACCEPT_LABELS: Record<string, string> = {
  'image/png': 'PNG',
  'image/jpeg': 'JPEG',
  'image/webp': 'WebP',
}

export function acceptLabel(accept: string[]): string {
  return accept.map(value => ACCEPT_LABELS[value] ?? value).join(' / ')
}

export interface CreationImageEntry {
  id: string
  role: CreationImageRole
  /** queued = 已入队等待前一张传完（契约 §2.1 逐张上传）。 */
  status: 'queued' | 'uploading' | 'ready' | 'failed' | 'expired'
  /** 本地预览（object URL）；已过期时不再展示。 */
  previewUrl?: string
  url?: string
  uploadId?: string
  expiresAt?: number
  width?: number
  height?: number
  progress: number
  fileName?: string
  errorKey?: CreationErrorKey
  retryable?: boolean
  file?: File
}

export interface CreationFieldError {
  /** 稳定 id，供 `aria-describedby` 引用（规范 §7.1、§10）。 */
  id: string
  /** 错误归属的图片字段，字段内错误不会串到其它 role（规范 §4.5）。 */
  role: CreationImageRole
  entryId?: string
  fileName?: string
  errorKey: CreationErrorKey
  params?: Record<string, unknown>
}

export interface UseCreationUploadsDeps {
  limits: ComputedRef<CreationUploadLimits | undefined>
  available: ComputedRef<boolean>
  /** 上传返回 CREATION_STORAGE_DISABLED 时通知页面重新拉 options（规范 §7.1）。 */
  onUnavailable?: () => void
}

/** 传给 `ImageDropzone` 的字段级状态（视图按 role 组装）。 */
export interface ImageFieldBinding {
  entries: CreationImageEntry[]
  errors: CreationFieldError[]
  available: boolean
  uploading: boolean
  limitReached: boolean
  removedUnavailable: boolean
}

function megabytes(bytes: number): string {
  return String(Math.round((bytes / (1024 * 1024)) * 10) / 10)
}

function nowSeconds(): number {
  return Math.floor(Date.now() / 1000)
}

export function useCreationUploads(deps: UseCreationUploadsDeps) {
  const entries = ref<CreationImageEntry[]>([])
  const errors = ref<CreationFieldError[]>([])
  const limitReached = ref(false)
  const removedUnavailable = ref(false)

  const controllers = new Map<string, AbortController>()
  /** 串行上传队列：一次只有一张在途（契约 §2.1）。 */
  const queue: string[] = []
  let active = false
  let sequence = 0

  function entryById(id: string): CreationImageEntry | undefined {
    return entries.value.find(entry => entry.id === id)
  }

  function entriesFor(role: CreationImageRole): CreationImageEntry[] {
    return entries.value.filter(entry => entry.role === role)
  }

  function pushError(error: Omit<CreationFieldError, 'id'>) {
    sequence += 1
    errors.value = [...errors.value, { ...error, id: `cs-upload-error-${sequence}` }]
  }

  function clearErrors(role?: CreationImageRole) {
    errors.value = role ? errors.value.filter(error => error.role !== role) : []
  }

  function errorsFor(role: CreationImageRole): CreationFieldError[] {
    return errors.value.filter(error => error.role === role)
  }

  function errorParamsFor(
    key: CreationErrorKey,
    metadata: Record<string, string> = {},
  ): Record<string, unknown> | undefined {
    const limits = deps.limits.value
    if (key === 'uploadTooLarge') {
      const maxBytes = Number(metadata.max_bytes ?? limits?.max_bytes ?? 0)
      return { max: megabytes(maxBytes) }
    }
    if (key === 'uploadInvalidImage') {
      // metadata 优先，回落 upload_limits（规范 §7.1）。
      return {
        maxSide: metadata.max_side_px ?? limits?.max_side_px ?? 4096,
        maxPixels: limits?.max_pixels ?? 16777216,
      }
    }
    if (key === 'uploadUnsupportedType') return { types: acceptLabel(limits?.accept ?? []) }
    return undefined
  }

  function validateFile(file: File): { key: CreationErrorKey; params?: Record<string, unknown> } | null {
    const limits = deps.limits.value
    if (limits && file.size > limits.max_bytes) {
      return { key: 'uploadTooLarge', params: { max: megabytes(limits.max_bytes) } }
    }
    if (limits && limits.accept.length > 0 && !limits.accept.includes(file.type)) {
      return { key: 'uploadUnsupportedType', params: { types: acceptLabel(limits.accept) } }
    }
    return null
  }

  function createEntry(role: CreationImageRole, file: File): CreationImageEntry {
    sequence += 1
    return {
      id: `local-${Date.now()}-${sequence}`,
      role,
      status: 'queued',
      progress: 0,
      fileName: file.name,
      file,
      previewUrl: typeof URL !== 'undefined' && 'createObjectURL' in URL ? URL.createObjectURL(file) : undefined,
    }
  }

  function applyUpload(entry: CreationImageEntry, upload: CreationUpload) {
    entry.status = 'ready'
    entry.uploadId = upload.upload_id
    entry.url = upload.url
    entry.expiresAt = upload.expires_at
    entry.width = upload.width
    entry.height = upload.height
    entry.progress = 100
    entry.errorKey = undefined
    entry.retryable = false
  }

  function releasePreview(entry: CreationImageEntry) {
    if (entry.previewUrl) {
      URL.revokeObjectURL(entry.previewUrl)
      entry.previewUrl = undefined
    }
  }

  function removeEntry(id: string) {
    const entry = entryById(id)
    if (entry) releasePreview(entry)
    controllers.get(id)?.abort()
    controllers.delete(id)
    entries.value = entries.value.filter(item => item.id !== id)
  }

  async function runUpload(id: string): Promise<void> {
    const entry = entryById(id)
    if (!entry || !entry.file) return
    entry.status = 'uploading'
    entry.progress = 0
    entry.errorKey = undefined
    entry.retryable = false
    const controller = new AbortController()
    controllers.set(id, controller)
    try {
      const upload = await creationStudioApi.upload(
        entry.file,
        percent => {
          // 必须改代理：直接改闭包里的原始对象不会触发 computed 更新。
          const live = entryById(id)
          if (live) live.progress = percent
        },
        controller.signal,
      )
      const live = entryById(id)
      if (live) applyUpload(live, upload)
    } catch (caught) {
      if (isCreationRequestCanceled(caught)) return
      handleFailure(id, caught)
    } finally {
      controllers.delete(id)
    }
  }

  function handleFailure(id: string, caught: unknown) {
    const entry = entryById(id)
    const error = toCreationApiError(caught)
    if (error.reason === 'CREATION_STORAGE_DISABLED') {
      removeEntry(id)
      deps.onUnavailable?.()
      return
    }
    if (isRetryableUploadReason(error.reason, error.status)) {
      // 可重试（502 写存储失败 / 网络中断）：保留缩略图，原位重试（规范 §4.5）。
      if (entry) {
        entry.status = 'failed'
        entry.errorKey = 'uploadFailed'
        entry.retryable = true
      }
      return
    }
    const errorKey = errorKeyForReason(error.reason, error.status)
    const role = entry?.role
    const fileName = entry?.fileName
    removeEntry(id)
    if (role) {
      pushError({ role, fileName, errorKey, params: errorParamsFor(errorKey, error.metadata) })
    }
    if (error.reason === 'CREATION_UPLOAD_LIMIT_EXCEEDED') limitReached.value = true
  }

  function pump() {
    if (active) return
    const nextId = queue.shift()
    if (!nextId) return
    if (!entryById(nextId)) {
      pump()
      return
    }
    active = true
    void runUpload(nextId).finally(() => {
      active = false
      pump()
    })
  }

  /** 加入文件：先预检、再入队逐张上传。达到 maxCount 后多余文件忽略。 */
  function pick(role: CreationImageRole, files: FileList | File[], maxCount: number) {
    clearErrors(role)
    if (!deps.available.value || limitReached.value) return
    for (const file of Array.from(files)) {
      if (entriesFor(role).length >= maxCount) break
      const invalid = validateFile(file)
      if (invalid) {
        pushError({ role, fileName: file.name, errorKey: invalid.key, params: invalid.params })
        continue
      }
      const entry = createEntry(role, file)
      entries.value = [...entries.value, entry]
      queue.push(entry.id)
    }
    pump()
  }

  /** 原位重试（保留缩略图与位置）。 */
  function retry(id: string) {
    const entry = entryById(id)
    if (!entry || !entry.file) return
    entry.errorKey = undefined
    entry.retryable = false
    entry.status = 'queued'
    queue.unshift(id)
    pump()
  }

  /** 过期缩略图的「原位替换」：新图占回原位置。 */
  function replace(id: string, files: FileList | File[]) {
    const entry = entryById(id)
    const file = Array.from(files)[0]
    if (!entry || !file) return
    clearErrors(entry.role)
    const invalid = validateFile(file)
    if (invalid) {
      pushError({ role: entry.role, fileName: file.name, errorKey: invalid.key, params: invalid.params })
      return
    }
    releasePreview(entry)
    entry.file = file
    entry.fileName = file.name
    entry.previewUrl = typeof URL !== 'undefined' && 'createObjectURL' in URL ? URL.createObjectURL(file) : undefined
    entry.url = undefined
    entry.uploadId = undefined
    entry.expiresAt = undefined
    entry.width = undefined
    entry.height = undefined
    entry.errorKey = undefined
    entry.retryable = false
    entry.status = 'queued'
    queue.unshift(id)
    pump()
  }

  /** 图片加载失败（链接已失效）→ 该缩略图进「已过期」（规范 §4.5 过期来源③）。 */
  function markExpired(id: string) {
    const entry = entryById(id)
    if (!entry || entry.status !== 'ready') return
    entry.status = 'expired'
  }

  /** 定时检查：剩余不足 15 分钟的图转「已过期」，阻断提交。 */
  function refreshExpiry() {
    const timestamp = nowSeconds()
    for (const entry of entries.value) {
      if (entry.status === 'ready' && entry.expiresAt !== undefined && entry.expiresAt - timestamp < UPLOAD_EXPIRY_MARGIN_SECONDS) {
        entry.status = 'expired'
      }
    }
  }

  /** 提交返回 404/410 时调用：先标本地已不足 15 分钟的，一张都没有时标全部。 */
  function markExpiredFromServer() {
    refreshExpiry()
    if (entries.value.some(entry => entry.status === 'expired')) return
    for (const entry of entries.value) {
      if (entry.status === 'ready') entry.status = 'expired'
    }
  }

  /** options 显示上传不可用：移除已有缩略图（规范 §4.5）。 */
  function pruneUnavailable() {
    if (entries.value.length === 0) return
    for (const entry of entries.value) releasePreview(entry)
    entries.value = []
    errors.value = []
    queue.length = 0
    removedUnavailable.value = true
  }

  /** 复用参数：链接未过期的原样回填，不足 15 分钟的进「已过期」状态。 */
  function restore(images: CreationSnapshotImage[]) {
    for (const image of images) {
      sequence += 1
      const timestamp = nowSeconds()
      entries.value = [
        ...entries.value,
        {
          id: `restored-${Date.now()}-${sequence}`,
          role: image.role,
          status: image.expires_at - timestamp < UPLOAD_EXPIRY_MARGIN_SECONDS ? 'expired' : 'ready',
          progress: 100,
          url: image.url,
          uploadId: image.upload_id,
          expiresAt: image.expires_at,
          width: image.width,
          height: image.height,
        },
      ]
    }
  }

  function reset() {
    for (const entry of entries.value) releasePreview(entry)
    for (const controller of controllers.values()) controller.abort()
    controllers.clear()
    queue.length = 0
    active = false
    entries.value = []
    errors.value = []
    limitReached.value = false
    removedUnavailable.value = false
  }

  /** 提交用的 `images[]`（只含 ready 的图）。 */
  const imageRefs = computed<CreationImageRef[]>(() =>
    entries.value
      .filter(entry => entry.status === 'ready' && entry.uploadId)
      .map(entry => ({ role: entry.role, upload_id: entry.uploadId as string })),
  )

  const uploading = computed(() =>
    entries.value.some(entry => entry.status === 'uploading' || entry.status === 'queued'),
  )
  const failed = computed(() => entries.value.some(entry => entry.status === 'failed'))
  const expired = computed(() => entries.value.some(entry => entry.status === 'expired'))
  const hasImages = computed(() => entries.value.length > 0)

  const expiryTimer = setInterval(refreshExpiry, 30000)
  onUnmounted(() => {
    clearInterval(expiryTimer)
    for (const controller of controllers.values()) controller.abort()
    for (const entry of entries.value) releasePreview(entry)
  })

  return {
    entries,
    errors,
    limitReached,
    removedUnavailable,
    uploading,
    failed,
    expired,
    hasImages,
    imageRefs,
    entriesFor,
    errorsFor,
    clearErrors,
    pick,
    retry,
    replace,
    markExpired,
    remove: removeEntry,
    refreshExpiry,
    markExpiredFromServer,
    pruneUnavailable,
    restore,
    reset,
  }
}
