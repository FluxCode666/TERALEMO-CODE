import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseIdentity } from '@/types/enterprise'

const mocks = vi.hoisted(() => ({
  auth: { user: null as { balance: number; enterprise: EnterpriseIdentity | null } | null, isSimpleMode: false, refreshUser: vi.fn() },
  stats: vi.fn(), trend: vi.fn(), models: vi.fn(), recent: vi.fn(), platformQuotas: vi.fn()
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/api/usage', () => ({ usageAPI: {
  getDashboardStats: mocks.stats, getDashboardTrend: mocks.trend,
  getDashboardModels: mocks.models, getByDateRange: mocks.recent
} }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: mocks.platformQuotas }))
vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key })
}))

import DashboardView from '../DashboardView.vue'

const wrappers: VueWrapper[] = []
async function render() {
  const wrapper = mount(DashboardView, { global: { stubs: {
    AppLayout: { template: '<main><slot /></main>' }, Icon: true,
    UserDashboardCharts: { template: '<div data-testid="dashboard-charts" />' },
    UserDashboardRecentUsage: true, UserDashboardQuickActions: true
  } } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
function expectOrder(wrapper: VueWrapper, selectors: string[]) {
  const elements = selectors.map(selector => wrapper.get(selector).element)
  for (let i = 1; i < elements.length; i++) {
    expect(elements[i - 1].compareDocumentPosition(elements[i]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.auth.user = { balance: 999, enterprise: {
    id: 1, name: 'Acme', role: 'member', department_id: null, department_name: null,
    member_status: 'active', enterprise_status: 'active', balance_visible: true, balance: 10, frozen_balance: 0,
    member_quota: null
  } }
  mocks.auth.isSimpleMode = false
  mocks.auth.refreshUser.mockResolvedValue(mocks.auth.user)
  mocks.stats.mockResolvedValue({ total_actual_cost: 1, by_platform: [{ platform: 'openai', total_actual_cost: 1 }] })
  mocks.trend.mockResolvedValue({ trend: [] })
  mocks.models.mockResolvedValue({ models: [] })
  mocks.recent.mockResolvedValue({ items: [] })
  mocks.platformQuotas.mockResolvedValue({ platform_quotas: [] })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('dashboard member quota placement', () => {
  it('renders one quota row after Token stats and before platform breakdown and charts', async () => {
    const wrapper = await render()
    expect(wrapper.findAll('[data-testid="member-quota-daily"]')).toHaveLength(1)
    expectOrder(wrapper, [
      '[data-testid="dashboard-core-stats"]', '[data-testid="dashboard-token-stats"]',
      '[data-testid="member-quota-daily"]', '[data-testid="dashboard-platform-breakdown"]', '[data-testid="dashboard-charts"]'
    ])
  })

  it.each(['simple mode', 'no platform data'])('keeps quota between Token stats and charts in %s', async scenario => {
    if (scenario === 'simple mode') mocks.auth.isSimpleMode = true
    else mocks.stats.mockResolvedValue({ total_actual_cost: 0, by_platform: [] })
    const wrapper = await render()
    expect(wrapper.find('[data-testid="dashboard-platform-breakdown"]').exists()).toBe(false)
    expectOrder(wrapper, ['[data-testid="dashboard-token-stats"]', '[data-testid="member-quota-daily"]', '[data-testid="dashboard-charts"]'])
  })

  it('omits member quotas for personal users', async () => {
    mocks.auth.user!.enterprise = null
    const wrapper = await render()
    expect(wrapper.find('[data-testid="member-quota-daily"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="dashboard-platform-breakdown"]').exists()).toBe(true)
  })
})
