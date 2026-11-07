import { afterEach, describe, expect, it, vi } from 'vitest'
import { creationStudioApi } from '@/features/creation-studio/api'
import { useCreationSubmit } from '@/features/creation-studio/useCreationSubmit'
import { submitImageFixture, errors } from '@/features/creation-studio/fixtures'
import type { CreationSubmitResponse } from '@/features/creation-studio/types'
import { deferred, request } from './helpers'

afterEach(() => vi.restoreAllMocks())
describe('useCreationSubmit (F10)', () => {
  it('allows only one request in flight and reports success with the original form', async () => {
    const pending = deferred<CreationSubmitResponse>()
    vi.spyOn(creationStudioApi, 'submit').mockReturnValue(pending.promise)
    const body = request()
    const onSuccess = vi.fn()
    const state = useCreationSubmit({ buildRequest: () => body, onSuccess })
    const first = state.submit()
    expect(state.submitting.value).toBe(true)
    expect(await state.submit()).toBe(false)
    expect(creationStudioApi.submit).toHaveBeenCalledTimes(1)
    pending.resolve(submitImageFixture)
    expect(await first).toBe(true)
    expect(onSuccess).toHaveBeenCalledTimes(1)
    expect(onSuccess).toHaveBeenCalledWith(submitImageFixture, body)
    expect(state.submitting.value).toBe(false)
  })

  it('does not submit an invalid form', async () => {
    vi.spyOn(creationStudioApi, 'submit')
    expect(await useCreationSubmit({ buildRequest: () => null, onSuccess: vi.fn() }).submit()).toBe(false)
    expect(creationStudioApi.submit).not.toHaveBeenCalled()
  })

  it.each([
    [errors['402'], 'balanceInsufficient'], [errors['403'], 'balanceLow'], [errors['429'], 'rateLimited'],
  ] as const)('preserves input and maps $reason to %s', async (error, key) => {
    vi.spyOn(creationStudioApi, 'submit').mockRejectedValue(error)
    const body = request()
    const onSuccess = vi.fn()
    const state = useCreationSubmit({ buildRequest: () => body, onSuccess })
    expect(await state.submit()).toBe(false)
    expect(state.errorKey.value).toBe(key)
    expect(state.error.value).toEqual(error)
    expect(body).toEqual(request())
    expect(onSuccess).not.toHaveBeenCalled()
    expect(state.submitting.value).toBe(false)
    state.clearError()
    expect(state.error.value).toBeNull()
  })

  it.each(['CREATION_UPLOAD_NOT_FOUND', 'CREATION_UPLOAD_EXPIRED'])('marks thumbnails expired for %s', async reason => {
    vi.spyOn(creationStudioApi, 'submit').mockRejectedValue({ ...errors['410'], reason })
    const onUploadExpired = vi.fn()
    const state = useCreationSubmit({ buildRequest: () => request(), onSuccess: vi.fn(), onUploadExpired })
    await state.submit()
    expect(onUploadExpired).toHaveBeenCalledTimes(1)
    expect(state.errorKey.value).toBe('uploadExpired')
  })
})
