import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { Bar, Line } from 'vue-chartjs'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import { apiClient } from '@/api/client'
import type { EnterpriseOverview, EnterpriseQuery } from '@/types/enterprise'
import Pagination from '@/components/common/Pagination.vue'
import UsagePanel from '../components/EnterpriseUsagePanel.vue'
import DateRangePicker from '../components/EnterpriseDateRangePicker.vue'
import Distribution from '../components/EnterpriseUsageDistribution.vue'
import Ranking from '../components/EnterpriseUsageRanking.vue'
import Details from '../components/EnterpriseUsageDetails.vue'
import { usageTimeRange } from '../useEnterpriseUsage'
import { usageAllowsHourly } from '../EnterpriseUsageSeries'

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn().mockResolvedValue({ data: [] }) } }))
vi.mock('vue-i18n', async original => {
  const { default: messages } = await import('@/i18n/locales/zh/enterprise')
  return {
    ...await original<typeof import('vue-i18n')>(),
    useI18n: () => ({ locale: { value: 'zh' }, t: (key: string) => messages.enterprise.usageAnalysis[key.split('.').at(-1) as keyof typeof messages.enterprise.usageAnalysis] ?? key })
  }
})
vi.mock('vue-chartjs', () => Object.fromEntries(['Bar', 'Doughnut', 'Line'].map(name => [name, { name, props: ['data', 'options'], template: '<canvas />' }])))
const wrappers: VueWrapper[] = []
const now = new Date('2026-10-18T12:34:56.789+08:00')
const latest = (request: ReturnType<typeof vi.fn>): EnterpriseQuery => request.mock.calls.at(-1)![0]
beforeEach(() => { vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(now); vi.mocked(apiClient.get).mockResolvedValue({ data: [] }) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.useRealTimers(); vi.restoreAllMocks() })
async function setup(admin = false) {
  const points = Array.from({ length: 25 }, (_, hour) => ({ date: new Date(now.getTime() - (24 - hour) * 3600000).toISOString().slice(0, 13).replace('T', ' ') + ':00', requests: 1, total_tokens: 10, actual_cost: 0.1, input_tokens: 10 }))
  const api = {
    departments: vi.fn().mockResolvedValue([]), members: vi.fn().mockResolvedValue({ items: [], total: 0 }),
    usage: vi.fn().mockResolvedValue({ items: [], total: 40 }),
    usageStats: vi.fn().mockResolvedValue({ total_requests: 25, total_tokens: 250, total_actual_cost: 2.5 }),
    usageTrend: vi.fn().mockResolvedValue(points), usageModels: vi.fn().mockResolvedValue([]),
    usageMembers: vi.fn().mockResolvedValue([]), usageDepartments: vi.fn().mockResolvedValue([]),
    usageModelsTrend: vi.fn().mockResolvedValue(points.map(point => ({ ...point, model: 'gpt' }))),
    usageMembersTrend: vi.fn().mockResolvedValue(points.map(point => ({ ...point, user_id: 1, username: 'Alice' }))),
    usageDepartmentsTrend: vi.fn().mockResolvedValue(points.map(point => ({ ...point, department_id: 1, department_name: 'Company' })))
  }
  const wrapper = mount(UsagePanel, {
    props: { api: api as unknown as EnterpriseResourceAPI, admin, overview: { permissions: { view_members: true, view_department_usage: true } } as EnterpriseOverview },
    global: { stubs: { Teleport: { template: '<div><slot /></div>' }, Pagination: true, EnterpriseUsageDetails: true } }
  })
  wrappers.push(wrapper); await flushPromises()
  return { wrapper, api }
}
async function submit(wrapper: VueWrapper) { await wrapper.get('form').trigger('submit'); await flushPromises() }
async function preset(wrapper: VueWrapper, period: string) {
  await wrapper.get('[data-test="usage-date-range"]').trigger('click'); await flushPromises()
  await wrapper.get(`[data-test="usage-period-${period}"]`).trigger('click'); await flushPromises()
}

