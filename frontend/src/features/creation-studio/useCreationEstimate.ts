/**
 * F9：预计费用（300ms 防抖）。
 *
 * 任一入参变化就重新估算；估算失败时只隐藏余额行，费用显示「暂无法估算」，
 * 并且**不禁用提交**（契约 §4.2：只有 `priced=false` 或 `sufficient=false` 才禁用）。
 */
import { computed, onUnmounted, ref, watch } from 'vue'
import { creationStudioApi, isCreationRequestCanceled, toCreationApiError, type CreationApiError } from './api'
import type { CreationEstimateRequest, CreationEstimateResponse } from './types'

export const ESTIMATE_DEBOUNCE_MS = 300

export function useCreationEstimate(buildRequest: () => CreationEstimateRequest | null) {
  const estimate = ref<CreationEstimateResponse | null>(null)
  const loading = ref(false)
  const error = ref<CreationApiError | null>(null)
  const balanceHidden = ref(false)

  let timer: ReturnType<typeof setTimeout> | null = null
  let controller: AbortController | null = null
  let generation = 0

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  async function run(request: CreationEstimateRequest) {
    controller?.abort()
    const current = ++generation
    const requestController = new AbortController()
    controller = requestController
    loading.value = true
    try {
      const result = await creationStudioApi.estimate(request, requestController.signal)
      if (current !== generation) return
      estimate.value = result
      error.value = null
      balanceHidden.value = false
    } catch (caught) {
      if (current !== generation || isCreationRequestCanceled(caught)) return
      error.value = toCreationApiError(caught)
      estimate.value = null
      balanceHidden.value = true
    } finally {
      if (current === generation) loading.value = false
    }
  }

  function schedule() {
    clearTimer()
    // 入参一变就让在途请求立即失效（R1-04）：旧响应不得在防抖窗口内写回。
    generation += 1
    controller?.abort()
    controller = null
    const request = buildRequest()
    if (!request) {
      estimate.value = null
      error.value = null
      loading.value = false
      balanceHidden.value = false
      return
    }
    loading.value = true
    timer = setTimeout(() => {
      timer = null
      void run(request)
    }, ESTIMATE_DEBOUNCE_MS)
  }

  /** 手动重算（提交失败后重试）。 */
  function refresh() {
    schedule()
  }

  const canSubmit = computed(() => {
    // 估算不可用不等于不能提交：以提交结果为准。
    if (!estimate.value) return true
    if (!estimate.value.priced) return false
    return estimate.value.sufficient !== false
  })

  const costUnavailable = computed(
    () => !loading.value && (!estimate.value || !estimate.value.priced),
  )

  watch(buildRequest, schedule, { deep: true, immediate: true })

  onUnmounted(() => {
    clearTimer()
    generation += 1
    controller?.abort()
  })

  return { estimate, loading, error, balanceHidden, canSubmit, costUnavailable, refresh }
}
