import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SubscriptionsView from '../SubscriptionsView.vue'

const { getMySubscriptions, getUserGroupRates, showError } = vi.hoisted(() => ({
  getMySubscriptions: vi.fn(), getUserGroupRates: vi.fn(), showError: vi.fn()
}))
vi.mock('@/api/subscriptions', () => ({ default: { getMySubscriptions } }))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getUserGroupRates } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { enterprise: { id: 1 } } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('vue-router', async original => ({ ...await original<typeof import('vue-router')>(), useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const wrappers: VueWrapper[] = []
function render() {
  const wrapper = mount(SubscriptionsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  getMySubscriptions.mockResolvedValue([
    { id: 1, group_id: 7, status: 'active', group: { name: 'Enterprise group', platform: 'openai', rate_multiplier: 1.5 } },
    { id: 2, group_id: 8, status: 'active', group: { name: 'Default group', platform: 'openai', rate_multiplier: 2 } }
  ])
  getUserGroupRates.mockResolvedValue({})
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })

describe('subscription effective group rates', () => {
  it('shows the enterprise effective rate from groups/rates and keeps the default for an uncovered group', async () => {
    getUserGroupRates.mockResolvedValue({ 7: 0.6 })
    const wrapper = render(); await flushPromises()
    expect(getUserGroupRates).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('payment.planCard.rate: ×0.6')
    expect(wrapper.text()).toContain('payment.planCard.rate: ×2')
    expect(wrapper.text()).not.toContain('payment.planCard.rate: ×1.5')
    expect(wrapper.text()).not.toContain('payment.renewNow')
  })

  it('continues showing subscriptions if the rate lookup fails', async () => {
    const error = new Error('Unavailable'), consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    getUserGroupRates.mockRejectedValue(error)
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('Enterprise group')
    expect(wrapper.text()).toContain('payment.planCard.rate: ×1.5')
    expect(consoleError).toHaveBeenCalledWith('Failed to load user group rates:', error)
    expect(showError).not.toHaveBeenCalled()
  })
})
