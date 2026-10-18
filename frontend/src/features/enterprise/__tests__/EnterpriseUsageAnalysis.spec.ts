import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseOverview } from '@/types/enterprise'
import DateRangePicker from '../components/EnterpriseDateRangePicker.vue'
import UsageCombobox from '../components/EnterpriseUsageCombobox.vue'
import { DropdownMenuContent } from '@/components/ui/dropdown-menu'
import { Select } from '@/components/ui/select'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import EnterpriseUsagePanel from '../components/EnterpriseUsagePanel.vue'
import { usageDateRange } from '../useEnterpriseUsage'

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh' } }) }))
vi.mock('vue-chartjs', () => ({ Line: { template: '<canvas />' }, Doughnut: { template: '<canvas />' }, Bar: { template: '<canvas />' } }))
const stubs = {
  Teleport: { template: '<div><slot /></div>' },
  Pagination: { name: 'Pagination', props: ['page', 'pageSize'], emits: ['update:page', 'update:pageSize'], template: '<div />' },
  DataTable: {
    props: ['columns', 'data'], emits: ['sort'],
    template: '<table><thead><tr><th v-for="column in columns" :key="column.key"><button v-if="column.sortable" :data-test="`sort-${column.key}`" @click="$emit(\'sort\', column.key, \'asc\')">{{ column.label }}</button><span v-else>{{ column.label }}</span></th></tr></thead><tbody><tr v-for="row in data" :key="row.id"><td v-for="column in columns" :key="column.key" :data-column="column.key"><slot :name="`cell-${column.key}`" :row="row" :value="row[column.key]">{{ column.formatter ? column.formatter(row[column.key], row) : row[column.key] }}</slot></td></tr></tbody></table>'
  }, IpGeoCell: true
}
const wrappers: VueWrapper[] = []
function api() {
  return {
    departments: vi.fn().mockResolvedValue([]), members: vi.fn().mockResolvedValue({ items: [], total: 0, pages: 0 }),
    usage: vi.fn().mockResolvedValue({ items: [{ id: 1, model: 'legacy-model', created_at: '2026-10-18T08:00:00Z' }], total: 32 }),
    usageStats: vi.fn().mockResolvedValue({ total_requests: 32, total_tokens: 100, total_actual_cost: 0.12 }),
    usageTrend: vi.fn().mockResolvedValue([]), usageModels: vi.fn().mockResolvedValue([]),
    usageMembers: vi.fn().mockResolvedValue([]), usageDepartments: vi.fn().mockResolvedValue([])
  }
}
async function setup(admin = true, overview?: EnterpriseOverview) {
  const requests = api()
  const wrapper = mount(EnterpriseUsagePanel, { props: { api: requests as unknown as EnterpriseResourceAPI, admin, overview }, global: { stubs }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, requests }
}
async function model(wrapper: VueWrapper, value: string) { wrapper.getComponent('[data-test="usage-model"]').vm.$emit('update:modelValue', value); await flushPromises() }
async function dates(wrapper: VueWrapper, start: string, end: string) {
  const picker = wrapper.getComponent(DateRangePicker)
  picker.vm.$emit('update:startDate', start); picker.vm.$emit('update:endDate', end); await flushPromises()
}
async function submit(wrapper: VueWrapper) { await wrapper.get('form').trigger('submit'); await flushPromises() }
beforeEach(() => { localStorage.clear(); vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date(2026, 9, 18, 12)) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.useRealTimers(); vi.restoreAllMocks() })

