import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { PopoverContent } from '@/components/ui/popover'
import { Input } from '@/components/ui/input'
import EnterpriseUsageCombobox from '../components/EnterpriseUsageCombobox.vue'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
beforeEach(() => { HTMLElement.prototype.scrollIntoView = vi.fn() })
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers(); vi.restoreAllMocks(); Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView') })
async function setup() {
  const searchOptions = vi.fn().mockResolvedValue([{ value: '8', label: 'Production · Alice', detail: '42 requests' }])
  const wrapper = mount(EnterpriseUsageCombobox, { props: { modelValue: '', label: 'API Key', searchOptions }, attachTo: document.body, global: { stubs: { Teleport: { template: '<div><slot /></div>' } } } })
  wrappers.push(wrapper)
  await wrapper.get('[role="combobox"]').trigger('click'); await flushPromises()
  return { wrapper, searchOptions }
}
describe('enterprise searchable usage filters', () => {
  it('debounces remote searches, aborts obsolete requests and selects the named candidate', async () => {
    const { wrapper, searchOptions } = await setup()
    vi.useFakeTimers()
    const content = wrapper.getComponent(PopoverContent)
    await content.getComponent(Input).setValue('Pro')
    await vi.advanceTimersByTimeAsync(150)
    expect(searchOptions).toHaveBeenCalledTimes(1)
    await content.getComponent(Input).setValue('Production')
    await vi.advanceTimersByTimeAsync(249)
    expect(searchOptions).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1); await flushPromises()
    expect(searchOptions).toHaveBeenLastCalledWith('Production', expect.any(AbortSignal))
    expect(searchOptions.mock.calls[0][1].aborted).toBe(true)
    const candidate = content.findAll('[role="option"]').find(item => item.text().includes('Production'))!
    await candidate.trigger('click'); await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['8']])
    expect(wrapper.emitted('select')).toEqual([[{ value: '8', label: 'Production · Alice', detail: '42 requests' }]])
    await wrapper.setProps({ modelValue: '8', selectedLabel: 'Production · Alice' })
    expect(wrapper.get('[role="combobox"]').text()).toContain('Production · Alice')
    await wrapper.get('button[aria-label="enterprise.usageAnalysis.clear API Key"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([''])
  })
  it('supports arrow navigation and Enter selection from the search field', async () => {
    const { wrapper } = await setup()
    const content = wrapper.getComponent(PopoverContent)
    const input = content.get('input')
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['8'])
  })
  it('ignores late search results, exposes failures and retries', async () => {
    const { wrapper, searchOptions } = await setup()
    let resolveOld!: (items: unknown[]) => void
    searchOptions.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    vi.useFakeTimers()
    const content = wrapper.getComponent(PopoverContent)
    await content.getComponent(Input).setValue('old'); await vi.advanceTimersByTimeAsync(250)
    searchOptions.mockRejectedValueOnce(new Error('offline'))
    await content.getComponent(Input).setValue('new'); await vi.advanceTimersByTimeAsync(250); await flushPromises()
    resolveOld([{ value: 'old', label: 'Obsolete' }]); await flushPromises()
    expect(content.text()).not.toContain('Obsolete')
    expect(content.find('[role="alert"]').exists()).toBe(true)
    await content.get('button').trigger('click'); await flushPromises()
    expect(content.find('[role="alert"]').exists()).toBe(false)
    expect(content.text()).toContain('Production · Alice')
  })
  it('searches the existing member list without another endpoint and shows empty matches', async () => {
    const wrapper = mount(EnterpriseUsageCombobox, { props: { modelValue: '', label: 'Members', options: [{ value: '42', label: 'Alice', detail: 'alice@example.com' }] }, attachTo: document.body, global: { stubs: { Teleport: { template: '<div><slot /></div>' } } } })
    wrappers.push(wrapper)
    await wrapper.get('[role="combobox"]').trigger('click'); await flushPromises()
    vi.useFakeTimers()
    const content = wrapper.getComponent(PopoverContent)
    await content.getComponent(Input).setValue('example.com'); await vi.advanceTimersByTimeAsync(250); await flushPromises()
    expect(content.text()).toContain('Alice')
    await content.getComponent(Input).setValue('missing'); await vi.advanceTimersByTimeAsync(250); await flushPromises()
    expect(content.text()).toContain('enterprise.usageAnalysis.noOptions')
  })
})
