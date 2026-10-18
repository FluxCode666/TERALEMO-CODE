import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseQuery } from '@/types/enterprise'
import EnterpriseOrganizationPanel from '../components/EnterpriseOrganizationPanel.vue'
import EnterpriseUsagePanel from '../components/EnterpriseUsagePanel.vue'
import EnterpriseLogsPanel from '../components/EnterpriseLogsPanel.vue'
import EnterpriseGroupsPanel from '../components/EnterpriseGroupsPanel.vue'
import DateRangePicker from '../components/EnterpriseDateRangePicker.vue'
import { loadMemberOptions } from '../memberOptions'

const { showError } = vi.hoisted(() => ({ showError: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: vi.fn() }, groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh' } }) }))
vi.mock('vue-chartjs', () => ({ Line: { template: '<div />' } }))

const stubs = {
  DataTable: { props: ['data'], template: '<div data-test="rows">{{ JSON.stringify(data) }}</div>' },
  Pagination: { name: 'Pagination', props: ['page', 'pageSize', 'total'], emits: ['update:page', 'update:pageSize'], template: '<div />' },
  BaseDialog: true,
  ConfirmDialog: true,
  StatCard: { props: ['value'], template: '<div data-test="stat">{{ value }}</div>' }
}
Object.assign(stubs, { EnterpriseTable: stubs.DataTable, EnterprisePagination: stubs.Pagination, EnterpriseDialog: stubs.BaseDialog, EnterpriseConfirm: stubs.ConfirmDialog })
const wrappers: VueWrapper[] = []
function track<T extends VueWrapper>(wrapper: T) { wrappers.push(wrapper); return wrapper }
function page(items: unknown[], current = 1) { return { items, total: 100, page: current, page_size: 20, pages: 5 } }
function api() {
  return {
    departments: vi.fn().mockResolvedValue([]), members: vi.fn().mockResolvedValue(page([])),
    usage: vi.fn().mockResolvedValue(page([])), usageStats: vi.fn().mockResolvedValue({ total_requests: 12, total_tokens: 34, total_actual_cost: 1 }),
    usageTrend: vi.fn().mockResolvedValue([]), usageModels: vi.fn().mockResolvedValue([]),
    usageMembers: vi.fn().mockResolvedValue([]), usageDepartments: vi.fn().mockResolvedValue([]),
    auditLogs: vi.fn().mockResolvedValue(page([])), balanceLogs: vi.fn().mockResolvedValue(page([])),
    groups: vi.fn().mockResolvedValue([]), allowedGroups: vi.fn().mockResolvedValue([]), groupWhitelists: vi.fn().mockResolvedValue([])
  }
}
function resource(value: ReturnType<typeof api>) { return value as unknown as EnterpriseResourceAPI }
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}
async function changePage(wrapper: VueWrapper, value: number) {
  (wrapper.findComponent({ name: 'EnterprisePagination' }).exists() ? wrapper.findComponent({ name: 'EnterprisePagination' }) : wrapper.findComponent({ name: 'Pagination' })).vm.$emit('update:page', value)
  await flushPromises()
}

afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount() })
beforeEach(() => vi.clearAllMocks())

describe('enterprise paginated reads', () => {
  it('keeps the latest organization refresh when the earlier member request resolves last', async () => {
    const requests = api(), first = deferred<ReturnType<typeof page>>(), latest = deferred<ReturnType<typeof page>>()
    requests.members.mockReturnValueOnce(first.promise).mockReturnValueOnce(latest.promise)
    const wrapper = track(mount(EnterpriseOrganizationPanel, { props: { api: resource(requests), admin: true }, global: { stubs } }))
    await wrapper.setProps({ refreshKey: 1 })
    latest.resolve({ ...page([{ id: 3, username: 'latest', email: 'latest@example.com' }]), total: 1 })
    await flushPromises()
    first.resolve({ ...page([{ id: 2, username: 'stale', email: 'stale@example.com' }]), total: 1 })
    await flushPromises()
    expect(wrapper.find('[data-test="org-node-member-3"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="org-node-member-2"]').exists()).toBe(false)
  })

  it.each(['audit', 'balanceLogs'] as const)('keeps the latest %s page and reloads it on refresh', async kind => {
    const requests = api(), request = kind === 'audit' ? requests.auditLogs : requests.balanceLogs
    const wrapper = track(mount(EnterpriseLogsPanel, { props: { api: resource(requests), kind }, global: { stubs } }))
    await flushPromises()
    const second = deferred<ReturnType<typeof page>>(), third = deferred<ReturnType<typeof page>>()
    request.mockImplementation((params: EnterpriseQuery) => params.page === 2 ? second.promise : third.promise)
    await changePage(wrapper, 2)
    await changePage(wrapper, 3)
    third.resolve(page([{ id: 3 }], 3))
    await flushPromises()
    second.resolve(page([{ id: 2 }], 2))
    await flushPromises()
    expect(wrapper.get('[data-test="rows"]').text()).toBe('[{"id":3}]')
    request.mockResolvedValue(page([{ id: 4 }], 3))
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ page: 3 }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.get('[data-test="rows"]').text()).toBe('[{"id":4}]')
  })

  it('refreshes an organization panel without recreating its instance', async () => {
    const requests = api()
    const wrapper = track(mount(EnterpriseOrganizationPanel, { props: { api: resource(requests), mode: 'departments' }, global: { stubs } }))
    await flushPromises()
    requests.departments.mockResolvedValue([{ id: 1, name: 'fresh department' }])
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(requests.departments).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('fresh department')
  })

  it('refreshes available groups with a cancellable read', async () => {
    const requests = api()
    const wrapper = track(mount(EnterpriseGroupsPanel, { props: { api: resource(requests), enterpriseId: 1 }, global: { stubs } }))
    await flushPromises()
    requests.groups.mockResolvedValue([{ id: 1, name: 'fresh group' }])
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(wrapper.text()).toContain('fresh group')
    expect(requests.groups).toHaveBeenCalledTimes(2)
    const signal = requests.groups.mock.calls[1][0].signal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
})

