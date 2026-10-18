import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseUsageDetails from '../components/EnterpriseUsageDetails.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); localStorage.clear(); vi.restoreAllMocks() })

describe('enterprise usage table on narrow screens', () => {
  function mobileViewport() {
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: false, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() }))
  }
  it('keeps a scrollable table and working sort headers on narrow screens', async () => {
    mobileViewport()
    const wrapper = mount(EnterpriseUsageDetails, { props: { rows: [{ id: 1, model: 'gpt-5', user: { id: 42, username: 'Alice', email: 'alice@example.com' }, created_at: '2026-10-18T00:00:00Z' }], total: 1, departments: [], loading: false }, global: { stubs: { IpGeoCell: true } } })
    wrappers.push(wrapper); await flushPromises()
    expect(wrapper.get('.table-wrapper').find('table').exists()).toBe(true)
    const member = wrapper.get('[data-test="usage-row-member"]')
    expect(member.findAll('p').map(line => line.text())).toEqual(['Alice', 'alice@example.com'])
    expect(member.get('p.text-xs').classes()).toEqual(expect.arrayContaining(['truncate', 'text-gray-500', 'dark:text-gray-400']))
    expect(wrapper.find('.sticky-col').exists()).toBe(false)
    expect(wrapper.find('[data-field="model"]').exists()).toBe(false)
    const modelHeader = wrapper.findAll('th').find(header => header.text() === 'usage.model')!
    await modelHeader.trigger('click')
    expect(wrapper.emitted('sort')?.at(-1)).toEqual(['model', 'asc'])
    await modelHeader.trigger('click')
    expect(wrapper.emitted('sort')?.at(-1)).toEqual(['model', 'desc'])
  })

  it('keeps the shared personal table in its default card layout on narrow screens', async () => {
    mobileViewport()
    const wrapper = mount(UsageTable, { props: { data: [], columns: [{ key: 'model', label: 'Model' }], loading: false }, global: { stubs: { IpGeoCell: true } } })
    wrappers.push(wrapper); await flushPromises()
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.table-wrapper').exists()).toBe(false)
  })
})
