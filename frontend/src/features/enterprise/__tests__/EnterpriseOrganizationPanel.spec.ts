import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseOrganizationPanel from '../components/EnterpriseOrganizationPanel.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseOverview } from '@/types/enterprise'
import { departments, members } from './organizationFixtures'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: vi.fn() } } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, args?: Record<string, string>) => key + (args?.path ? ` ${args.path}` : '') }) }))
const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
function api() {
  return { departments: vi.fn().mockResolvedValue(departments), members: vi.fn().mockResolvedValue({ items: members, total: 4 }), createDepartment: vi.fn().mockResolvedValue({}), updateDepartment: vi.fn().mockResolvedValue({}), createMember: vi.fn().mockResolvedValue({}), updateMember: vi.fn().mockResolvedValue({}), deleteMember: vi.fn().mockResolvedValue(undefined), deleteDepartment: vi.fn().mockResolvedValue(undefined) }
}
function render(requests = api(), overview?: EnterpriseOverview) {
  const wrapper = mount(EnterpriseOrganizationPanel, { props: { api: requests as unknown as EnterpriseResourceAPI, admin: !overview, enterpriseName: 'Acme', overview }, global: { stubs: {
    EnterpriseDialog: { name: 'EnterpriseDialog', props: ['show'], template: '<div v-if="show"><slot /></div>' },
    EnterpriseConfirm: { props: ['show', 'disabled', 'message'], emits: ['confirm'], template: '<div v-if="show">{{ message }}<button data-test="confirm-remove" :disabled="disabled" @click="$emit(\'confirm\')">Confirm</button></div>' }
  } } }); wrappers.push(wrapper); return wrapper
}
function manager(manage = true): EnterpriseOverview { return { enterprise: { name: 'Acme' }, me: { role: 'manager', department_id: 3 }, settings: { manager_can_manage_members: manage }, permissions: { view_members: true, manage_members: manage, create_members: manage, manage_departments: false } } as EnterpriseOverview }
describe('organization tree and details', () => {
  it('shows root, department, and member details and direct action buttons', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    const detail = () => wrapper.get('[data-test="organization-detail"]')
    const card = wrapper.getComponent({ name: 'EnterpriseOrganizationDetail' })
    expect(card.getComponent({ name: 'CardHeader' }).classes()).toContain('p-4')
    expect(card.getComponent({ name: 'CardContent' }).classes()).toContain('p-4')
    expect(detail().text()).toContain('Acme'); expect(detail().text()).toContain('Owner')
    await wrapper.get('[data-test="org-node-4"]').trigger('click')
    expect(detail().text()).toContain('研发中心 / 后端组'); expect(detail().text()).toContain('平台组')
    expect(detail().find('[data-test="department-move"]').exists()).toBe(true)
    await wrapper.get('[data-test="org-node-member-8"]').trigger('click')
    expect(detail().text()).toContain('bob@example.com'); expect(detail().text()).toContain('enterprise.orgTree.neverLoggedIn')
    expect(detail().find('[data-test="member-edit-8"]').exists()).toBe(true)
    await detail().get('[data-test="member-toggle"]').trigger('click'); await flushPromises()
    expect(requests.updateMember).toHaveBeenCalledWith(8, { status: 'disabled' })
    await detail().get('[data-test="member-remove-8"]').trigger('click')
    expect(requests.deleteMember).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-remove"]').trigger('click'); await flushPromises()
    expect(requests.deleteMember).toHaveBeenCalledWith(8)
  })
  it('filters live, highlights names and expands collapsed ancestors, with keyboard navigation', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-test="org-node-3"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(wrapper.find('[data-test="org-node-member-10"]').exists()).toBe(false)
    await wrapper.get('input[type="search"]').setValue('Carol')
    expect(wrapper.get('mark').text()).toBe('Carol')
    expect(wrapper.get('[data-test="org-node-5"]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('[data-test="org-node-6"]').exists()).toBe(false)
    await wrapper.get('[data-test="org-node-member-10"]').trigger('keydown', { key: 'Enter' })
    expect(wrapper.get('[data-test="organization-detail"]').text()).toContain('carol@example.com')
  })
  it('limits managers to all descendants and permits only employee operations when enabled', async () => {
    const wrapper = render(api(), manager()); await flushPromises()
    expect(wrapper.find('[data-test="org-node-6"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="org-node-0"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="org-node-member-10"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="department-create"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="organization-import"]').exists()).toBe(true)
    await wrapper.get('[data-test="org-node-member-7"]').trigger('click')
    expect(wrapper.find('[data-test="member-actions"]').exists()).toBe(false)
    await wrapper.get('[data-test="org-node-member-10"]').trigger('click')
    expect(wrapper.find('[data-test="member-actions"]').exists()).toBe(true)
    await wrapper.setProps({ overview: manager(false) })
    expect(wrapper.find('[data-test="member-actions"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="organization-import"]').exists()).toBe(false)
  })
  it('preserves member visibility and readonly restrictions', async () => {
    const requests = api(), wrapper = render(requests, { me: { role: 'member' }, permissions: { view_members: false } } as EnterpriseOverview); await flushPromises()
    expect(requests.members).not.toHaveBeenCalled(); expect(wrapper.text()).toContain('后端组')
    expect(wrapper.find('[data-test="organization-create"]').exists()).toBe(false)
    const admin = render(); await flushPromises(); await admin.setProps({ readonly: true })
    await admin.get('[data-test="org-node-member-8"]').trigger('click')
    expect(admin.find('[data-test="member-actions"]').exists()).toBe(false)
    expect(admin.find('[data-test="department-create"]').exists()).toBe(false)
  })
  it('disables self and descendant move targets, and sends parent_id zero to move to root', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    await wrapper.get('[data-test="org-node-3"]').trigger('click'); await wrapper.get('[data-test="department-move"]').trigger('click')
    const picker = wrapper.getComponent({ name: 'EnterpriseDepartmentPicker' })
    expect([...picker.props('disabledKeys')]).toEqual(['3', '4', '5'])
    await picker.get('[data-test="org-node-5"]').trigger('click')
    expect(picker.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('form.space-y-4').trigger('submit'); await flushPromises()
    expect(requests.updateDepartment).toHaveBeenCalledWith(3, { name: '研发中心', sort_order: 2, parent_id: 0 })
  })
  it('creates a child department with the selected parent and prevents nonempty deletion', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    await wrapper.get('[data-test="org-node-4"]').trigger('click'); await wrapper.get('[data-test="department-child"]').trigger('click')
    await wrapper.get('[data-test="department-name"]').setValue('新团队'); await wrapper.get('form.space-y-4').trigger('submit'); await flushPromises()
    expect(requests.createDepartment).toHaveBeenCalledWith({ name: '新团队', sort_order: 0, parent_id: 4 })
    await wrapper.get('[data-test="department-delete"]').trigger('click')
    expect(wrapper.get('[data-test="confirm-remove"]').attributes('disabled')).toBeDefined()
    expect(requests.deleteDepartment).not.toHaveBeenCalled()
  })
  it('defaults root creation to unassigned and department creation to the selected node', async () => {
    const wrapper = render(api(), { enterprise: { name: 'Acme' }, me: { role: 'owner', department_id: 3 }, permissions: { view_members: true, create_members: true, manage_members: true } } as EnterpriseOverview)
    await flushPromises()
    await wrapper.get('[data-test="organization-create"]').trigger('click')
    expect(wrapper.getComponent({ name: 'EnterpriseMemberForm' }).props('defaultDepartmentId')).toBe(0)
    const departmentWrapper = render(); await flushPromises()
    await departmentWrapper.get('[data-test="org-node-4"]').trigger('click')
    await departmentWrapper.get('[data-test="department-create-member"]').trigger('click')
    expect(departmentWrapper.getComponent({ name: 'EnterpriseMemberForm' }).props('defaultDepartmentId')).toBe(4)
  })
  it.each([true, false])('displays the current member concurrency as a read-only field (admin %s)', async admin => {
    const requests = api()
    requests.members.mockResolvedValue({ items: members.map(member => ({ ...member, concurrency: 24 })), total: members.length })
    const wrapper = render(requests, admin ? undefined : { enterprise: { name: 'Acme' }, me: { role: 'owner' }, permissions: { view_members: true } } as EnterpriseOverview)
    await flushPromises(); await wrapper.get('[data-test="org-node-member-8"]').trigger('click')
    const field = wrapper.get('[data-test="organization-detail"]').findAll('dl > div').find(row => row.find('dt').text() === 'enterprise.memberConcurrency')!
    expect(field.get('dd').text()).toBe('24')
    expect(field.find('input').exists()).toBe(false)
    expect(field.find('button').exists()).toBe(false)
  })

  it('loads members beyond the first page to populate the tree', async () => {
    const requests = api(); requests.members.mockResolvedValueOnce({ items: members.slice(0, 2), total: 4 }).mockResolvedValueOnce({ items: members.slice(2), total: 4 })
    const wrapper = render(requests); await flushPromises()
    expect(requests.members).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-test="org-node-member-10"]').exists()).toBe(true)
  })
})
