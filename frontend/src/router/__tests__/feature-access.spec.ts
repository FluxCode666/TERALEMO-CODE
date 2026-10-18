import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { EnterpriseRole } from '@/types/enterprise'

type NavigationGuard = (
  to: Record<string, any>,
  from: Record<string, any>,
  next: ReturnType<typeof vi.fn>
) => Promise<void>

const routerHarness = vi.hoisted(() => ({
  guard: null as NavigationGuard | null,
  routes: [] as Array<{ name?: string; path: string; meta?: Record<string, unknown> }>,
}))

const enterpriseStore = vi.hoisted(() => ({ ensureLoaded: vi.fn(), tabs: ['overview', 'organization', 'groups', 'usage'], overview: {} as object | null }))
vi.mock('@/stores/enterprise', () => ({ useEnterpriseStore: () => enterpriseStore }))

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  user: null as null | { enterprise?: { id: number; role: EnterpriseRole } | null },
  isAuthenticated: true,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  publicSettingsLoaded: false,
  cachedPublicSettings: null as null | {
    payment_enabled?: boolean
    risk_control_enabled?: boolean
    subscription_enabled?: boolean
    custom_menu_items?: []
  },
  fetchPublicSettings: vi.fn(),
}))

vi.mock('vue-router', () => ({
  createWebHistory: vi.fn(() => ({})),
  createRouter: vi.fn((config) => {
    routerHarness.routes = config.routes
    return ({
    beforeEach: vi.fn((guard: NavigationGuard) => {
      routerHarness.guard = guard
    }),
    afterEach: vi.fn(),
    onError: vi.fn(),
  }) }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: () => ({
    initialized: true,
    fetchStatus: vi.fn(),
    requireAcknowledgement: vi.fn(),
  }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function runGuard(meta: Record<string, unknown>, path: string) {
  if (!routerHarness.guard) {
    throw new Error('router guard was not registered')
  }

  const next = vi.fn()
  const navigation = routerHarness.guard(
    {
      path,
      fullPath: path,
      name: 'FeatureRoute',
      params: {},
      meta: { requiresAuth: true, ...meta },
    },
    {},
    next
  )
  return { navigation, next }
}

describe('feature route guard', () => {
  beforeAll(async () => {
    await import('@/router')
  })

  beforeEach(() => {
    authStore.user = null
    authStore.isAuthenticated = true
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    appStore.publicSettingsLoaded = false
    appStore.cachedPublicSettings = null
    appStore.fetchPublicSettings.mockReset()
  })

  it('waits for the first public-settings request before deciding payment access', async () => {
    const deferred = createDeferred<{ payment_enabled: boolean }>()
    appStore.fetchPublicSettings.mockImplementation(async () => {
      const settings = await deferred.promise
      appStore.cachedPublicSettings = settings
      appStore.publicSettingsLoaded = true
      return settings
    })

    const { navigation, next } = runGuard({ requiresPayment: true }, '/purchase')

    await vi.waitFor(() => expect(appStore.fetchPublicSettings).toHaveBeenCalledTimes(1))
    expect(next).not.toHaveBeenCalled()

    deferred.resolve({ payment_enabled: true })
    await navigation
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['payment', { requiresPayment: true }, '/purchase'],
    ['risk control', { requiresRiskControl: true }, '/admin/risk-control'],
    ['subscription', { requiresSubscription: true }, '/subscriptions'],
  ])('does not treat a failed %s settings load as explicitly disabled', async (_name, meta, path) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.fetchPublicSettings.mockResolvedValue(null)

    const { navigation, next } = runGuard(meta, path)
    await navigation

    expect(appStore.publicSettingsLoaded).toBe(false)
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['payment', { requiresPayment: true }, { payment_enabled: false }, '/dashboard'],
    [
      'risk control',
      { requiresRiskControl: true },
      { risk_control_enabled: false },
      '/admin/settings',
    ],
    ['subscription', { requiresSubscription: true }, { subscription_enabled: false }, '/dashboard'],
  ])('redirects when loaded settings explicitly disable %s', async (_name, meta, settings, target) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.cachedPublicSettings = settings
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(meta, '/feature')
    await navigation

    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith(target)
  })
})

describe('subscription route guard (opt-out flag)', () => {
  beforeEach(() => {
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    appStore.publicSettingsLoaded = true
    appStore.fetchPublicSettings.mockReset()
  })

  it.each([
    ['missing key', {}],
    ['explicit true', { subscription_enabled: true }],
  ])('lets /subscriptions through when the flag is %s', async (_name, settings) => {
    appStore.cachedPublicSettings = settings

    const { navigation, next } = runGuard({ requiresSubscription: true }, '/subscriptions')
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it('sends admins to the admin dashboard when subscriptions are disabled', async () => {
    authStore.isAdmin = true
    appStore.cachedPublicSettings = { subscription_enabled: false }

    const { navigation, next } = runGuard({ requiresSubscription: true }, '/subscriptions')
    await navigation

    expect(next).toHaveBeenCalledWith('/admin/dashboard')
  })
})


describe('enterprise route access', () => {
  beforeEach(() => {
    authStore.isAuthenticated = true
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    authStore.user = { enterprise: { id: 1, role: 'owner' } }
    appStore.publicSettingsLoaded = true
    appStore.cachedPublicSettings = { payment_enabled: true }
    appStore.fetchPublicSettings.mockReset()
  })

  const personalWalletRoutes = [
    'Redeem', 'PurchaseSubscription', 'OrderList', 'PaymentQRCode', 'PaymentResult',
    'StripePayment', 'AirwallexPayment', 'StripePopup', 'BatchImageGuide',
  ]
  it.each((['owner', 'manager', 'member'] as const).flatMap(role => personalWalletRoutes.map(name => ({ role, name }))))(
    'redirects enterprise $role away from personal-wallet route $name', async ({ role, name }) => {
      authStore.user = { enterprise: { id: 1, role } }
      const route = routerHarness.routes.find(item => item.name === name)!
      expect(route).toBeDefined()
      const { navigation, next } = runGuard(route.meta!, route.path)
      await navigation
      expect(next).toHaveBeenCalledOnce()
      expect(next).toHaveBeenCalledWith(role === 'member' ? '/dashboard' : '/enterprise')
    }
  )

  it.each(['owner', 'manager', 'member'] as const)('checks enterprise management permission for %s before navigation', async role => {
    authStore.user = { enterprise: { id: 1, role } }
    const route = routerHarness.routes.find(item => item.name === 'Enterprise')!
    const { navigation, next } = runGuard(route.meta!, route.path)
    await navigation
    expect(next).toHaveBeenCalledOnce()
    if (role === 'member') expect(next).toHaveBeenCalledWith('/dashboard')
    else expect(next).toHaveBeenCalledWith('/enterprise/overview')
  })

  it.each([null, 'owner', 'manager', 'member'] as const)('preserves enterprise access for a system admin with enterprise role %s', async role => {
    authStore.isAdmin = true
    authStore.user = { enterprise: role ? { id: 1, role } : null }
    const route = routerHarness.routes.find(item => item.name === 'Enterprise')!
    const { navigation, next } = runGuard(route.meta!, route.path)
    await navigation
    expect(next).toHaveBeenCalledOnce()
    if (role === null) expect(next).toHaveBeenCalledWith('/dashboard')
    else expect(next).toHaveBeenCalledWith('/enterprise/overview')
  })

  it('still sends unauthenticated enterprise visitors to login', async () => {
    authStore.isAuthenticated = false
    authStore.user = null
    const route = routerHarness.routes.find(item => item.name === 'Enterprise')!
    const { navigation, next } = runGuard(route.meta!, route.path)
    await navigation
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith({ path: '/login', query: { redirect: '/enterprise' } })
  })

  it('redirects non-members from the enterprise route', async () => {
    authStore.user = { enterprise: null }
    const route = routerHarness.routes.find(item => item.name === 'Enterprise')!
    const { navigation, next } = runGuard(route.meta!, route.path)
    await navigation
    expect(next).toHaveBeenCalledWith('/dashboard')
  })

  it('keeps ordinary users able to purchase', async () => {
    authStore.user = { enterprise: null }
    const route = routerHarness.routes.find(item => item.name === 'PurchaseSubscription')!
    const { navigation, next } = runGuard(route.meta!, route.path)
    await navigation
    expect(next).toHaveBeenCalledWith()
  })

  it.each(['AdminEnterprises', 'AdminEnterpriseDetail'])('protects actual admin route %s', async name => {
    const route = routerHarness.routes.find(item => item.name === name)!
    const { navigation, next } = runGuard(route.meta!, route.path)
    await navigation
    expect(next).toHaveBeenCalledWith('/dashboard')
  })

  it('keeps existing subscriptions accessible to enterprise members', async () => {
    const route = routerHarness.routes.find(item => item.name === 'Subscriptions')!
    const { navigation, next } = runGuard(route.meta!, route.path)
    await navigation
    expect(next).toHaveBeenCalledWith()
  })
})


describe('企业子路由', () => {
  beforeEach(() => {
    authStore.isAuthenticated = true
    authStore.isAdmin = false
    authStore.user = { enterprise: { id: 1, role: 'manager' } }
    enterpriseStore.overview = {}
    enterpriseStore.tabs = ['overview', 'organization', 'groups', 'usage']
    enterpriseStore.ensureLoaded.mockReset()
  })

  it.each(['overview', 'organization', 'quotas', 'groups', 'usage', 'audit', 'settings'])('直接访问或刷新 %s 时先加载权限，提供独立标题', async section => {
    const route = routerHarness.routes.find(item => item.path === `/enterprise/${section}`)!
    expect(route.meta?.titleKey).toBe(`enterprise.${section}`)
    const deferred = createDeferred<void>()
    enterpriseStore.ensureLoaded.mockReturnValue(deferred.promise)
    const { navigation, next } = runGuard(route.meta!, route.path)
    expect(next).not.toHaveBeenCalled()
    deferred.resolve()
    await navigation
    expect(enterpriseStore.ensureLoaded).toHaveBeenCalledOnce()
    if (enterpriseStore.tabs.includes(section)) expect(next).toHaveBeenCalledWith()
    else expect(next).toHaveBeenCalledWith('/enterprise/overview')
  })

  it.each(['quotas', 'audit', 'settings'])('权限允许时保留 %s 子页', async section => {
    enterpriseStore.tabs.push(section)
    const { navigation, next } = runGuard({ requiresEnterprise: true, enterpriseSection: section }, `/enterprise/${section}`)
    await navigation
    expect(next).toHaveBeenCalledWith()
  })

  it('member 不请求 overview，也不渲染任何企业子页', async () => {
    authStore.user = { enterprise: { id: 1, role: 'member' } }
    const { navigation, next } = runGuard({ requiresEnterprise: true, enterpriseSection: 'usage' }, '/enterprise/usage')
    await navigation
    expect(next).toHaveBeenCalledWith('/dashboard')
    expect(enterpriseStore.ensureLoaded).not.toHaveBeenCalled()
  })

  it('overview 失败后保留子路由以显示重试，不循环重定向', async () => {
    enterpriseStore.overview = null
    enterpriseStore.tabs = []
    const { navigation, next } = runGuard({ requiresEnterprise: true, enterpriseSection: 'audit' }, '/enterprise/audit')
    await navigation
    expect(next).toHaveBeenCalledWith()
  })
})
