import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseMember, EnterpriseOverview, EnterpriseRole } from '@/types/enterprise'
import { canResetMemberPassword } from '../memberPassword'
import EnterpriseOrganizationPanel from '../components/EnterpriseOrganizationPanel.vue'
import EnterprisePasswordResetDialog from '../components/EnterprisePasswordResetDialog.vue'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { departments, members } from './organizationFixtures'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: vi.fn() } } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = ''; vi.restoreAllMocks() })
function overview(role: EnterpriseRole = 'owner', permission = true): EnterpriseOverview {
  return { enterprise: { name: 'Acme' }, me: { member_id: 9, user_id: 9, role, department_id: 3 }, settings: { manager_can_manage_members: permission }, permissions: { view_members: true, manage_members: permission } } as EnterpriseOverview
}
const resettable: EnterpriseMember = { ...members[1]!, password_resettable: true }
describe('password reset permissions and button visibility', () => {
  it.each([
    ['admin owner', true, 'owner', { role: 'owner', id: 9, user_id: 9 }, true],
    ['owner employee', false, 'owner', {}, true],
    ['owner manager', false, 'owner', { role: 'manager' }, true],
    ['other owner', false, 'owner', { role: 'owner' }, false],
    ['self', false, 'owner', { id: 9, user_id: 9 }, false],
    ['manager subtree member', false, 'manager', { department_id: 5 }, true],
    ['manager peer', false, 'manager', { role: 'manager' }, false],
    ['manager outside', false, 'manager', { department_id: 6 }, false],
    ['member', false, 'member', {}, false],
    ['account not created by enterprise', false, 'owner', { password_resettable: false }, false],
    ['old backend', false, 'owner', { password_resettable: undefined }, false]
  ] as const)('%s', async (_name, admin, role, overrides, expected) => {
    const member = { ...resettable, ...overrides } as EnterpriseMember
    const context = { admin, overview: overview(role), departments }
    expect(canResetMemberPassword(member, context)).toBe(expected)
    const api = { departments: vi.fn().mockResolvedValue(departments), members: vi.fn().mockResolvedValue({ items: [member], total: 1 }) }
    const wrapper = mount(EnterpriseOrganizationPanel, { props: { api: api as unknown as EnterpriseResourceAPI, admin, overview: context.overview } }); wrappers.push(wrapper)
    await flushPromises()
    const node = wrapper.find(`[data-test="org-node-member-${member.id}"]`)
    if (node.exists()) await node.trigger('click')
    expect(wrapper.find('[data-test="member-reset-password"]').exists()).toBe(expected)
  })
  it('rejects readonly and managers without management permission or with the setting disabled', () => {
    expect(canResetMemberPassword(resettable, { admin: true, readonly: true, departments })).toBe(false)
    expect(canResetMemberPassword(resettable, { overview: overview('manager', false), departments })).toBe(false)
    const value = overview('manager'); value.permissions.manage_members = false
    expect(canResetMemberPassword(resettable, { overview: value, departments })).toBe(false)
  })
})
async function render(result: { generated_password?: string } = {}) {
  const resetMemberPassword = vi.fn().mockResolvedValue(result)
  const wrapper = mount(EnterprisePasswordResetDialog, { attachTo: document.body, props: { api: { resetMemberPassword } as unknown as EnterpriseResourceAPI, member: resettable } }); wrappers.push(wrapper)
  await flushPromises()
  const dialog = new DOMWrapper(document.querySelector<HTMLElement>('[role="dialog"]')!)
  return { wrapper, dialog, resetMemberPassword }
}
describe('password reset dialog', () => {
  it('defaults to generated passwords, copies the one-time result and requires saved confirmation', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const { wrapper, dialog, resetMemberPassword } = await render({ generated_password: 'Generated-once-123' })
    expect(dialog.text()).toContain('enterprise.passwordReset.sessions')
    expect(dialog.get('[data-test="password-mode-auto"]').attributes('aria-checked')).toBe('true')
    await dialog.get('form').trigger('submit'); await flushPromises()
    expect(resetMemberPassword).toHaveBeenCalledWith(8, {})
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(dialog.get('[data-test="generated-password"]').text()).toBe('Generated-once-123')
    expect(dialog.get('[data-test="password-result-close"]').attributes('disabled')).toBeDefined()
    wrapper.getComponent(Dialog).vm.$emit('update:open', false)
    wrapper.getComponent(DialogContent).vm.$emit('escapeKeyDown', { preventDefault: vi.fn() })
    await flushPromises(); expect(wrapper.emitted('close')).toBeUndefined()
    await dialog.get('[data-test="password-copy"]').trigger('click'); await flushPromises()
    expect(writeText).toHaveBeenCalledWith('Generated-once-123')
    await dialog.get('[data-test="password-saved"]').trigger('click')
    await dialog.get('[data-test="password-result-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(dialog.find('[data-test="generated-password"]').exists()).toBe(false)
    expect(resetMemberPassword).toHaveBeenCalledTimes(1)
  })
  it('validates manual passwords and sends only the new password without a generated result', async () => {
    const { wrapper, dialog, resetMemberPassword } = await render()
    await dialog.get('[data-test="password-mode-manual"]').trigger('click')
    await dialog.get('[data-test="reset-new-password"]').setValue('short')
    await dialog.get('form').trigger('submit')
    expect(dialog.text()).toContain('enterprise.passwordReset.minLength')
    await dialog.get('[data-test="reset-new-password"]').setValue('New-password-123')
    await dialog.get('[data-test="reset-confirm-password"]').setValue('New-password-456')
    await dialog.get('form').trigger('submit'); expect(dialog.text()).toContain('enterprise.passwordReset.mismatch')
    expect(resetMemberPassword).not.toHaveBeenCalled()
    await dialog.get('[data-test="reset-confirm-password"]').setValue('New-password-123')
    await dialog.get('form').trigger('submit'); await flushPromises()
    expect(resetMemberPassword).toHaveBeenCalledWith(8, { password: 'New-password-123' })
    expect(dialog.find('[data-test="generated-password"]').exists()).toBe(false)
    expect(dialog.find('[data-test="password-saved"]').exists()).toBe(false)
    await dialog.get('[data-test="password-result-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
