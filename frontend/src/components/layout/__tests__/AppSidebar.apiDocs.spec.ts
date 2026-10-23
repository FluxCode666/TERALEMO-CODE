import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import type { EnterpriseRole } from '@/types/enterprise'

const mocks = vi.hoisted(() => ({
  auth: { user: { enterprise: null as null | { id: number; role: EnterpriseRole } }, isAdmin: false, isSimpleMode: false },
  app: { sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0, siteName: 'Test', siteLogo: '', siteVersion: '', publicSettingsLoaded: true, cachedPublicSettings: { custom_menu_items: [] }, toggleSidebar: vi.fn(), setMobileOpen: vi.fn() },
  route: { path: '/dashboard' },
  enterprise: { tabs: ['overview', 'organization', 'groups', 'usage'] as string[], ensureLoaded: vi.fn() },
  admin: { customMenuItems: [], fetch: vi.fn(), paymentEnabled: true, opsMonitoringEnabled: true }
}))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRoute: () => mocks.route, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/stores', () => ({ useAuthStore: () => mocks.auth, useAppStore: () => mocks.app, useAdminSettingsStore: () => mocks.admin, useOnboardingStore: () => ({ isCurrentStep: () => false }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: {}, makeSidebarFlag: () => () => true, resolveFeatureFlag: () => true }))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: { value: true }, refreshBatchImageAccess: vi.fn() }) }))
vi.mock('@/stores/enterprise', () => ({ useEnterpriseStore: () => mocks.enterprise }))

import AppSidebar from '../AppSidebar.vue'

function render() {
  return mount(AppSidebar, { global: { stubs: { VersionBadge: true, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
}

beforeEach(() => {
  mocks.auth.user.enterprise = null
  mocks.auth.isAdmin = false
  mocks.auth.isSimpleMode = false
  mocks.route = reactive({ path: '/dashboard' })
  mocks.enterprise.tabs = ['overview', 'organization', 'groups', 'usage']
})

describe('AppSidebar API 文档入口', () => {
  it.each([
    { isAdmin: false, isSimpleMode: false },
    { isAdmin: false, isSimpleMode: true },
    { isAdmin: true, isSimpleMode: false },
    { isAdmin: true, isSimpleMode: true },
  ])('管理员=$isAdmin，简易模式=$isSimpleMode 时入口唯一、紧随异步任务且可高亮', ({ isAdmin, isSimpleMode }) => {
    mocks.auth.isAdmin = isAdmin
    mocks.auth.isSimpleMode = isSimpleMode
    mocks.route.path = '/docs/api'
    const wrapper = render()
    const links = wrapper.findAll('a[href="/docs/api"]')
    expect(links).toHaveLength(1)
    expect(links[0]?.text()).toBe('nav.apiDocs')
    expect(links[0]?.classes()).toContain('sidebar-link-active')
    const navLinks = wrapper.findAll('nav a')
    const index = navLinks.findIndex(link => link.attributes('href') === '/docs/api')
    expect(index).toBeGreaterThan(0)
    expect(navLinks[index - 1]?.attributes('href')).toBe('/async-tasks')
    wrapper.unmount()
  })

  it.each(['owner', 'manager', 'member'] as const)('企业角色 %s 可见入口', role => {
    mocks.auth.user.enterprise = { id: 1, role }
    const wrapper = render()
    expect(wrapper.find('a[href="/docs/api"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
