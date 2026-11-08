/**
 * F6：按 `api_key_id` 加载并缓存 options。
 *
 * 切换 Key 时取消旧请求、立刻清空模型（避免旧模型带着旧参数被提交）；
 * 只有最后一次请求的响应会被采用。
 */
import { ref } from 'vue'
import { creationStudioApi, isCreationRequestCanceled, toCreationApiError, type CreationApiError } from './api'
import type { CreationOptionsResponse } from './types'

export function useCreationOptions() {
  const cache = new Map<number, CreationOptionsResponse>()
  const options = ref<CreationOptionsResponse | null>(null)
  const loading = ref(false)
  const error = ref<CreationApiError | null>(null)

  let controller: AbortController | null = null
  let generation = 0

  function cancel() {
    controller?.abort()
    controller = null
  }

  async function load(apiKeyId: number, force = false) {
    cancel()
    const current = ++generation
    const cached = cache.get(apiKeyId)
    error.value = null
    // 命中缓存时先渲染，再后台刷新，避免切 Key 时表单闪一下空态。
    options.value = cached ?? null
    if (!force && cached) return
    loading.value = true
    const requestController = new AbortController()
    controller = requestController
    try {
      const result = await creationStudioApi.getOptions(apiKeyId, requestController.signal)
      if (current !== generation) return
      cache.set(apiKeyId, result)
      options.value = result
    } catch (caught) {
      if (current !== generation || isCreationRequestCanceled(caught)) return
      error.value = toCreationApiError(caught)
      options.value = null
    } finally {
      if (current === generation) loading.value = false
    }
  }

  /** options 已过时（例如提交返回 storage_disabled）：清掉该 Key 的缓存后重拉。 */
  async function refresh(apiKeyId: number) {
    cache.delete(apiKeyId)
    await load(apiKeyId, true)
  }

  function invalidate(apiKeyId?: number) {
    if (apiKeyId === undefined) cache.clear()
    else cache.delete(apiKeyId)
  }

  function dispose() {
    generation += 1
    cancel()
  }

  return { options, loading, error, load, refresh, invalidate, dispose }
}
