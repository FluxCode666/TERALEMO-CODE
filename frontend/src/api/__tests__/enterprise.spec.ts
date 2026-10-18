import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put, remove } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  remove: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post, put, delete: remove }
}))

import { createEnterpriseAPI, enterpriseAPI, enterpriseErrorMessage } from '@/api/enterprise'
import { enterprisesAPI as enterprises } from '@/api/admin/enterprises'

const page = { items: [], total: 0, page: 2, page_size: 20, pages: 0 }
const limits = {
  daily_limit_usd: 0,
  weekly_limit_usd: null,
  monthly_limit_usd: 200,
  total_limit_usd: null
}

describe('enterprise API contract', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    get.mockResolvedValue({ data: page })
    post.mockResolvedValue({ data: { id: 7 } })
    put.mockResolvedValue({ data: { id: 7 } })
    remove.mockResolvedValue({ data: undefined })
  })

  it('preserves overview visibility nulls and the unwrapped response body', async () => {
    const overview = {
      enterprise: { id: 1, name: 'Acme', status: 'active' },
      balance_visible: false,
      balance: null,
      enterprise_quota: null,
      department_quota_visible: false,
      department_quota: null,
      member_quota_visible: false,
      member_quota: null,
      settings: null
    }
    get.mockResolvedValue({ data: overview })

    expect(await enterpriseAPI.overview()).toEqual(overview)
    expect(get).toHaveBeenCalledWith('/enterprise/overview')
  })

  it('keeps pagination and scope filters in members and usage requests', async () => {
    const memberParams = { page: 2, page_size: 20, search: 'alice', department_id: 3, role: 'member' as const }
    const usageParams = {
      page: 2,
      page_size: 20,
      start_date: '2026-10-01',
      end_date: '2026-10-17',
      user_id: 42,
      department_id: 3,
      model: 'claude-x',
      api_key_id: 9
    }

    expect(await enterpriseAPI.members(memberParams)).toEqual(page)
    expect(get).toHaveBeenLastCalledWith('/enterprise/members', { params: memberParams })
    expect(await enterpriseAPI.usage(usageParams)).toEqual(page)
    expect(get).toHaveBeenLastCalledWith('/enterprise/usage', { params: usageParams })
  })

  it('uses member record IDs for scoped quotas and preserves zero and null limits', async () => {
    await enterpriseAPI.updateQuota('member', 7, limits)

    expect(put).toHaveBeenCalledWith('/enterprise/quotas/member/7', limits)
    expect(limits.daily_limit_usd).toBe(0)
    expect(limits.weekly_limit_usd).toBeNull()
  })

  it('does not drop department_id zero when moving a member out of a department', async () => {
    await enterpriseAPI.updateMember(7, { department_id: 0, title: '' })

    expect(put).toHaveBeenCalledWith('/enterprise/members/7', { department_id: 0, title: '' })
  })

  it('passes enterprise-side new account credentials without switching to registration', async () => {
    const member = {
      email: 'new@example.com', password: 'a-long-password', username: 'Alice',
      department_id: 3, role: 'member' as const, title: 'Engineer'
    }
    await enterpriseAPI.createMember(member)

    expect(post).toHaveBeenCalledWith('/enterprise/members', member)
  })

  it('uses the enterprise groups endpoint for selectable whitelist groups', async () => {
    const groups = [{ id: 5, name: 'Claude', platform: 'anthropic', status: 'active' }]
    get.mockResolvedValue({ data: groups })

    expect(await enterpriseAPI.groups()).toEqual(groups)
    expect(get).toHaveBeenCalledWith('/enterprise/groups', { params: undefined })
  })

  it('wraps whitelist group IDs and quota reset windows in the documented payloads', async () => {
    await enterpriseAPI.updateGroupWhitelist('member', 7, [])
    expect(put).toHaveBeenLastCalledWith('/enterprise/group-whitelists/member/7', { group_ids: [] })

    await enterpriseAPI.resetQuota('department', 3, ['monthly', 'total'])
    expect(post).toHaveBeenLastCalledWith('/enterprise/quotas/department/3/reset', { windows: ['monthly', 'total'] })
  })

  it.each([
    ['usageStats', 'stats'],
    ['usageTrend', 'trend'],
    ['usageModels', 'models'],
    ['usageMembers', 'members'],
    ['usageDepartments', 'departments']
  ] as const)('passes scoped date filters to %s', async (method, path) => {
    const params = { start_date: '2026-10-01', end_date: '2026-10-17', department_id: 3 }
    await enterpriseAPI[method](params)
    expect(get).toHaveBeenCalledWith(`/enterprise/usage/${path}`, { params })
  })

  it('shares endpoint construction without leaking the admin enterprise ID into request data', async () => {
    const api = createEnterpriseAPI('/admin/enterprises/12')
    await api.createDepartment({ name: '研发部', sort_order: 1 })
    expect(post).toHaveBeenCalledWith('/admin/enterprises/12/departments', { name: '研发部', sort_order: 1 })

    await api.deleteMember(7)
    expect(remove).toHaveBeenCalledWith('/admin/enterprises/12/members/7')
  })

  it('propagates enterprise errors with their code and metadata intact', async () => {
    const error = {
      status: 429,
      code: 'MEMBER_QUOTA_EXCEEDED',
      metadata: { scope: 'member', window: 'monthly', window_resets_at: '2026-11-01T00:00:00Z' }
    }
    get.mockRejectedValue(error)

    await expect(enterpriseAPI.overview()).rejects.toBe(error)
  })
})

