import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseGroupsPanel from '../components/EnterpriseGroupsPanel.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseOverview, EnterpriseRole } from '@/types/enterprise'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAll: vi.fn() } } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const groups = [1, 2].map(id => ({ id, name: `Group ${id}`, platform: 'openai', rate_multiplier: 1.5, is_exclusive: id === 2, subscription_type: id === 2 ? 'subscription' : 'standard', status: 'active' }))
function api() {
  return { groups: vi.fn().mockResolvedValue(groups), groupWhitelists: vi.fn().mockResolvedValue([]), departments: vi.fn().mockResolvedValue([{ id: 3, name: '研发部' }]),
    members: vi.fn().mockResolvedValue({ items: [{ id: 7, username: 'Alice', department_id: 3, department_name: '研发部', role: 'member' }, { id: 8, username: 'Bob', department_id: 4, role: 'member' }], total: 2 }), updateGroupWhitelist: vi.fn().mockResolvedValue({}) }
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
  it('shows the group pool and configuration without the removed employee lookup', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    expect(wrapper.text()).toContain('Group 1')
    expect(wrapper.text()).toContain('Group 2')
    expect(wrapper.find('[data-test="effective-groups"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="effective-member"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="group-section-enterprise"]').exists()).toBe(true)
  })
  it('filters employee candidates using the pool and parent whitelists alone', async () => {
    const requests = api()
    requests.groupWhitelists.mockResolvedValue([
      { scope_type: 'enterprise', scope_id: 1, group_ids: [1, 2] },
      { scope_type: 'department', scope_id: 3, group_ids: [2] }
    ])
    const wrapper = render(requests); await flushPromises()
    const member = wrapper.get('[data-test="group-section-member"]')
    await member.get('[data-test="group-mode-selected"]').trigger('click')
    expect(member.find('[data-test="group-card-1"]').exists()).toBe(false)
    await member.get('[data-test="group-card-2"]').trigger('click')
    await member.get('form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenCalledWith('member', 7, [2])
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
  it('shows only the enterprise pool to ordinary members without requesting configurations', async () => {
    const requests = api(), wrapper = render(requests, 'member'); await flushPromises()
    expect(requests.groups).toHaveBeenCalledOnce()
    expect(requests.members).not.toHaveBeenCalled()
    expect(requests.groupWhitelists).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="group-save"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="effective-groups"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Group 1')
    expect(wrapper.text()).toContain('Group 2')
  })
})
