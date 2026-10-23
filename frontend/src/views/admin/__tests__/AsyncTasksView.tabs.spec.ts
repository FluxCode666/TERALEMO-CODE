import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AsyncTasksView from '../AsyncTasksView.vue'
import { adminAsyncTasksAPI, type AdminAsyncTask } from '@/api/admin/asyncTasks'

vi.mock('@/api/admin/asyncTasks', () => ({ adminAsyncTasksAPI: { list: vi.fn(), get: vi.fn(), refresh: vi.fn(), download: vi.fn() } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/common/Pagination.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show'], template: '<section v-if="show" role="dialog"><slot /></section>' } }))
vi.mock('@/components/admin/asyncTasks/VideoBillingReservationsPanel.vue', () => ({ default: { template: '<div data-testid="vbr-panel" />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const task: AdminAsyncTask = { id: 'vidtask_1', task_id: 'task-1', user_id: 7, user_email: 'user@example.com', key_name: 'Video key', api_key_id: 9, group_id: 24, group_name: 'Video group', kind: 'video', protocol: 'minimax', model: 'MiniMax-H3', status: 'completed', result_url: 'https://cdn.example/video.mp4', created_at: 100, expires_at: 200 }

describe('Admin AsyncTasksView tabs', () => {
  beforeEach(() => {
    vi.useFakeTimers(); vi.clearAllMocks()
    vi.mocked(adminAsyncTasksAPI.list).mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20 })
  })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

  it('shows the task list tab by default and hides the reservations panel', async () => {
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    expect(wrapper.get('[data-testid="async-tasks-tab-tasks"]').text()).toBe('adminAsyncTasks.tabs.tasks')
    expect(wrapper.get('[data-testid="async-tasks-tab-reservations"]').text()).toBe('adminAsyncTasks.tabs.reservations')
    expect(wrapper.text()).toContain('user@example.com')
    expect(wrapper.find('[data-testid="vbr-panel"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('switches to the reservations panel and pauses task auto refresh', async () => {
    const wrapper = mount(AsyncTasksView)
    await flushPromises()
    await wrapper.get('[data-testid="async-tasks-tab-reservations"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="vbr-panel"]').exists()).toBe(true)
    const calls = vi.mocked(adminAsyncTasksAPI.list).mock.calls.length
    await vi.advanceTimersByTimeAsync(20000)
    expect(adminAsyncTasksAPI.list).toHaveBeenCalledTimes(calls)
    await wrapper.get('[data-testid="async-tasks-tab-tasks"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="vbr-panel"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('user@example.com')
    wrapper.unmount()
  })
})
