import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { parseDate } from '@internationalized/date'
import { RangeCalendar } from '@/components/ui/range-calendar'
import { PopoverContent } from '@/components/ui/popover'
import EnterpriseDateRangePicker from '../components/EnterpriseDateRangePicker.vue'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh' } }) }))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers(); vi.restoreAllMocks() })
async function setup(wide = true, startDate = '', endDate = '') {
  vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date(2026, 9, 18, 12))
  vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: wide, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() }))
  const wrapper = mount(EnterpriseDateRangePicker, { props: { startDate, endDate }, attachTo: document.body, global: { stubs: { Teleport: { template: '<div><slot /></div>' } } } })
  wrappers.push(wrapper)
  await wrapper.get('button').trigger('click'); await flushPromises()
  return wrapper
}
describe('enterprise date range picker', () => {
  it.each([
    ['today', '2026-10-18', '2026-10-18'], ['last7', '2026-10-12', '2026-10-18'],
    ['last30', '2026-09-19', '2026-10-18'], ['month', '2026-10-01', '2026-10-18'],
    ['lastMonth', '2026-09-01', '2026-09-30']
  ])('commits the %s shortcut using API date strings', async (period, start, end) => {
    const wrapper = await setup()
    const content = wrapper.getComponent(PopoverContent)
    expect(content.findAll('input[type="date"]')).toHaveLength(0)
    expect(content.get(`[data-test="usage-period-${period}"]`).attributes('type')).toBe('button')
    await content.get(`[data-test="usage-period-${period}"]`).trigger('click')
    expect(wrapper.emitted('update:startDate')?.at(-1)).toEqual([start])
    expect(wrapper.emitted('update:endDate')?.at(-1)).toEqual([end])
  })
  it('keeps a partial interval staged and commits a complete calendar selection', async () => {
    const wrapper = await setup()
    const calendar = wrapper.getComponent(RangeCalendar)
    expect(calendar.props()).toMatchObject({ locale: 'zh-CN', numberOfMonths: 2 })
    calendar.vm.$emit('update:modelValue', { start: parseDate('2026-10-02'), end: undefined }); await flushPromises()
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    calendar.vm.$emit('update:modelValue', { start: parseDate('2026-10-02'), end: parseDate('2026-10-09') }); await flushPromises()
    expect(wrapper.emitted('update:startDate')).toEqual([['2026-10-02']])
    expect(wrapper.emitted('update:endDate')).toEqual([['2026-10-09']])
  })
  it('accepts a date interval through calendar day buttons', async () => {
    const wrapper = await setup()
    const content = wrapper.getComponent(PopoverContent)
    await content.get('[role="button"][aria-label="2026年10月2日星期五"]').trigger('click')
    expect(wrapper.emitted('update:startDate')).toBeUndefined()
    await content.get('[role="button"][aria-label="2026年10月9日星期五"]').trigger('mouseenter')
    await content.get('[role="button"][aria-label="2026年10月9日星期五"]').trigger('click')
    expect(wrapper.emitted('update:startDate')?.at(-1)).toEqual(['2026-10-02'])
    expect(wrapper.emitted('update:endDate')?.at(-1)).toEqual(['2026-10-09'])
  })
  it('uses one month on narrow screens, shows one combined trigger and clears both dates', async () => {
    const wrapper = await setup(false, '2026-10-01', '2026-10-09')
    expect(wrapper.getComponent(RangeCalendar).props('numberOfMonths')).toBe(1)
    expect(wrapper.get('[data-test="usage-date-range"]').text()).toContain('2026-10-01 – 2026-10-09')
    await wrapper.getComponent(PopoverContent).get('[data-test="usage-date-clear"]').trigger('click')
    expect(wrapper.emitted('update:startDate')).toEqual([['']])
    expect(wrapper.emitted('update:endDate')).toEqual([['']])
  })
})
