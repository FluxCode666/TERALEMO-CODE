import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { Enterprise, EnterpriseOverview, EnterprisePermissions, EnterpriseQuota, EnterpriseRole } from '@/types/enterprise'
import { defaultEnterpriseSettings } from '@/types/enterprise'
import EnterpriseWorkspace from '../EnterpriseWorkspace.vue'
import { getEnterpriseTabs, type EnterpriseSection } from '../navigation'
import EnterpriseQuotaDisplay from '../components/EnterpriseQuotaDisplay.vue'
import EnterpriseDetailView from '@/views/admin/EnterpriseDetailView.vue'

const { api, getEnterprise, showError, updateMemberConcurrency } = vi.hoisted(() => ({
  api: {
    overview: vi.fn(), departments: vi.fn(), createDepartment: vi.fn(), updateDepartment: vi.fn(), deleteDepartment: vi.fn(),
    members: vi.fn(), createMember: vi.fn(), updateMember: vi.fn(), deleteMember: vi.fn(),
    quotas: vi.fn(), updateQuota: vi.fn(), resetQuota: vi.fn(),
    groups: vi.fn(), allowedGroups: vi.fn(), updateAllowedGroups: vi.fn(),
    groupWhitelists: vi.fn(), updateGroupWhitelist: vi.fn(),
    settings: vi.fn(), updateSettings: vi.fn(),
    usage: vi.fn(), usageStats: vi.fn(), usageTrend: vi.fn(), usageModels: vi.fn(), usageMembers: vi.fn(), usageDepartments: vi.fn(),
    auditLogs: vi.fn(), balanceLogs: vi.fn()
  },
  getEnterprise: vi.fn(),
  updateMemberConcurrency: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/enterprise', async () => {
  const actual = await vi.importActual<typeof import('@/api/enterprise')>('@/api/enterprise')
  return { ...actual, enterpriseAPI: api }
})

vi.mock('@/api/admin/enterprises', () => ({
  enterprisesAPI: { forEnterprise: () => api, get: getEnterprise, updateMemberConcurrency }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { groups: { getAll: vi.fn() }, users: { list: vi.fn() } }
}))

vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('vue-router', async original => ({ ...await original<typeof import('vue-router')>(), useRoute: () => ({ params: { id: '1' }, query: {} }), useRouter: () => ({ push: vi.fn() }) }))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string, params?: { amount: string }) => params ? `${key} ${params.amount}` : key, locale: { value: 'zh' } }) }
})

const allPermissions: EnterprisePermissions = {
  manage_departments: true, manage_members: true, create_members: true,
  edit_member_quota: true, edit_member_groups: true, edit_quotas: true,
  edit_settings: true, view_members: true, view_audit_logs: true,
  view_department_usage: true
}

function quota(scope: EnterpriseQuota['scope_type'], id: number): EnterpriseQuota {
  return {
    scope_type: scope, scope_id: id,
    daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null, total_limit_usd: null,
    daily_usage_usd: 1, weekly_usage_usd: 2, monthly_usage_usd: 3, total_usage_usd: 4
  }
}

function overview(role: EnterpriseRole, overrides: Partial<EnterpriseOverview> = {}): EnterpriseOverview {
  const isOwner = role === 'owner'
  return {
    enterprise: { id: 1, name: 'Acme', status: 'active' },
    me: { member_id: 7, user_id: 42, role, status: 'active', department_id: 3, department_name: '研发部', title: '' },
    balance_visible: isOwner,
    balance: isOwner ? 120.5 : null,
    frozen_balance: isOwner ? 7.25 : null,
    enterprise_quota: isOwner ? quota('enterprise', 1) : null,
    department_quota_visible: role !== 'member',
    department_quota: role !== 'member' ? quota('department', 3) : null,
    member_quota_visible: true,
    member_quota: quota('member', 7),
    settings: isOwner ? { ...defaultEnterpriseSettings } : null,
    permissions: isOwner ? { ...allPermissions } : {
      manage_departments: false, manage_members: false, create_members: false,
      edit_member_quota: false, edit_member_groups: false, edit_quotas: false,
      edit_settings: false, view_members: role === 'manager', view_audit_logs: false,
      view_department_usage: false
    },
    stats: { today_cost: 1.2, month_cost: 30.5 },
    ...overrides
  }
}