describe('admin enterprises API contract', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    get.mockResolvedValue({ data: page })
    post.mockResolvedValue({ data: { id: 12 } })
    put.mockResolvedValue({ data: { id: 12 } })
    remove.mockResolvedValue({ data: undefined })
  })

  it('sends list status and search filters using the standard pagination contract', async () => {
    const params = { page: 2, page_size: 20, search: 'Acme', status: 'active' as const }
    expect(await enterprises.list(params)).toEqual(page)
    expect(get).toHaveBeenCalledWith('/admin/enterprises', { params })
  })

  it.each([
    { user_id: 42 },
    { email: 'owner@example.com', password: 'a-long-password', username: 'owner' }
  ])('supports each initial owner shape without creating a separate user request: %o', async (owner) => {
    const payload = { name: 'Acme', owner, low_balance_threshold: null }
    expect(await enterprises.create(payload)).toEqual({ id: 12 })
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/admin/enterprises', payload)
  })

  it('requires an explicit dissolution flag to send the dissolve query', async () => {
    await enterprises.remove(12)
    expect(remove).toHaveBeenLastCalledWith('/admin/enterprises/12')
    await enterprises.remove(12, true)
    expect(remove).toHaveBeenLastCalledWith('/admin/enterprises/12', { params: { dissolve: true } })
    await enterprises.remove(12, false)
    expect(remove).toHaveBeenLastCalledWith('/admin/enterprises/12')
  })

  it('keeps balance adjustment operation and positive amount in the documented payload', async () => {
    const payload = { operation: 'subtract' as const, amount: 1.25, notes: 'Correction' }
    expect(await enterprises.adjustBalance(12, payload)).toEqual({ id: 12 })
    expect(post).toHaveBeenCalledWith('/admin/enterprises/12/balance', payload)
  })

  it('uses the scoped admin API for existing-user membership and balance logs', async () => {
    const api = enterprises.forEnterprise(12)
    await api.createMember({ user_id: 42, role: 'member', department_id: 3 })
    expect(post).toHaveBeenCalledWith('/admin/enterprises/12/members', { user_id: 42, role: 'member', department_id: 3 })

    await api.balanceLogs({ page: 2, page_size: 20 })
    expect(get).toHaveBeenCalledWith('/admin/enterprises/12/balance-logs', { params: { page: 2, page_size: 20 } })
  })

  it('wraps the admin group pool, including clearing it with an empty array', async () => {
    await enterprises.forEnterprise(12).updateAllowedGroups([])
    expect(put).toHaveBeenCalledWith('/admin/enterprises/12/allowed-groups', { group_ids: [] })
  })
})

describe('enterprise errors', () => {
  const translate = (key: string) => `translated:${key}`

  it.each([
    { code: 'ENTERPRISE_LAST_OWNER' },
    { reason: 'ENTERPRISE_LAST_OWNER', code: 'CONFLICT' },
    { response: { data: { code: 'ENTERPRISE_LAST_OWNER' } } },
    { response: { data: { reason: 'ENTERPRISE_LAST_OWNER', code: 'CONFLICT' } } }
  ])('maps backend reason and code envelopes to a friendly locale key: %o', (error) => {
    expect(enterpriseErrorMessage(error, translate)).toBe('translated:enterprise.errors.ENTERPRISE_LAST_OWNER')
  })

  it.each([null, undefined, new Error('sensitive upstream error'), { code: 'UNKNOWN_ERROR' }])('uses the friendly fallback for an unknown error: %o', (error) => {
    expect(enterpriseErrorMessage(error, translate)).toBe('translated:enterprise.errors.generic')
  })
})