describe('enterprise usage analysis queries', () => {
  it('stages quick dates until search, then shares the snapshot across every endpoint', async () => {
    const { wrapper, requests } = await setup()
    expect(wrapper.get('form').classes()).toEqual(expect.arrayContaining(['sticky', 'z-20', 'bg-card']))
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(true)
    expect(wrapper.find('select').exists()).toBe(false)
    for (const [period, start] of [['today', '2026-10-18'], ['last7', '2026-10-12'], ['last30', '2026-09-19'], ['month', '2026-10-01']]) {
      const range = usageDateRange(period as 'today' | 'last7' | 'last30' | 'month')
      await dates(wrapper, range.start_date, range.end_date)
      expect(wrapper.getComponent(DateRangePicker).props('startDate')).toBe(start)
      expect(requests.usageStats).toHaveBeenCalledTimes(1)
    }
    await model(wrapper, 'submitted-model')
    wrapper.getComponent('[data-test="usage-api-key"]').vm.$emit('update:modelValue', '8')
    await submit(wrapper)
    const snapshot = { start_date: '2026-10-01', end_date: '2026-10-18', model: 'submitted-model', api_key_id: 8 }
    for (const request of [requests.usage, requests.usageStats, requests.usageTrend, requests.usageModels, requests.usageMembers, requests.usageDepartments]) expect(request).toHaveBeenLastCalledWith(expect.objectContaining(snapshot), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    await model(wrapper, 'draft-model')
    await dates(wrapper, '2026-10-18', '2026-10-18')
    wrapper.getComponent({ name: 'Pagination' }).vm.$emit('update:page', 2)
    await flushPromises()
    expect(requests.usage).toHaveBeenLastCalledWith(expect.objectContaining({ ...snapshot, page: 2 }), expect.anything())
    await wrapper.get('[data-test="sort-model"]').trigger('click')
    await flushPromises()
    expect(requests.usage).toHaveBeenLastCalledWith(expect.objectContaining({ ...snapshot, sort_by: 'model', sort_order: 'asc', page: 1 }), expect.anything())
    await wrapper.get('[data-test="sort-created_at"]').trigger('click')
    await flushPromises()
    expect(requests.usage).toHaveBeenLastCalledWith(expect.objectContaining({ ...snapshot, sort_by: 'created_at', sort_order: 'asc' }), expect.anything())
    await wrapper.setProps({ refreshKey: 1 }); await flushPromises()
    expect(requests.usageStats).toHaveBeenLastCalledWith(expect.objectContaining(snapshot), expect.anything())
    await wrapper.get('[data-test="usage-reset"]').trigger('click'); await flushPromises()
    expect(requests.usageStats).toHaveBeenLastCalledWith(expect.objectContaining({ start_time: expect.any(String), end_time: expect.any(String), model: undefined, api_key_id: undefined }), expect.anything())
    expect(requests.usageStats.mock.calls.at(-1)![0]).not.toHaveProperty('start_date')
    expect(requests.usageStats.mock.calls.at(-1)![0]).not.toHaveProperty('end_date')
  })

  it('computes local calendar quick ranges across a month and year boundary', () => {
    expect(usageDateRange('last7', new Date(2026, 0, 2, 0, 15))).toEqual({ start_date: '2025-12-27', end_date: '2026-01-02' })
    expect(usageDateRange('month', new Date(2024, 1, 29, 23, 50))).toEqual({ start_date: '2024-02-01', end_date: '2024-02-29' })
  })

  it('skips department options and rankings without permission and grants them to admins', async () => {
    const overview = { permissions: { view_members: true, view_department_usage: false } } as EnterpriseOverview
    const { wrapper, requests } = await setup(false, overview)
    expect(requests.usageMembers).toHaveBeenCalledOnce()
    expect(requests.usageDepartments).not.toHaveBeenCalled()
    expect(requests.departments).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="usage-department-ranking"]').exists()).toBe(false)
    expect(wrapper.get('form').findAllComponents(Select)).toHaveLength(0)
    expect(wrapper.get('form').findAllComponents(UsageCombobox)).toHaveLength(3)
    await wrapper.setProps({ admin: true, refreshKey: 1 }); await flushPromises()
    expect(requests.usageDepartments).toHaveBeenCalledOnce()
    expect(requests.departments).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-test="usage-department-ranking"]').exists()).toBe(true)
  })

  it('aborts obsolete summary reads and ignores late responses when searches overlap', async () => {
    const { wrapper, requests } = await setup()
    let resolveOld!: (value: unknown) => void
    requests.usageStats.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    await model(wrapper, 'old')
    await submit(wrapper)
    const signal = requests.usageStats.mock.calls.at(-1)![1].signal
    await model(wrapper, 'new')
    requests.usageStats.mockResolvedValue({ total_requests: 99, total_tokens: 888, total_actual_cost: 1 })
    await submit(wrapper)
    expect(signal.aborted).toBe(true)
    resolveOld({ total_requests: 1, total_tokens: 1, total_actual_cost: 0 }); await flushPromises()
    expect(wrapper.text()).toContain('99')
    const lastSignal = requests.usageStats.mock.calls.at(-1)![1].signal
    wrapper.unmount()
    expect(lastSignal.aborted).toBe(true)
  })

  it('shows errors without mixing previous summaries into a new query, and retries the submitted snapshot', async () => {
    const { wrapper, requests } = await setup()
    requests.usageStats.mockRejectedValueOnce(new Error('offline'))
    await model(wrapper, 'failed-query')
    await submit(wrapper)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('[data-usage-card]').exists()).toBe(false)
    await model(wrapper, 'unsaved-query')
    await wrapper.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(requests.usageStats).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'failed-query' }), expect.anything())
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('32')
  })
})

