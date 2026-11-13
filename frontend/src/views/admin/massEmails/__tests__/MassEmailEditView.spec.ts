import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import MassEmailEditView from '../MassEmailEditView.vue'
import { createEmptyRecipientForm } from '../recipientForm'

const mocks = vi.hoisted(() => ({
  create: vi.fn(), update: vi.fn(), send: vi.fn(), getById: vi.fn(),
  sendTest: vi.fn(), previewRecipients: vi.fn(), push: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
  route: { params: {} as Record<string, string> }
}))
vi.mock('@/api/admin', () => ({ adminAPI: { massEmails: mocks } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-router', () => ({ useRouter: () => mocks, useRoute: () => mocks.route }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

describe('mass email edit submission', () => {
  let wrapper: VueWrapper
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.route = reactive({ params: {} })
    mocks.create.mockResolvedValue({ id: 1 })
    mocks.update.mockResolvedValue({ id: 1 })
    mocks.send.mockResolvedValue({ campaign: { id: 1 } })
    mocks.sendTest.mockResolvedValue({ sent: true, to: 'admin@example.com' })
    mocks.getById.mockResolvedValue({ campaign: {
      subject: 'Existing', body_html: '<p>Existing body</p>', status: 'scheduled',
      recipient_filter: { include_all_users: true }, scheduled_at: '2099-11-20T02:30:00Z'
    } })
  })
  afterEach(() => wrapper?.unmount())

  function render() {
    wrapper = mount(MassEmailEditView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' }, MassEmailEditor: true, RecipientSelector: true
    } } })
    return wrapper
  }
  async function fill() {
    await wrapper.get('[data-test="subject"]').setValue('Important notice')
    wrapper.getComponent({ name: 'MassEmailEditor' }).vm.$emit('update:modelValue', '<p>Hello {{name}}</p>')
    wrapper.getComponent({ name: 'RecipientSelector' }).vm.$emit('update:modelValue', {
      ...createEmptyRecipientForm(), includeAllUsers: true
    })
    await nextTick()
  }

  it('saves a draft with no scheduled_at even when a time is filled', async () => {
    render()
    await fill()
    await wrapper.get('[data-test="schedule-at"]').setValue('2099-11-20T10:30')
    await wrapper.get('[data-test="save-draft"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ scheduled_at: null, send_now: false }))
  })

  it('converts the browser local scheduled time to an ISO UTC timestamp', async () => {
    render()
    await fill()
    await wrapper.get('[data-test="schedule-at"]').setValue('2099-11-20T10:30')
    await wrapper.get('[data-test="schedule"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      scheduled_at: new Date(2099, 10, 20, 10, 30).toISOString(), send_now: false
    }))
    expect(mocks.create.mock.calls[0]![0].scheduled_at).toMatch(/Z$/)
  })

  it('clears an existing schedule when editing and saving as draft', async () => {
    mocks.route.params = { id: '1' }
    render()
    await flushPromises()
    await wrapper.get('[data-test="save-draft"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, expect.objectContaining({ scheduled_at: null }))
    expect(mocks.send).not.toHaveBeenCalled()
  })

  it('submits an edited email before sending it', async () => {
    mocks.route.params = { id: '1' }
    render()
    await flushPromises()
    await wrapper.get('[data-test="send-now"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, expect.objectContaining({ scheduled_at: null }))
    expect(mocks.send).toHaveBeenCalledWith(1)
    expect(mocks.update.mock.invocationCallOrder[0]).toBeLessThan(mocks.send.mock.invocationCallOrder[0]!)
  })

  it('defaults test recipient to the administrator through a null to field', async () => {
    render()
    await fill()
    await wrapper.get('[data-test="test-btn"]').trigger('click')
    await flushPromises()
    expect(mocks.sendTest).toHaveBeenCalledWith(expect.objectContaining({ to: null }))
  })

  it('invalidates previews and ignores a response for changed recipient conditions', async () => {
    let resolvePreview!: (value: unknown) => void
    mocks.previewRecipients.mockImplementation(() => new Promise((resolve) => { resolvePreview = resolve }))
    render()
    await fill()
    const selector = wrapper.getComponent({ name: 'RecipientSelector' })
    selector.vm.$emit('preview')
    await nextTick()
    const signal = mocks.previewRecipients.mock.calls[0]![2].signal as AbortSignal
    selector.vm.$emit('update:modelValue', { ...createEmptyRecipientForm(), useManual: true, manualText: 'ops@example.com' })
    await nextTick()
    expect(signal.aborted).toBe(true)
    resolvePreview({ total: 100 })
    await flushPromises()
    expect(selector.props('preview')).toBeNull()
    expect(selector.props('previewing')).toBe(false)
  })

  describe('empty user filter guard', () => {
    async function setForm(extra: Record<string, unknown>) {
      wrapper.getComponent({ name: 'RecipientSelector' }).vm.$emit('update:modelValue', {
        ...createEmptyRecipientForm(), useUserFilter: true, ...extra
      })
      await nextTick()
    }

    it('blocks preview with a valid manual source and shows emptyUserFilter', async () => {
      render()
      await fill()
      await setForm({ useManual: true, manualText: 'ops@example.com' })
      wrapper.getComponent({ name: 'RecipientSelector' }).vm.$emit('preview')
      await flushPromises()
      expect(mocks.previewRecipients).not.toHaveBeenCalled()
      expect(mocks.showError).toHaveBeenCalledWith('admin.massEmails.form.emptyUserFilter')
    })

    it('shows emptyUserFilter rather than noSource when only the empty filter is ticked', async () => {
      render()
      await fill()
      await setForm({})
      wrapper.getComponent({ name: 'RecipientSelector' }).vm.$emit('preview')
      await flushPromises()
      expect(mocks.previewRecipients).not.toHaveBeenCalled()
      expect(mocks.showError).toHaveBeenCalledWith('admin.massEmails.form.emptyUserFilter')
      expect(mocks.showError).not.toHaveBeenCalledWith('admin.massEmails.form.noSource')
    })

    it('blocks save draft and send now with the same message', async () => {
      render()
      await fill()
      await setForm({ useManual: true, manualText: 'ops@example.com' })
      await wrapper.get('[data-test="save-draft"]').trigger('click')
      await wrapper.get('[data-test="send-now"]').trigger('click')
      await flushPromises()
      expect(mocks.create).not.toHaveBeenCalled()
      expect(mocks.showError).toHaveBeenCalledTimes(2)
      expect(mocks.showError).toHaveBeenNthCalledWith(1, 'admin.massEmails.form.emptyUserFilter')
      expect(mocks.showError).toHaveBeenNthCalledWith(2, 'admin.massEmails.form.emptyUserFilter')
    })
  })

  it('loads a different campaign when Vue reuses the edit component', async () => {
    mocks.route.params = { id: '1' }
    render()
    await flushPromises()
    mocks.route.params.id = '2'
    await flushPromises()
    expect(mocks.getById).toHaveBeenLastCalledWith(2, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })

  it('rejects edits for campaigns that already started', async () => {
    mocks.route.params = { id: '1' }
    mocks.getById.mockResolvedValueOnce({ campaign: { status: 'sending' } })
    render()
    await flushPromises()
    expect(mocks.push).toHaveBeenCalledWith('/admin/mass-emails/1')
    expect(mocks.showError).toHaveBeenCalledWith('admin.massEmails.errors.CAMPAIGN_NOT_EDITABLE')
  })
})
