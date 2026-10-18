import { selectOption } from './selectOption'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EnterpriseMemberForm from '../components/EnterpriseMemberForm.vue'

const { listUsers } = vi.hoisted(() => ({ listUsers: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: listUsers } } }))
vi.mock('@/api/enterprise', () => ({ enterpriseErrorMessage: () => 'enterprise.errors.generic' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const departments = [{ id: 3, enterprise_id: 1, name: '研发部', sort_order: 0, member_count: 1, created_at: '' }]

describe('EnterpriseMemberForm identity and role validation', () => {
  beforeEach(() => vi.clearAllMocks())

  it('validates email and password before creating an enterprise-side account', async () => {
    const wrapper = mount(EnterpriseMemberForm, { props: { departments } })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[data-test="member-validation"]').text()).toBe('enterprise.validationEmail')
    await wrapper.get('[data-test="member-email"]').setValue('alice@example.com')
    await wrapper.get('[data-test="member-password"]').setValue('short')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[data-test="member-validation"]').text()).toBe('enterprise.validationPassword')
    expect(wrapper.emitted('submit')).toBeUndefined()
    await wrapper.get('[data-test="member-password"]').setValue('password123')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual({ email: 'alice@example.com', password: 'password123', username: undefined, department_id: undefined, role: 'member', title: '' })
    expect(listUsers).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires a department for the manager role', async () => {
    const wrapper = mount(EnterpriseMemberForm, { props: { departments } })
    await selectOption(wrapper, '[data-test="member-role"]', 'enterprise.roles.manager')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[data-test="member-validation"]').text()).toBe('enterprise.validationManagerDepartment')
    expect(wrapper.emitted('submit')).toBeUndefined()
    wrapper.unmount()
  })

  it('searches existing non-enterprise users and sends only user_id identity for the administrator flow', async () => {
    listUsers.mockResolvedValue({ items: [
      { id: 42, username: 'Alice', email: 'alice@example.com', enterprise: null },
      { id: 43, username: 'Bob', email: 'bob@example.com', enterprise: { id: 2 } }
    ], total: 2 })
    const wrapper = mount(EnterpriseMemberForm, { props: { admin: true, departments } })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[data-test="member-validation"]').text()).toBe('enterprise.validationUser')
    await wrapper.get('#enterprise-user-search').setValue('alice')
    await wrapper.get('[data-test="member-search"]').trigger('click')
    await flushPromises()
    expect(listUsers).toHaveBeenCalledWith(1, 50, { search: 'alice', role: 'user' })
    expect(wrapper.find('[data-test="member-user"] option[value="43"]').exists()).toBe(false)
    await selectOption(wrapper, '[data-test="member-user"]', 'Alice · alice@example.com')
    await wrapper.get('[data-test="member-department"] [data-test="org-node-3"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual({ user_id: 42, department_id: 3, role: 'member', title: '' })
    expect(wrapper.find('[data-test="member-email"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('defaults manager creation to their department and restricts the role to member', async () => {
    const wrapper = mount(EnterpriseMemberForm, { props: { departments, limitedManager: true, departmentId: 3 } })
    expect(wrapper.find('[data-test="member-department"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="member-role"]').exists()).toBe(false)
    await wrapper.get('[data-test="member-email"]').setValue('employee@example.com')
    await wrapper.get('[data-test="member-password"]').setValue('password123')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toMatchObject({ department_id: 3, role: 'member' })
    wrapper.unmount()
  })

  it('allows a department manager to update status, title and department within the permitted tree', async () => {
    const member = { id: 7, enterprise_id: 1, user_id: 42, email: 'alice@example.com', username: 'Alice', department_id: 3, department_name: '研发部', role: 'member' as const, status: 'active' as const, title: 'Engineer', joined_at: '', user_status: 'active' as const }
    const wrapper = mount(EnterpriseMemberForm, { props: { member, departments, limitedManager: true, departmentId: 3 } })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual({ department_id: 3, status: 'active', title: 'Engineer' })
    wrapper.unmount()
  })

  it('preserves an existing member without a department when the operator has a department', async () => {
    const member = { id: 7, enterprise_id: 1, user_id: 42, email: 'alice@example.com', username: 'Alice', department_id: null, department_name: null, role: 'member' as const, status: 'active' as const, title: 'Engineer', joined_at: '', user_status: 'active' as const }
    const wrapper = mount(EnterpriseMemberForm, { props: { member, departments, departmentId: 3 } })
    expect(wrapper.get('[data-test="member-department"]').text()).toContain('enterprise.orgTree.unassigned')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual({ department_id: 0, role: 'member', status: 'active', title: 'Engineer' })
    wrapper.unmount()
  })

  it('uses the member department rather than the operator department while editing', async () => {
    const member = { id: 7, enterprise_id: 1, user_id: 42, email: 'alice@example.com', username: 'Alice', department_id: 3, department_name: '研发部', role: 'member' as const, status: 'active' as const, title: '', joined_at: '', user_status: 'active' as const }
    const wrapper = mount(EnterpriseMemberForm, { props: { member, departments, departmentId: 8 } })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toMatchObject({ department_id: 3 })
    wrapper.unmount()
  })

  it('does not default a new owner-managed member to the operator department', async () => {
    const wrapper = mount(EnterpriseMemberForm, { props: { departments, departmentId: 3 } })
    await wrapper.get('[data-test="member-email"]').setValue('employee@example.com')
    await wrapper.get('[data-test="member-password"]').setValue('password123')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toMatchObject({ department_id: undefined })
    wrapper.unmount()
  })
})
