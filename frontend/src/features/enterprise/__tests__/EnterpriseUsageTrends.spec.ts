import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { Bar, Doughnut } from 'vue-chartjs'
import type { ChartOptions, TooltipItem } from 'chart.js'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseOverview } from '@/types/enterprise'
import { Select } from '@/components/ui/select'
import { PopoverContent } from '@/components/ui/popover'
import UsagePanel from '../components/EnterpriseUsagePanel.vue'
import DateRangePicker from '../components/EnterpriseDateRangePicker.vue'
import Distribution from '../components/EnterpriseUsageDistribution.vue'
import Ranking from '../components/EnterpriseUsageRanking.vue'
import StackedTrend from '../components/EnterpriseUsageStackedTrend.vue'
import Controls from '../components/EnterpriseUsageChartControls.vue'
import DepartmentTree from '../components/EnterpriseUsageDepartmentTree.vue'
import { usageAllowsHourly, usageDepartmentTree, usageStackedSeries, type UsageDepartment, type UsageSeriesPoint } from '../EnterpriseUsageSeries'

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh' } }) }))
vi.mock('vue-chartjs', () => Object.fromEntries(['Bar', 'Doughnut', 'Line'].map(name => [name, { name, props: ['data', 'options'], template: '<canvas />' }])))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })
const departments = [
  { id: 1, name: 'Company', parent_id: null, path_ids: [1], path_names: ['Company'], sort_order: 0 },
  { id: 2, name: 'Engineering', parent_id: 1, path_ids: [1, 2], path_names: ['Company', 'Engineering'], sort_order: 0 },
  { id: 3, name: 'Platform', parent_id: 2, path_ids: [1, 2, 3], path_names: ['Company', 'Engineering', 'Platform'], sort_order: 0 },
  { id: 4, name: 'Sales', parent_id: 1, path_ids: [1, 4], path_names: ['Company', 'Sales'], sort_order: 1 }
] as UsageDepartment[]
const point = (id: number | string, cost = 1, date = '2026-10-01'): UsageSeriesPoint => ({ id, label: String(id), date, requests: 2, total_tokens: 20, actual_cost: cost })
async function setup(admin = true, overview?: EnterpriseOverview) {
  const api = {
    departments: vi.fn().mockResolvedValue(departments), members: vi.fn().mockResolvedValue({ items: [], total: 0 }),
    usage: vi.fn().mockResolvedValue({ items: [], total: 0 }), usageStats: vi.fn().mockResolvedValue({ total_requests: 4, total_tokens: 60, total_actual_cost: 3 }),
    usageTrend: vi.fn().mockResolvedValue([]), usageModels: vi.fn().mockResolvedValue([]), usageMembers: vi.fn().mockResolvedValue([]), usageDepartments: vi.fn().mockResolvedValue([]),
    usageModelsTrend: vi.fn().mockResolvedValue([{ ...point('gpt'), model: 'gpt' }, { ...point('__other__'), model: '__other__' }]),
    usageMembersTrend: vi.fn().mockResolvedValue([{ ...point(7), user_id: 7, username: 'Alice' }]),
    usageDepartmentsTrend: vi.fn().mockResolvedValue([{ ...point(3), department_id: 3, department_name: 'Company / Engineering / Platform' }])
  }
  const wrapper = mount(UsagePanel, { props: { api: api as unknown as EnterpriseResourceAPI, admin, overview }, global: { stubs: { EnterpriseUsageDetails: true, Pagination: true } } })
  wrappers.push(wrapper); await flushPromises()
  return { wrapper, api }
}
async function dates(wrapper: VueWrapper, start: string, end: string) {
  const picker = wrapper.getComponent(DateRangePicker)
  picker.vm.$emit('update:startDate', start); picker.vm.$emit('update:endDate', end)
  await wrapper.get('form').trigger('submit'); await flushPromises()
}
async function allTrends(wrapper: VueWrapper) {
  for (const chart of [wrapper.getComponent(Distribution), ...wrapper.findAllComponents(Ranking)]) chart.vm.$emit('update:view', 'trend')
  await flushPromises()
}

