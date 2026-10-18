import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import EnterpriseUsagePanel from '../components/EnterpriseUsagePanel.vue'
import DateRangePicker from '../components/EnterpriseDateRangePicker.vue'
import UsageCombobox from '../components/EnterpriseUsageCombobox.vue'
import DepartmentTree from '../components/EnterpriseUsageDepartmentTree.vue'

vi.mock('vue-i18n', async original => {
  const { default: messages } = await import('@/i18n/locales/zh/enterprise')
  return {
    ...await original<typeof import('vue-i18n')>(),
    useI18n: () => ({
      locale: { value: 'zh' },
      t: (key: string) => {
        const value = key.split('.').reduce<unknown>((value, part) => value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined, messages)
        return typeof value === 'string' ? value : key
      }
    })
  }
})
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))

const intersections: { callback: IntersectionObserverCallback; options?: IntersectionObserverInit; disconnect: ReturnType<typeof vi.fn> }[] = []
const resizes: ResizeObserverCallback[] = []
const wrappers: VueWrapper[] = []
let header: HTMLElement
let height = 72

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 18, 12))
  header = document.createElement('header')
  header.dataset.appHeader = ''
  vi.spyOn(header, 'getBoundingClientRect').mockImplementation(() => ({ height }) as DOMRect)
  document.body.append(header)
  vi.stubGlobal('IntersectionObserver', class {
    disconnect = vi.fn()
    observe = vi.fn()
    constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
      intersections.push({ callback, options, disconnect: this.disconnect })
    }
  })
  vi.stubGlobal('ResizeObserver', class {
    observe = vi.fn()
    disconnect = vi.fn()
    constructor(callback: ResizeObserverCallback) { resizes.push(callback) }
  })
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  header.remove()
  height = 72
  intersections.length = 0
  resizes.length = 0
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

