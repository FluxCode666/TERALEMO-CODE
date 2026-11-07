import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AsyncTasksView from '@/views/user/AsyncTasksView.vue'
import { asyncTasksAPI } from '@/api/asyncTasks'

vi.mock('@/api/asyncTasks', () => ({ asyncTasksAPI: { list: vi.fn(), refresh: vi.fn(), download: vi.fn() } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/common/Pagination.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('Creation tasks in the shared history (round2 Q5)', () => {
  afterEach(() => { vi.clearAllMocks() })

  it('makes every safe result_urls image reachable from the async-tasks page', async () => {
    const urls = ['https://storage.example/qa-1.png', 'https://storage.example/qa-2.png']
    vi.mocked(asyncTasksAPI.list).mockResolvedValue({
      items: [{
        id: 'imgtask_csqa_multi', task_id: 'imgtask_csqa_multi', kind: 'image',
        protocol: 'openai', model: 'gpt-image-2', status: 'completed', api_key_id: 201,
        result_url: urls[0], result_urls: urls, created_at: 100, expires_at: 200,
      }], total: 1, page: 1, page_size: 20,
    })
    const wrapper = mount(AsyncTasksView)
    try {
      await flushPromises()
      const hrefs = wrapper.findAll('a').map(anchor => anchor.attributes('href'))
      for (const url of urls) expect(hrefs).toContain(url)
    } finally { wrapper.unmount() }
  })
})