describe('global enterprise usage granularity', () => {
  it('uses submitted filters for all four hourly endpoints, keeps totals and details unchanged and maps identities', async () => {
    const { wrapper, api } = await setup()
    expect(api.usageModelsTrend).not.toHaveBeenCalled()
    await dates(wrapper, '2026-10-01', '2026-10-31')
    await allTrends(wrapper)
    wrapper.getComponent('[data-test="usage-model"]').vm.$emit('update:modelValue', 'draft')
    await wrapper.get('[data-test="usage-granularity-hour"]').trigger('click'); await flushPromises()
    for (const request of [api.usageTrend, api.usageModelsTrend, api.usageMembersTrend, api.usageDepartmentsTrend]) {
      expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-01', end_date: '2026-10-31', granularity: 'hour', model: undefined }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    }
    expect(api.usageModelsTrend.mock.calls.at(-1)![0].top).toBe(8)
    expect(api.usageStats).toHaveBeenCalledTimes(2)
    expect(api.usage).toHaveBeenCalledTimes(2)
    expect(wrapper.getComponent(Distribution).props('trendItems').map((item: UsageSeriesPoint) => item.label)).toEqual(['gpt', '__other__'])
    expect(wrapper.findAllComponents(Ranking)[0].props('trendItems')[0]).toMatchObject({ id: 7, label: 'Alice' })
    expect(wrapper.findAllComponents(Ranking)[1].props('trendItems')[0].label).toBe('Company / Engineering / Platform')
    await wrapper.get('[data-test="usage-granularity-day"]').trigger('click'); await flushPromises()
    expect(api.usageDepartmentsTrend.mock.calls.at(-1)![0].granularity).toBe('day')
  })
  it('disables unbounded and >31-day hourly ranges and automatically falls back on submitted ranges', async () => {
    const { wrapper, api } = await setup()
    expect(wrapper.get('[data-test="usage-granularity-hour"]').attributes('disabled')).toBeUndefined()
    wrapper.getComponent(DateRangePicker).vm.$emit('update:period', null)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[data-test="usage-granularity-hour"]').attributes('disabled')).toBeDefined()
    await dates(wrapper, '2026-10-01', '2026-10-31')
    expect(wrapper.get('[data-test="usage-granularity-hour"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="usage-granularity-hour"]').trigger('click'); await flushPromises()
    await dates(wrapper, '2026-10-01', '2026-11-01')
    expect(wrapper.get('[data-test="usage-granularity-hour"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="usage-granularity-day"]').attributes('data-state')).toBe('on')
    expect(wrapper.get('#usage-hour-limit').text()).toContain('enterprise.usageAnalysis.switchedToDay')
    expect(api.usageTrend.mock.calls.at(-1)![0].granularity).toBe('day')
    expect(api.usageTrend.mock.calls.some(([query]) => query.end_date === '2026-11-01' && query.granularity === 'hour')).toBe(false)
    expect(usageAllowsHourly({ start_date: '2026-10-01', end_date: '2026-10-31' })).toBe(true)
    expect(usageAllowsHourly({ start_date: '2026-10-01', end_date: '2026-11-01' })).toBe(false)
    expect(usageAllowsHourly({ start_date: '2026-10-01' })).toBe(false)
  })
  it('cancels obsolete breakdown reads and rejects late commits', async () => {
    const { wrapper, api } = await setup()
    let resolve!: (value: unknown[]) => void
    api.usageModelsTrend.mockReturnValueOnce(new Promise(done => { resolve = done }))
    await allTrends(wrapper)
    const signal = api.usageModelsTrend.mock.calls.at(-1)![1].signal as AbortSignal
    await dates(wrapper, '2026-10-01', '2026-10-01')
    expect(signal.aborted).toBe(true)
    resolve([{ ...point('stale'), model: 'stale' }]); await flushPromises()
    expect(wrapper.getComponent(Distribution).props('trendItems')[0].label).toBe('gpt')
    const latest = api.usageModelsTrend.mock.calls.at(-1)![1].signal as AbortSignal
    wrapper.unmount(); expect(latest.aborted).toBe(true)
  })
  it('uses fixed compact widths and wraps controls while actions align right', async () => {
    const { wrapper } = await setup()
    const controls = wrapper.get('[data-test="usage-filter-controls"]')
    expect(controls.classes()).toEqual(expect.arrayContaining(['flex', 'flex-wrap', 'gap-2']))
    expect(controls.get('[data-test="usage-date-range"]').element.parentElement?.classList.contains('w-fit')).toBe(true)
    expect(controls.get('[data-test="usage-member"]').classes()).toContain('w-40')
    expect(controls.get('[data-test="usage-api-key"]').classes()).toContain('w-44')
    expect(controls.get('[data-test="usage-search"]').element.parentElement?.classList.contains('ml-auto')).toBe(true)
  })
})

describe('stacked usage trends', () => {
  it('ranks by interval cost, merges excess and backend other rows, sorts buckets and fills missing zeros', () => {
    const rows = Array.from({ length: 10 }, (_, id) => point(id, id + 1))
    rows.push({ ...point(0, 0.5, '2026-10-02'), label: '__other__' }, point(9, 5, '2026-10-02'))
    const result = usageStackedSeries(rows.reverse(), '其他')
    expect(result.dates).toEqual(['2026-10-01', '2026-10-02'])
    expect(result.series).toHaveLength(9)
    expect(result.series[0].id).toBe(9)
    expect(result.series.at(-1)).toMatchObject({ label: '其他', points: [{ actual_cost: 3, total_tokens: 40, requests: 4 }, { actual_cost: 0.5 }] })
    expect(result.series[1].points[1]).toMatchObject({ requests: 0, total_tokens: 0, actual_cost: 0 })
    expect(usageStackedSeries([], '其他')).toEqual({ dates: [], series: [] })
  })
  it('renders a single stacked bucket, all tooltip metrics, shared metric changes and empty state', async () => {
    const wrapper = mount(StackedTrend, { props: { title: 'Models', items: [point('one'), { ...point('__other__'), label: '__other__' }], metric: 'actual_cost', loading: false } })
    wrappers.push(wrapper)
    const chart = wrapper.getComponent(Bar)
    expect(chart.props('data')).toMatchObject({ labels: ['2026-10-01'], datasets: [{ data: [1] }, { label: 'enterprise.usageAnalysis.other', data: [1] }] })
    const options = chart.props('options') as ChartOptions<'bar'>
    expect(options.scales).toMatchObject({ x: { stacked: true }, y: { stacked: true } })
    const tooltip = options.plugins!.tooltip!.callbacks!.label!
    expect(tooltip.call({} as never, { datasetIndex: 0, dataIndex: 0 } as TooltipItem<'bar'>)).toEqual(['one', 'enterprise.requests: 2', 'enterprise.tokens: 20', 'enterprise.cost: $1.0000'])
    await wrapper.setProps({ metric: 'tokens' }); expect(chart.props('data').datasets[0].data).toEqual([20])
    await wrapper.setProps({ metric: 'requests' }); expect(chart.props('data').datasets[0].data).toEqual([2])
    await wrapper.setProps({ items: [] }); expect(wrapper.findComponent(Bar).exists()).toBe(false)
    expect(wrapper.text()).toContain('enterprise.empty')
  })
  it('keeps the metric across share/trend and offers request counts in both model and ranking views', async () => {
    const wrapper = mount(Distribution, { props: { items: [{ model: 'one', requests: 9, total_tokens: 90, actual_cost: 1 }], trendItems: [point('one')], loading: false } })
    wrappers.push(wrapper)
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'requests'); await flushPromises()
    expect(wrapper.getComponent(Doughnut).props('data').datasets[0].data).toEqual([9])
    await wrapper.get('[data-test="usage-view-trend"]').trigger('click'); await flushPromises()
    expect(wrapper.getComponent(Bar).props('data').datasets[0].data).toEqual([2])
    await wrapper.get('[data-test="usage-view-share"]').trigger('click'); await flushPromises()
    expect(wrapper.getComponent(Controls).props('metric')).toBe('requests')
    const ranking = mount(Ranking, { props: { title: 'Members', items: [{ id: 1, label: 'Alice', requests: 9, total_tokens: 90, actual_cost: 1 }], loading: false } })
    wrappers.push(ranking)
    ranking.getComponent(Select).vm.$emit('update:modelValue', 'requests'); await flushPromises()
    expect(ranking.getComponent(Bar).props('data').datasets[0].data).toEqual([9])
  })
})

describe('department usage subtree selection', () => {
  it('builds hierarchy from flat departments and supports keyboard collapse and subtree selection', async () => {
    const wrapper = mount(DepartmentTree, { props: { modelValue: '', departments }, attachTo: document.body, global: { stubs: { Teleport: { template: '<div><slot /></div>' } } } })
    wrappers.push(wrapper)
    await wrapper.get('[data-test="usage-department"]').trigger('click'); await flushPromises()
    const content = wrapper.getComponent(PopoverContent)
    expect(content.findAll('[role="treeitem"]').map(item => item.attributes('aria-level'))).toEqual(['1', '2', '3', '2'])
    await content.get('[data-department-id="2"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(content.find('[data-department-id="3"]').exists()).toBe(false)
    await content.get('[data-department-id="2"]').trigger('keydown', { key: 'ArrowRight' })
    expect(content.find('[data-department-id="3"]').exists()).toBe(true)
    await content.get('[data-department-id="2"]').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['2'])
  })
  it('only exposes manager subtree and sends the selected ancestor id without enumerating descendants', async () => {
    const overview = { me: { role: 'manager', department_id: 2 }, permissions: { view_members: true, view_department_usage: true } } as EnterpriseOverview
    const { wrapper, api } = await setup(false, overview)
    const selector = wrapper.getComponent(DepartmentTree)
    expect(usageDepartmentTree(departments, selector.props('rootId')).map(node => node.department.id)).toEqual([2])
    selector.vm.$emit('update:modelValue', '2')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.usageDepartments.mock.calls.at(-1)![0].department_id).toBe(2)
    selector.vm.$emit('update:modelValue', '4')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.usageDepartments.mock.calls.at(-1)![0].department_id).toBeUndefined()
  })
  it('does not fetch or display department trends without permission', async () => {
    const overview = { me: { role: 'member', department_id: 3 }, permissions: { view_members: false, view_department_usage: false } } as EnterpriseOverview
    const { wrapper, api } = await setup(false, overview)
    expect(wrapper.findComponent(DepartmentTree).exists()).toBe(false)
    expect(wrapper.findAllComponents(Ranking)).toHaveLength(0)
    await allTrends(wrapper)
    expect(api.usageDepartmentsTrend).not.toHaveBeenCalled()
    expect(api.departments).not.toHaveBeenCalled()
  })
})