describe('rolling 24-hour enterprise usage', () => {
  it.each([false, true])('defaults to an exact hourly window in admin scope %s', async admin => {
    const { wrapper, api } = await setup(admin)
    const times = usageTimeRange(now)
    for (const request of [api.usage, api.usageStats, api.usageTrend, api.usageModels, api.usageMembers, api.usageDepartments]) {
      expect(latest(request)).toMatchObject(times)
      expect(latest(request)).not.toHaveProperty('start_date')
      expect(latest(request)).not.toHaveProperty('end_date')
    }
    expect(Date.parse(times.end_time) - Date.parse(times.start_time)).toBe(86400000)
    expect(times.end_time).toMatch(/T\d{2}:\d{2}:\d{2}\.\d{3}[+-]\d{2}:\d{2}$/)
    expect(Date.parse(times.end_time)).toBe(now.getTime())
    expect(latest(api.usageTrend).granularity).toBe('hour')
    expect(wrapper.getComponent(Line).props('data').labels).toHaveLength(25)
    expect(wrapper.get('[data-test="usage-granularity-hour"]').attributes('data-state')).toBe('on')
    expect(wrapper.get('[data-test="usage-granularity-hour"]').attributes('disabled')).toBeUndefined()
    const trigger = wrapper.get('[data-test="usage-date-range"]')
    expect(trigger.text()).toBe('近 24 小时')
    expect(trigger.attributes('title')).toBe(`${times.start_time} – ${times.end_time}`)
  })

  it('reuses snapshots for pagination, sorting, granularity and all breakdown views; search and refresh advance them', async () => {
    const { wrapper, api } = await setup()
    const initial = usageTimeRange(now)
    vi.setSystemTime(now.getTime() + 3600000)
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2); await flushPromises()
    expect(latest(api.usage)).toMatchObject({ ...initial, page: 2 })
    wrapper.getComponent(Details).vm.$emit('sort', 'model', 'asc'); await flushPromises()
    expect(latest(api.usage)).toMatchObject({ ...initial, sort_by: 'model', page: 1 })
    wrapper.getComponent(Pagination).vm.$emit('update:pageSize', 50); await flushPromises()
    expect(latest(api.usage)).toMatchObject({ ...initial, page_size: 50 })
    for (const chart of [wrapper.getComponent(Distribution), ...wrapper.findAllComponents(Ranking)]) chart.vm.$emit('update:view', 'trend')
    await flushPromises()
    for (const request of [api.usageModelsTrend, api.usageMembersTrend, api.usageDepartmentsTrend]) expect(latest(request)).toMatchObject({ ...initial, granularity: 'hour', top: 8 })
    for (const chart of wrapper.findAllComponents(Bar)) expect(chart.props('data').labels).toHaveLength(25)
    await wrapper.get('[data-test="usage-granularity-day"]').trigger('click'); await flushPromises()
    expect(latest(api.usageTrend)).toMatchObject({ ...initial, granularity: 'day' })
    await wrapper.get('[data-test="usage-granularity-hour"]').trigger('click'); await flushPromises()
    expect(latest(api.usageTrend)).toMatchObject({ ...initial, granularity: 'hour' })
    await submit(wrapper)
    const submitted = usageTimeRange(new Date())
    expect(submitted).not.toEqual(initial)
    for (const request of [api.usage, api.usageStats, api.usageTrend, api.usageModelsTrend, api.usageMembersTrend, api.usageDepartmentsTrend]) expect(latest(request)).toMatchObject(submitted)
    vi.setSystemTime(now.getTime() + 7200000)
    wrapper.getComponent('[data-test="usage-model"]').vm.$emit('update:modelValue', 'unsaved')
    await wrapper.setProps({ refreshKey: 1 }); await flushPromises()
    const refreshed = usageTimeRange(new Date())
    for (const request of [api.usage, api.usageStats, api.usageTrend, api.usageModelsTrend, api.usageMembersTrend, api.usageDepartmentsTrend]) {
      expect(latest(request)).toMatchObject(refreshed)
      expect(latest(request).model).toBeUndefined()
    }
    expect(wrapper.get('[data-test="usage-date-range"]').attributes('title')).toBe(`${refreshed.start_time} – ${refreshed.end_time}`)
  })

  it.each(['today', 'last7', 'last30', 'month', 'lastMonth'])('keeps %s as calendar dates and resets to the current hourly window', async period => {
    const { wrapper, api } = await setup()
    await preset(wrapper, period)
    expect(api.usageStats).toHaveBeenCalledOnce()
    await submit(wrapper)
    expect(latest(api.usageStats).start_date).toBeTruthy()
    expect(latest(api.usageStats).end_date).toBeTruthy()
    expect(latest(api.usageStats)).not.toHaveProperty('start_time')
    expect(latest(api.usageStats)).not.toHaveProperty('end_time')
    await wrapper.get('[data-test="usage-granularity-day"]').trigger('click'); await flushPromises()
    vi.setSystemTime(now.getTime() + 3600000)
    await wrapper.get('[data-test="usage-reset"]').trigger('click'); await flushPromises()
    expect(latest(api.usageStats)).toMatchObject(usageTimeRange(new Date()))
    expect(latest(api.usageStats)).not.toHaveProperty('start_date')
    expect(latest(api.usageTrend).granularity).toBe('hour')
    expect(wrapper.get('[data-test="usage-date-range"]').text()).toBe('近 24 小时')
  })

  it('returns from calendar dates to the rolling preset and falls back to daily for >31-day dates', async () => {
    const { wrapper, api } = await setup()
    const picker = wrapper.getComponent(DateRangePicker)
    picker.vm.$emit('update:startDate', '2026-09-01'); picker.vm.$emit('update:endDate', '2026-10-18')
    await submit(wrapper)
    expect(latest(api.usageTrend)).toMatchObject({ start_date: '2026-09-01', end_date: '2026-10-18', granularity: 'day' })
    expect(wrapper.get('[data-test="usage-granularity-hour"]').attributes('disabled')).toBeDefined()
    vi.setSystemTime(now.getTime() + 3600000)
    await preset(wrapper, 'last24')
    expect(wrapper.get('[data-test="usage-date-range"]').text()).toBe('近 24 小时')
    await submit(wrapper)
    await wrapper.get('[data-test="usage-granularity-hour"]').trigger('click'); await flushPromises()
    expect(latest(api.usageTrend)).toMatchObject({ ...usageTimeRange(new Date()), granularity: 'hour' })
    expect(latest(api.usageTrend)).not.toHaveProperty('start_date')
  })

  it('scopes model options to the selected interval and refreshes an applied rolling window independently of draft dates', async () => {
    const { wrapper, api } = await setup()
    const signal = new AbortController().signal
    const searchModels = wrapper.getComponent('[data-test="usage-model"]').props('searchOptions')
    await searchModels('gpt', signal)
    expect(apiClient.get).toHaveBeenLastCalledWith('/enterprise/usage/model-options', { params: { search: 'gpt', limit: 50, ...usageTimeRange(now) }, signal })
    await preset(wrapper, 'last7')
    await searchModels('gpt', signal)
    expect(apiClient.get).toHaveBeenLastCalledWith('/enterprise/usage/model-options', { params: { search: 'gpt', limit: 50, start_date: '2026-10-12', end_date: '2026-10-18' }, signal })
    vi.setSystemTime(now.getTime() + 3600000)
    await wrapper.setProps({ refreshKey: 1 }); await flushPromises()
    expect(latest(api.usageStats)).toMatchObject(usageTimeRange(new Date()))
    expect(latest(api.usageStats)).not.toHaveProperty('start_date')
    expect(wrapper.get('[data-test="usage-date-range"]').text()).toBe('近 7 天')
  })

  it('checks exact elapsed duration and gives time parameters precedence over dates', () => {
    const start_time = '2026-10-01T00:00:00+08:00'
    expect(usageAllowsHourly({ start_time, end_time: '2026-11-01T00:00:00+08:00' })).toBe(true)
    expect(usageAllowsHourly({ start_time, end_time: '2026-11-01T00:00:00.001+08:00' })).toBe(false)
    expect(usageAllowsHourly({ start_time, end_time: start_time })).toBe(false)
    expect(usageAllowsHourly({ start_time, end_time: 'invalid' })).toBe(false)
    expect(usageAllowsHourly({ start_time, start_date: '2026-10-01', end_date: '2026-10-02' })).toBe(false)
    expect(usageAllowsHourly({ ...usageTimeRange(now), start_date: '2026-01-01', end_date: '2026-10-18' })).toBe(true)
    // The clock repeats on this DST transition; elapsed duration remains exactly 24 hours.
    const range = usageTimeRange(new Date('2026-11-01T12:00:00-05:00'))
    expect(Date.parse(range.end_time) - Date.parse(range.start_time)).toBe(86400000)
  })
})