const wrappers: VueWrapper[] = []

afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
})

function mountWorkspace(value: EnterpriseOverview) {
  const wrapper = mount(EnterpriseWorkspace, {
    props: { overview: value },
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="header" /><slot name="filters" /><slot /><slot name="pagination" /></div>' },
        EnterprisePagination: true,
        Line: true,
        Icon: true
      }
    }
  })
  wrappers.push(wrapper)
  return wrapper
}

function tabNames(wrapper: VueWrapper) {
  return getEnterpriseTabs(wrapper.props('overview'), wrapper.props('admin'))
}

async function openTab(wrapper: VueWrapper, name: string) {
  if (wrapper.props('admin')) await wrapper.get(`[data-test="tab-${name}"]`).trigger('click')
  else await wrapper.setProps({ section: name as EnterpriseSection })
  await flushPromises()
}

describe('EnterpriseWorkspace server permissions and visibility', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    for (const request of Object.values(api)) request.mockResolvedValue([])
    const page = { items: [], total: 0, page: 1, page_size: 20, pages: 0 }
    api.members.mockResolvedValue(page)
    api.usage.mockResolvedValue(page)
    api.auditLogs.mockResolvedValue(page)
    api.balanceLogs.mockResolvedValue(page)
    api.usageStats.mockResolvedValue({ total_requests: 0, total_tokens: 0, total_actual_cost: 0 })
    api.settings.mockResolvedValue({ ...defaultEnterpriseSettings })
  })

  it.each([
    ['owner', ['overview', 'organization', 'quotas', 'groups', 'usage', 'audit', 'settings']],
    ['manager', ['overview', 'organization', 'quotas', 'groups', 'usage']],
    ['member', ['overview', 'organization', 'quotas', 'groups', 'usage']]
  ] as const)('renders only permitted tabs for %s', async (role, expected) => {
    const wrapper = mountWorkspace(overview(role))
    await flushPromises()
    expect(tabNames(wrapper).sort()).toEqual([...expected].sort())
  })

  it('用户侧按子页渲染，隐藏 Tab 栏并保留懒加载', async () => {
    const wrapper = mountWorkspace(overview('owner'))
    await flushPromises()
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'EnterpriseOrganizationPanel' }).exists()).toBe(false)
    await openTab(wrapper, 'organization')
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'EnterpriseOrganizationPanel' }).exists()).toBe(true)
    expect(wrapper.find('[data-tab-content]').exists()).toBe(true)
  })

  it('hides forbidden tabs and all invisible null overview cards instead of showing zero', async () => {
    const wrapper = mountWorkspace(overview('member', {
      member_quota_visible: false, member_quota: null,
      department_quota_visible: false, department_quota: null,
      balance_visible: false, balance: null
    }))
    await flushPromises()

    expect(tabNames(wrapper)).not.toContain('quotas')
    for (const card of ['enterprise-balance', 'enterprise-quota', 'department-quota', 'member-quota']) {
      expect(wrapper.find(`[data-test="${card}"]`).exists(), card).toBe(false)
    }
    expect(wrapper.text()).toContain('Acme')
  })

  it('keeps a visible zero enterprise balance visible', async () => {
    const wrapper = mountWorkspace(overview('member', { balance_visible: true, balance: 0 }))
    await flushPromises()

    expect(wrapper.find('[data-test="enterprise-balance"]').exists()).toBe(true)
  })

  it('does not show the enterprise frozen balance in the member overview', async () => {
    const wrapper = mountWorkspace(overview('member', { balance_visible: true, balance: 120.5, frozen_balance: 7.25 }))
    await flushPromises()
    expect(wrapper.find('[data-test="enterprise-frozen-balance"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('enterprise.frozen')
    expect(wrapper.get('[data-test="enterprise-balance"]').text()).toContain('$120.5000')
  })

  it('does not show the enterprise frozen balance in the administrator overview', async () => {
    getEnterprise.mockResolvedValue({
      id: 1, name: 'Acme', status: 'active', balance: 100, frozen_balance: 7.25,
      total_recharged: 150, member_count: 2, department_count: 1
    })
    const wrapper = mount(EnterpriseWorkspace, { props: { admin: true, enterpriseId: 1 } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.find('[data-test="enterprise-frozen-balance"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('$7.2500')
    expect(wrapper.get('[data-test="enterprise-balance"]').text()).toContain('$100.0000')
  })

  it('keeps the quota tab available when only member quota editing is permitted', async () => {
    const value = overview('manager', {
      enterprise_quota: null, department_quota_visible: false, department_quota: null,
      member_quota_visible: false, member_quota: null
    })
    value.permissions.edit_member_quota = true
    const wrapper = mountWorkspace(value)
    await flushPromises()

    expect(tabNames(wrapper)).toContain('quotas')
  })

  it('waits for fresh overview data and refreshes the current tab without remounting it', async () => {
    const value = overview('owner')
    const wrapper = mount(EnterpriseWorkspace, {
      props: { overview: value },
      global: { stubs: { EnterpriseSettingsPanel: { name: 'EnterpriseSettingsPanel', props: ['refreshKey'], template: '<div />' } } }
    })
    wrappers.push(wrapper)
    await openTab(wrapper, 'settings')
    const panel = wrapper.findComponent({ name: 'EnterpriseSettingsPanel' })
    const instance = panel.vm
    const refreshKey = panel.props('refreshKey')

    await wrapper.get('[data-test="enterprise-refresh"]').trigger('click')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(panel.props('refreshKey')).toBe(refreshKey)

    await wrapper.setProps({ overview: { ...value, balance: 250 } })
    expect(wrapper.props('section')).toBe('settings')
    expect(wrapper.findComponent({ name: 'EnterpriseSettingsPanel' }).vm).toBe(instance)
    expect(panel.props('refreshKey')).toBe(refreshKey + 1)
  })

  it('reloads the current administrator tab after refreshing enterprise details', async () => {
    const enterprise: Enterprise = {
      id: 1, name: 'Acme', status: 'active', balance: 100, frozen_balance: 7.25, total_recharged: 100,
      low_balance_threshold: null, contact_name: '', contact_email: '', contact_phone: '', notes: '',
      settings: { ...defaultEnterpriseSettings }, member_count: 0, department_count: 0,
      created_at: '', updated_at: ''
    }
    getEnterprise.mockResolvedValue(enterprise)
    const wrapper = mount(EnterpriseWorkspace, {
      props: { admin: true, enterpriseId: 1 },
      global: { stubs: { EnterpriseSettingsPanel: { name: 'EnterpriseSettingsPanel', props: ['refreshKey'], template: '<div />' }, EnterpriseBasicsForm: true } }
    })
    wrappers.push(wrapper)
    await flushPromises()
    await openTab(wrapper, 'settings')
    const panel = wrapper.findComponent({ name: 'EnterpriseSettingsPanel' })
    const instance = panel.vm
    const refreshKey = panel.props('refreshKey')

    getEnterprise.mockResolvedValue({ ...enterprise, balance: 200 })
    await wrapper.get('[data-test="enterprise-refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="tablist"]').exists()).toBe(true)
    expect(getEnterprise).toHaveBeenCalledTimes(2)
    expect(panel.vm).toBe(instance)
    expect(panel.props('refreshKey')).toBe(refreshKey + 1)
    expect(wrapper.get('[data-test="tab-settings"]').attributes('aria-current')).toBe('page')
  })

  it('cancels the administrator detail read when leaving the workspace and ignores a late failure', async () => {
    let reject!: (reason: unknown) => void
    getEnterprise.mockReturnValueOnce(new Promise<Enterprise>((_resolve, no) => { reject = no }))
    const wrapper = mount(EnterpriseWorkspace, { props: { admin: true, enterpriseId: 7 } })
    await flushPromises()
    const signal = getEnterprise.mock.calls.at(-1)![1].signal as AbortSignal
    expect(getEnterprise).toHaveBeenLastCalledWith(7, { signal })
    expect(signal.aborted).toBe(false)
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    reject(new Error('late detail failure'))
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
  })

  it.each([0, 1, undefined])('shows the administrator owner warning only for an explicit zero count (%s)', async activeOwnerCount => {
    const enterprise: Enterprise = {
      id: 1, name: 'Acme', status: 'active', balance: 100, frozen_balance: 7.25, total_recharged: 100,
      low_balance_threshold: null, contact_name: '', contact_email: '', contact_phone: '', notes: '',
      settings: { ...defaultEnterpriseSettings }, member_count: 2, department_count: 1,
      active_owner_count: activeOwnerCount, created_at: '', updated_at: ''
    }
    getEnterprise.mockResolvedValue(enterprise)
    const wrapper = mount(EnterpriseDetailView, {
      global: { stubs: { AppLayout: { template: '<main><slot /></main>' } } }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const warning = wrapper.find('[data-test="enterprise-owner-warning"]')
    expect(warning.exists()).toBe(activeOwnerCount === 0)
    if (activeOwnerCount === 0) {
      expect(warning.text()).toContain('admin.enterprises.noActiveOwner')
      expect(warning.text()).toContain('admin.enterprises.noActiveOwnerHint')
      await wrapper.get('[data-test="tab-organization"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-test="enterprise-owner-warning"]').exists()).toBe(true)
      getEnterprise.mockResolvedValue({ ...enterprise, active_owner_count: 1 })
      wrapper.findComponent({ name: 'EnterpriseOrganizationPanel' }).vm.$emit('updated')
      await flushPromises()
      expect(wrapper.find('[data-test="enterprise-owner-warning"]').exists()).toBe(false)
    }
  })

  it('places detail deletion beside refresh and opens the existing confirmation dialog', async () => {
    getEnterprise.mockResolvedValue({
      id: 1, name: 'Acme', status: 'active', balance: 100, frozen_balance: 7.25, total_recharged: 100,
      member_count: 2, department_count: 1
    })
    const wrapper = mount(EnterpriseDetailView, {
      global: {
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          EnterpriseDeleteDialog: { name: 'EnterpriseDeleteDialog', props: ['enterpriseId'], template: '<div />' }
        }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const actions = wrapper.get('[data-test="enterprise-header-actions"]')
    expect(actions.get('[data-test="enterprise-refresh"]').text()).toBe('enterprise.refresh')
    const deleteButton = actions.get('[data-test="enterprise-delete-detail"]')
    expect(deleteButton.classes()).toContain('bg-destructive')
    await deleteButton.trigger('click')
    expect(wrapper.findComponent({ name: 'EnterpriseDeleteDialog' }).props('enterpriseId')).toBe(1)
  })

  it('does not request members, settings, audit, or department rankings for an ordinary member', async () => {
    const wrapper = mountWorkspace(overview('member'))
    await flushPromises()
    for (const name of ['organization', 'quotas', 'groups', 'usage']) await openTab(wrapper, name)

    expect(api.departments).toHaveBeenCalled()
    expect(api.quotas).toHaveBeenCalled()
    expect(api.groups).toHaveBeenCalled()
    expect(api.usage).toHaveBeenCalled()
    for (const name of ['members', 'settings', 'auditLogs', 'usageMembers', 'usageDepartments', 'groupWhitelists', 'balanceLogs'] as const) {
      expect(api[name], name).not.toHaveBeenCalled()
    }
    expect(showError).not.toHaveBeenCalled()
  })

  it('renders the redacted D6 usage DTO without depending on personal wallet or upstream account fields', async () => {
    api.usage.mockResolvedValue({
      items: [{
        id: 1, user: { id: 42, username: 'Alice', email: 'alice@example.com' }, department_id: 3,
        api_key: { id: 9, name: 'work-key' }, group: { id: 1, name: 'Claude' }, model: 'claude-x',
        input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0,
        actual_cost: 0.01, billing_type: 0, request_type: 0, stream: true,
        duration_ms: 1000, first_token_ms: 200, created_at: '2026-10-17T00:00:00Z'
      }],
      total: 1, page: 1, page_size: 20, pages: 1
    })
    const wrapper = mountWorkspace(overview('member'))
    await flushPromises()
    await openTab(wrapper, 'usage')

    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('work-key')
    expect(wrapper.text()).toContain('claude-x')
    expect(wrapper.text()).toContain('$0.0100')
    expect(showError).not.toHaveBeenCalled()
  })

  it('does not infer owner-only access from manager member-management permissions', async () => {
    const value = overview('manager')
    value.permissions.manage_members = true
    value.permissions.create_members = true
    value.permissions.edit_member_quota = true
    value.permissions.edit_member_groups = true
    const wrapper = mountWorkspace(value)
    await flushPromises()

    expect(tabNames(wrapper)).toContain('organization')
    expect(tabNames(wrapper)).not.toContain('settings')
    expect(tabNames(wrapper)).not.toContain('audit')
    await openTab(wrapper, 'organization')
    expect(api.members).toHaveBeenCalled()
    expect(api.settings).not.toHaveBeenCalled()
    expect(api.auditLogs).not.toHaveBeenCalled()
  })

  it('loads privileged content only when its permitted tab is opened', async () => {
    const wrapper = mountWorkspace(overview('owner'))
    await flushPromises()
    expect(api.settings).not.toHaveBeenCalled()
    expect(api.auditLogs).not.toHaveBeenCalled()

    await openTab(wrapper, 'settings')
    expect(api.settings).toHaveBeenCalledTimes(1)
    expect(api.auditLogs).not.toHaveBeenCalled()
    await openTab(wrapper, 'audit')
    expect(api.auditLogs).toHaveBeenCalledTimes(1)
  })

  it.each(['enterprise', 'member'] as const)('makes management panels readonly when the %s is disabled', async (disabledScope) => {
    const value = overview('owner')
    if (disabledScope === 'enterprise') value.enterprise.status = 'disabled'
    else value.me.status = 'disabled'
    const wrapper = mountWorkspace(value)
    await flushPromises()

    await openTab(wrapper, 'organization')
    expect(wrapper.find('[data-test="organization-create"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'EnterpriseOrganizationPanel' }).props('readonly')).toBe(true)
    await openTab(wrapper, 'organization')
    expect(wrapper.find('[data-test="organization-create"]').exists()).toBe(false)
    await openTab(wrapper, 'quotas')
    expect(wrapper.findComponent({ name: 'EnterpriseQuotaPanel' }).props('readonly')).toBe(true)
    await openTab(wrapper, 'groups')
    expect(wrapper.findComponent({ name: 'EnterpriseGroupsPanel' }).props('readonly')).toBe(true)
    await openTab(wrapper, 'settings')
    expect(wrapper.findComponent({ name: 'EnterpriseSettingsPanel' }).props('readonly')).toBe(true)
  })
})

it.each(['owner', 'manager', 'member'] as const)('labels the %s current-user quota as my quota and forwards the enterprise name', async role => {
  const wrapper = mountWorkspace(overview(role))
  await flushPromises()
  expect(wrapper.getComponent('[data-test="member-quota"]').props('title')).toBe('enterprise.myQuota')
  await openTab(wrapper, 'quotas')
  expect(wrapper.getComponent({ name: 'EnterpriseQuotaPanel' }).props('enterpriseName')).toBe('Acme')
})

describe('EnterpriseQuotaDisplay four quota windows', () => {
  it('shows null limits as unlimited for daily, weekly, monthly, and total windows', () => {
    const wrapper = mount(EnterpriseQuotaDisplay, { props: { title: 'Quota', quota: quota('member', 7) } })
    wrappers.push(wrapper)

    const windows = wrapper.findAll('dl > div')
    expect(windows).toHaveLength(4)
    for (const [index, window] of ['daily', 'weekly', 'monthly', 'total'].entries()) {
      expect(windows[index].text()).toContain(`enterprise.${window}`)
      expect(windows[index].text()).toContain('enterprise.unlimited')
    }
  })

  it('keeps zero limits distinct from unlimited without rendering recorded usage', () => {
    const value = quota('member', 7)
    value.daily_limit_usd = 0
    const wrapper = mount(EnterpriseQuotaDisplay, { props: { title: 'Quota', quota: value } })
    wrappers.push(wrapper)

    const daily = wrapper.findAll('dl > div')[0]
    expect(daily.text()).toContain('enterprise.quotaSections.disabled')
    expect(daily.text()).not.toContain('$1.0000')
    expect(daily.text()).not.toContain('enterprise.unlimited')
  })
})

it('declares narrow and wide tab layouts with a reduced-motion-safe width transition', async () => {
  const wrapper = mountWorkspace(overview('owner'))
  await flushPromises()
  for (const name of ['overview', 'quotas', 'groups', 'settings', 'audit', 'organization', 'usage']) {
    await openTab(wrapper, name)
    const wide = ['organization', 'usage'].includes(name)
    expect(wrapper.attributes('data-layout')).toBe(wide ? 'wide' : 'narrow')
    expect(wrapper.classes()).toContain(wide ? 'max-w-full' : 'max-w-6xl')
  }
  expect(wrapper.classes()).toContain('motion-safe:transition-[max-width]')
  expect(wrapper.classes()).toContain('motion-reduce:transition-none')
})

it('applies the same layout declarations to administrator enterprise details including balance logs', async () => {
  getEnterprise.mockResolvedValue({ id: 1, name: 'Acme', status: 'active', balance: 100, frozen_balance: 0, total_recharged: 100, member_count: 0, department_count: 0, settings: { ...defaultEnterpriseSettings } })
  const wrapper = mount(EnterpriseWorkspace, { props: { admin: true, enterpriseId: 1 }, global: { stubs: { EnterpriseOrganizationPanel: true, EnterpriseUsagePanel: true, EnterpriseQuotaPanel: true, EnterpriseGroupsPanel: true, EnterpriseSettingsPanel: true, EnterpriseLogsPanel: true, EnterpriseBasicsForm: true, EnterpriseAdminQuotaSummary: true } } })
  wrappers.push(wrapper); await flushPromises()
  for (const name of ['overview', 'quotas', 'groups', 'settings', 'audit', 'balanceLogs', 'organization', 'usage']) {
    await openTab(wrapper, name)
    expect(wrapper.attributes('data-layout')).toBe(['organization', 'usage'].includes(name) ? 'wide' : 'narrow')
  }
})

it('refreshes administrator data after unified concurrency changes and offers no enterprise-side entry', async () => {
  const value = { id: 1, name: 'Acme', status: 'active', balance: 0, total_recharged: 0, member_count: 5, department_count: 0, settings: { ...defaultEnterpriseSettings }, member_concurrency: null }
  getEnterprise.mockResolvedValue(value)
  updateMemberConcurrency.mockResolvedValue({ member_concurrency: 24, updated_count: 5 })
  const wrapper = mount(EnterpriseWorkspace, { props: { admin: true, enterpriseId: 1 }, global: { stubs: { EnterpriseBasicsForm: true, EnterpriseSettingsPanel: true, EnterpriseConfirm: { name: 'EnterpriseConfirm', props: ['show'], emits: ['confirm'], template: '<button v-if="show" data-test="confirm-concurrency" @click="$emit(\'confirm\')">Confirm</button>' } } } })
  wrappers.push(wrapper); await flushPromises(); await openTab(wrapper, 'settings')
  expect(wrapper.find('[data-test="member-concurrency-card"]').exists()).toBe(true)
  await wrapper.get('[data-test="member-concurrency-input"]').setValue('24')
  await wrapper.get('[data-test="member-concurrency-form"]').trigger('submit')
  getEnterprise.mockResolvedValue({ ...value, member_concurrency: 24 })
  const reads = getEnterprise.mock.calls.length
  await wrapper.get('[data-test="confirm-concurrency"]').trigger('click'); await flushPromises()
  expect(updateMemberConcurrency).toHaveBeenCalledWith(1, { concurrency: 24 })
  expect(getEnterprise).toHaveBeenCalledTimes(reads + 1)
  expect(wrapper.getComponent({ name: 'EnterpriseMemberConcurrencyCard' }).props('enterprise').member_concurrency).toBe(24)
  const user = mountWorkspace(overview('owner'))
  await openTab(user, 'settings')
  expect(user.find('[data-test="member-concurrency-card"]').exists()).toBe(false)
})
