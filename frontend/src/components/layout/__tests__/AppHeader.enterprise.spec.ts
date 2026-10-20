import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { EnterpriseIdentity } from '@/types/enterprise'

const mocks = vi.hoisted(() => ({
  auth: {
    user: null as { email: string; username: string; role: string; balance: number; frozen_balance: number; enterprise?: EnterpriseIdentity | null } | null,
    isAdmin: false,
    isSimpleMode: false,
  },
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ name: 'Dashboard', meta: {}, params: {} }),
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => mocks.auth,
  useAppStore: () => ({ contactInfo: '', docUrl: '', cachedPublicSettings: {}, toggleMobileSidebar: vi.fn() }),
  useOnboardingStore: () => ({ replay: vi.fn() }),
}))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/utils/featureFlags', () => ({
  FeatureFlags: { subscription: 'subscription', modelPlaza: 'modelPlaza' },
  isFeatureFlagEnabled: () => false,
  resolveFeatureFlag: () => false,
}))

import AppHeader from '../AppHeader.vue'

const identity = (overrides: Partial<EnterpriseIdentity> = {}): EnterpriseIdentity => ({
  id: 1,
  name: 'Acme',
  role: 'member',
  department_id: null,
  department_name: null,
  member_status: 'active',
  enterprise_status: 'active',
  balance_visible: false,
  balance: null,
  frozen_balance: null,
  ...overrides,
})
function render() {
  return mount(AppHeader, {
    global: { stubs: { LocaleSwitcher: true, SubscriptionProgressMini: true, AnnouncementBell: true, Icon: true, RouterLink: true } },
  })
}

beforeEach(() => {
  mocks.auth.user = { email: 'member@example.com', username: 'Member', role: 'user', balance: 987.65, frozen_balance: 12.34 }
})

describe('AppHeader 企业余额可见性', () => {
  it.each([null, 9.25])('隐藏企业余额时桌面和手机均不显示个人或企业金额（冻结 %s）', async frozen_balance => {
    mocks.auth.user!.enterprise = identity({ frozen_balance })
    const wrapper = render()
    await wrapper.get('button[aria-label="common.userMenu"]').trigger('click')
    expect(wrapper.find('[data-testid="header-balance"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="header-mobile-balance"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('987.65')
    expect(wrapper.text()).not.toContain('12.34')
    wrapper.unmount()
  })

  it('显示企业余额，个人余额与个人冻结余额不泄露', async () => {
    mocks.auth.user!.enterprise = identity({ balance_visible: true, balance: 56.78 })
    const wrapper = render()
    await wrapper.get('button[aria-label="common.userMenu"]').trigger('click')
    expect(wrapper.get('[data-testid="header-balance"]').text()).toContain('enterprise.companyBalance')
    expect(wrapper.get('[data-testid="header-mobile-balance"]').text()).toContain('$56.78')
    expect(wrapper.text()).not.toContain('987.65')
    expect(wrapper.text()).not.toContain('12.34')
    wrapper.unmount()
  })

  it('shows enterprise frozen funds and explanation on desktop and mobile using only enterprise amounts', async () => {
    mocks.auth.user!.enterprise = identity({ balance_visible: true, balance: 56.78, frozen_balance: 9.25 })
    const wrapper = render()
    await wrapper.get('button[aria-label="common.userMenu"]').trigger('click')
    expect(wrapper.get('[data-testid="header-frozen-balance"]').text()).toContain('$9.25')
    const details = wrapper.get('[data-testid="header-balance-details"]')
    expect(details.text()).toContain('$66.03')
    expect(details.text()).toContain('enterprise.frozenBalanceHint')
    expect(wrapper.get('[data-testid="header-mobile-frozen-balance"]').text()).toContain('$9.25')
    expect(wrapper.get('[data-testid="header-mobile-frozen-balance"]').text()).toContain('enterprise.frozenBalanceHint')
    expect(wrapper.text()).not.toContain('987.65')
    expect(wrapper.text()).not.toContain('12.34')
    wrapper.unmount()
  })

  it.each([null, 0])('does not show a frozen badge or details for enterprise frozen balance %s', async frozen_balance => {
    mocks.auth.user!.enterprise = identity({ balance_visible: true, balance: 56.78, frozen_balance })
    const wrapper = render()
    await wrapper.get('button[aria-label="common.userMenu"]').trigger('click')
    expect(wrapper.find('[data-testid="header-frozen-balance"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="header-mobile-frozen-balance"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="header-balance-details"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('12.34')
    wrapper.unmount()
  })

  it('balance_visible=true 但 balance=null 时仍隐藏余额，不能显示为零', () => {
    mocks.auth.user!.enterprise = identity({ balance_visible: true })
    const wrapper = render()
    expect(wrapper.find('[data-testid="header-balance"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('可见的企业零余额和普通用户余额均保留', () => {
    mocks.auth.user!.enterprise = identity({ balance_visible: true, balance: 0 })
    const enterprise = render()
    expect(enterprise.get('[data-testid="header-balance"]').text()).toContain('$0.00')
    enterprise.unmount()
    mocks.auth.user!.enterprise = null
    const personal = render()
    expect(personal.get('[data-testid="header-balance"]').text()).toContain('$987.65')
    expect(personal.text()).toContain('$12.34')
    personal.unmount()
  })
})
