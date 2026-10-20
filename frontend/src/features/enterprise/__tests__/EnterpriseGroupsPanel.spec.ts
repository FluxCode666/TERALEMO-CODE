import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseGroupsPanel from '../components/EnterpriseGroupsPanel.vue'
import { Input } from '@/components/ui/input'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseGroup, EnterpriseOverview, EnterpriseRole } from '@/types/enterprise'
const { getAllGroups } = vi.hoisted(() => ({ getAllGroups: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin/groups', () => ({ getAll: getAllGroups }))
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
function adminAPI(pool: EnterpriseGroup[] = groups) {
  return { ...api(), allowedGroups: vi.fn().mockResolvedValue(pool), updateAllowedGroups: vi.fn().mockResolvedValue(pool) }
}
async function renderAdmin(requests: ReturnType<typeof adminAPI>) {
  const wrapper = render(requests)
  await wrapper.setProps({ admin: true, refreshKey: 1 })
  await flushPromises()
  return wrapper
}
beforeEach(() => { vi.clearAllMocks(); getAllGroups.mockResolvedValue(groups) })
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
  it('keeps company whitelist updates separate from the administrator group pool editor', async () => {
    const requests = adminAPI(), wrapper = await renderAdmin(requests)
    expect(wrapper.find('[data-test="group-save"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="group-pool-edit"]').exists()).toBe(true)
    await wrapper.get('[data-test="group-section-enterprise"] form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenCalledWith('enterprise', 1, [])
    expect(requests.updateAllowedGroups).not.toHaveBeenCalled()
  })
  it('saves enterprise rates and clears a previous override with null', async () => {
    const pool = groups.map(group => ({ ...group, rate_multiplier: group.id === 1 ? 0.8 : null, default_rate_multiplier: group.id === 1 ? 1.5 : 2, effective_rate_multiplier: group.id === 1 ? 0.8 : 2 }))
    const requests = adminAPI(pool), wrapper = await renderAdmin(requests)
    expect(wrapper.get('[data-test="group-pool"] [data-test="group-card-1"]').text()).toContain('0.8×')
    expect(wrapper.get('[data-test="group-pool"] [data-test="group-card-2"]').text()).toContain('2×')
    await wrapper.get('[data-test="group-pool-edit"]').trigger('click'); await flushPromises()
    expect(getAllGroups).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-test="group-pool-rate-1"]').attributes('placeholder')).toBe('1.5')
    expect((wrapper.get('[data-test="group-pool-rate-1"]').element as HTMLInputElement).value).toBe('0.8')
    await wrapper.get('[data-test="group-pool-rate-1"]').setValue('')
    await wrapper.get('[data-test="group-pool-rate-2"]').setValue('0.6')
    await wrapper.get('[data-test="group-pool-form"]').trigger('submit'); await flushPromises()
    expect(requests.updateAllowedGroups).toHaveBeenCalledWith({ groups: [{ group_id: 1, rate_multiplier: null }, { group_id: 2, rate_multiplier: 0.6 }] })
    expect(wrapper.find('[data-test="group-pool-form"]').exists()).toBe(false)
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(requests.allowedGroups).toHaveBeenCalledTimes(2)
  })
  it('validates zero, negative and non-finite rates before saving', async () => {
    const requests = adminAPI(), wrapper = await renderAdmin(requests)
    await wrapper.get('[data-test="group-pool-edit"]').trigger('click'); await flushPromises()
    const input = wrapper.findAllComponents(Input).find(component => component.attributes('data-test') === 'group-pool-rate-1')!
    for (const value of ['0', '-0.1', 'Infinity', 'NaN']) {
      input.vm.$emit('update:modelValue', value)
      await wrapper.get('[data-test="group-pool-form"]').trigger('submit'); await flushPromises()
      expect(wrapper.get('[role="alert"]').text()).toContain('enterprise.groupAccess.validationRate')
      expect(requests.updateAllowedGroups).not.toHaveBeenCalled()
    }
    input.vm.$emit('update:modelValue', '0.6')
    await wrapper.get('[data-test="group-pool-form"]').trigger('submit'); await flushPromises()
    expect(requests.updateAllowedGroups).toHaveBeenCalledWith({ groups: [{ group_id: 1, rate_multiplier: 0.6 }, { group_id: 2, rate_multiplier: 1.5 }] })
  })
  it('saves rates only for selected groups, including a new group following its default', async () => {
    const requests = adminAPI([groups[0]]), wrapper = await renderAdmin(requests)
    await wrapper.get('[data-test="group-pool-edit"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="group-pool-form"] [data-test="group-card-1"]').trigger('click')
    await wrapper.get('[data-test="group-pool-form"] [data-test="group-card-2"]').trigger('click')
    expect(wrapper.find('[data-test="group-pool-rate-1"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="group-pool-rate-2"]').attributes('placeholder')).toBe('1.5')
    await wrapper.get('[data-test="group-pool-form"]').trigger('submit'); await flushPromises()
    expect(requests.updateAllowedGroups).toHaveBeenCalledWith({ groups: [{ group_id: 2, rate_multiplier: null }] })
  })
  it.each(['owner', 'manager', 'member'] as const)('shows final rates to %s without enterprise rate editing', async role => {
    const requests = adminAPI()
    requests.groups.mockResolvedValue(groups.map(group => ({ ...group, rate_multiplier: 0.6, enterprise_rate_multiplier: 0.6, default_rate_multiplier: 1.5 })))
    const wrapper = render(requests, role); await flushPromises()
    expect(wrapper.get('[data-test="group-pool"] [data-test="group-card-1"]').text()).toContain('0.6×')
    expect(wrapper.find('[data-test="group-pool-edit"]').exists()).toBe(false)
    expect(wrapper.find('[data-test^="group-pool-rate-"]').exists()).toBe(false)
    expect(requests.allowedGroups).not.toHaveBeenCalled()
    expect(getAllGroups).not.toHaveBeenCalled()
    expect(requests.updateAllowedGroups).not.toHaveBeenCalled()
  })
  it('hides the pool editor for a readonly administrator', async () => {
    const requests = adminAPI(), wrapper = await renderAdmin(requests)
    await wrapper.setProps({ readonly: true })
    expect(wrapper.find('[data-test="group-pool-edit"]').exists()).toBe(false)
  })
  it('lets administrators pick public groups when the enterprise pool is unrestricted', async () => {
    const requests = adminAPI([]), wrapper = await renderAdmin(requests)
    expect(getAllGroups).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-test="group-pool-empty"]').text()).toBe('enterprise.groupAccess.adminEmptyPool')
    const enterprise = wrapper.get('[data-test="group-section-enterprise"]')
    await enterprise.get('[data-test="group-radio-selected"]').trigger('click')
    expect(enterprise.find('[data-test="group-card-1"]').exists()).toBe(true)
    expect(enterprise.find('[data-test="group-card-2"]').exists()).toBe(false)
    await enterprise.get('[data-test="group-card-1"]').trigger('click')
    await enterprise.get('form').trigger('submit'); await flushPromises()
    expect(requests.updateGroupWhitelist).toHaveBeenCalledWith('enterprise', 1, [1])
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
