import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { asyncTasksAPI } from '@/api/asyncTasks'
import { useCreationFeed } from '@/features/creation-studio/useCreationFeed'
import { NOW, page, setupComposable, task } from './helpers'

function setup() {
  return setupComposable(() => useCreationFeed({ snapshotFor: () => undefined, autoRefresh: ref(true) }))
}
beforeEach(() => {
  vi.useFakeTimers(); vi.setSystemTime(NOW * 1000)
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.spyOn(asyncTasksAPI, 'list').mockResolvedValue(page([]))
  vi.spyOn(asyncTasksAPI, 'refresh').mockImplementation(async id => task({ id, kind: 'video', status: 'processing' }))
})
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('useCreationFeed (F11)', () => {
  it('appends load-more results without clearing prior cards or duplicating ids', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValueOnce(page([task()], 3))
      .mockResolvedValueOnce(page([task(), task({ id: 'second' })], 3, 2))
    const state = setup()
    await state.load()
    await state.loadMore()
    expect(state.items.value.map(item => item.catalogId)).toEqual(['imgtask_qa', 'second'])
    expect(asyncTasksAPI.list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 20 }), expect.any(AbortSignal))
  })

  it('retains old results when refresh or load-more fails', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValueOnce(page([task()], 2)).mockRejectedValue(new Error('offline'))
    const state = setup()
    await state.load()
    await state.loadMore()
    expect(state.items.value.map(item => item.catalogId)).toEqual(['imgtask_qa'])
    await state.refreshLoaded()
    expect(state.items.value.map(item => item.catalogId)).toEqual(['imgtask_qa'])
    expect(state.error.value).toBe(true)
  })

  it('polls every loaded page at 10 seconds only while processing', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([task({ status: 'processing' })], 2))
    const state = setup()
    await state.load(); await state.loadMore()
    vi.mocked(asyncTasksAPI.list).mockClear()
    await vi.advanceTimersByTimeAsync(9999)
    expect(asyncTasksAPI.list).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(vi.mocked(asyncTasksAPI.list).mock.calls.map(call => call[0].page)).toEqual([1, 2])
    expect(asyncTasksAPI.refresh).not.toHaveBeenCalled()
  })

  it('pauses while hidden and refreshes immediately on return to foreground', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([task({ status: 'processing' })]))
    const state = setup()
    await state.load()
    vi.mocked(asyncTasksAPI.list).mockClear()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    await vi.advanceTimersByTimeAsync(20000)
    expect(asyncTasksAPI.list).not.toHaveBeenCalled()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
    expect(asyncTasksAPI.list).toHaveBeenCalledTimes(1)
  })

  it('throttles each processing video refresh to 30 seconds and never refreshes images', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([
      task({ status: 'processing' }), task({ id: 'video', kind: 'video', status: 'processing' }),
    ]))
    const state = setup()
    await state.load()
    expect(asyncTasksAPI.refresh).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(29999)
    expect(asyncTasksAPI.refresh).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(asyncTasksAPI.refresh).toHaveBeenCalledTimes(2)
    expect(asyncTasksAPI.refresh).toHaveBeenLastCalledWith('video')
  })

  it('filters all/image/video independently of form mode', async () => {
    const state = setup()
    state.setFilter('image')
    await vi.advanceTimersByTimeAsync(0)
    expect(asyncTasksAPI.list).toHaveBeenLastCalledWith(expect.objectContaining({ kind: 'image', page: 1 }), expect.any(AbortSignal))
    state.setFilter('video')
    await vi.advanceTimersByTimeAsync(0)
    expect(state.filter.value).toBe('video')
  })

  it('uses result_urls in order, falls back to legacy result_url, and marks expired tasks', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([
      task({ id: 'multi', result_urls: ['https://qa/1', 'https://qa/2'], result_url: 'https://qa/legacy' }),
      task({ id: 'legacy', result_url: 'https://qa/legacy' }),
      task({ id: 'expired', expires_at: NOW - 1 }),
    ]))
    const state = setup(); await state.load()
    expect(state.items.value[0].urls).toEqual(['https://qa/1', 'https://qa/2'])
    expect(state.items.value[1].urls).toEqual(['https://qa/legacy'])
    expect(state.items.value[2].status).toBe('expired')
  })
})
