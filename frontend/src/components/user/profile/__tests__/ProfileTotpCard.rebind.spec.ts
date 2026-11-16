import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ProfileTotpCard from '../ProfileTotpCard.vue'
import TotpSetupModal from '../TotpSetupModal.vue'

const mocks = vi.hoisted(() => ({
  showError: vi.fn(), showSuccess: vi.fn(),
  totpAPI: {
    getStatus: vi.fn(), getVerificationMethod: vi.fn(), sendVerifyCode: vi.fn(),
    initiateSetup: vi.fn(), initiateRebind: vi.fn(), enable: vi.fn(), disable: vi.fn()
  }
}))
vi.mock('@/api', () => ({ totpAPI: mocks.totpAPI }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,qr') } }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.resetAllMocks()
  mocks.totpAPI.getVerificationMethod.mockResolvedValue({ method: 'password' })
})

describe('ProfileTotpCard rebind', () => {
  it('hides disable and offers rebind when 2FA is required by the administrator', async () => {
    mocks.totpAPI.getStatus.mockResolvedValue({ enabled: true, enabled_at: 1700000000, feature_enabled: true, required: true })
    const wrapper = mount(ProfileTotpCard)
    await flushPromises()

    expect(wrapper.find('[data-testid="totp-disable"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="totp-required-hint"]').exists()).toBe(true)

    await wrapper.get('[data-testid="totp-rebind"]').trigger('click')
    await flushPromises()
    const modal = wrapper.findComponent(TotpSetupModal)
    expect(modal.exists()).toBe(true)
    expect(modal.props('mode')).toBe('rebind')
  })

  it('keeps both rebind and disable when 2FA is optional', async () => {
    mocks.totpAPI.getStatus.mockResolvedValue({ enabled: true, enabled_at: 1700000000, feature_enabled: true, required: false })
    const wrapper = mount(ProfileTotpCard)
    await flushPromises()

    expect(wrapper.find('[data-testid="totp-rebind"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="totp-disable"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="totp-required-hint"]').exists()).toBe(false)
  })
})

describe('TotpSetupModal rebind mode', () => {
  it('requests a rebind secret instead of initial setup and confirms with the new code', async () => {
    mocks.totpAPI.initiateRebind.mockResolvedValue({ secret: 'NEWSECRET', qr_code_url: 'otpauth://totp/x', setup_token: 'rebind-token', countdown: 300 })
    mocks.totpAPI.enable.mockResolvedValue({ success: true })
    const wrapper = mount(TotpSetupModal, { props: { mode: 'rebind' } })
    await flushPromises()

    expect(wrapper.text()).toContain('profile.totp.rebindTitle')
    await wrapper.get('input[type="password"]').setValue('password')
    await wrapper.get('.btn-primary').trigger('click')
    await flushPromises()
    expect(mocks.totpAPI.initiateRebind).toHaveBeenCalledWith({ password: 'password' })
    expect(mocks.totpAPI.initiateSetup).not.toHaveBeenCalled()

    await wrapper.get('.btn-primary').trigger('click')
    for (const input of wrapper.findAll('input[maxlength="1"]')) await input.setValue('2')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.totpAPI.enable).toHaveBeenCalledWith({ totp_code: '222222', setup_token: 'rebind-token' })
    expect(mocks.showSuccess).toHaveBeenCalledWith('profile.totp.rebindSuccess')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })
})
