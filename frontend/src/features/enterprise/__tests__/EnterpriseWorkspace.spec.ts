import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { Enterprise, EnterpriseOverview, EnterprisePermissions, EnterpriseQuota, EnterpriseRole } from '@/types/enterprise'
import { defaultEnterpriseSettings } from '@/types/enterprise'
import EnterpriseWorkspace from '../EnterpriseWorkspace.vue'
import EnterpriseQuotaDisplay from '../components/EnterpriseQuotaDisplay.vue'

const { api, getEnterprise, showError } = vi.hoisted(() => ({
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
  showError: vi.fn()
}))

vi.mock('@/api/enterprise', async () => {
  const actual = await vi.importActual<typeof import('@/api/enterprise')>('@/api/enterprise')
  return { ...actual, enterpriseAPI: api }
})

vi.mock('@/api/admin/enterprises', () => ({
  enterprisesAPI: { forEnterprise: () => api, get: getEnterprise }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { groups: { getAll: vi.fn() }, users: { list: vi.fn() } }
}))

vi.mock('@/api/client', () => ({ apiClient: {} }))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh' } }) }
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
        Pagination: true,
        Line: true,
        Icon: true
      }
    }
  })
  wrappers.push(wrapper)
  return wrapper
}

function tabNames(wrapper: VueWrapper) {
  return wrapper.findAll('[data-test^="tab-"]').map(tab => tab.attributes('data-test')!.slice(4))
}

async function openTab(wrapper: VueWrapper, name: string) {
  await wrapper.get(`[data-test="tab-${name}"]`).trigger('click')
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
    ['owner', ['overview', 'departments', 'members', 'quotas', 'groups', 'usage', 'audit', 'settings']],
    ['manager', ['overview', 'departments', 'members', 'quotas', 'groups', 'usage']],
    ['member', ['overview', 'departments', 'quotas', 'groups', 'usage']]
  ] as const)('renders only permitted tabs for %s', async (role, expected) => {
    const wrapper = mountWorkspace(overview(role))
    await flushPromises()
    expect(tabNames(wrapper).sort()).toEqual([...expected].sort())
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

    await wrapper.get('button.btn-secondary').trigger('click')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(panel.props('refreshKey')).toBe(refreshKey)

    await wrapper.setProps({ overview: { ...value, balance: 250 } })
    expect(wrapper.get('[data-test="tab-settings"]').attributes('aria-current')).toBe('page')
    expect(wrapper.findComponent({ name: 'EnterpriseSettingsPanel' }).vm).toBe(instance)
    expect(panel.props('refreshKey')).toBe(refreshKey + 1)
  })

  it('reloads the current administrator tab after refreshing enterprise details', async () => {
    const enterprise: Enterprise = {
      id: 1, name: 'Acme', status: 'active', balance: 100, total_recharged: 100,
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
    await wrapper.get('button.btn-secondary').trigger('click')
    await flushPromises()

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

  it('does not request members, settings, audit, or department rankings for an ordinary member', async () => {
    const wrapper = mountWorkspace(overview('member'))
    await flushPromises()
    for (const name of ['departments', 'quotas', 'groups', 'usage']) await openTab(wrapper, name)

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

    expect(tabNames(wrapper)).toContain('members')
    expect(tabNames(wrapper)).not.toContain('settings')
    expect(tabNames(wrapper)).not.toContain('audit')
    await openTab(wrapper, 'members')
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

    await openTab(wrapper, 'departments')
    expect(wrapper.find('[data-test="organization-create"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'EnterpriseOrganizationPanel' }).props('readonly')).toBe(true)
    await openTab(wrapper, 'members')
    expect(wrapper.find('[data-test="organization-create"]').exists()).toBe(false)
    await openTab(wrapper, 'quotas')
    expect(wrapper.findComponent({ name: 'EnterpriseQuotaPanel' }).props('readonly')).toBe(true)
    await openTab(wrapper, 'groups')
    expect(wrapper.findComponent({ name: 'EnterpriseGroupsPanel' }).props('readonly')).toBe(true)
    await openTab(wrapper, 'settings')
    expect(wrapper.findComponent({ name: 'EnterpriseSettingsPanel' }).props('readonly')).toBe(true)
  })
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

  it('keeps zero limits distinct from unlimited and renders the recorded usage', () => {
    const value = quota('member', 7)
    value.daily_limit_usd = 0
    const wrapper = mount(EnterpriseQuotaDisplay, { props: { title: 'Quota', quota: value } })
    wrappers.push(wrapper)

    const daily = wrapper.findAll('dl > div')[0]
    expect(daily.text()).toContain('$1.0000 / $0')
    expect(daily.text()).not.toContain('enterprise.unlimited')
  })
})
