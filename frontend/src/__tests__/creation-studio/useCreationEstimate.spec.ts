import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { creationStudioApi } from '@/features/creation-studio/api'
import { useCreationEstimate } from '@/features/creation-studio/useCreationEstimate'
import { estimateSufficient, estimateInsufficient, estimateUnpriced } from '@/features/creation-studio/fixtures'
import type { CreationEstimateRequest, CreationEstimateResponse } from '@/features/creation-studio/types'
import { deferred, request, setupComposable } from './helpers'

beforeEach(() => { vi.useFakeTimers(); vi.spyOn(creationStudioApi, 'estimate').mockResolvedValue(estimateSufficient) })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('useCreationEstimate (F9)', () => {
  it('debounces all request inputs for 300ms and sends the newest params/images', async () => {
    const body = ref<CreationEstimateRequest>(request())
    setupComposable(() => useCreationEstimate(() => body.value))
    await vi.advanceTimersByTimeAsync(299)
    expect(creationStudioApi.estimate).not.toHaveBeenCalled()
    body.value = { ...body.value, params: { n: 2 }, images: [{ role: 'source', upload_id: 'cupl_qa' }] }
    await nextTick()
    await vi.advanceTimersByTimeAsync(299)
    expect(creationStudioApi.estimate).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(creationStudioApi.estimate).toHaveBeenCalledTimes(1)
    expect(creationStudioApi.estimate).toHaveBeenCalledWith(body.value, expect.any(AbortSignal))
  })

  it.each([
    ['unpriced', estimateUnpriced, false], ['insufficient', estimateInsufficient, false],
    ['sufficient', estimateSufficient, true], ['enterprise', { ...estimateSufficient, sufficient: null, balance_scope: 'enterprise' }, true],
    ['subscription', { ...estimateSufficient, sufficient: null, balance_scope: 'subscription' }, true],
  ] as const)('%s sets canSubmit=%s', async (_name, result, allowed) => {
    vi.mocked(creationStudioApi.estimate).mockResolvedValue(result)
    const state = setupComposable(() => useCreationEstimate(() => request()))
    await vi.advanceTimersByTimeAsync(300)
    expect(state.canSubmit.value).toBe(allowed)
  })

  it('hides balance but permits submission after estimate failure', async () => {
    vi.mocked(creationStudioApi.estimate).mockRejectedValue({ status: 503, reason: 'CREATION_ESTIMATE_UNAVAILABLE' })
    const state = setupComposable(() => useCreationEstimate(() => request()))
    await vi.advanceTimersByTimeAsync(300)
    expect(state.balanceHidden.value).toBe(true)
    expect(state.canSubmit.value).toBe(true)
    expect(state.costUnavailable.value).toBe(true)
  })

  it('invalidates an in-flight result immediately when request inputs change', async () => {
    const old = deferred<CreationEstimateResponse>()
    vi.mocked(creationStudioApi.estimate).mockReturnValueOnce(old.promise).mockResolvedValueOnce(estimateInsufficient)
    const body = ref<CreationEstimateRequest | null>(request())
    const state = setupComposable(() => useCreationEstimate(() => body.value))
    await vi.advanceTimersByTimeAsync(300)
    const oldSignal = vi.mocked(creationStudioApi.estimate).mock.calls[0][1]!
    body.value = { ...request(), api_key_id: 8 }
    await nextTick()
    old.resolve(estimateSufficient)
    await Promise.resolve()
    expect(state.estimate.value).toBeNull()
    expect(oldSignal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(300)
    expect(state.estimate.value).toEqual(estimateInsufficient)
    body.value = null
    await nextTick()
    expect(state.estimate.value).toBeNull()
    expect(state.loading.value).toBe(false)
  })
})
