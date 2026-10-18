import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseOverview, EnterpriseQuota } from '@/types/enterprise'
import EnterpriseQuotaCell from '../components/EnterpriseQuotaCell.vue'
import EnterpriseQuotaTable from '../components/EnterpriseQuotaTable.vue'
import EnterpriseQuotaPanel from '../components/EnterpriseQuotaPanel.vue'
import { quotaCell, quotaPercentage, quotaStatus } from '../quotaPresentation'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, args?: Record<string, number>) => key + (args ? ' ' + JSON.stringify(args) : '') }) }))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })
function quota(overrides: Partial<EnterpriseQuota> = {}): EnterpriseQuota {
  return { scope_type: 'member', scope_id: 7, daily_limit_usd: 10, weekly_limit_usd: null, monthly_limit_usd: null, total_limit_usd: null, daily_usage_usd: 8, weekly_usage_usd: 9, monthly_usage_usd: 9, total_usage_usd: 9, ...overrides }
}
describe('quota usage cells and status', () => {
  it.each([
    [{ daily_limit_usd: null }, '$8 / enterprise.unlimited', false, 'unlimited'],
    [{ daily_limit_usd: 0 }, 'enterprise.quotaSections.disabled', false, 'disabled'],
    [{ daily_usage_usd: null }, 'enterprise.quotaTable.hidden', false, 'hidden'],
    [{ usage_visible: false }, 'enterprise.quotaTable.hidden', false, 'hidden'],
    [{ daily_usage_usd: 7.99 }, '79.9%', true, 'normal'],
    [{ daily_usage_usd: 8 }, '80%', true, 'warning'],
    [{ daily_usage_usd: 10 }, '100%', true, 'exceeded'],
    [{ daily_usage_usd: 12.5 }, '125%', true, 'exceeded']
  ] as const)('renders the limit/usage rule %j', (overrides, expected, progress, tone) => {
    const row = quota(overrides)
    const wrapper = mount(EnterpriseQuotaCell, { props: { quota: row, window: 'daily' } }); wrappers.push(wrapper)
    expect(wrapper.text()).toContain(expected)
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(progress)
    expect(quotaStatus(row).tone).toBe(tone)
    if (!progress) expect(wrapper.text()).not.toContain('%')
  })
  it('uses the highest percentage across windows, caps visual progress, and never leaks hidden usage', () => {
    const row = quota({ daily_usage_usd: 1, weekly_limit_usd: 20, weekly_usage_usd: 22 })
    expect(quotaPercentage(1, 4)).toBe(25)
    expect(quotaStatus(row).tone).toBe('exceeded')
    expect(quotaStatus(row).percent).toBeCloseTo(110)
    const wrapper = mount(EnterpriseQuotaCell, { props: { quota: row, window: 'weekly' } }); wrappers.push(wrapper)
    expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('100')
    expect(wrapper.text()).toContain('110%')
    expect(quotaCell({ ...row, usage_visible: false }, 'weekly').percent).toBeNull()
    const table = mount(EnterpriseQuotaTable, { props: { kind: 'member', rows: [{ id: 7, label: 'Alice', quota: { ...row, usage_visible: false } }], canEdit: () => false } }); wrappers.push(table)
    expect(table.get('[data-test="quota-status"]').attributes('data-status')).toBe('hidden')
    expect(table.text()).not.toContain('110%'); expect(table.find('[role="progressbar"]').exists()).toBe(false)
  })
})
function requests(count = 12) {
  const members = Array.from({ length: count }, (_, i) => ({ id: i + 7, username: `Member ${i}`, email: `member${i}@example.com`, department_id: 3, role: 'member' }))
  return { quotas: vi.fn().mockResolvedValue(members.map(member => quota({ scope_id: member.id }))), departments: vi.fn().mockResolvedValue([]), members: vi.fn().mockResolvedValue({ items: members, total: count }), batchMemberQuotas: vi.fn().mockResolvedValue({ updated: members.map(member => member.id), failed: [] }) }
}
async function render(api = requests(), overview?: EnterpriseOverview) {
  const wrapper = mount(EnterpriseQuotaPanel, { props: { api: api as unknown as EnterpriseResourceAPI, admin: !overview, overview }, global: { stubs: { EnterpriseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, EnterpriseConfirm: true } } }); wrappers.push(wrapper)
  await flushPromises(); return { wrapper, api }
}
describe('member quota selection and batch editing', () => {
  it('selects the current page, supports indeterminate state, and sends only checked fields including zero/null', async () => {
    const { wrapper, api } = await render()
    expect(wrapper.findAll('[data-test^="quota-select-"]').length).toBe(11)
    await wrapper.get('[data-test="quota-select-7"]').trigger('click')
    expect(wrapper.get('[data-test="quota-select-page"]').attributes('data-state')).toBe('indeterminate')
    await wrapper.get('[data-test="quota-select-page"]').trigger('click')
    expect(wrapper.findAll('[data-test^="quota-select-"][aria-checked="true"]').length).toBe(11)
    expect(wrapper.get('[data-test="quota-batch-toolbar"]').text()).toContain('10')
    await wrapper.get('[data-test="quota-batch-open"]').trigger('click')
    await wrapper.get('[data-test="quota-daily"]').setValue('0')
    await wrapper.get('[data-test="quota-batch-field-total"]').trigger('click')
    await wrapper.get('[data-test="quota-edit-form"]').trigger('submit'); await flushPromises()
    expect(api.batchMemberQuotas).toHaveBeenCalledWith({ member_ids: Array.from({ length: 10 }, (_, i) => i + 7), fields: ['daily', 'total'], daily_limit_usd: 0, total_limit_usd: null })
    expect(wrapper.find('[data-test="quota-batch-result"]').exists()).toBe(true)
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })
  it('lists partial failures by member name and keeps the failure reason after refreshing rows', async () => {
    const api = requests(2); api.batchMemberQuotas.mockResolvedValue({ updated: [7], failed: [{ member_id: 8, code: 'ENTERPRISE_QUOTA_EXCEEDS_PARENT', message: '超过上级上限' }] } as never)
    const { wrapper } = await render(api)
    await wrapper.get('[data-test="quota-select-page"]').trigger('click')
    await wrapper.get('[data-test="quota-batch-open"]').trigger('click')
    await wrapper.get('[data-test="quota-edit-form"]').trigger('submit'); await flushPromises()
    const result = wrapper.get('[data-test="quota-batch-result"]')
    expect(result.text()).toContain('"updated":1,"failed":1')
    expect(result.text()).toContain('Member 1'); expect(result.text()).toContain('超过上级上限')
    expect(wrapper.get('[data-test="quota-select-7"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[data-test="quota-select-8"]').attributes('aria-checked')).toBe('true')
    expect(api.quotas).toHaveBeenCalledTimes(2)
  })
  it('does not submit with no fields, and limits editable selections to manager subtree members', async () => {
    const api = requests(3)
    api.departments.mockResolvedValue([{ id: 3, name: 'Root' }, { id: 4, name: 'Child', parent_id: 3 }, { id: 6, name: 'Outside' }] as never)
    api.members.mockResolvedValue({ items: [{ id: 7, username: 'Alice', role: 'member', department_id: 4 }, { id: 8, username: 'Manager', role: 'manager', department_id: 3 }, { id: 9, username: 'Outside', role: 'member', department_id: 6 }], total: 3 } as never)
    const overview = { me: { member_id: 8, department_id: 3, role: 'manager' }, permissions: { edit_member_quota: true } } as EnterpriseOverview
    const { wrapper } = await render(api, overview)
    expect(wrapper.find('[data-test="quota-select-7"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="quota-select-8"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="quota-select-9"]').exists()).toBe(false)
    await wrapper.get('[data-test="quota-select-page"]').trigger('click')
    await wrapper.get('[data-test="quota-batch-open"]').trigger('click')
    await wrapper.get('[data-test="quota-batch-field-daily"]').trigger('click')
    await wrapper.get('[data-test="quota-edit-form"]').trigger('submit')
    expect(wrapper.text()).toContain('enterprise.quotaTable.fieldsRequired')
    expect(api.batchMemberQuotas).not.toHaveBeenCalled()
  })
})
