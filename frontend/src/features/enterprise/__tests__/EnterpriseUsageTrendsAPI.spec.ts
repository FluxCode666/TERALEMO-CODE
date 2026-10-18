import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import { enterpriseAPI } from '@/api/enterprise'
import { enterprisesAPI } from '@/api/admin/enterprises'
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn() } }))
afterEach(() => vi.clearAllMocks())
describe('enterprise breakdown trend endpoints', () => {
  it.each([[enterpriseAPI, '/enterprise'], [enterprisesAPI.forEnterprise(7), '/admin/enterprises/7']] as const)('forwards exact time windows to every usage endpoint', async (api, base) => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: [] })
    const params = { start_time: '2026-10-17T12:34:56+08:00', end_time: '2026-10-18T12:34:56+08:00', granularity: 'hour' as const }
    const signal = new AbortController().signal
    for (const [method, path] of [['usage', ''], ['usageStats', '/stats'], ['usageTrend', '/trend'], ['usageModels', '/models'], ['usageMembers', '/members'], ['usageDepartments', '/departments'], ['usageModelsTrend', '/models/trend'], ['usageMembersTrend', '/members/trend'], ['usageDepartmentsTrend', '/departments/trend']] as const) {
      await api[method](params, { signal })
      expect(apiClient.get).toHaveBeenLastCalledWith(`${base}/usage${path}`, { params, signal })
    }
  })
  it.each([[enterpriseAPI, '/enterprise'], [enterprisesAPI.forEnterprise(7), '/admin/enterprises/7']] as const)('preserves scope, all filters, resolution, top and cancellation', async (api, base) => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: [] })
    const params = { start_date: '2026-10-01', end_date: '2026-10-31', granularity: 'hour' as const, top: 8, user_id: 7, department_id: 2, model: 'gpt', api_key_id: 8 }
    const signal = new AbortController().signal
    for (const [method, path] of [['usageModelsTrend', 'models'], ['usageMembersTrend', 'members'], ['usageDepartmentsTrend', 'departments']] as const) {
      await api[method](params, { signal })
      expect(apiClient.get).toHaveBeenLastCalledWith(`${base}/usage/${path}/trend`, { params, signal })
    }
  })
})
