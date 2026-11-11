import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ForceTotpPanel from '../ForceTotpPanel.vue'

const api = vi.hoisted(() => ({ getForceTotp: vi.fn(), updateForceTotp: vi.fn() }))
vi.mock('@/api/admin/forceTotp', () => api)
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { count: number }) => values ? `${key}:${values.count}` : key }) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: (action: () => unknown) => action() }), isStepUpCancelled: () => false }))

beforeEach(() => { vi.clearAllMocks(); api.getForceTotp.mockResolvedValue({ enabled: false, users_without_totp: 8, encryption_key_configured: true }) })

describe('forced TOTP admin confirmation', () => {
  it('shows the affected account count before enabling', async () => {
    const wrapper = mount(ForceTotpPanel, { global: { stubs: { ConfirmDialog: true, TotpStepUpDialog: true } } })
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    const dialog = wrapper.getComponent({ name: 'ConfirmDialog' })
    expect(dialog.props('show')).toBe(true)
    expect(dialog.props('message')).toBe('forceTotp.confirm:8')
    expect(api.updateForceTotp).not.toHaveBeenCalled()
  })

  it('disables enabling without a fixed TOTP encryption key', async () => {
    api.getForceTotp.mockResolvedValue({ enabled: false, users_without_totp: 8, encryption_key_configured: false })
    const wrapper = mount(ForceTotpPanel, { global: { stubs: { ConfirmDialog: true, TotpStepUpDialog: true } } })
    await flushPromises()
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })
})