describe('enterprise usage details', () => {
  it('reuses personal cells and renders old payloads, including missing token/cost tooltip fields', async () => {
    const { wrapper } = await setup()
    expect(wrapper.text()).toContain('enterprise.usageAnalysis.details')
    expect(wrapper.text()).not.toContain('enterprise.usage ')
    const table = wrapper.getComponent(UsageTable)
    expect(table.props()).toMatchObject({ showAccountBilling: false, showUpstreamEndpoint: false, serverSideSort: true })
    expect(table.props('data')[0]).toMatchObject({ member: '—', department_name: 'enterprise.noDepartment', duration_ms: null, first_token_ms: null, input_tokens: 0, cache_creation_5m_tokens: 0, input_cost: 0, actual_cost: 0, rate_multiplier: 1 })
    expect(wrapper.get('[data-column="reasoning_effort"]').text()).toBe('-')
    expect(wrapper.get('[data-column="latency"]').text()).toContain('-')
    await wrapper.get('[data-column="tokens"] .group.relative').trigger('mouseenter')
    expect(document.body.textContent).toContain('0')
    await wrapper.get('[data-column="tokens"] .group.relative').trigger('mouseleave')
    await wrapper.get('[data-column="cost"] .group.relative').trigger('mouseenter')
    expect(document.body.textContent).toContain('0.00000000')
    await wrapper.get('[data-column="cost"] .group.relative').trigger('mouseleave')
  })

  it('toggles and persists optional columns, locks time, and isolates enterprise and admin preferences', async () => {
    const { wrapper } = await setup(false)
    const keys = () => wrapper.getComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)
    expect(keys()).not.toContain('user_agent')
    await wrapper.get('[data-test="usage-column-settings"]').trigger('click', { button: 0, ctrlKey: false }); await flushPromises()
    const menu = wrapper.getComponent(DropdownMenuContent)
    expect(menu.get('[data-test="usage-column-toggle-created_at"]').attributes('data-disabled')).toBeDefined()
    await menu.get('[data-test="usage-column-toggle-user_agent"]').trigger('click')
    await menu.get('[data-test="usage-column-toggle-model"]').trigger('click')
    expect(keys()).toContain('user_agent')
    expect(keys()).not.toContain('model')
    expect(JSON.parse(localStorage.getItem('enterprise-usage-hidden-columns')!)).toContain('model')
    await wrapper.setProps({ admin: true }); await flushPromises()
    expect(keys()).toContain('model')
    expect(keys()).not.toContain('user_agent')
    await menu.get('[data-test="usage-column-toggle-request_id"]').trigger('click')
    expect(localStorage.getItem('admin-enterprise-usage-hidden-columns')).not.toBeNull()
    const second = await setup(false)
    expect(second.wrapper.getComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)).toContain('user_agent')
    expect(second.wrapper.getComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)).not.toContain('model')
  })

  it('recovers malformed preferences and never hides time from persisted values', async () => {
    localStorage.setItem('enterprise-usage-hidden-columns', '{broken')
    const first = await setup(false)
    expect(first.wrapper.getComponent(UsageTable).props('columns').some((column: { key: string }) => column.key === 'created_at')).toBe(true)
    localStorage.setItem('enterprise-usage-hidden-columns', '["created_at","model"]')
    const second = await setup(false)
    const keys = second.wrapper.getComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)
    expect(keys).toContain('created_at'); expect(keys).not.toContain('model')
  })
})