async function setup() {
  const api = {
    departments: vi.fn().mockResolvedValue([{ id: 3, name: '研发' }]),
    members: vi.fn().mockResolvedValue({ items: [{ id: 7, user_id: 42, username: 'Alice', email: 'alice@example.com' }], total: 1 }),
    usage: vi.fn().mockResolvedValue({ items: [], total: 0 }),
    usageStats: vi.fn().mockResolvedValue({ total_requests: 0, total_tokens: 0, total_actual_cost: 0 }),
    usageTrend: vi.fn().mockResolvedValue([]), usageModels: vi.fn().mockResolvedValue([]),
    usageMembers: vi.fn().mockResolvedValue([]), usageDepartments: vi.fn().mockResolvedValue([])
  }
  const wrapper = mount(EnterpriseUsagePanel, {
    props: { api: api as unknown as EnterpriseResourceAPI, admin: true },
    global: {
      stubs: { EnterpriseUsageTrend: true, EnterpriseUsageDistribution: true, EnterpriseUsageRanking: true, EnterpriseUsageDetails: true, Pagination: true }
    }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, api }
}
async function intersect(isIntersecting: boolean, top: number) {
  intersections.at(-1)!.callback([{ isIntersecting, boundingClientRect: { top } } as IntersectionObserverEntry], {} as IntersectionObserver)
  await flushPromises()
}

describe('enterprise usage sticky filters', () => {
  it('collapses below the measured header, temporarily expands, restores at the top and follows header resizing', async () => {
    const { wrapper } = await setup()
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(true)
    expect(intersections.at(-1)?.options?.rootMargin).toBe('-72px 0px 0px 0px')
    expect(wrapper.attributes('style')).toContain('--usage-header-height: 72px')
    // A sentinel outside the viewport below the fold has not reached sticky position.
    await intersect(false, 900)
    expect(wrapper.find('[data-test="usage-filter-summary"]').exists()).toBe(false)
    await intersect(false, 50)
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('全部时间 · 按天')
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(false)
    expect(wrapper.get('form').classes()).toContain('z-20')
    await wrapper.get('[data-test="usage-expand-filters"]').trigger('click')
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(true)
    expect(wrapper.get('[data-test="usage-expand-filters"]').attributes('aria-expanded')).toBe('true')
    await intersect(true, 100)
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(true)
    expect(wrapper.find('[data-test="usage-expand-filters"]').exists()).toBe(false)
    await intersect(false, 50)
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(false)
    height = 80
    resizes[0]([], {} as ResizeObserver)
    await flushPromises()
    expect(intersections.at(-1)?.options?.rootMargin).toBe('-80px 0px 0px 0px')
    expect(wrapper.attributes('style')).toContain('--usage-header-height: 80px')
    wrapper.unmount()
    expect(intersections.every(observer => observer.disconnect.mock.calls.length > 0)).toBe(true)
  })

  it('summarizes submitted dates, member, department, model and key names while edits stay staged', async () => {
    const { wrapper, api } = await setup()
    const picker = wrapper.getComponent(DateRangePicker)
    picker.vm.$emit('update:startDate', '2026-10-01'); picker.vm.$emit('update:endDate', '2026-10-10')
    const combos = wrapper.findAllComponents(UsageCombobox)
    combos[0].vm.$emit('update:modelValue', '42')
    combos[1].vm.$emit('update:modelValue', 'gpt-5.4')
    combos[2].vm.$emit('update:modelValue', '8')
    combos[2].vm.$emit('select', { value: '8', label: 'Production · Alice' })
    wrapper.getComponent(DepartmentTree).vm.$emit('update:modelValue', '3')
    expect(api.usageStats).toHaveBeenCalledOnce()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    for (const request of [api.usage, api.usageStats, api.usageTrend, api.usageModels, api.usageMembers, api.usageDepartments]) {
      expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-01', end_date: '2026-10-10', user_id: 42, department_id: 3, model: 'gpt-5.4', api_key_id: 8 }), expect.anything())
    }
    await intersect(false, 0)
    const snapshot = '2026-10-01 – 2026-10-10 · 按天 · Alice · 研发 · gpt-5.4 · Production · Alice'
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe(snapshot)
    await wrapper.get('[data-test="usage-expand-filters"]').trigger('click')
    wrapper.getComponent('[data-test="usage-model"]').vm.$emit('update:modelValue', 'draft-model')
    wrapper.getComponent(DateRangePicker).vm.$emit('update:startDate', '2026-10-02')
    await flushPromises()
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe(snapshot)
    expect(api.usageStats).toHaveBeenCalledTimes(2)
    await wrapper.get('[data-test="usage-reset"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('全部时间 · 按天')
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(false)
    expect(wrapper.get('[data-test="usage-average-cost"]').text()).toContain('$0.0000')
  })

  it('includes the active hourly granularity in the collapsed summary', async () => {
    const { wrapper } = await setup()
    const picker = wrapper.getComponent(DateRangePicker)
    picker.vm.$emit('update:startDate', '2026-10-01'); picker.vm.$emit('update:endDate', '2026-10-10')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    await wrapper.get('[data-test="usage-granularity-hour"]').trigger('click'); await flushPromises()
    await intersect(false, 0)
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('2026-10-01 – 2026-10-10 · 按小时')
  })

  it('preserves shortcut names across sticky collapse and clears them on reset', async () => {
    const { wrapper, api } = await setup()
    const picker = wrapper.getComponent(DateRangePicker)
    picker.vm.$emit('update:startDate', '2026-10-12'); picker.vm.$emit('update:endDate', '2026-10-18')
    picker.vm.$emit('update:period', 'last7')
    await flushPromises()
    expect(wrapper.get('[data-test="usage-date-range"]').text()).toBe('近 7 天')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.usageStats).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-12', end_date: '2026-10-18' }), expect.anything())
    await intersect(false, 0)
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('2026-10-12 – 2026-10-18 · 按天')
    await wrapper.get('[data-test="usage-expand-filters"]').trigger('click')
    expect(wrapper.get('[data-test="usage-date-range"]').text()).toBe('近 7 天')
    await wrapper.get('[data-test="usage-reset"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="usage-expand-filters"]').trigger('click')
    expect(wrapper.get('[data-test="usage-date-range"]').text()).toBe('全部时间')
  })

  it('validates reversed or excessive ranges before issuing requests', async () => {
    const { wrapper, api } = await setup()
    const picker = wrapper.getComponent(DateRangePicker)
    for (const [start, end] of [['2026-10-10', '2026-10-01'], ['2024-01-01', '2026-10-18']]) {
      picker.vm.$emit('update:startDate', start); picker.vm.$emit('update:endDate', end)
      await wrapper.get('form').trigger('submit'); await flushPromises()
      expect(wrapper.find('[role="alert"]').exists()).toBe(true)
      expect(api.usageStats).toHaveBeenCalledOnce()
    }
  })
})
