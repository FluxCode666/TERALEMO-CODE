import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  auth: { user: { enterprise: null as null | { id: number } }, isAdmin: false, isSimpleMode: false },
  app: { sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0, siteName: 'Test', siteLogo: '', siteVersion: '', publicSettingsLoaded: true, cachedPublicSettings: { custom_menu_items: [] }, toggleSidebar: vi.fn(), setMobileOpen: vi.fn() },
  admin: { customMenuItems: [], fetch: vi.fn(), paymentEnabled: true, opsMonitoringEnabled: true }
}))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ path: '/dashboard' }), useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/stores', () => ({ useAuthStore: () => mocks.auth, useAppStore: () => mocks.app, useAdminSettingsStore: () => mocks.admin, useOnboardingStore: () => ({ isCurrentStep: () => false }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: {}, makeSidebarFlag: () => () => true, resolveFeatureFlag: () => true }))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: { value: true }, refreshBatchImageAccess: vi.fn() }) }))

import AppSidebar from '../AppSidebar.vue'

function render() {
  return mount(AppSidebar, { global: { stubs: { VersionBadge: true, Icon: true, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
}

beforeEach(() => {
  mocks.auth.user.enterprise = null
  mocks.auth.isAdmin = false
  mocks.auth.isSimpleMode = false
})

describe('AppSidebar 企业菜单与钱包入口', () => {
  it('企业成员显示我的企业并隐藏个人钱包及批量图片入口', () => {
    mocks.auth.user.enterprise = { id: 1 }
    const wrapper = render()
    expect(wrapper.find('a[href="/enterprise"]').exists()).toBe(true)
    for (const path of ['/purchase', '/orders', '/redeem', '/batch-image']) expect(wrapper.find(`a[href="${path}"]`).exists(), path).toBe(false)
    expect(wrapper.find('a[href="/subscriptions"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('普通用户保留个人钱包菜单', () => {
    const wrapper = render()
    expect(wrapper.find('a[href="/enterprise"]').exists()).toBe(false)
    for (const path of ['/purchase', '/orders', '/redeem']) expect(wrapper.find(`a[href="${path}"]`).exists(), path).toBe(true)
    wrapper.unmount()
  })

  it('管理员显示企业管理菜单', () => {
    mocks.auth.isAdmin = true
    const wrapper = render()
    expect(wrapper.find('a[href="/admin/enterprises"]').text()).toBe('nav.enterprises')
    wrapper.unmount()
  })

  it('简易模式下企业成员仍可进入我的企业', () => {
    mocks.auth.user.enterprise = { id: 1 }
    mocks.auth.isSimpleMode = true
    const wrapper = render()
    expect(wrapper.find('a[href="/enterprise"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
