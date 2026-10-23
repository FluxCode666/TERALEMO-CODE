import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AsyncTasksView from '../AsyncTasksView.vue'
import { adminAsyncTasksAPI, type AdminAsyncTask } from '@/api/admin/asyncTasks'

vi.mock('@/api/admin/asyncTasks', () => ({ adminAsyncTasksAPI: { list: vi.fn(), get: vi.fn(), refresh: vi.fn(), download: vi.fn() } }))
vi.mock('@/components/admin/asyncTasks/VideoBillingReservationsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/common/Pagination.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show'], template: '<section v-if="show" role="dialog"><slot /></section>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const task: AdminAsyncTask = { id: 'vidtask_1', task_id: 'task-1', user_id: 7, user_email: 'user@example.com', key_name: 'Video key', api_key_id: 9, group_id: 24, group_name: 'Video group', kind: 'video', protocol: 'minimax', model: 'MiniMax-H3', status: 'completed', result_url: 'https://cdn.example/video.mp4', created_at: 100, expires_at: 200 }

describe('Admin AsyncTasksView', () => {
  beforeEach(() => {
    vi.useFakeTimers(); vi.clearAllMocks()
    vi.mocked(adminAsyncTasksAPI.list).mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
  })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

  it('shows tasks across users and submits all global filters', async () => {
    vi.mocked(adminAsyncTasksAPI.list).mockResolvedValue({ items: [task, { ...task, id: 'vidtask_2', user_id: 8, user_email: 'second@example.com' }], total: 2, page: 1, page_size: 20 })
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(wrapper.text()).toContain('user@example.com')
    expect(wrapper.text()).toContain('second@example.com')
    expect(wrapper.text()).toContain('Video key #9')
    expect(wrapper.text()).toContain('Video group #24')
    await wrapper.get('#admin-task-user_id').setValue('8')
    await wrapper.get('#admin-task-api_key_id').setValue('9')
    await wrapper.get('#admin-task-group_id').setValue('24')
    await wrapper.get('#admin-task-search').setValue('MiniMax-H3')
    await wrapper.get('#admin-task-kind').setValue('video')
    await wrapper.get('#admin-task-status').setValue('failed')
    await wrapper.get('#admin-task-protocol').setValue('minimax')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adminAsyncTasksAPI.list).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: 8, api_key_id: 9, group_id: 24, search: 'MiniMax-H3', kind: 'video', status: 'failed', protocol: 'minimax', page: 1 }), expect.any(AbortSignal))
    wrapper.unmount()
  })

  it('refreshes pending videos under the recorded owner and stops polling after unmount', async () => {
    vi.mocked(adminAsyncTasksAPI.list).mockResolvedValue({ items: [{ ...task, status: 'processing' }], total: 1, page: 1, page_size: 20 })
    vi.mocked(adminAsyncTasksAPI.refresh).mockResolvedValue(task)
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(adminAsyncTasksAPI.refresh).toHaveBeenCalledWith(expect.objectContaining({ user_id: 7, id: task.id }), expect.any(AbortSignal))
    expect(wrapper.text()).toContain('asyncTasks.completed')
    wrapper.unmount()
    const calls = vi.mocked(adminAsyncTasksAPI.list).mock.calls.length
    await vi.advanceTimersByTimeAsync(30000)
    expect(adminAsyncTasksAPI.list).toHaveBeenCalledTimes(calls)
  })

  it('loads details through the admin endpoint and does not render unsafe links', async () => {
    vi.mocked(adminAsyncTasksAPI.get).mockResolvedValue({ ...task, status: 'failed', error: 'upstream failed', result_url: 'javascript:alert(1)' })
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    const details = wrapper.findAll('button').find(button => button.text() === 'adminAsyncTasks.details')!
    await details.trigger('click')
    await flushPromises()
    expect(adminAsyncTasksAPI.get).toHaveBeenCalledWith(task, expect.any(AbortSignal))
    expect(wrapper.get('[role="dialog"]').text()).toContain('upstream failed')
    expect(wrapper.get('[role="dialog"]').find('a').exists()).toBe(false)
    wrapper.unmount()
  })

  it('downloads an owned task through the admin endpoint', async () => {
    vi.mocked(adminAsyncTasksAPI.download).mockResolvedValue(new Blob(['video'], { type: 'video/mp4' }))
    vi.stubGlobal('URL', class extends URL {
      static createObjectURL = vi.fn(() => 'blob:admin-video')
      static revokeObjectURL = vi.fn()
    })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'asyncTasks.download')!.trigger('click')
    await flushPromises()
    expect(adminAsyncTasksAPI.download).toHaveBeenCalledWith(task)
    expect(click).toHaveBeenCalledOnce()
    expect((click.mock.instances[0] as HTMLAnchorElement).download).toBe('minimax-task-1.mp4')
    wrapper.unmount()
  })

  it('rejects a non-integer ID filter without calling the API, then accepts a valid one', async () => {
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    const calls = vi.mocked(adminAsyncTasksAPI.list).mock.calls.length
    await wrapper.get('#admin-task-user_id').setValue('1.5')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adminAsyncTasksAPI.list).toHaveBeenCalledTimes(calls)
    expect(wrapper.text()).toContain('adminAsyncTasks.invalidIdFilter')
    await wrapper.get('#admin-task-user_id').setValue('8')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(adminAsyncTasksAPI.list).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: 8 }), expect.any(AbortSignal))
    expect(wrapper.text()).not.toContain('adminAsyncTasks.invalidIdFilter')
    wrapper.unmount()
  })

  it('tracks downloading per user and task when ids collide across users', async () => {
    const other = { ...task, user_id: 8, user_email: 'second@example.com' }
    vi.mocked(adminAsyncTasksAPI.list).mockResolvedValue({ items: [task, other], total: 2, page: 1, page_size: 20 })
    let resolveDownload!: (blob: Blob) => void
    vi.mocked(adminAsyncTasksAPI.download).mockImplementation(() => new Promise<Blob>(resolve => { resolveDownload = resolve }))
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    const buttons = wrapper.findAll('button').filter(button => button.text() === 'asyncTasks.download')
    expect(buttons).toHaveLength(2)
    await buttons[0].trigger('click')
    expect(buttons[0].attributes('disabled')).toBeDefined()
    expect(buttons[1].attributes('disabled')).toBeUndefined()
    resolveDownload(new Blob(['video'], { type: 'video/mp4' }))
    await flushPromises()
    expect(buttons[0].attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps an action error across auto refresh and clears it on manual refresh', async () => {
    vi.mocked(adminAsyncTasksAPI.download).mockRejectedValue(new Error('boom'))
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

  it('shows an empty state and a recoverable load failure', async () => {
    vi.mocked(adminAsyncTasksAPI.list).mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 }).mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(wrapper.text()).toContain('adminAsyncTasks.emptyHint')
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('asyncTasks.loadFailed')
    wrapper.unmount()
  })
})
