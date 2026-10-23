import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VideoBillingReservationsPanel from '../VideoBillingReservationsPanel.vue'
import { adminVideoBillingReservationsAPI, type VideoBillingReservation } from '@/api/admin/videoBillingReservations'

vi.mock('@/api/admin/videoBillingReservations', () => ({
  VIDEO_BILLING_RESERVATION_STATES: ['preparing', 'accepted', 'manual', 'settled', 'released'],
  adminVideoBillingReservationsAPI: { list: vi.fn(), release: vi.fn(), settle: vi.fn() }
}))
vi.mock('@/components/common/Pagination.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({
  default: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><slot /><slot name="footer" /></section>' }
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const reservation: VideoBillingReservation = {
  id: 'res-1', user_id: 7, api_key_id: 9, group_id: 24, account_id: 33,
  platform: 'grok', protocol: 'grok', task_id: 'task-9', file_id: '',
  state: 'manual', hold_amount: 0.6, actual_amount: 0, model: 'grok-video', resolution: '720p',
  billing_mode: 'video', effective_multiplier: 1.5, attempts: 2,
  review_note: 'manual review: upstream never returned a terminal state',
  created_at: '2026-10-20T10:00:00Z', updated_at: '2026-10-21T10:00:00Z'
}

function pageWith(items: VideoBillingReservation[], total = items.length) {
  return { items, total, page: 1, page_size: 20 }
}

async function mountPanel(items: VideoBillingReservation[] = [reservation]) {
  vi.mocked(adminVideoBillingReservationsAPI.list).mockResolvedValue(pageWith(items))
  const wrapper = mount(VideoBillingReservationsPanel)
  await flushPromises()
  return wrapper
}

async function openSettle(wrapper: Awaited<ReturnType<typeof mountPanel>>) {
  await wrapper.get('[data-testid="vbr-settle"]').trigger('click')
}

describe('VideoBillingReservationsPanel', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('loads manual reservations by default and renders amounts like usage records', async () => {
    const wrapper = await mountPanel()
    expect(adminVideoBillingReservationsAPI.list).toHaveBeenCalledWith({ state: 'manual', page: 1, page_size: 20 }, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('res-1')
    expect(wrapper.text()).toContain('$0.600000')
    expect(wrapper.text()).toContain('$0.000000')
    expect(wrapper.text()).toContain('×1.5')
    expect(wrapper.text()).toContain('grok-video')
    expect(wrapper.text()).toContain(reservation.review_note)
    expect(wrapper.get(`[title="${reservation.review_note}"]`).exists()).toBe(true)
    wrapper.unmount()
  })

  it('flags rows without an upstream task ID and hides actions for non-manual rows', async () => {
    const settled: VideoBillingReservation = { ...reservation, id: 'res-2', state: 'settled', task_id: '', actual_amount: 0.4 }
    const wrapper = await mountPanel([reservation, settled])
    expect(wrapper.text()).toContain('adminVideoReservations.noTaskId')
    const buttons = wrapper.findAll('[data-testid="vbr-settle"]')
    expect(buttons).toHaveLength(1)
    expect(wrapper.text()).toContain('adminVideoReservations.states.settled')
    wrapper.unmount()
  })

  it('reloads with the selected state and resets to page 1', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('#vbr-state').setValue('settled')
    await flushPromises()
    expect(adminVideoBillingReservationsAPI.list).toHaveBeenLastCalledWith({ state: 'settled', page: 1, page_size: 20 }, expect.any(AbortSignal))
    wrapper.unmount()
  })

  it('shows a dedicated message when the list is rejected with 403', async () => {
    vi.mocked(adminVideoBillingReservationsAPI.list).mockRejectedValue({ status: 403, message: 'forbidden' })
    const wrapper = mount(VideoBillingReservationsPanel)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('adminVideoReservations.errors.forbidden')
    expect(wrapper.find('[data-testid="vbr-settle"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('requires a reason before the release confirmation and sends only the reason', async () => {
    vi.mocked(adminVideoBillingReservationsAPI.release).mockResolvedValue({ ...reservation, state: 'released' })
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="vbr-release"]').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('adminVideoReservations.release.warning')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    expect(wrapper.text()).toContain('adminVideoReservations.form.reasonRequired')
    expect(adminVideoBillingReservationsAPI.release).not.toHaveBeenCalled()
    await wrapper.get('#vbr-reason').setValue('  upstream bill shows failure ')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    await wrapper.get('[data-testid="vbr-submit"]').trigger('click')
    await flushPromises()
    expect(adminVideoBillingReservationsAPI.release).toHaveBeenCalledWith('res-1', 'upstream bill shows failure', expect.any(AbortSignal))
    expect(wrapper.text()).toContain('adminVideoReservations.released')
    wrapper.unmount()
  })

  it('validates per-second settle inputs and sends only allowed usage fields', async () => {
    vi.mocked(adminVideoBillingReservationsAPI.settle).mockResolvedValue({ ...reservation, state: 'settled', actual_amount: 0.4 })
    const wrapper = await mountPanel()
    await openSettle(wrapper)
    expect(wrapper.get('[role="dialog"]').text()).toContain('adminVideoReservations.settle.cap')
    await wrapper.get('#vbr-reason').setValue('checked against upstream invoice')
    await wrapper.get('#vbr-duration').setValue('-5')
    await wrapper.get('#vbr-count').setValue('1.5')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    expect(wrapper.text()).toContain('adminVideoReservations.form.durationInvalid')
    await wrapper.get('#vbr-duration').setValue('6')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    expect(wrapper.text()).toContain('adminVideoReservations.form.videoCountInvalid')
    await wrapper.get('#vbr-count').setValue('1')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    await wrapper.get('[data-testid="vbr-submit"]').trigger('click')
    await flushPromises()
    const payload = vi.mocked(adminVideoBillingReservationsAPI.settle).mock.calls[0][1]
    expect(payload).toEqual({ reason: 'checked against upstream invoice', duration_seconds: 6, video_count: 1 })
    expect(Object.keys(payload).sort()).toEqual(['duration_seconds', 'reason', 'video_count'])
    expect(wrapper.text()).toContain('adminVideoReservations.settled')
    wrapper.unmount()
  })

  it('requires completion tokens for token mode but allows zero, sending no other usage fields', async () => {
    const tokenReservation: VideoBillingReservation = { ...reservation, id: 'res-token', billing_mode: 'token' }
    vi.mocked(adminVideoBillingReservationsAPI.settle).mockResolvedValue({ ...tokenReservation, state: 'settled' })
    const wrapper = await mountPanel([tokenReservation])
    await openSettle(wrapper)
    expect(wrapper.find('#vbr-duration').exists()).toBe(false)
    expect(wrapper.find('#vbr-count').exists()).toBe(false)
    await wrapper.get('#vbr-reason').setValue('token reconciliation')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    expect(wrapper.text()).toContain('adminVideoReservations.form.tokensInvalid')
    expect(adminVideoBillingReservationsAPI.settle).not.toHaveBeenCalled()
    await wrapper.get('#vbr-tokens').setValue('0')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    await wrapper.get('[data-testid="vbr-submit"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(adminVideoBillingReservationsAPI.settle).mock.calls[0][1]).toEqual({ reason: 'token reconciliation', completion_tokens: 0 })
    wrapper.unmount()
  })

  it('sends only the reason for per-request billing', async () => {
    const perRequest: VideoBillingReservation = { ...reservation, id: 'res-pr', billing_mode: 'per_request' }
    vi.mocked(adminVideoBillingReservationsAPI.settle).mockResolvedValue({ ...perRequest, state: 'settled' })
    const wrapper = await mountPanel([perRequest])
    await openSettle(wrapper)
    expect(wrapper.find('#vbr-duration').exists()).toBe(false)
    expect(wrapper.find('#vbr-tokens').exists()).toBe(false)
    await wrapper.get('#vbr-reason').setValue('upstream delivered once')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    await wrapper.get('[data-testid="vbr-submit"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(adminVideoBillingReservationsAPI.settle).mock.calls[0][1]).toEqual({ reason: 'upstream delivered once' })
    wrapper.unmount()
  })

  it('rejects reasons longer than 1000 characters', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="vbr-release"]').trigger('click')
    await wrapper.get('#vbr-reason').setValue('x'.repeat(1001))
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    expect(wrapper.text()).toContain('adminVideoReservations.form.reasonTooLong')
    expect(adminVideoBillingReservationsAPI.release).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('maps 409 and 503 failures to localized messages and keeps the dialog open', async () => {
    vi.mocked(adminVideoBillingReservationsAPI.release).mockRejectedValueOnce({ status: 409, message: 'conflict' })
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="vbr-release"]').trigger('click')
    await wrapper.get('#vbr-reason').setValue('duplicate resolution attempt')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    await wrapper.get('[data-testid="vbr-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('adminVideoReservations.errors.conflict')
    vi.mocked(adminVideoBillingReservationsAPI.release).mockRejectedValueOnce({ status: 503, message: 'disabled' })
    await wrapper.get('[data-testid="vbr-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('adminVideoReservations.errors.unavailable')
    wrapper.unmount()
  })

  it('disables the submit button while the request is in flight and refreshes after success', async () => {
    let resolveSettle!: (value: VideoBillingReservation) => void
    vi.mocked(adminVideoBillingReservationsAPI.settle).mockImplementation(() => new Promise(resolve => { resolveSettle = resolve }))
    const wrapper = await mountPanel()
    const listCalls = vi.mocked(adminVideoBillingReservationsAPI.list).mock.calls.length
    await openSettle(wrapper)
    await wrapper.get('#vbr-reason').setValue('verified upstream')
    await wrapper.get('#vbr-duration').setValue('3')
    await wrapper.get('#vbr-count').setValue('1')
    await wrapper.get('[data-testid="vbr-continue"]').trigger('click')
    await wrapper.get('[data-testid="vbr-submit"]').trigger('click')
    expect(wrapper.get('[data-testid="vbr-submit"]').attributes('disabled')).toBeDefined()
    resolveSettle({ ...reservation, state: 'settled' })
    await flushPromises()
    expect(vi.mocked(adminVideoBillingReservationsAPI.list).mock.calls.length).toBe(listCalls + 1)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows an empty state and a recoverable load failure', async () => {
    vi.mocked(adminVideoBillingReservationsAPI.list)
      .mockResolvedValueOnce(pageWith([]))
      .mockRejectedValueOnce({ status: 0, message: 'offline' })
    const wrapper = mount(VideoBillingReservationsPanel)
    await flushPromises()
    expect(wrapper.text()).toContain('adminVideoReservations.emptyHint')
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('adminVideoReservations.errors.generic')
    wrapper.unmount()
  })
})
