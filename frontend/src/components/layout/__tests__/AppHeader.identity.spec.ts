import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { EnterpriseIdentity, EnterpriseRole } from '@/types/enterprise'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

const mocks = vi.hoisted(() => ({
  auth: {
    user: {
      email: 'member@example.com', username: 'Member', role: 'user', balance: 0,
      enterprise: null as EnterpriseIdentity | null,
    },
    isAdmin: false,
    isSimpleMode: false,
  },
  push: vi.fn(),
  route: { name: 'Dashboard', meta: {} as { titleKey?: string }, params: {} },
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.push }),
  useRoute: () => mocks.route,
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

function identity(overrides: Partial<EnterpriseIdentity> & { department_path_names?: string[] } = {}): EnterpriseIdentity {
  return {
    id: 1, name: 'Acme', role: 'member', department_id: null, department_name: null,
    member_status: 'active', enterprise_status: 'active', balance_visible: false,
    balance: null, frozen_balance: null, ...overrides,
  }
}

function render(locale: 'zh' | 'en' = 'zh') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: { zh, en },
    // Vitest uses the runtime-only build; these header labels have no interpolation.
    messageCompiler: message => () => String(message),
  })
  return mount(AppHeader, {
    global: {
      plugins: [i18n],
      stubs: { LocaleSwitcher: true, SubscriptionProgressMini: true, AnnouncementBell: true, Icon: true, RouterLink: true },
    },
  })
}

beforeEach(() => {
  mocks.auth.user = { email: 'member@example.com', username: 'Member', role: 'user', balance: 0, enterprise: null }
  mocks.auth.isAdmin = false
  mocks.push.mockReset()
  mocks.route.meta = {}
})

describe('AppHeader 企业身份', () => {
  it.each([
    { department_name: '研发中心/后端组' },
    { department_name: '后端组', department_path_names: ['研发中心', '后端组'] },
    { department_name: '研发中心/后端组', department_path_names: [] },
  ])('显示末级部门并在悬浮和菜单中保留完整路径：%j', async department => {
    mocks.auth.user.enterprise = identity({ role: 'manager', department_id: 2, ...department })
    const wrapper = render()
    const label = wrapper.get('[data-testid="header-user-identity"]')
    expect(label.text()).toBe('后端组 · 部门负责人')
    expect(label.attributes('title')).toBe('研发中心/后端组 · 部门负责人')
    await wrapper.get('button[aria-label="用户菜单"]').trigger('click')
    const menuLabel = wrapper.get('[data-testid="header-menu-identity"]')
    expect(menuLabel.text()).toBe('Acme / 研发中心/后端组 · 部门负责人')
    expect(menuLabel.attributes('title')).toBe(menuLabel.text())
    wrapper.unmount()
  })

  it('兼容旧的单级部门名称', () => {
    mocks.auth.user.enterprise = identity({ department_id: 2, department_name: '后端组' })
    const wrapper = render()
    const label = wrapper.get('[data-testid="header-user-identity"]')
    expect(label.text()).toBe('后端组 · 员工')
    expect(label.attributes('title')).toBe('后端组 · 员工')
    wrapper.unmount()
  })

  it.each([null, '', ' / '])('无部门 %s 时使用企业名', async department_name => {
    mocks.auth.user.enterprise = identity({ role: 'owner', department_name })
    const wrapper = render()
    const label = wrapper.get('[data-testid="header-user-identity"]')
    expect(label.text()).toBe('Acme · 企业负责人')
    expect(label.attributes('title')).toBe('Acme · 企业负责人')
    await wrapper.get('button[aria-label="用户菜单"]').trigger('click')
    expect(wrapper.get('[data-testid="header-menu-identity"]').text()).toBe('Acme · 企业负责人')
    wrapper.unmount()
  })

  it.each([
    ['owner', '企业负责人', 'Owner'],
    ['manager', '部门负责人', 'Manager'],
    ['member', '员工', 'Member'],
  ] as const)('角色 %s 的中英文文案齐全', (role: EnterpriseRole, chinese, english) => {
    mocks.auth.user.enterprise = identity({ role })
    for (const [locale, label] of [['zh', chinese], ['en', english]] as const) {
      const wrapper = render(locale)
      expect(wrapper.get('[data-testid="header-user-identity"]').text()).toBe(`Acme · ${label}`)
      wrapper.unmount()
    }
  })

  it.each([
    { role: 'user', enterprise: null, label: '用户' },
    { role: 'admin', enterprise: null, label: '管理员' },
    { role: 'admin', enterprise: identity({ department_name: '研发中心/后端组' }), label: '管理员' },
  ])('系统角色 $role 保留原有身份 $label', async ({ role, enterprise, label }) => {
    mocks.auth.user.role = role
    mocks.auth.user.enterprise = enterprise
    mocks.auth.isAdmin = role === 'admin'
    const wrapper = render()
    expect(wrapper.get('[data-testid="header-user-identity"]').text()).toBe(label)
    expect(wrapper.get('[data-testid="header-user-identity"]').attributes('title')).toBeUndefined()
    await wrapper.get('button[aria-label="用户菜单"]').trigger('click')
    expect(wrapper.find('[data-testid="header-menu-identity"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('普通员工的企业余额在桌面及手机端都不会导航到企业页', async () => {
    mocks.auth.user.enterprise = identity({ balance_visible: true, balance: 50 })
    const wrapper = render()
    const balance = wrapper.get('[data-testid="header-balance"]')
    expect(balance.element.closest('a, button')).toBeNull()
    await balance.trigger('click')
    await wrapper.get('button[aria-label="用户菜单"]').trigger('click')
    const mobileBalance = wrapper.get('[data-testid="header-mobile-balance"]')
    expect(mobileBalance.element.closest('a, button')).toBeNull()
    await mobileBalance.trigger('click')
    expect(mocks.push).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})


it.each([
  ['overview', '概览'], ['organization', '组织架构'], ['quotas', '限额管理'],
  ['groups', '成员可调用分组'], ['usage', '用量分析'], ['audit', '审计'], ['settings', '设置']
])('顶栏使用企业 %s 子页的标题', (section, expected) => {
  mocks.route.meta = { titleKey: `enterprise.${section}` }
  const wrapper = render()
  expect(wrapper.get('h1').text()).toBe(expected)
  wrapper.unmount()
})
