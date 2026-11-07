import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { EnterpriseRole } from '@/types/enterprise'
import { track } from '../creation-studio/helpers'

const mocks = vi.hoisted(() => ({
  auth: { user: { enterprise: null as null | { id: number; role: EnterpriseRole } }, isAuthenticated: false,
    isAdmin: false, isSimpleMode: false, hasPendingAuthSession: false, checkAuth: vi.fn() },
  app: { sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0, siteName: 'QA', siteLogo: '',
    siteVersion: '', publicSettingsLoaded: true, backendModeEnabled: false,
    cachedPublicSettings: { custom_menu_items: [] }, toggleSidebar: vi.fn(), setMobileOpen: vi.fn() },
  enterprise: { tabs: ['overview', 'organization', 'groups', 'usage'], ensureLoaded: vi.fn() },
  admin: { customMenuItems: [], fetch: vi.fn(), paymentEnabled: true, opsMonitoringEnabled: true },
}))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => mocks.admin }))
vi.mock('@/stores/enterprise', () => ({ useEnterpriseStore: () => mocks.enterprise }))
vi.mock('@/stores', () => ({ useAuthStore: () => mocks.auth, useAppStore: () => mocks.app,
  useAdminSettingsStore: () => mocks.admin, useOnboardingStore: () => ({ isCurrentStep: () => false }) }))
vi.mock('@/composables/useNavigationLoading', () => ({ useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: vi.fn() }) }))
vi.mock('@/composables/useRoutePrefetch', () => ({ useRoutePrefetch: () => ({ triggerPrefetch: vi.fn() }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: {}, makeSidebarFlag: () => () => true, resolveFeatureFlag: () => true }))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: { value: true }, refreshBatchImageAccess: vi.fn() }) }))
vi.mock('@/views/auth/LoginView.vue', () => ({ default: { template: '<div>QA login</div>' } }))
vi.mock('@/views/user/CreationStudioView.vue', () => ({ default: { template: '<div>QA creation</div>' } }))

import router from '@/router'
import AppSidebar from '@/components/layout/AppSidebar.vue'

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  mocks.auth.isAuthenticated = false
  mocks.auth.isAdmin = false
  mocks.auth.isSimpleMode = false
  mocks.auth.user.enterprise = null
})
afterEach(() => vi.restoreAllMocks())

describe('Creation studio route and navigation (Q4 / contract 10)', () => {
  it('runs the actual route guard and redirects anonymous /creation to login', async () => {
    await router.push('/creation?qa=1')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/creation?qa=1')
  })

  it('registers the frozen route metadata without personal-wallet or simple-mode restrictions', () => {
    const routes = router.options.routes
    const index = routes.findIndex(route => route.path === '/creation')
    expect(index).toBeGreaterThan(-1)
    expect(routes[index + 1].path).toBe('/async-tasks')
    expect(routes[index].name).toBe('CreationStudio')
    expect(routes[index].meta).toEqual({ requiresAuth: true, requiresAdmin: false, title: 'Creation studio',
      titleKey: 'creationStudio.title', descriptionKey: 'creationStudio.description' })
    expect(routes[index].meta).not.toHaveProperty('requiresPersonalWallet')
    expect(routes[index].meta).not.toHaveProperty('hideInSimpleMode')
  })

  it.each([
    [false, false], [false, true], [true, false], [true, true],
  ])('admin=%s simple=%s sees one highlighted creation link directly before async tasks', async (admin, simple) => {
    mocks.auth.isAuthenticated = true
    mocks.auth.isAdmin = admin
    mocks.auth.isSimpleMode = simple
    await router.push('/creation')
    expect(router.currentRoute.value.path).toBe('/creation')
    const wrapper = track(mount(AppSidebar, { global: { plugins: [router], stubs: { VersionBadge: true } } }))
    const links = wrapper.findAll('nav a')
    const index = links.findIndex(link => link.attributes('href') === '/creation')
    expect(wrapper.findAll('a[href="/creation"]')).toHaveLength(1)
    expect(links[index].text()).toBe('nav.creationStudio')
    expect(links[index].classes()).toContain('sidebar-link-active')
    expect(links[index].attributes('aria-current')).toBe('page')
    expect(links[index + 1].attributes('href')).toBe('/async-tasks')
    if (admin) expect(wrapper.find('a[href="/admin/async-tasks"]').exists()).toBe(true)
  })

  it.each(['owner', 'manager', 'member'] as const)('enterprise %s can access creation in both menu modes', async role => {
    mocks.auth.isAuthenticated = true
    mocks.auth.user.enterprise = { id: 1, role }
    await router.push('/creation')
    expect(router.currentRoute.value.path).toBe('/creation')
    for (const simple of [false, true]) {
      mocks.auth.isSimpleMode = simple
      const wrapper = track(mount(AppSidebar, { global: { plugins: [router], stubs: { VersionBadge: true } } }))
      expect(wrapper.findAll('a[href="/creation"]')).toHaveLength(1)
      expect(wrapper.find('a[href="/purchase"]').exists()).toBe(false)
      await nextTick()
      wrapper.unmount()
    }
  })
})
