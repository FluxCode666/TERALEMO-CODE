import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { creationStudioApi } from '@/features/creation-studio/api'
import { useCreationUploads } from '@/features/creation-studio/useCreationUploads'
import { optionsOpenAI, uploadFixture } from '@/features/creation-studio/fixtures'
import type { CreationUpload } from '@/features/creation-studio/types'
import { deferred, NOW, setupComposable } from './helpers'

const file = () => new File(['qa'], 'qa.png', { type: 'image/png' })
function setup(available = true) {
  return setupComposable(() => useCreationUploads({
    limits: computed(() => optionsOpenAI.upload_limits), available: computed(() => available),
  }))
}
beforeEach(() => {
  vi.useFakeTimers(); vi.setSystemTime(NOW * 1000)
  vi.spyOn(creationStudioApi, 'upload').mockResolvedValue({ ...uploadFixture })
})
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('useCreationUploads (F8)', () => {
  it.each([
    [new File(['qa'], 'qa.gif', { type: 'image/gif' }), 'uploadUnsupportedType'],
    [new File([new Uint8Array(10485761)], 'qa.png', { type: 'image/png' }), 'uploadTooLarge'],
  ] as const)('prevalidates invalid file #%# without network or a thumbnail', (input, key) => {
    const state = setup()
    state.pick('source', [input], 1)
    expect(creationStudioApi.upload).not.toHaveBeenCalled()
    expect(state.entries.value).toEqual([])
    expect(state.errors.value[0].errorKey).toBe(key)
  })

  it('does not upload while unavailable and enforces the per-role image cap', async () => {
    setup(false).pick('source', [file()], 1)
    expect(creationStudioApi.upload).not.toHaveBeenCalled()
    const state = setup()
    state.pick('source', [file(), file()], 1)
    await flushPromises()
    expect(creationStudioApi.upload).toHaveBeenCalledTimes(1)
    expect(state.imageRefs.value).toEqual([{ role: 'source', upload_id: uploadFixture.upload_id }])
  })

  it('uploads multiple files sequentially with observable progress', async () => {
    const first = deferred<CreationUpload>()
    vi.mocked(creationStudioApi.upload).mockImplementationOnce((_file, progress) => {
      progress?.(42)
      return first.promise
    })
    const state = setup()
    state.pick('reference', [file(), file()], 4)
    expect(state.entries.value[0].progress).toBe(42)
    expect(creationStudioApi.upload).toHaveBeenCalledTimes(1)
    first.resolve(uploadFixture)
    await flushPromises()
    expect(creationStudioApi.upload).toHaveBeenCalledTimes(2)
    expect(state.uploading.value).toBe(false)
  })

  it('publishes reactive ready/uploading/imageRefs after upload resolves', async () => {
    const pending = deferred<CreationUpload>()
    vi.mocked(creationStudioApi.upload).mockReturnValueOnce(pending.promise)
    const state = setup()
    state.pick('source', [file()], 1)
    expect(state.uploading.value).toBe(true)
    expect(state.imageRefs.value).toEqual([])
    pending.resolve(uploadFixture)
    await flushPromises()
    expect(state.entries.value[0].status).toBe('ready')
    expect(state.uploading.value).toBe(false)
    expect(state.imageRefs.value).toEqual([{ role: 'source', upload_id: uploadFixture.upload_id }])
  })

  it.each([
    [413, 'CREATION_UPLOAD_TOO_LARGE', 'uploadTooLarge'],
    [415, 'CREATION_UPLOAD_UNSUPPORTED_TYPE', 'uploadUnsupportedType'],
    [422, 'CREATION_UPLOAD_INVALID_IMAGE', 'uploadInvalidImage'],
    [429, 'CREATION_UPLOAD_LIMIT_EXCEEDED', 'uploadLimit'],
  ] as const)('%s %s removes placeholder and exposes field error %s', async (status, reason, key) => {
    vi.mocked(creationStudioApi.upload).mockRejectedValue({ status, reason, metadata: {} })
    const state = setup()
    state.pick('source', [file()], 1)
    await flushPromises()
    expect(state.entries.value).toEqual([])
    expect(state.errorsFor('source')[0].errorKey).toBe(key)
    if (status === 429) {
      expect(state.limitReached.value).toBe(true)
      state.pick('source', [file()], 1)
      expect(creationStudioApi.upload).toHaveBeenCalledTimes(1)
    }
  })

  it('honors upload invalid-image metadata.max_side_px', async () => {
    vi.mocked(creationStudioApi.upload).mockRejectedValue({ status: 422, reason: 'CREATION_UPLOAD_INVALID_IMAGE', metadata: { max_side_px: '2048' } })
    const state = setup()
    state.pick('source', [file()], 1)
    await flushPromises()
    expect(Number(state.errors.value[0].params?.maxSide)).toBe(2048)
    expect(state.errors.value[0].params?.maxPixels).toBe(16777216)
  })

  it.each([
    [502, 'CREATION_UPLOAD_STORAGE_FAILED'], [0, 'upstream_error'],
  ] as const)('keeps retryable %s failure in place with uploadFailed copy', async (status, reason) => {
    vi.mocked(creationStudioApi.upload).mockRejectedValueOnce({ status, reason })
    const state = setup()
    state.pick('source', [file()], 1)
    await flushPromises()
    expect(state.entries.value[0]).toMatchObject({ status: 'failed', errorKey: 'uploadFailed', retryable: true })
    const id = state.entries.value[0].id
    state.retry(id)
    await flushPromises()
    expect(state.entries.value[0]).toMatchObject({ id, status: 'ready', progress: 100 })
  })

  it('treats 900 seconds as valid and 899 as expired, with timer checks', async () => {
    const state = setup()
    state.restore([{
      role: 'source', upload_id: uploadFixture.upload_id, url: uploadFixture.url,
      width: 1024, height: 1024, expires_at: NOW + 900,
    }])
    expect(state.expired.value).toBe(false)
    await vi.advanceTimersByTimeAsync(30000)
    expect(state.expired.value).toBe(true)
    expect(state.imageRefs.value).toEqual([])
    state.reset()
    state.restore([{ role: 'source', upload_id: uploadFixture.upload_id, url: uploadFixture.url,
      width: 1024, height: 1024, expires_at: NOW + 899 }])
    expect(state.expired.value).toBe(true)
  })

  it('marks server-rejected refs expired and removes them when uploads become unavailable', async () => {
    const state = setup()
    state.pick('source', [file()], 1)
    await flushPromises()
    state.markExpiredFromServer()
    expect(state.expired.value).toBe(true)
    state.pruneUnavailable()
    expect(state.entries.value).toEqual([])
    expect(state.removedUnavailable.value).toBe(true)
  })

  it('keeps local validation errors scoped to the originating image role', () => {
    const state = setup()
    state.pick('first_frame', [new File(['qa'], 'bad.gif', { type: 'image/gif' })], 1)
    expect(state.errorsFor('first_frame')).toHaveLength(1)
    expect(state.errorsFor('last_frame')).toHaveLength(0)
  })
})
