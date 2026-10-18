import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseOrganizationPanel from '../components/EnterpriseOrganizationPanel.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview } from '@/types/enterprise'
import { clickMenuItem } from './selectOption'
import { selectOption } from './selectOption'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: vi.fn() } } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const departments: EnterpriseDepartment[] = [
  { id: 3, enterprise_id: 1, name: '研发部', sort_order: 2, member_count: 1, created_at: '' },
  { id: 4, enterprise_id: 1, name: '设计部', sort_order: 1, member_count: 1, created_at: '' }
]
const members = [
  { id: 7, username: 'Alice', email: 'alice@example.com', department_id: 3, department_name: '研发部', role: 'member', status: 'active', title: '', joined_at: '' },
  { id: 8, username: 'Bob', email: 'bob@example.com', department_id: 4, department_name: '设计部', role: 'member', status: 'active', title: '', joined_at: '' },
  { id: 9, username: 'Owner', email: 'owner@example.com', department_id: null, role: 'owner', status: 'active', title: '', joined_at: '' }
] as EnterpriseMember[]
const wrappers: VueWrapper[] = []
const stubs = {
  EnterpriseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
  EnterpriseConfirm: { props: ['show'], emits: ['confirm'], template: '<div v-if="show"><slot /><button data-test="confirm-remove" @click="$emit(\'confirm\')">Confirm</button></div>' },
  EnterprisePagination: true
}
function api() {
  return {
    departments: vi.fn().mockResolvedValue(departments),
    members: vi.fn().mockImplementation((query = {}) => Promise.resolve({ items: members.filter(item => query.department_id === undefined || (item.department_id ?? 0) === query.department_id), total: query.department_id === undefined ? 3 : 1 })),
    deleteDepartment: vi.fn().mockResolvedValue(undefined), updateDepartment: vi.fn().mockResolvedValue({}), createMember: vi.fn().mockResolvedValue({}), deleteMember: vi.fn().mockResolvedValue(undefined)
  }
}
function render(requests: ReturnType<typeof api>, overview?: EnterpriseOverview) {
  const wrapper = mount(EnterpriseOrganizationPanel, { props: { api: requests as unknown as EnterpriseResourceAPI, admin: !overview, enterpriseName: 'Acme', overview }, global: { stubs } })
  wrappers.push(wrapper); return wrapper
}
beforeEach(() => vi.clearAllMocks())
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
describe('enterprise organization tree', () => {
  it('uses one member action menu and retains edit and confirmed removal', async () => {
    const requests = api(), wrapper = render(requests)
    await flushPromises()
    expect(wrapper.findAll('[data-test^="member-menu-"]')).toHaveLength(3)
    expect(wrapper.find('[data-test="member-edit-7"]').exists()).toBe(false)
    await clickMenuItem(wrapper, '[data-test="member-menu-7"]', '[data-test="member-edit-7"]')
    expect(wrapper.getComponent({ name: 'EnterpriseMemberForm' }).props('member')).toEqual(members[0])
    await clickMenuItem(wrapper, '[data-test="member-menu-7"]', '[data-test="member-remove-7"]')
    expect(requests.deleteMember).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-remove"]').trigger('click'); await flushPromises()
    expect(requests.deleteMember).toHaveBeenCalledWith(7)
  })
  it('restricts menus to managers’ own department employees and hides all menus without manage permission or in readonly mode', async () => {
    const requests = api()
    requests.members.mockResolvedValue({ items: [...members, { ...members[0], id: 10, role: 'manager' }], total: 4 })
    const overview = { enterprise: { name: 'Acme' }, me: { role: 'manager', department_id: 3 }, permissions: { view_members: true, manage_members: true } } as EnterpriseOverview
    const wrapper = render(requests, overview); await flushPromises()
    expect(wrapper.findAll('[data-test^="member-menu-"]').map(item => item.attributes('data-test'))).toEqual(['member-menu-7'])
    await wrapper.get('[data-test="member-menu-7"]').trigger('keydown', { key: 'Enter' }); await flushPromises()
    expect(document.querySelector('[data-test="member-edit-7"]')).not.toBeNull()
    expect(document.querySelector('[data-test="member-remove-7"]')).not.toBeNull()
    await wrapper.get('[data-test="member-menu-7"]').trigger('keydown', { key: 'Escape' }); await flushPromises()
    await wrapper.setProps({ readonly: true })
    expect(wrapper.find('[data-test^="member-menu-"]').exists()).toBe(false)
    await wrapper.setProps({ readonly: false, overview: { ...overview, permissions: { ...overview.permissions, manage_members: false } } })
    expect(wrapper.find('[data-test^="member-menu-"]').exists()).toBe(false)
  })
  it('shows counts, sorts departments, and filters members using department IDs including zero', async () => {
    const requests = api(), wrapper = render(requests)
    await flushPromises()
    expect(wrapper.get('[data-test="org-node-all"]').text()).toBe('Acme3')
    expect(wrapper.get('[aria-label="enterprise.role"]').text()).toContain('enterprise.organizationUi.allRoles')
    expect(wrapper.get('[aria-label="enterprise.status"]').text()).toContain('enterprise.organizationUi.allStatuses')
    expect(wrapper.get('[data-test="enterprise-status-badge"]').text()).toBe('enterprise.active')
    expect(wrapper.get('[data-test="org-node-0"]').text()).toContain('1')
    expect(wrapper.findAll('nav [data-test^="org-node-"]').map(node => node.attributes('data-test'))).toEqual(['org-node-all', 'org-node-0', 'org-node-4', 'org-node-3'])
    await wrapper.get('[data-test="org-node-3"]').trigger('click'); await flushPromises()
    expect(requests.members).toHaveBeenLastCalledWith(expect.objectContaining({ department_id: 3, page: 1 }), expect.anything())
    expect(wrapper.findComponent({ name: 'EnterpriseTable' }).props('data')).toEqual([members[0]])
    await wrapper.get('[data-test="org-node-0"]').trigger('click'); await flushPromises()
    expect(requests.members).toHaveBeenLastCalledWith(expect.objectContaining({ department_id: 0 }), expect.anything())
    expect(wrapper.findComponent({ name: 'EnterpriseTable' }).props('data')).toEqual([members[2]])
    await wrapper.get('[data-test="org-node-all"]').trigger('click'); await flushPromises()
    expect(wrapper.findComponent({ name: 'EnterpriseTable' }).props('data')).toHaveLength(3)
  })
  it('only shows a manager’s department and its members even if a response contains other departments', async () => {
    const requests = api(); requests.members.mockResolvedValue({ items: members, total: 1 })
    const overview = { enterprise: { name: 'Acme' }, me: { role: 'manager', department_id: 3 }, permissions: { view_members: true, manage_members: true, create_members: true, manage_departments: false } } as EnterpriseOverview
    const wrapper = render(requests, overview); await flushPromises()
    expect(wrapper.findAll('nav [data-test^="org-node-"]').map(node => node.text())).toEqual(['研发部1'])
    expect(wrapper.findComponent({ name: 'EnterpriseTable' }).props('data')).toEqual([members[0]])
    expect(wrapper.find('[data-test="department-create"]').exists()).toBe(false)
    expect(requests.members).toHaveBeenCalledWith(expect.objectContaining({ department_id: 3 }), expect.anything())
  })
  it('preserves the directory without requesting members when member visibility is denied', async () => {
    const requests = api(), wrapper = render(requests, { enterprise: { name: 'Acme' }, me: { role: 'member' }, permissions: { view_members: false } } as EnterpriseOverview)
    await flushPromises()
    expect(wrapper.text()).toContain('研发部'); expect(requests.members).not.toHaveBeenCalled()
    expect(wrapper.find('form').exists()).toBe(false)
  })
  it('shows a friendly backend error and preserves the department after a nonempty deletion', async () => {
    const requests = api(); requests.deleteDepartment.mockRejectedValue({ reason: 'ENTERPRISE_DEPARTMENT_NOT_EMPTY', code: 'CONFLICT' })
    const wrapper = render(requests); await flushPromises()
    await wrapper.get('[data-test="org-node-3"]').trigger('click'); await flushPromises()
    await clickMenuItem(wrapper, '[data-test="org-menu-3"]', '[data-test="org-delete-3"]')
    await wrapper.get('[data-test="confirm-remove"]').trigger('click'); await flushPromises()
    expect(requests.deleteDepartment).toHaveBeenCalledWith(3)
    expect(wrapper.text()).toContain('enterprise.errors.ENTERPRISE_DEPARTMENT_NOT_EMPTY')
    expect(wrapper.find('[data-test="org-node-3"]').exists()).toBe(true)
  })
  it('submits department name and sort order, and defaults a new member to the selected department', async () => {
    const requests = api(), wrapper = render(requests); await flushPromises()
    await wrapper.get('[data-test="org-node-3"]').trigger('click'); await flushPromises()
    await clickMenuItem(wrapper, '[data-test="org-menu-3"]', '[data-test="org-edit-3"]')
    await wrapper.get('[data-test="department-name"]').setValue('工程部'); await wrapper.get('[data-test="department-sort"]').setValue('5')
    await wrapper.get('form.space-y-4').trigger('submit'); await flushPromises()
    expect(requests.updateDepartment).toHaveBeenCalledWith(3, { name: '工程部', sort_order: 5 })
    await wrapper.get('[data-test="organization-create"]').trigger('click')
    expect(wrapper.get('[data-test="member-department"]').text()).toContain('研发部')
    expect(wrapper.find('select').exists()).toBe(false)
    await selectOption(wrapper, '[data-test="member-role"]', 'enterprise.roles.manager')
    await wrapper.get('button[role="radio"][value="new"]').trigger('click')
    await wrapper.get('[data-test="member-email"]').setValue('new@example.com')
    await wrapper.get('[data-test="member-password"]').setValue('password123')
    await wrapper.get('#enterprise-member-form').trigger('submit'); await flushPromises()
    expect(requests.createMember).toHaveBeenCalledWith(expect.objectContaining({ department_id: 3, role: 'manager' }))
  })
})
