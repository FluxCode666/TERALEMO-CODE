import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import MassEmailDetailView from '../MassEmailDetailView.vue'

const mocks = vi.hoisted(() => ({
  getById: vi.fn(), listRecipients: vi.fn(), send: vi.fn(), cancel: vi.fn(), retryFailed: vi.fn(), retryRecipient: vi.fn(),
  push: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), route: { params: { id: '1' } }
}))
vi.mock('@/api/admin', () => ({ adminAPI: { massEmails: mocks } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-router', () => ({ useRouter: () => mocks, useRoute: () => mocks.route }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

function detail(status = 'sending') {
  return {
    campaign: { id: 1, subject: 'Notice', body_html: '<p>Hello</p>', status, sent_count: 0, failed_count: 0, total_count: 1 },
    stats: { total: 1, pending: 1, sent: 0, failed: 0, opened: 0, open_rate: 0 }
  }
}

describe('mass email detail lifecycle', () => {
  let wrapper: VueWrapper
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mocks.route = reactive({ params: { id: '1' } })
    mocks.getById.mockResolvedValue(detail())
    mocks.listRecipients.mockResolvedValue({ items: [], total: 0 })
  })
  afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })
  function render() {
    wrapper = mount(MassEmailDetailView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, DataTable: true, Pagination: true, ConfirmDialog: true, EmptyState: true
    } } })
  }

  it('polls only while sending and stops once the campaign completes', async () => {
    render()
    await flushPromises()
    mocks.getById.mockResolvedValue(detail('completed'))
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(mocks.getById).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(6000)
    expect(mocks.getById).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="status"]').text()).toBe('admin.massEmails.status.completed')
  })

  it('aborts pending detail/recipient requests and search debounce when leaving', async () => {
    render()
    await flushPromises()
    await wrapper.get('input').setValue('alice')
    const detailSignal = mocks.getById.mock.calls[0]![1].signal as AbortSignal
    const recipientSignal = mocks.listRecipients.mock.calls[0]![4].signal as AbortSignal
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(6000)
    expect(detailSignal.aborted).toBe(true)
    expect(recipientSignal.aborted).toBe(true)
    expect(mocks.getById).toHaveBeenCalledTimes(1)
    expect(mocks.listRecipients).toHaveBeenCalledTimes(1)
  })

  it('ignores a sending response that finishes after leaving the page', async () => {
    let resolveDetail!: (value: unknown) => void
    mocks.getById.mockImplementationOnce(() => new Promise((resolve) => { resolveDetail = resolve }))
    render()
    wrapper.unmount()
    resolveDetail(detail())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    expect(mocks.getById).toHaveBeenCalledTimes(1)
  })

  it('uses the contract filters for opened, unopened and failed tabs', async () => {
    render()
    await flushPromises()
    for (const [tab, filters] of [
      ['opened', { opened: 'true' }], ['unopened', { opened: 'false', status: 'sent' }], ['failed', { status: 'failed' }]
    ] as const) {
      await wrapper.get(`[data-test="tab-${tab}"]`).trigger('click')
      await flushPromises()
      expect(mocks.listRecipients).toHaveBeenLastCalledWith(1, 1, 50,
        { ...filters, search: undefined }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    }
  })

  it('reloads when the route changes to another campaign', async () => {
    render()
    await flushPromises()
    mocks.route.params.id = '2'
    await flushPromises()
    expect(mocks.getById).toHaveBeenLastCalledWith(2, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(mocks.listRecipients).toHaveBeenLastCalledWith(2, 1, 50, { search: undefined }, expect.anything())
  })

  it('sanitizes the HTML preview', async () => {
    const result = detail('completed')
    result.campaign.body_html = '<p>Hello</p><script>alert(1)</script><img src="x" onerror="alert(1)"><a href="javascript:alert(1)">Bad</a>'
    mocks.getById.mockResolvedValueOnce(result)
    render()
    await flushPromises()
    expect(wrapper.html()).not.toMatch(/<script|onerror|javascript:/)
  })
})
