import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import type { EnterpriseOverview, EnterpriseRole } from '@/types/enterprise'

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
import { getEnterpriseTabs } from '@/features/enterprise/navigation'

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

describe('AppSidebar 企业菜单与钱包入口', () => {
  it.each(['owner', 'manager', 'member'] as const)('企业角色 %s 的管理入口按权限显示，个人钱包及批量图片入口隐藏', role => {
    mocks.auth.user.enterprise = { id: 1, role }
    const wrapper = render()
    expect(wrapper.find('[data-nav-group="/enterprise"]').exists()).toBe(role !== 'member')
    for (const path of ['/purchase', '/orders', '/redeem', '/batch-image']) expect(wrapper.find(`a[href="${path}"]`).exists(), path).toBe(false)
    expect(wrapper.find('a[href="/subscriptions"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('普通用户保留个人钱包菜单', () => {
    const wrapper = render()
    expect(wrapper.find('[data-nav-group="/enterprise"]').exists()).toBe(false)
    for (const path of ['/purchase', '/orders', '/redeem']) expect(wrapper.find(`a[href="${path}"]`).exists(), path).toBe(true)
    wrapper.unmount()
  })

  it.each([null, 'owner', 'manager', 'member'] as const)('管理员的企业角色为 %s 时保留原有导航', role => {
    mocks.auth.isAdmin = true
    mocks.auth.user.enterprise = role ? { id: 1, role } : null
    const wrapper = render()
    expect(wrapper.find('a[href="/admin/enterprises"]').text()).toBe('nav.enterprises')
    expect(wrapper.find('[data-nav-group="/enterprise"]').exists()).toBe(role !== null)
    wrapper.unmount()
  })

  it.each(['owner', 'manager', 'member'] as const)('简易模式下企业角色 %s 仍遵循管理入口权限', role => {
    mocks.auth.user.enterprise = { id: 1, role }
    mocks.auth.isSimpleMode = true
    const wrapper = render()
    expect(wrapper.find('[data-nav-group="/enterprise"]').exists()).toBe(role !== 'member')
    wrapper.unmount()
  })
})


describe('企业子菜单共享服务端权限', () => {
  const base = {
    enterprise_quota: null, department_quota_visible: false, department_quota: null,
    member_quota_visible: false, member_quota: null,
    permissions: { edit_quotas: false, edit_member_quota: false, view_audit_logs: false, edit_settings: false }
  } as EnterpriseOverview

  it.each([
    ['owner', { edit_quotas: true, view_audit_logs: true, edit_settings: true }, ['overview', 'organization', 'quotas', 'groups', 'usage', 'audit', 'settings']],
    ['manager', {}, ['overview', 'organization', 'groups', 'usage']],
    ['manager', { edit_member_quota: true }, ['overview', 'organization', 'quotas', 'groups', 'usage']],
    ['manager', { view_audit_logs: true, edit_settings: true }, ['overview', 'organization', 'groups', 'usage', 'audit', 'settings']]
  ] as const)('%s 的子项与工作台权限一致 (%j)', async (role, permissions, expected) => {
    mocks.auth.user.enterprise = { id: 1, role }
    mocks.enterprise.tabs = getEnterpriseTabs({ ...base, permissions: { ...base.permissions, ...permissions } })
    mocks.route.path = '/enterprise/organization'
    const wrapper = render()
    expect(wrapper.get('[data-nav-group="/enterprise"]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('a[href^="/enterprise/"]').map(link => link.attributes('href'))).toEqual(expected.map(tab => `/enterprise/${tab}`))
    expect(wrapper.get('a[href="/enterprise/organization"]').classes()).toContain('sidebar-link-active')
    expect(wrapper.get('a[href="/enterprise/organization"]').attributes('aria-current')).toBe('page')
    expect(wrapper.get('[data-nav-group="/enterprise"] svg').attributes('stroke-width')).toBe('1.5')
    expect(wrapper.get('[data-nav-group="/enterprise"] svg').classes()).toContain('lucide-building-2')
    await wrapper.get('[data-nav-group="/enterprise"]').trigger('click')
    expect(wrapper.find('a[href="/enterprise/overview"]').exists()).toBe(false)
    mocks.route.path = '/enterprise/groups'
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-nav-group="/enterprise"]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('a[href="/enterprise/groups"]').classes()).toContain('sidebar-link-active')
    wrapper.unmount()
  })

  it.each(['enterprise_quota', 'department_quota', 'member_quota'] as const)('显示可见的 %s 时提供限额入口', field => {
    const value = { ...base, department_quota_visible: true, member_quota_visible: true, [field]: { scope_id: 1 } } as EnterpriseOverview
    expect(getEnterpriseTabs(value)).toContain('quotas')
    if (field !== 'enterprise_quota') expect(getEnterpriseTabs({ ...value, department_quota_visible: false, member_quota_visible: false })).not.toContain('quotas')
  })

  it('管理员企业详情入口使用楼宇图标，个人企业分组仍按 overview 权限显示', () => {
    mocks.auth.isAdmin = true
    mocks.auth.user.enterprise = { id: 1, role: 'member' }
    mocks.route.path = '/enterprise/overview'
    const wrapper = render()
    expect(wrapper.get('a[href="/admin/enterprises"] svg').classes()).toContain('lucide-building-2')
    expect(wrapper.findAll('a[href^="/enterprise/"]')).toHaveLength(4)
    expect(wrapper.get('a[href="/admin/users"] svg').classes()).not.toContain('lucide-building-2')
    wrapper.unmount()
  })
})
