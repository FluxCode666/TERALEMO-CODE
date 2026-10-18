import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseQuotaPanel from '../components/EnterpriseQuotaPanel.vue'
import EnterpriseQuotaDisplay from '../components/EnterpriseQuotaDisplay.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseQuota } from '@/types/enterprise'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
function quota(scope_type: EnterpriseQuota['scope_type'], scope_id: number): EnterpriseQuota {
  return { scope_type, scope_id, daily_limit_usd: 0, weekly_limit_usd: null, monthly_limit_usd: 20, total_limit_usd: 30, daily_usage_usd: 0, weekly_usage_usd: 0, monthly_usage_usd: null, total_usage_usd: 15 }
}
describe('enterprise quota sections', () => {
  it('shows only disabled, unlimited and numeric limits for every window', () => {
    const wrapper = mount(EnterpriseQuotaDisplay, { props: { title: 'Quota', quota: quota('enterprise', 1) } }); wrappers.push(wrapper)
    expect(wrapper.findAll('dd').map(item => item.text())).toEqual(['enterprise.quotaSections.disabled', 'enterprise.unlimited', '$20', '$30'])
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('$15')
  })
  it('groups employees under departments, collapses larger groups, and finds employees by email', async () => {
    const members = Array.from({ length: 7 }, (_, index) => ({ id: index + 10, username: `Employee ${index}`, email: `employee${index}@example.com`, department_id: 3, department_name: '研发部', role: 'member' }))
    const api = { quotas: vi.fn().mockResolvedValue([quota('enterprise', 1), quota('department', 3), ...members.map(member => quota('member', member.id))]), departments: vi.fn().mockResolvedValue([{ id: 3, name: '研发部' }]), members: vi.fn().mockResolvedValue({ items: members, total: 7 }) } as unknown as EnterpriseResourceAPI
    const wrapper = mount(EnterpriseQuotaPanel, { props: { api, admin: true, enterpriseName: 'Acme' }, global: { stubs: { BaseDialog: true, ConfirmDialog: true } } }); wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.get('[data-test="quota-section-enterprise"]').getComponent(EnterpriseQuotaDisplay).props('title')).toBe('Acme')
    expect(wrapper.text()).not.toContain('enterprise.enterpriseScope #1')
    for (const kind of ['enterprise', 'department', 'member']) {
      const section = wrapper.get(`[data-test="quota-section-${kind}"]`)
      expect(section.findAll('dd').slice(0, 4).map(item => item.text())).toEqual(['enterprise.quotaSections.disabled', 'enterprise.unlimited', '$20', '$30'])
      expect(section.find('[role="progressbar"]').exists()).toBe(false)
    }
    expect(wrapper.get('details').attributes('open')).toBeUndefined()
    await wrapper.get('input[type="text"]').setValue('employee5@example.com')
    expect(wrapper.get('details').attributes('open')).toBeDefined()
    expect(wrapper.get('[data-test="quota-section-member"]').text()).toContain('Employee 5')
    expect(wrapper.get('[data-test="quota-section-member"]').text()).not.toContain('Employee 4')
  })
})

it('uses the same compact quota display for all three administrator overview levels', async () => {
  const { default: EnterpriseAdminQuotaSummary } = await import('../components/EnterpriseAdminQuotaSummary.vue')
  const rows = [quota('enterprise', 1), quota('department', 3), quota('member', 7)]
  const api = { quotas: vi.fn().mockResolvedValue(rows), departments: vi.fn().mockResolvedValue([{ id: 3, name: '研发部' }]), members: vi.fn().mockResolvedValue({ items: [{ id: 7, username: 'Alice' }], total: 1 }) } as unknown as EnterpriseResourceAPI
  const wrapper = mount(EnterpriseAdminQuotaSummary, { props: { api } }); wrappers.push(wrapper)
  await flushPromises()
  expect(wrapper.findAllComponents(EnterpriseQuotaDisplay)).toHaveLength(3)
  for (const card of wrapper.findAllComponents(EnterpriseQuotaDisplay)) {
    expect(card.props('compact')).toBe(true)
    expect(card.findAll('dd').map(item => item.text())).toEqual(['enterprise.quotaSections.disabled', 'enterprise.unlimited', '$20', '$30'])
    expect(card.find('[role="progressbar"]').exists()).toBe(false)
  }
  expect(wrapper.text()).toContain('研发部'); expect(wrapper.text()).toContain('Alice')
})