describe('usage applied filters and latest page', () => {
  function mountUsage(requests: ReturnType<typeof api>) {
    return track(mount(EnterpriseUsagePanel, { props: { api: resource(requests), admin: true }, global: { stubs } }))
  }

  it('uses the last searched query for pagination and refresh while draft filters are edited', async () => {
    const requests = api(), wrapper = mountUsage(requests)
    await flushPromises()
    const dates = wrapper.getComponent(DateRangePicker)
    const model = wrapper.getComponent('[data-test="usage-model"]')
    model.vm.$emit('update:modelValue', 'submitted-model')
    dates.vm.$emit('update:startDate', '2024-01-01')
    dates.vm.$emit('update:endDate', '2024-12-31')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(requests.usageStats).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'submitted-model', start_date: '2024-01-01', end_date: '2024-12-31' }), expect.anything())
    model.vm.$emit('update:modelValue', 'unsaved-model')
    dates.vm.$emit('update:endDate', '2025-01-01')
    const count = requests.usageStats.mock.calls.length
    await changePage(wrapper, 2)
    expect(requests.usage).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, model: 'submitted-model', end_date: '2024-12-31' }), expect.anything())
    expect(requests.usageStats).toHaveBeenCalledTimes(count)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(requests.usageStats).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'submitted-model', end_date: '2024-12-31' }), expect.anything())
    expect(wrapper.findAll('[data-usage-value]')[0].text()).toBe('12')
  })

  it('rejects 367 inclusive days, allows 366 and same-day ranges, and leaves the applied page unchanged on invalid search', async () => {
    const requests = api(), wrapper = mountUsage(requests)
    await flushPromises()
    await changePage(wrapper, 2)
    const dates = wrapper.getComponent(DateRangePicker)
    dates.vm.$emit('update:startDate', '2024-01-01')
    dates.vm.$emit('update:endDate', '2025-01-01')
    const count = requests.usage.mock.calls.length
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('enterprise.validationDateRange')
    expect(requests.usage).toHaveBeenCalledTimes(count)
    expect((wrapper.findComponent({ name: 'EnterprisePagination' }).exists() ? wrapper.findComponent({ name: 'EnterprisePagination' }) : wrapper.findComponent({ name: 'Pagination' })).props('page')).toBe(2)
    for (const end of ['2024-12-31', '2024-01-01']) {
      dates.vm.$emit('update:endDate', end)
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(wrapper.find('[role="alert"]').exists()).toBe(false)
      expect(requests.usage).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, end_date: end }), expect.anything())
    }
  })

  it('ignores stale usage rows and cancels outstanding requests when unmounted', async () => {
    const requests = api(), wrapper = mountUsage(requests)
    await flushPromises()
    const second = deferred<ReturnType<typeof page>>(), third = deferred<ReturnType<typeof page>>()
    requests.usage.mockImplementation((params: EnterpriseQuery) => params.page === 2 ? second.promise : third.promise)
    await changePage(wrapper, 2)
    await changePage(wrapper, 3)
    third.resolve(page([{ id: 3, model: 'latest' }], 3))
    await flushPromises()
    second.resolve(page([{ id: 2, model: 'stale' }], 2))
    await flushPromises()
    expect(wrapper.findAll('[data-test="rows"]').at(-1)!.text()).toContain('latest')
    expect(wrapper.findAll('[data-test="rows"]').at(-1)!.text()).not.toContain('stale')
    const signal = requests.usage.mock.calls.at(-1)![1].signal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    expect(showError).not.toHaveBeenCalled()
  })
})

it('stops member option pagination after disposal even if the current transport ignores abort', async () => {
  const requests = api(), pending = deferred<ReturnType<typeof page>>()
  requests.members.mockReturnValue(pending.promise)
  const controller = new AbortController()
  const result = loadMemberOptions(resource(requests), controller.signal)
  controller.abort()
  pending.resolve(page([{ id: 1 }]))
  await expect(result).rejects.toMatchObject({ name: 'AbortError' })
  expect(requests.members).toHaveBeenCalledTimes(1)
})
