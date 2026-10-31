import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ScheduledQuestionTestsModal from '../ScheduledQuestionTestsModal.vue'

const mocks = vi.hoisted(() => ({
  listPlans: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), listRuns: vi.fn(), runNow: vi.fn(),
  listQuestions: vi.fn(), getModelIntersection: vi.fn(), showError: vi.fn(), showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    scheduledQuestionTests: { list: mocks.listPlans, create: mocks.create, update: mocks.update, delete: mocks.remove, listRuns: mocks.listRuns, runNow: mocks.runNow },
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

  it('sends only form fields when updating a plan', async () => {
    const wrapper = await open()
    await wrapper.get('[data-test="edit-plan"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="save-plan"]').trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, {
      name: 'Nightly', account_ids: [1, 2], question_ids: [10], model_id: '', cron_expression: '0 * * * *',
      min_pass_ratio: 80, cooldown_minutes: 360, enabled: true, notify_enabled: true
    })
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
})
