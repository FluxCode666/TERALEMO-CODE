import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { EnterpriseIdentity, EnterpriseQuota } from '@/types/enterprise'

const mocks = vi.hoisted(() => ({
  auth: {
    user: null as { email: string; username: string; role: string; balance: number; frozen_balance: number; enterprise?: EnterpriseIdentity | null } | null,
    isAdmin: false,
    isSimpleMode: false,
    userLoadedAt: 0,
    refreshUser: vi.fn(() => Promise.resolve({})),
  },
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }),
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
const quota = (overrides: Partial<EnterpriseQuota> = {}): EnterpriseQuota => ({
  scope_type: 'member',
  scope_id: 7,
  daily_limit_usd: 10,
  weekly_limit_usd: null,
  monthly_limit_usd: 100,
  total_limit_usd: null,
  daily_usage_usd: 2.5,
  weekly_usage_usd: 8,
  monthly_usage_usd: 99,
  total_usage_usd: 120,
  daily_resets_at: '2030-01-02T00:00:00Z',
  weekly_resets_at: null,
  monthly_resets_at: null,
  ...overrides,
})
function render() {
  return mount(AppHeader, {
    global: { stubs: { LocaleSwitcher: true, SubscriptionProgressMini: true, AnnouncementBell: true, Icon: true, RouterLink: true } },
  })
}

beforeEach(() => {
  mocks.auth.user = { email: 'member@example.com', username: 'Member', role: 'user', balance: 987.65, frozen_balance: 12.34 }
  mocks.auth.userLoadedAt = Date.now()
  mocks.auth.refreshUser.mockReset()
  mocks.auth.refreshUser.mockImplementation(async () => {
    mocks.auth.userLoadedAt = Date.now()
    return {}
  })
})

describe('AppHeader 我的限额入口', () => {
  it('非企业用户不显示限额入口', () => {
    mocks.auth.user!.enterprise = null
    const wrapper = render()
    expect(wrapper.find('[data-testid="header-quota"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('看不到公司余额的成员也有可聚焦且带无障碍名称的限额入口', () => {
    mocks.auth.user!.enterprise = identity({ member_quota: quota() })
    const wrapper = render()
    const pill = wrapper.get('[data-testid="header-quota"]')
    expect(pill.attributes('aria-label')).toBe('enterprise.myQuota')
    expect(pill.attributes('tabindex')).toBe('0')
    const daily = pill.get('[data-testid="header-quota-daily"]')
    expect(daily.text()).toContain('$2.50')
    expect(daily.text()).toContain('$10.00')
    expect(wrapper.find('[data-testid="header-balance"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('悬停卡片展示四档限额，不限时显示不限并带进度条', () => {
    mocks.auth.user!.enterprise = identity({ member_quota: quota() })
    const wrapper = render()
    const details = wrapper.get('[data-testid="header-quota-details"]')
    for (const key of ['daily', 'weekly', 'monthly', 'total']) {
      expect(details.get(`[data-testid="header-quota-${key}"]`).text()).toContain(`enterprise.quotaUsage.${key}`)
    }
    expect(details.get('[data-testid="header-quota-weekly"]').text()).toContain('enterprise.unlimited')
    expect(details.get('[data-testid="header-quota-weekly"]').text()).toContain('$8.00')
    expect(details.get('[data-testid="header-quota-monthly"]').text()).toContain('$99.00')
    expect(details.text()).toContain('enterprise.quotaResetsAt')
    // 99/100 → 99% ≥ 95 → red bar
    expect(details.get('[data-testid="header-quota-monthly"]').html()).toContain('bg-red-500')
    expect(details.get('[data-testid="header-quota-daily"]').html()).toContain('bg-green-500')
    wrapper.unmount()
  })

  it('没有限额记录时入口按不限/零用量渲染', () => {
    mocks.auth.user!.enterprise = identity({ member_quota: null })
    const wrapper = render()
    const pill = wrapper.get('[data-testid="header-quota"]')
    expect(pill.text()).toContain('$0.00')
    expect(pill.text()).toContain('enterprise.unlimited')
    wrapper.unmount()
  })

  it('零限额且零用量时显示满格红色和已禁用', () => {
    mocks.auth.user!.enterprise = identity({ member_quota: quota({ daily_limit_usd: 0, daily_usage_usd: 0 }) })
    const wrapper = render()
    const row = wrapper.get('[data-testid="header-quota-daily"]')
    expect(row.text()).toContain('enterprise.quotaDisabled')
    expect(row.find('.bg-red-500').attributes('style')).toContain('width: 100%')
    wrapper.unmount()
  })

  it('刚加载用户后首次悬停不刷新，过期后刷新并在 60 秒内复用结果', async () => {
    vi.useFakeTimers()
    try {
      mocks.auth.user!.enterprise = identity({ member_quota: quota() })
      const wrapper = render()
      const pill = wrapper.get('[data-testid="header-quota"]')
      await pill.trigger('mouseenter')
      await pill.trigger('mouseenter')
      expect(mocks.auth.refreshUser).not.toHaveBeenCalled()
      vi.advanceTimersByTime(61 * 1000)
      await pill.trigger('focus')
      await flushPromises()
      expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(1)
      await pill.trigger('mouseenter')
      expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(1)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })
  it('每次切回标签页都强制刷新限额，切走和非企业用户不请求', async () => {
    const visibility = vi.spyOn(document, 'visibilityState', 'get')
    try {
      mocks.auth.user!.enterprise = identity({ member_quota: quota() })
      const wrapper = render()
      visibility.mockReturnValue('hidden')
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(mocks.auth.refreshUser).not.toHaveBeenCalled()
      visibility.mockReturnValue('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(2)
      expect(mocks.auth.refreshUser).toHaveBeenLastCalledWith({ fresh: true })
      wrapper.unmount()
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(2)

      mocks.auth.user!.enterprise = null
      const personal = render()
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(2)
      personal.unmount()
    } finally {
      visibility.mockRestore()
    }
  })
  it('从其他应用切回浏览器（窗口获得焦点）也强制刷新，与可见性事件同时触发时只请求一次', async () => {
    let resolveRefresh!: () => void
    mocks.auth.refreshUser.mockImplementation(() => new Promise(resolve => { resolveRefresh = () => resolve({}) }))
    mocks.auth.user!.enterprise = identity({ member_quota: quota() })
    const wrapper = render()
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(1)
    expect(mocks.auth.refreshUser).toHaveBeenLastCalledWith({ fresh: true })
    resolveRefresh()
    await flushPromises()

    document.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(2)
    resolveRefresh()
    await flushPromises()
    wrapper.unmount()
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(mocks.auth.refreshUser).toHaveBeenCalledTimes(2)
  })
})
