import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import EnterpriseUsagePanel from '../components/EnterpriseUsagePanel.vue'
import { selectOption } from './selectOption'

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
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('全部时间')
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

  it('summarizes applied conditions and keeps edits staged until search, including reset', async () => {
    const { wrapper, api } = await setup()
    await wrapper.get('[data-test="usage-period-last7"]').trigger('click')
    await selectOption(wrapper, '[data-test="usage-department"]', '研发')
    await wrapper.get('input:not([type])').setValue('gpt-5.4')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    await intersect(false, 0)
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('近 7 天 · 研发 · gpt-5.4')
    await wrapper.get('[data-test="usage-expand-filters"]').trigger('click')
    await wrapper.get('input:not([type])').setValue('draft-model')
    await wrapper.get('[data-test="usage-period-today"]').trigger('click')
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('近 7 天 · 研发 · gpt-5.4')
    expect(api.usageStats).toHaveBeenCalledTimes(2)
    await wrapper.get('form .btn-secondary').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('全部时间')
    expect(wrapper.find('#enterprise-usage-filters').exists()).toBe(false)
    expect(wrapper.get('[data-test="usage-average-cost"]').text()).toContain('$0.0000')
  })

  it('submits common Select and DateRangePicker values with the original API parameter types', async () => {
    const { wrapper, api } = await setup()
    expect(wrapper.find('select').exists()).toBe(false)
    await selectOption(wrapper, '[data-test="usage-member"]', 'Alice')
    await selectOption(wrapper, '[data-test="usage-department"]', '研发')
    await wrapper.get('[data-test="usage-date-range"] button').trigger('click')
    const dates = wrapper.findAll('input[type="date"]')
    await dates[0].setValue('2026-10-01')
    await dates[1].setValue('2026-10-10')
    expect(wrapper.get('.date-picker-apply').attributes('type')).toBe('button')
    expect(wrapper.get('.date-picker-preset').attributes('type')).toBe('button')
    await wrapper.get('.date-picker-apply').trigger('click')
    expect(api.usageStats).toHaveBeenCalledOnce()
    await wrapper.get('input:not([type])').setValue('gpt-5.4')
    await wrapper.get('input[type="number"]').setValue('8')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    for (const request of [api.usage, api.usageStats, api.usageTrend, api.usageModels, api.usageMembers, api.usageDepartments]) {
      expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-01', end_date: '2026-10-10', user_id: 42, department_id: 3, model: 'gpt-5.4', api_key_id: 8 }), expect.anything())
    }
    await intersect(false, 0)
    expect(wrapper.get('[data-test="usage-filter-summary"]').text()).toBe('2026-10-01 – 2026-10-10 · Alice · 研发 · gpt-5.4 · API 密钥 #8')
  })
})
