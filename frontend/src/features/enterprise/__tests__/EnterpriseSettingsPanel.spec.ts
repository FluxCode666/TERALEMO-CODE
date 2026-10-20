import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import { defaultEnterpriseSettings, type EnterpriseSettings } from '@/types/enterprise'
import EnterpriseSettingsPanel from '../components/EnterpriseSettingsPanel.vue'

const { updateEnterprise } = vi.hoisted(() => ({ updateEnterprise: vi.fn() }))
vi.mock('@/api/admin/enterprises', () => ({ enterprisesAPI: { update: updateEnterprise } }))
vi.mock('@/api/enterprise', () => ({ enterpriseErrorMessage: () => 'enterprise.errors.generic' }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const wrappers: VueWrapper[] = []
const settings = vi.fn()
const updateSettings = vi.fn()
const api = { settings, updateSettings } as unknown as EnterpriseResourceAPI

function mountSettings(admin = true) {
  const wrapper = mount(EnterpriseSettingsPanel, {
    props: { api, admin, enterpriseId: 1, settings: { ...defaultEnterpriseSettings }, refreshKey: 0 }
  })
  wrappers.push(wrapper)
  return wrapper
}

function checkbox(wrapper: VueWrapper, key: keyof EnterpriseSettings) {
  return wrapper.get(`[data-test="setting-${key}"]`)
}

afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount() })

beforeEach(() => {
  vi.clearAllMocks()
  settings.mockResolvedValue({ ...defaultEnterpriseSettings })
  updateEnterprise.mockResolvedValue({})
  updateSettings.mockResolvedValue({ ...defaultEnterpriseSettings })
})

describe('Enterprise settings refresh', () => {
  it('bounds the settings controls inside a full-width card', () => {
    const wrapper = mountSettings()
    expect(wrapper.get('section').classes()).toEqual(expect.arrayContaining(['w-full', 'max-w-4xl']))
    expect(wrapper.classes()).not.toContain('max-w-4xl')
  })

  it('synchronizes a clean settings form when the server snapshot changes', async () => {
    const wrapper = mountSettings()
    await wrapper.setProps({ settings: { ...defaultEnterpriseSettings, balance_visible_to_member: true } })

    expect(checkbox(wrapper, 'balance_visible_to_member').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('preserves edited fields, merges other server changes, and saves the merged snapshot', async () => {
    const wrapper = mountSettings()
    expect(checkbox(wrapper, 'balance_visible_to_member').attributes('role')).toBe('switch')
    await checkbox(wrapper, 'balance_visible_to_member').trigger('click')
    await wrapper.setProps({ settings: { ...defaultEnterpriseSettings, manager_can_manage_members: true } })

    expect(checkbox(wrapper, 'balance_visible_to_member').attributes('aria-checked')).toBe('true')
    expect(checkbox(wrapper, 'manager_can_manage_members').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="status"]').text()).toBe('enterprise.settingsRefreshedWithEdits')

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateEnterprise).toHaveBeenCalledWith(1, {
      settings: { ...defaultEnterpriseSettings, balance_visible_to_member: true, manager_can_manage_members: true }
    })
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })

  it('reloads the enterprise settings when the workspace refresh token changes', async () => {
    const wrapper = mountSettings(false)
    await flushPromises()
    expect(settings).toHaveBeenCalledTimes(1)
    expect(settings).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) })

    settings.mockResolvedValue({ ...defaultEnterpriseSettings, balance_visible_to_member: true })
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()

    expect(settings).toHaveBeenCalledTimes(2)
    expect(checkbox(wrapper, 'balance_visible_to_member').attributes('aria-checked')).toBe('true')
  })
})

it('hides the retired member-group switch and always sends false for its legacy setting', async () => {
  const wrapper = mountSettings()
  await wrapper.setProps({ settings: { ...defaultEnterpriseSettings, manager_can_edit_member_groups: true } })
  expect(wrapper.find('[data-test="setting-manager_can_edit_member_groups"]').exists()).toBe(false)
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateEnterprise).toHaveBeenCalledWith(1, { settings: { ...defaultEnterpriseSettings, manager_can_edit_member_groups: false } })
})

it('hides the retired self-quota visibility switch because every member always sees their own quota', async () => {
  const wrapper = mountSettings()
  await wrapper.setProps({ settings: { ...defaultEnterpriseSettings, member_quota_visible_to_member: false } })
  expect(wrapper.find('[data-test="setting-member_quota_visible_to_member"]').exists()).toBe(false)
  // Manager-side visibility stays configurable.
  expect(wrapper.find('[data-test="setting-member_quota_visible_to_manager"]').exists()).toBe(true)
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(updateEnterprise).toHaveBeenCalledWith(1, { settings: { ...defaultEnterpriseSettings, member_quota_visible_to_member: false } })
})
