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
  const wrapper = mount(EnterpriseGroupsPanel, { props: { api: requests as unknown as EnterpriseResourceAPI, enterpriseId: 1, overview }, global: { stubs: { EnterpriseDialog: true } } }); wrappers.push(wrapper); return wrapper
}
beforeEach(() => vi.clearAllMocks())
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
describe('enterprise callable groups', () => {
  it('saves selected cards and explicitly clears restrictions when inheriting', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    const enterprise = wrapper.get('[data-test="group-section-enterprise"]')
    expect(enterprise.text()).not.toContain('enterprise.quotaScope')
    expect(enterprise.find('input[type="checkbox"]').exists()).toBe(false)
    await enterprise.get('[data-test="group-radio-selected"]').trigger('click')
    expect(enterprise.get('[data-test="group-card-2"]').text()).toContain('enterprise.groupAccess.exclusive')
    expect(enterprise.get('[data-test="group-card-2"]').text()).toContain('enterprise.groupAccess.subscription')
    expect(enterprise.get('[data-test="group-card-1"]').text()).toContain('OpenAI')
    await enterprise.get('form').trigger('submit')
    expect(requests.updateGroupWhitelist).not.toHaveBeenCalled()
    expect(enterprise.text()).toContain('enterprise.groupAccess.selectRequired')
    await enterprise.get('[data-test="group-card-2"]').trigger('click')
    await enterprise.get('form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenLastCalledWith('enterprise', 1, [2])
    await enterprise.get('[data-test="group-radio-inherit"]').trigger('click')
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
  it.each([true, false])('keeps managers read-only regardless of the legacy edit permission (%s)', async editable => {
    const requests = api(), wrapper = render(requests, 'manager', editable); await flushPromises()
    expect(wrapper.find('[data-test="group-section-enterprise"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="group-section-department"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="group-section-member"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="group-radio-selected"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="group-save"]').exists()).toBe(false)
    expect(requests.members).not.toHaveBeenCalled(); expect(requests.departments).not.toHaveBeenCalled()
  })
  it('selects all candidate cards and clears only the enterprise whitelist', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    const enterprise = wrapper.get('[data-test="group-section-enterprise"]')
    await enterprise.get('[data-test="group-radio-selected"]').trigger('click')
    await enterprise.get('[data-test="group-select-all"]').trigger('click')
    expect(enterprise.findAll('[aria-pressed="true"]')).toHaveLength(2)
    await enterprise.get('form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenLastCalledWith('enterprise', 1, [1, 2])
    await enterprise.get('[data-test="group-radio-selected"]').trigger('click')
    await enterprise.get('[data-test="group-select-all"]').trigger('click')
    await enterprise.get('[data-test="group-clear"]').trigger('click')
    expect(enterprise.findAll('[aria-pressed="true"]')).toHaveLength(0)
  })
  it('displays an unconfigured pool and never shows department/member configurations or a pool editor', async () => {
    const requests = api(); requests.groups.mockResolvedValue([])
    const wrapper = render(requests); await flushPromises()
    expect(wrapper.text()).toContain('enterprise.groupAccess.personalRules')
    expect(wrapper.find('[data-test="group-section-department"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="group-section-member"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('enterprise.groupAccess.editAvailable')
    expect(requests.departments).not.toHaveBeenCalled(); expect(requests.members).not.toHaveBeenCalled()
  })
  it('allows the administrator to configure only the company while the pool remains read-only', async () => {
    const requests = { ...api(), allowedGroups: vi.fn().mockResolvedValue(groups), updateAllowedGroups: vi.fn() }
    const wrapper = render(requests); await wrapper.setProps({ admin: true }); await flushPromises()
    expect(wrapper.find('[data-test="group-save"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="group-pool"]').find('button').exists()).toBe(false)
    await wrapper.get('[data-test="group-section-enterprise"] form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenCalledWith('enterprise', 1, [])
    expect(requests.updateAllowedGroups).not.toHaveBeenCalled()
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
