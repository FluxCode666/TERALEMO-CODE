import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ScheduledQuestionTestsModal from '../ScheduledQuestionTestsModal.vue'

const mocks = vi.hoisted(() => ({
  listPlans: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), listRuns: vi.fn(), runNow: vi.fn(), testFeishu: vi.fn(),
  listQuestions: vi.fn(), getModelIntersection: vi.fn(), showError: vi.fn(), showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    scheduledQuestionTests: { list: mocks.listPlans, create: mocks.create, update: mocks.update, delete: mocks.remove, listRuns: mocks.listRuns, runNow: mocks.runNow, testFeishu: mocks.testFeishu },
    accountTestQuestions: { list: mocks.listQuestions },
    accounts: { getModelIntersection: mocks.getModelIntersection }
  }
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }) }))

enableAutoUnmount(afterEach)

const stubs = {
  BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show"><button data-test="dialog-close" @click="$emit(\'close\')">Close</button><slot /><slot name="footer" /></div>' },
  Select: { props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select data-test="select-stub"><option v-for="option in options" :key="option.value">{{ option.label }}</option></select>' }
}

const plan = { id: 1, name: 'Nightly', account_ids: [1, 2], question_ids: [10], model_id: '', cron_expression: '0 * * * *', enabled: true, min_pass_ratio: 80, cooldown_minutes: 360, notify_enabled: true, running: false, last_run_at: null, next_run_at: null, last_status: 'success', passed_accounts: 2, total_accounts: 2, alerted_accounts: 0 }
const intersection = { platform: 'anthropic', models: [{ id: 'claude', display_name: 'Claude' }], account_count: 2, failed_account_ids: [] }
const run = { id: 20, plan_id: 1, status: 'success', started_at: '2026-10-31T00:00:00Z', finished_at: null, total_accounts: 2, passed_accounts: 2, alerted_accounts: 0, error_message: '', notifications_suppressed: false, summary: [{ account_id: 1, account_name: 'A', passed: 1, total: 1, judged: 1, ratio: 100, alerting: false, state_after: 'ok', error: '' }] }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

async function open() {
  const wrapper = mount(ScheduledQuestionTestsModal, { props: { show: false, selectedAccountIds: [1, 2] }, global: { stubs } })
  await wrapper.setProps({ show: true }); await flushPromises(); return wrapper
}

beforeEach(() => {
  vi.resetAllMocks()
  mocks.listPlans.mockImplementation(async () => [{ ...plan }])
  mocks.listQuestions.mockResolvedValue([{ id: 10, name: 'Identity', enabled: true }])
  mocks.getModelIntersection.mockResolvedValue(intersection)
  mocks.listRuns.mockResolvedValue([run])
  mocks.runNow.mockResolvedValue({ accepted: true, plan_id: 1 })
})

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('ScheduledQuestionTestsModal', () => {
  it('renders list state and latest run summary', async () => {
    const wrapper = await open()
    expect(wrapper.get('tbody').text()).toContain('Nightly')
    expect(wrapper.get('tbody').text()).toContain('admin.scheduledQuestionTests.recentRun')
  })

  it('validates form and disables cross-platform plans', async () => {
    mocks.listPlans.mockResolvedValue([])
    mocks.getModelIntersection.mockRejectedValue({ status: 400, code: 400, message: 'Batch test requires accounts from the same platform' })
    const wrapper = await open()
    await wrapper.get('[data-test="create-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('admin.scheduledQuestionTests.crossPlatform')
  })

  it('submits a valid plan with selected account and question IDs', async () => {
    mocks.listPlans.mockResolvedValue([]); mocks.create.mockResolvedValue(plan)
    const wrapper = await open(); await wrapper.get('[data-test="create-plan"]').trigger('click')
    await wrapper.get('[data-test="plan-name"]').setValue('Hourly')
    await wrapper.get('[data-test="account-ids"]').setValue('1, 2')
    await wrapper.get('[data-test="question-option"]').setValue(true)
    await wrapper.get('[data-test="cron"]').setValue('0 * * * *')
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Hourly', account_ids: [1, 2], question_ids: [10], cron_expression: '0 * * * *' }))
  })

  it('loads and expands run history', async () => {
    const wrapper = await open(); await wrapper.get('[data-test="history-plan"]').trigger('click'); await flushPromises()
    expect(mocks.listRuns).toHaveBeenCalledWith(1, 50)
    expect(wrapper.text()).toContain('2026-10-31T00:00:00Z')
    await wrapper.findAll('button').find(button => button.text().includes('2026-10-31'))?.trigger('click')
    expect(wrapper.text()).toContain('A: 1/1')
  })

  it('clears cross-platform errors after correcting the account IDs', async () => {
    mocks.getModelIntersection.mockRejectedValueOnce({ status: 400, code: 400, message: 'same platform required' })
    const wrapper = await open()
    await wrapper.get('[data-test="create-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="account-ids"]').element).toHaveProperty('value', '1, 2')
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="account-ids"]').setValue('1, 3'); await flushPromises()
    expect(wrapper.text()).not.toContain('admin.scheduledQuestionTests.crossPlatform')
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('input[value="custom"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('input[value="custom"]').setValue(true)
    expect(wrapper.get('[data-test="plan-model"]').text()).toContain('Claude')
  })

  it('ignores an older cross-platform failure after the latest request succeeds', async () => {
    const stale = deferred<typeof intersection>()
    mocks.getModelIntersection.mockReturnValueOnce(stale.promise)
    const wrapper = await open()
    await wrapper.get('[data-test="create-plan"]').trigger('click')
    await wrapper.get('[data-test="account-ids"]').setValue('1, 3'); await flushPromises()
    stale.reject({ status: 400, message: 'same platform required' }); await flushPromises()
    expect(wrapper.text()).not.toContain('admin.scheduledQuestionTests.crossPlatform')
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('input[value="custom"]').setValue(true)
    expect(wrapper.get('[data-test="plan-model"]').text()).toContain('Claude')
  })

  it('ignores an older success while the latest model request is still loading or has failed', async () => {
    const stale = deferred<typeof intersection>()
    const latest = deferred<typeof intersection>()
    mocks.getModelIntersection.mockReturnValueOnce(stale.promise).mockReturnValueOnce(latest.promise)
    const wrapper = await open()
    await wrapper.get('[data-test="create-plan"]').trigger('click')
    await wrapper.get('[data-test="account-ids"]').setValue('1, 9')
    stale.resolve(intersection); await flushPromises()
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeDefined()
    latest.reject({ status: 400, message: 'same platform required' }); await flushPromises()
    expect(wrapper.text()).toContain('admin.scheduledQuestionTests.crossPlatform')
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeDefined()
  })

  it('discards a model failure from a previously closed form', async () => {
    const stale = deferred<typeof intersection>()
    mocks.getModelIntersection.mockReturnValueOnce(stale.promise)
    const wrapper = await open()
    await wrapper.get('[data-test="create-plan"]').trigger('click')
    await wrapper.get('[data-test="dialog-close"]').trigger('click')
    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }); await flushPromises()
    await wrapper.get('[data-test="create-plan"]').trigger('click'); await flushPromises()
    stale.reject({ status: 400, message: 'same platform required' }); await flushPromises()
    expect(wrapper.text()).not.toContain('admin.scheduledQuestionTests.crossPlatform')
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeUndefined()
  })

  it('falls back to default models for other apiClient errors', async () => {
    mocks.getModelIntersection.mockRejectedValue({ status: 500, code: 500, message: 'platform not supported' })
    const wrapper = await open()
    await wrapper.get('[data-test="create-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('admin.scheduledQuestionTests.modelsLoadFailed')
    expect(wrapper.text()).not.toContain('admin.scheduledQuestionTests.crossPlatform')
    expect(wrapper.get('[data-test="save-plan"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('input[value="default"]').element).toHaveProperty('checked', true)
  })

  it('sends only form fields when updating a plan and keeps stored feishu credentials untouched', async () => {
    const wrapper = await open()
    await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, {
      name: 'Nightly', account_ids: [1, 2], question_ids: [10], model_id: '', cron_expression: '0 * * * *',
      min_pass_ratio: 80, cooldown_minutes: 360, enabled: true, notify_enabled: true,
      report_enabled: false, email_enabled: true, feishu_enabled: false, auto_pause_enabled: false, auto_resume_enabled: false
    })
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty('feishu_webhook_url')
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty('feishu_secret')
  })

  it('creates plans with the documented notify and schedule defaults', async () => {
    mocks.listPlans.mockResolvedValue([]); mocks.create.mockResolvedValue(plan)
    const wrapper = await open(); await wrapper.get('[data-test="create-plan"]').trigger('click')
    await wrapper.get('[data-test="plan-name"]').setValue('Hourly')
    await wrapper.get('[data-test="account-ids"]').setValue('1, 2')
    await wrapper.get('[data-test="question-option"]').setValue(true)
    await wrapper.get('[data-test="cron"]').setValue('0 * * * *')
    expect((wrapper.get('[data-test="report"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="channel-email"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="channel-feishu"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-test="auto-pause"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-test="auto-resume"]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      report_enabled: true, email_enabled: true, feishu_enabled: false, auto_pause_enabled: false, auto_resume_enabled: false
    }))
    expect(mocks.create.mock.calls[0][0]).not.toHaveProperty('feishu_webhook_url')
    expect(mocks.create.mock.calls[0][0]).not.toHaveProperty('feishu_secret')
  })

  it('sends typed feishu webhook and secret when the user edits them', async () => {
    mocks.listPlans.mockResolvedValue([{ ...plan, auto_pause_enabled: true, auto_resume_enabled: true, feishu_enabled: true, feishu_webhook_configured: true, feishu_webhook_hint: 'open.feishu.cn/…/hook/****ab12', feishu_secret_configured: true, report_enabled: true, email_enabled: false }])
    const wrapper = await open(); await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-webhook"]').setValue(' https://open.feishu.cn/open-apis/bot/v2/hook/new-token ')
    await wrapper.get('[data-test="feishu-secret"]').setValue('s3cret')
    expect(wrapper.get('[data-test="feishu-webhook"]').attributes('placeholder')).toContain('open.feishu.cn/…/hook/****ab12')
    expect(wrapper.get('[data-test="feishu-secret"]').attributes('placeholder')).toBe('admin.scheduledQuestionTests.form.feishuSecretPlaceholder')
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    const payload = mocks.update.mock.calls[0][1]
    expect(payload).toMatchObject({
      feishu_webhook_url: 'https://open.feishu.cn/open-apis/bot/v2/hook/new-token', feishu_secret: 's3cret',
      auto_pause_enabled: true, auto_resume_enabled: true, report_enabled: true, email_enabled: false
    })
  })

  it('sends an empty secret when the clear button is used', async () => {
    mocks.listPlans.mockResolvedValue([{ ...plan, feishu_enabled: true, feishu_webhook_configured: true, feishu_webhook_hint: 'open.feishu.cn/…/hook/****ab12', feishu_secret_configured: true }])
    const wrapper = await open(); await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-secret-clear"]').trigger('click')
    expect(wrapper.get('[data-test="feishu-secret"]').attributes('placeholder')).toBeUndefined()
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(mocks.update.mock.calls[0][1]).toMatchObject({ feishu_secret: '' })
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty('feishu_webhook_url')
  })

  it('sends an empty webhook after clearing it with feishu disabled', async () => {
    mocks.listPlans.mockResolvedValue([{ ...plan, feishu_enabled: true, feishu_webhook_configured: true, feishu_webhook_hint: 'open.feishu.cn/…/hook/****ab12' }])
    const wrapper = await open(); await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-webhook-clear"]').trigger('click')
    await wrapper.get('[data-test="channel-feishu"]').setValue(false); await flushPromises()
    expect(wrapper.find('[data-test="feishu-webhook"]').exists()).toBe(false)
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(mocks.update.mock.calls[0][1]).toMatchObject({ feishu_enabled: false, feishu_webhook_url: '' })
  })

  it('blocks saving feishu notifications without a configured webhook', async () => {
    mocks.listPlans.mockResolvedValue([]); mocks.create.mockResolvedValue(plan)
    const wrapper = await open(); await wrapper.get('[data-test="create-plan"]').trigger('click')
    await wrapper.get('[data-test="plan-name"]').setValue('Hourly')
    await wrapper.get('[data-test="account-ids"]').setValue('1, 2')
    await wrapper.get('[data-test="question-option"]').setValue(true)
    await wrapper.get('[data-test="cron"]').setValue('0 * * * *')
    await wrapper.get('[data-test="channel-feishu"]').setValue(true); await flushPromises()
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.scheduledQuestionTests.form.feishuWebhookRequired')
  })

  it('warns when every notification channel is disabled but a notification is on', async () => {
    const wrapper = await open(); await wrapper.get('[data-test="create-plan"]').trigger('click')
    expect(wrapper.find('[data-test="no-channel-hint"]').exists()).toBe(false)
    await wrapper.get('[data-test="channel-email"]').setValue(false); await flushPromises()
    expect(wrapper.get('[data-test="no-channel-hint"]').text()).toBe('admin.scheduledQuestionTests.form.noChannelHint')
    await wrapper.get('[data-test="notify"]').setValue(false)
    await wrapper.get('[data-test="report"]').setValue(false); await flushPromises()
    expect(wrapper.find('[data-test="no-channel-hint"]').exists()).toBe(false)
  })

  it('sends a feishu test message for the stored plan credentials', async () => {
    mocks.listPlans.mockResolvedValue([{ ...plan, feishu_enabled: true, feishu_webhook_configured: true, feishu_webhook_hint: 'open.feishu.cn/…/hook/****ab12' }])
    mocks.testFeishu.mockResolvedValue({ success: true })
    const wrapper = await open(); await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    expect(mocks.testFeishu).toHaveBeenCalledWith({ plan_id: 1 })
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.scheduledQuestionTests.feishuTestSuccess')
  })

  it('sends a feishu test message with typed credentials and reports failures', async () => {
    mocks.listPlans.mockResolvedValue([])
    mocks.testFeishu.mockRejectedValue({ status: 400, message: 'feishu code=19001 msg=param invalid' })
    const wrapper = await open(); await wrapper.get('[data-test="create-plan"]').trigger('click')
    expect(wrapper.find('[data-test="feishu-test"]').exists()).toBe(false)
    await wrapper.get('[data-test="channel-feishu"]').setValue(true); await flushPromises()
    expect(wrapper.get('[data-test="feishu-test"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="feishu-webhook"]').setValue('https://open.feishu.cn/open-apis/bot/v2/hook/token-1')
    await wrapper.get('[data-test="feishu-secret"]').setValue('secret-1')
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    expect(mocks.testFeishu).toHaveBeenCalledWith({ webhook_url: 'https://open.feishu.cn/open-apis/bot/v2/hook/token-1', secret: 'secret-1' })
    expect(mocks.showError).toHaveBeenCalledWith('feishu code=19001 msg=param invalid')
  })

  it('keeps the newer feishu test pending and silent when a superseded request resolves first', async () => {
    const first = deferred<{ success: boolean }>()
    const second = deferred<{ success: boolean }>()
    mocks.listPlans.mockResolvedValue([
      { ...plan, id: 1, feishu_enabled: true, feishu_webhook_configured: true, feishu_webhook_hint: 'open.feishu.cn/…/hook/****aaaa' },
      { ...plan, id: 2, name: 'Second', feishu_enabled: true, feishu_webhook_configured: true, feishu_webhook_hint: 'open.feishu.cn/…/hook/****bbbb' }
    ])
    mocks.testFeishu.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = await open()
    const editButtons = wrapper.findAll('[data-test="edit-plan"]')
    await editButtons[0].trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    expect(mocks.testFeishu).toHaveBeenCalledTimes(1)
    expect(mocks.testFeishu).toHaveBeenNthCalledWith(1, { plan_id: 1 })
    expect(wrapper.get('[data-test="feishu-test"]').attributes('disabled')).toBeDefined()
    await editButtons[1].trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    expect(mocks.testFeishu).toHaveBeenCalledTimes(2)
    expect(mocks.testFeishu).toHaveBeenNthCalledWith(2, { plan_id: 2 })
    first.resolve({ success: true }); await flushPromises()
    expect(wrapper.get('[data-test="feishu-test"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="feishu-test"]').text()).toBe('admin.scheduledQuestionTests.form.feishuTesting')
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    expect(mocks.testFeishu).toHaveBeenCalledTimes(2)
    second.resolve({ success: true }); await flushPromises()
    expect(mocks.showSuccess).toHaveBeenCalledTimes(1)
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.scheduledQuestionTests.feishuTestSuccess')
    expect(wrapper.get('[data-test="feishu-test"]').attributes('disabled')).toBeUndefined()
  })

  it('stays silent when a superseded feishu test fails after switching plans', async () => {
    const first = deferred<{ success: boolean }>()
    const second = deferred<{ success: boolean }>()
    mocks.listPlans.mockResolvedValue([
      { ...plan, id: 1, feishu_enabled: true, feishu_webhook_configured: true },
      { ...plan, id: 2, name: 'Second', feishu_enabled: true, feishu_webhook_configured: true }
    ])
    mocks.testFeishu.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = await open()
    const editButtons = wrapper.findAll('[data-test="edit-plan"]')
    await editButtons[0].trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    await editButtons[1].trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    first.reject({ status: 400, message: 'feishu code=19001 msg=stale' }); await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="feishu-test"]').attributes('disabled')).toBeDefined()
    second.resolve({ success: true }); await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(mocks.showSuccess).toHaveBeenCalledTimes(1)
  })

  it('drops a feishu test reply that arrives after the form is closed', async () => {
    const pendingTest = deferred<{ success: boolean }>()
    mocks.listPlans.mockResolvedValue([{ ...plan, feishu_enabled: true, feishu_webhook_configured: true }])
    mocks.testFeishu.mockReturnValueOnce(pendingTest.promise)
    const wrapper = await open()
    await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    expect(mocks.testFeishu).toHaveBeenCalledTimes(1)
    await wrapper.findAll('button').find(button => button.text() === 'common.cancel')?.trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="feishu-test"]').exists()).toBe(false)
    pendingTest.resolve({ success: true }); await flushPromises()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('drops a feishu test reply that arrives after the modal is closed', async () => {
    const pendingTest = deferred<{ success: boolean }>()
    mocks.listPlans.mockResolvedValue([{ ...plan, feishu_enabled: true, feishu_webhook_configured: true }])
    mocks.testFeishu.mockReturnValueOnce(pendingTest.promise)
    const wrapper = await open()
    await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="feishu-test"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="dialog-close"]').trigger('click'); await flushPromises()
    pendingTest.resolve({ success: true }); await flushPromises()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('shows backend validation messages for nonexistent accounts', async () => {
    mocks.update.mockRejectedValue({ status: 400, message: 'account_ids: account 99 not found or unavailable' })
    const wrapper = await open()
    await wrapper.get('[data-test="edit-plan"]').trigger('click')
    await wrapper.get('[data-test="account-ids"]').setValue('99'); await flushPromises()
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('account_ids: account 99 not found or unavailable')
  })

  it('keeps an accepted plan disabled and polls until it finishes, refreshing open history', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const pending = deferred<{ accepted: boolean; plan_id: number }>()
    mocks.runNow.mockReturnValueOnce(pending.promise)
    const wrapper = await open()
    await wrapper.get('[data-test="history-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="run-plan"]').trigger('click')
    expect(wrapper.get('[data-test="run-plan"]').attributes('disabled')).toBeDefined()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    mocks.listPlans.mockResolvedValue([{ ...plan, running: true }])
    pending.resolve({ accepted: true, plan_id: 1 }); await flushPromises()
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.scheduledQuestionTests.started')
    expect(wrapper.get('[data-test="run-plan"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="run-plan"]').text()).toBe('admin.scheduledQuestionTests.running')
    expect(mocks.listPlans).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(2999); await flushPromises()
    expect(mocks.listPlans).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1); await flushPromises()
    expect(mocks.listPlans).toHaveBeenCalledTimes(3)
    mocks.listPlans.mockResolvedValue([{ ...plan, running: false }])
    mocks.listRuns.mockResolvedValue([{ ...run, id: 21, started_at: '2026-10-31T01:00:00Z' }])
    await vi.advanceTimersByTimeAsync(3000); await flushPromises()
    expect(mocks.listPlans).toHaveBeenCalledTimes(4)
    expect(mocks.listRuns).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('2026-10-31T01:00:00Z')
    expect(wrapper.get('[data-test="run-plan"]').attributes('disabled')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(9000); await flushPromises()
    expect(mocks.listPlans).toHaveBeenCalledTimes(4)
    expect(mocks.listQuestions).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('shows the already-running message on 409 and follows the server running state', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const wrapper = await open()
    mocks.runNow.mockRejectedValue({ status: 409, code: 409, message: 'already running' })
    mocks.listPlans.mockResolvedValue([{ ...plan, running: true }])
    await wrapper.get('[data-test="run-plan"]').trigger('click'); await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('admin.scheduledQuestionTests.alreadyRunning')
    expect(wrapper.get('[data-test="run-plan"]').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(3000); await flushPromises()
    expect(mocks.listPlans).toHaveBeenCalledTimes(3)
  })

  it.each(['close', 'hide', 'unmount'])('cleans polling on %s', async action => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    mocks.listPlans.mockResolvedValue([{ ...plan, running: true }])
    const wrapper = await open()
    expect(vi.getTimerCount()).toBe(1)
    if (action === 'close') await wrapper.get('[data-test="dialog-close"]').trigger('click')
    else if (action === 'hide') await wrapper.setProps({ show: false })
    else wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(6000); await flushPromises()
    expect(mocks.listPlans).toHaveBeenCalledTimes(1)
  })

  it('does not restart polling when an in-flight refresh resolves after close', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    mocks.listPlans.mockResolvedValue([{ ...plan, running: true }])
    const wrapper = await open()
    const pending = deferred<Array<typeof plan>>()
    mocks.listPlans.mockReturnValueOnce(pending.promise)
    await vi.advanceTimersByTimeAsync(3000)
    await wrapper.get('[data-test="dialog-close"]').trigger('click')
    pending.resolve([{ ...plan, running: true }]); await flushPromises()
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(6000)
    expect(mocks.listPlans).toHaveBeenCalledTimes(2)
  })

  it('reports execution and deletion failures', async () => {
    mocks.runNow.mockRejectedValue({ status: 500, message: 'execution unavailable' })
    mocks.remove.mockRejectedValue({ status: 500, message: 'deletion unavailable' })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await open()
    await wrapper.get('[data-test="run-plan"]').trigger('click'); await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('execution unavailable')
    expect(wrapper.get('[data-test="run-plan"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="delete-plan"]').trigger('click'); await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('deletion unavailable')
  })

  it('renders the run conclusion, resulting account state, and notification suppression', async () => {
    mocks.listRuns.mockResolvedValue([{ ...run, notifications_suppressed: true, summary: [
      { ...run.summary[0], state_after: 'alert' },
      { ...run.summary[0], account_id: 2, account_name: 'B', alerting: true, state_after: 'ok' }
    ] }])
    const wrapper = await open()
    await wrapper.get('[data-test="history-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="run-20"]').text()).toContain('admin.scheduledQuestionTests.notificationsSuppressed')
    await wrapper.get('[data-test="run-20"]').trigger('click')
    const first = wrapper.get('[data-test="run-account-1"]').text()
    const second = wrapper.get('[data-test="run-account-2"]').text()
    expect(first).toContain('admin.scheduledQuestionTests.ok')
    expect(first).toContain('admin.scheduledQuestionTests.stateAfter:{"state":"admin.scheduledQuestionTests.stateAlert"}')
    expect(second).toContain('admin.scheduledQuestionTests.alerting')
    expect(second).toContain('admin.scheduledQuestionTests.stateAfter:{"state":"admin.scheduledQuestionTests.stateOk"}')
  })

  it('handles null history details and omits the suppression marker when false', async () => {
    mocks.listRuns.mockResolvedValue([{ ...run, summary: null }])
    const wrapper = await open()
    await wrapper.get('[data-test="history-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('admin.scheduledQuestionTests.notificationsSuppressed')
    await wrapper.get('[data-test="run-20"]').trigger('click')
    expect(wrapper.text()).toContain('admin.scheduledQuestionTests.noDetails')
  })

  it('shows enabled capability badges under the plan name', async () => {
    mocks.listPlans.mockResolvedValue([{ ...plan, auto_pause_enabled: true, auto_resume_enabled: true, feishu_enabled: true, report_enabled: true }])
    const wrapper = await open()
    const badges = wrapper.get('[data-test="plan-badges"]').text()
    expect(badges).toContain('admin.scheduledQuestionTests.autoPauseBadge')
    expect(badges).toContain('admin.scheduledQuestionTests.autoResumeBadge')
    expect(badges).toContain('admin.scheduledQuestionTests.feishuBadge')
    expect(badges).toContain('admin.scheduledQuestionTests.reportBadge')
  })

  it('omits capability badges when no capability is enabled', async () => {
    const wrapper = await open()
    expect(wrapper.find('[data-test="plan-badges"]').exists()).toBe(false)
  })

  it('renders schedule actions, auto-pause markers, and notification results in history', async () => {
    mocks.listRuns.mockResolvedValue([{
      ...run, paused_accounts: 2, resumed_accounts: 1, auto_pause_skipped: true,
      notification_results: [
        { kind: 'alert', channel: 'email', success: true, detail: 'sent 1/1 recipients' },
        { kind: 'report', channel: 'feishu', success: false, detail: 'feishu code=19001 msg=param invalid' }
      ],
      summary: [
        { ...run.summary[0], schedule_action: 'paused', auto_paused: true },
        { ...run.summary[0], account_id: 2, account_name: 'B', alerting: true, state_after: 'alert', schedule_action: 'pause_failed', incomplete: true, ratio: null }
      ]
    }])
    const wrapper = await open()
    await wrapper.get('[data-test="history-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="run-paused"]').text()).toBe('admin.scheduledQuestionTests.pausedCount:{"count":2}')
    expect(wrapper.get('[data-test="run-resumed"]').text()).toBe('admin.scheduledQuestionTests.resumedCount:{"count":1}')
    expect(wrapper.get('[data-test="run-auto-pause-skipped"]').text()).toBe('admin.scheduledQuestionTests.autoPauseSkipped')
    await wrapper.get('[data-test="run-20"]').trigger('click')
    expect(wrapper.get('[data-test="run-account-action-1"]').text()).toBe('admin.scheduledQuestionTests.scheduleActions.paused')
    expect(wrapper.get('[data-test="run-account-auto-paused-1"]').text()).toBe('admin.scheduledQuestionTests.autoPausedMarker')
    expect(wrapper.get('[data-test="run-account-action-2"]').text()).toBe('admin.scheduledQuestionTests.scheduleActions.pauseFailed')
    expect(wrapper.get('[data-test="run-account-2"]').text()).toContain('admin.scheduledQuestionTests.incompleteMarker')
    const notifications = wrapper.get('[data-test="run-notifications"]').text()
    expect(notifications).toContain('admin.scheduledQuestionTests.notifyKinds.alert')
    expect(notifications).toContain('admin.scheduledQuestionTests.notifyChannels.email')
    expect(notifications).toContain('admin.scheduledQuestionTests.notificationSuccess')
    expect(notifications).toContain('sent 1/1 recipients')
    expect(notifications).toContain('admin.scheduledQuestionTests.notifyKinds.report')
    expect(notifications).toContain('admin.scheduledQuestionTests.notificationFailed')
    expect(notifications).toContain('feishu code=19001 msg=param invalid')
  })

  it('hides schedule and notification markers when a run has none', async () => {
    const wrapper = await open()
    await wrapper.get('[data-test="history-plan"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="run-paused"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="run-resumed"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="run-auto-pause-skipped"]').exists()).toBe(false)
    await wrapper.get('[data-test="run-20"]').trigger('click')
    expect(wrapper.find('[data-test="run-account-action-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="run-account-auto-paused-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="run-notifications"]').exists()).toBe(false)
  })
})
