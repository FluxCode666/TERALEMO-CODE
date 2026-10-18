import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseGroupsPanel from '../components/EnterpriseGroupsPanel.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseEffectiveGroups, EnterpriseOverview, EnterpriseRole } from '@/types/enterprise'
import { selectOption } from './selectOption'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAll: vi.fn() } } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const groups = [1, 2].map(id => ({ id, name: `Group ${id}`, platform: 'openai', rate_multiplier: 1.5, is_exclusive: id === 2, subscription_type: id === 2 ? 'subscription' : 'standard', status: 'active' }))
function result(restricted = true): EnterpriseEffectiveGroups {
  return { member_id: 7, user_id: 42, department_id: 3, pool: { configured: restricted, group_ids: restricted ? [1, 2] : [] }, levels: { enterprise: { configured: restricted, group_ids: [1] }, department: { configured: false, group_ids: [] }, member: { configured: false, group_ids: [] } }, restricted, effective_group_ids: restricted ? [1] : null, groups: restricted ? [groups[0]] : groups }
}
function api() {
  return { groups: vi.fn().mockResolvedValue(groups), groupWhitelists: vi.fn().mockResolvedValue([]), departments: vi.fn().mockResolvedValue([{ id: 3, name: '研发部' }]),
    members: vi.fn().mockResolvedValue({ items: [{ id: 7, username: 'Alice', department_id: 3, department_name: '研发部', role: 'member' }, { id: 8, username: 'Bob', department_id: 4, role: 'member' }], total: 2 }), effectiveGroups: vi.fn().mockResolvedValue(result()), updateGroupWhitelist: vi.fn().mockResolvedValue({}) }
}
const wrappers: VueWrapper[] = []
function render(requests: ReturnType<typeof api>, role: EnterpriseRole = 'owner', editable = true) {
  const overview = { enterprise: { id: 1, name: 'Acme' }, me: { role, member_id: 7, department_id: 3 }, permissions: { view_members: role !== 'member', edit_member_groups: editable } } as EnterpriseOverview
  const wrapper = mount(EnterpriseGroupsPanel, { props: { api: requests as unknown as EnterpriseResourceAPI, enterpriseId: 1, overview }, global: { stubs: { BaseDialog: true } } }); wrappers.push(wrapper); return wrapper
}
beforeEach(() => vi.clearAllMocks())
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
describe('enterprise callable groups', () => {
  it('saves selected cards and explicitly clears restrictions when inheriting', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    const enterprise = wrapper.get('[data-test="group-section-enterprise"]')
    expect(enterprise.text()).not.toContain('enterprise.quotaScope')
    expect(enterprise.find('input[type="checkbox"]').exists()).toBe(false)
    await enterprise.get('[data-test="group-mode-selected"]').trigger('click')
    expect(enterprise.get('.badge-primary').classes()).toContain('badge')
    expect(enterprise.get('.badge-success').classes()).toContain('badge')
    expect(enterprise.get('.badge-gray').classes()).toContain('badge')
    await enterprise.get('form').trigger('submit')
    expect(requests.updateGroupWhitelist).not.toHaveBeenCalled()
    expect(enterprise.text()).toContain('enterprise.groupAccess.selectRequired')
    await enterprise.get('[data-test="group-card-2"]').trigger('click')
    await enterprise.get('form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenLastCalledWith('enterprise', 1, [2])
    await enterprise.get('[data-test="group-mode-inherit"]').trigger('click')
    await enterprise.get('form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenLastCalledWith('enterprise', 1, [])
  })
  it('shows final groups and the levels applying restrictions, and queries by member record ID', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    expect(requests.effectiveGroups).toHaveBeenCalledWith(7, { signal: expect.any(AbortSignal) })
    const final = wrapper.get('[data-test="effective-groups"]')
    expect(final.text()).toContain('Group 1'); expect(final.text()).not.toContain('Group 2')
    expect(final.text()).toContain('enterprise.groupAccess.restrictions.enterprise')
    expect(final.findAll('[data-test="effective-restrictions"] .badge-primary')).toHaveLength(2)
    expect(final.findAll('[data-test="effective-restrictions"] .badge-primary').every(badge => badge.classes().includes('badge'))).toBe(true)
    requests.effectiveGroups.mockResolvedValue(result(false))
    await selectOption(wrapper, '[data-test="effective-member"]', 'Bob · enterprise.noDepartment')
    expect(requests.effectiveGroups).toHaveBeenLastCalledWith(8, expect.anything())
    expect(final.text()).toContain('enterprise.groupAccess.personalRules')
    expect(final.find('[data-test="effective-restrictions"]').exists()).toBe(false)
  })
  it.each([true, false])('limits manager editing to employees in their department (permission %s)', async editable => {
    const requests = api(), wrapper = render(requests, 'manager', editable); await flushPromises()
    expect(wrapper.find('[data-test="group-section-enterprise"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="group-section-department"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="group-target-member"]').text()).toContain('Alice')
    expect(wrapper.text()).not.toContain('Bob')
    expect(wrapper.find('[data-test="group-save"]').exists()).toBe(editable)
    if (editable) {
      await wrapper.get('[data-test="group-mode-selected"]').trigger('click')
      await wrapper.get('[data-test="group-card-1"][aria-pressed]').trigger('click')
      await wrapper.get('form').trigger('submit'); await flushPromises()
      expect(requests.updateGroupWhitelist).toHaveBeenCalledWith('member', 7, [1])
    }
  })
  it('shows an ordinary member’s effective groups without requesting member lists or configurations', async () => {
    const requests = api(), wrapper = render(requests, 'member'); await flushPromises()
    expect(requests.members).not.toHaveBeenCalled(); expect(requests.groupWhitelists).not.toHaveBeenCalled()
    expect(requests.effectiveGroups).toHaveBeenCalledWith(7, expect.anything())
    expect(wrapper.find('[data-test="group-save"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('enterprise.groupAccess.myGroups')
  })
  it('keeps configuration visible when the effective endpoint is unavailable and allows retry', async () => {
    const requests = api(); requests.effectiveGroups.mockRejectedValueOnce({ status: 404 })
    const wrapper = render(requests); await flushPromises()
    expect(wrapper.find('[data-test="group-section-enterprise"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="effective-groups"]').text()).toContain('enterprise.errors.generic')
    await wrapper.get('[data-test="effective-groups"] [role="alert"] button').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="effective-groups"]').text()).toContain('Group 1')
  })
})
