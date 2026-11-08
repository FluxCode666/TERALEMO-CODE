/**
 * F10：提交。
 *
 * - 同一时间只允许一个提交在途（连续点击只发一次请求）。
 * - 成功后由调用方写快照并在任务流顶部插入卡片；失败保留用户输入。
 * - 404/410 上传类错误会把对应缩略图标成过期（规范 §4.5）。
 */
import { computed, ref } from 'vue'
import { creationStudioApi, toCreationApiError, type CreationApiError } from './api'
import { errorKeyForReason, isUploadExpiredReason } from './errorKeys'
import type { CreationErrorKey, CreationSubmitRequest, CreationSubmitResponse } from './types'

export interface UseCreationSubmitDeps {
  buildRequest: () => CreationSubmitRequest | null
  onSuccess: (response: CreationSubmitResponse, request: CreationSubmitRequest) => void
  onUploadExpired?: () => void
}

export function useCreationSubmit(deps: UseCreationSubmitDeps) {
  const submitting = ref(false)
  const error = ref<CreationApiError | null>(null)

  const errorKey = computed<CreationErrorKey | null>(() =>
    error.value ? errorKeyForReason(error.value.reason, error.value.status) : null,
  )
  const errorRetryAfter = computed(() => {
    const raw = error.value?.metadata.retry_after
    if (!raw) return 0
    const parsed = Number.parseInt(raw, 10)
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
  })

  function clearError() {
    error.value = null
  }

  async function submit(): Promise<boolean> {
    if (submitting.value) return false
    const request = deps.buildRequest()
    if (!request) return false
    submitting.value = true
    error.value = null
    try {
      const response = await creationStudioApi.submit(request)
      deps.onSuccess(response, request)
      return true
    } catch (caught) {
      const normalized = toCreationApiError(caught)
      error.value = normalized
      if (isUploadExpiredReason(normalized.reason)) deps.onUploadExpired?.()
      return false
    } finally {
      submitting.value = false
    }
  }

  return { submitting, error, errorKey, errorRetryAfter, submit, clearError }
}
