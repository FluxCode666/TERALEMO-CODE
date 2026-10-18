import { beforeEach, describe, expect, expectTypeOf, it, vi } from 'vitest'
import type { AdminUser, EnterpriseSummary } from '@/types'
import type { CreateEnterpriseAccountMemberRequest } from '@/types/enterprise'

const { get, post, put } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post, put, delete: vi.fn() } }))

import { createEnterpriseAPI, enterpriseAPI, enterpriseErrorMessage } from '@/api/enterprise'
import { enterprisesAPI } from '@/api/admin/enterprises'

describe('enterprise API scope and cancellation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    get.mockResolvedValue({ data: [] })
    post.mockResolvedValue({ data: { id: 7 } })
    put.mockResolvedValue({ data: [] })
  })

  it('exposes administrator resources only on the administrator client', async () => {
    expect(enterpriseAPI).not.toHaveProperty('allowedGroups')
    expect(enterpriseAPI).not.toHaveProperty('updateAllowedGroups')
    expect(enterpriseAPI).not.toHaveProperty('balanceLogs')

    const admin = createEnterpriseAPI('/admin/enterprises/1')
    await admin.createMember({ user_id: 42, role: 'member' })
    expect(post).toHaveBeenCalledWith('/admin/enterprises/1/members', { user_id: 42, role: 'member' })
    await admin.allowedGroups()
    expect(get).toHaveBeenLastCalledWith('/admin/enterprises/1/allowed-groups', { params: undefined })
  })

  it('keeps the new-account member payload and administrator summary types narrow', () => {
    expectTypeOf<Parameters<typeof enterpriseAPI.createMember>[0]>().toEqualTypeOf<CreateEnterpriseAccountMemberRequest>()
    expectTypeOf<AdminUser['enterprise']>().toEqualTypeOf<EnterpriseSummary | null>()
  })

  it.each([
    ['members', '/members'], ['quotas', '/quotas'], ['usage', '/usage'],
    ['usageStats', '/usage/stats'], ['usageTrend', '/usage/trend'], ['usageModels', '/usage/models'],
    ['usageMembers', '/usage/members'], ['usageDepartments', '/usage/departments'], ['auditLogs', '/audit-logs']
  ] as const)('passes filters and an abort signal to %s', async (method, path) => {
    const params = { page: 3, department_id: 4 }
    const controller = new AbortController()
    await enterpriseAPI[method](params, { signal: controller.signal })
    expect(get).toHaveBeenCalledWith(`/enterprise${path}`, { params, signal: controller.signal })
  })

  it.each([
    ['departments', '/departments'], ['groups', '/groups'],
    ['groupWhitelists', '/group-whitelists'], ['settings', '/settings']
  ] as const)('passes an abort signal to unfiltered %s', async (method, path) => {
    const controller = new AbortController()
    await enterpriseAPI[method]({ signal: controller.signal })
    expect(get).toHaveBeenCalledWith(`/enterprise${path}`, { params: undefined, signal: controller.signal })
  })

  it('preserves overview request shape with and without cancellation', async () => {
    await enterpriseAPI.overview()
    expect(get).toHaveBeenLastCalledWith('/enterprise/overview')
    const controller = new AbortController()
    await enterpriseAPI.overview({ signal: controller.signal })
    expect(get).toHaveBeenLastCalledWith('/enterprise/overview', { signal: controller.signal })
  })

  it('allows administrator-only reads to be canceled', async () => {
    const api = createEnterpriseAPI('/admin/enterprises/1')
    const controller = new AbortController()
    await api.balanceLogs({ page: 3 }, { signal: controller.signal })
    expect(get).toHaveBeenLastCalledWith('/admin/enterprises/1/balance-logs', { params: { page: 3 }, signal: controller.signal })
    await api.allowedGroups({ signal: controller.signal })
    expect(get).toHaveBeenLastCalledWith('/admin/enterprises/1/allowed-groups', { params: undefined, signal: controller.signal })
  })

  it('passes cancellation to the administrator enterprise list and detail while preserving legacy request shapes', async () => {
    const params = { page: 3, search: 'Acme', status: 'active' }
    const controller = new AbortController()
    await enterprisesAPI.list(params, { signal: controller.signal })
    expect(get).toHaveBeenLastCalledWith('/admin/enterprises', { params, signal: controller.signal })
    await enterprisesAPI.get(7, { signal: controller.signal })
    expect(get).toHaveBeenLastCalledWith('/admin/enterprises/7', { signal: controller.signal })
    await enterprisesAPI.list(params)
    expect(get).toHaveBeenLastCalledWith('/admin/enterprises', { params })
    await enterprisesAPI.get(7)
    expect(get).toHaveBeenLastCalledWith('/admin/enterprises/7')
  })

  it.each([
    { code: 'INVALID_DATE_RANGE' },
    { reason: 'INVALID_DATE_RANGE', code: 'BAD_REQUEST' },
    { response: { data: { reason: 'INVALID_DATE_RANGE' } } }
  ])('maps invalid date ranges to the friendly locale message', (error) => {
    expect(enterpriseErrorMessage(error, key => key)).toBe('enterprise.errors.INVALID_DATE_RANGE')
  })
})
