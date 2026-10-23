import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import AsyncTasksView from '../AsyncTasksView.vue'
import { asyncTasksAPI } from '@/api/asyncTasks'

vi.mock('@/api/asyncTasks', () => ({ asyncTasksAPI: { list: vi.fn(), refresh: vi.fn(), download: vi.fn() } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/common/Pagination.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('AsyncTasksView', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks() })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

  it('shows an empty state, sends filters and stops polling on unmount', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(wrapper.text()).toContain('asyncTasks.empty')
    await wrapper.get('#task-kind').setValue('video')
    await wrapper.get('#task-status').setValue('failed')
    await wrapper.get('#task-search').setValue('task-1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(asyncTasksAPI.list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, kind: 'video', status: 'failed', search: 'task-1' }), expect.any(AbortSignal))
    wrapper.unmount()
    const calls = vi.mocked(asyncTasksAPI.list).mock.calls.length
    await vi.advanceTimersByTimeAsync(30000)
    expect(asyncTasksAPI.list).toHaveBeenCalledTimes(calls)
  })

  it('refreshes pending video tasks and rejects unsafe result links', async () => {
    const task = { id: 'vidtask_1', task_id: 'task-1', kind: 'video' as const, protocol: 'seedance', model: 'seedance', status: 'processing' as const, api_key_id: 9, created_at: 100, expires_at: 200 }
    vi.mocked(asyncTasksAPI.list).mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
    vi.mocked(asyncTasksAPI.refresh).mockResolvedValue({ ...task, status: 'completed', result_url: 'javascript:alert(1)' })
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(asyncTasksAPI.refresh).toHaveBeenCalledWith(task.id, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('asyncTasks.completed')
    expect(wrapper.find('a').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows a recoverable load failure', async () => {
    vi.mocked(asyncTasksAPI.list).mockRejectedValue(new Error('offline'))
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('asyncTasks.loadFailed')
    wrapper.unmount()
  })

  it.each([
    ['minimax', 'https://cdn.example/expired.mp4'],
    ['seedance', '/api/v1/user/async-tasks/vidtask_private/content']
  ])('downloads a completed %s task through the owned task endpoint', async (protocol, resultURL) => {
    const task = { id: 'vidtask_private', task_id: 'task-1', kind: 'video' as const, protocol, status: 'completed' as const, result_url: resultURL, api_key_id: 9, created_at: 100, expires_at: 200 }
    vi.mocked(asyncTasksAPI.list).mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
    vi.mocked(asyncTasksAPI.download).mockResolvedValue(new Blob(['video'], { type: 'video/mp4' }))
    const revoke = vi.fn()
    vi.stubGlobal('URL', class extends URL {
      static createObjectURL = vi.fn(() => 'blob:generated-video')
      static revokeObjectURL = revoke
    })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    const download = wrapper.findAll('button').find(button => button.text() === 'asyncTasks.download')!
    await download.trigger('click')
    await flushPromises()
    expect(asyncTasksAPI.download).toHaveBeenCalledWith(task.id)
    expect(click).toHaveBeenCalledOnce()
    expect((click.mock.instances[0] as HTMLAnchorElement).download).toBe(`${protocol}-task-1.mp4`)
    expect(asyncTasksAPI.refresh).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1000)
    expect(revoke).toHaveBeenCalledWith('blob:generated-video')
    wrapper.unmount()
  })

  it('sanitizes the task id in the download filename and falls back when empty', async () => {
    const task = { id: 'vidtask_2', task_id: 'task/1:2', kind: 'video' as const, protocol: '', status: 'completed' as const, result_url: 'https://cdn.example/v.mp4', api_key_id: 9, created_at: 100, expires_at: 200 }
    vi.mocked(asyncTasksAPI.list).mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
    vi.mocked(asyncTasksAPI.download).mockResolvedValue(new Blob(['video'], { type: 'video/mp4' }))
    vi.stubGlobal('URL', class extends URL {
      static createObjectURL = vi.fn(() => 'blob:sanitized-video')
      static revokeObjectURL = vi.fn()
    })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'asyncTasks.download')!.trigger('click')
    await flushPromises()
    expect((click.mock.instances[0] as HTMLAnchorElement).download).toBe('video-task-1-2.mp4')
    wrapper.unmount()
  })

  it('does not offer upstream refresh for processing image tasks', async () => {
    const task = { id: 'imgtask_1', task_id: 'img-1', kind: 'image' as const, protocol: 'gpt-image', status: 'processing' as const, api_key_id: 9, created_at: 100, expires_at: 200 }
    vi.mocked(asyncTasksAPI.list).mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(asyncTasksAPI.refresh).not.toHaveBeenCalled()
    expect(wrapper.findAll('button.btn-ghost').filter(button => button.text() === 'common.refresh')).toHaveLength(0)
    wrapper.unmount()
  })

  it('keeps a download error across auto refresh and clears it on manual refresh', async () => {
    const task = { id: 'vidtask_1', task_id: 'task-1', kind: 'video' as const, protocol: 'minimax', status: 'completed' as const, result_url: 'https://cdn.example/video.mp4', api_key_id: 9, created_at: 100, expires_at: 200 }
    vi.mocked(asyncTasksAPI.list).mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
    vi.mocked(asyncTasksAPI.download).mockRejectedValue(new Error('boom'))
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'asyncTasks.download')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('asyncTasks.downloadFailed')
    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(wrapper.text()).toContain('asyncTasks.downloadFailed')
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('asyncTasks.downloadFailed')
    wrapper.unmount()
  })
})
