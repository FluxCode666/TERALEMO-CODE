import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import { createEnterpriseUsageOptionsAPI } from '../EnterpriseUsageOptionsAPI'
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn() } }))
afterEach(() => vi.clearAllMocks())
describe('enterprise usage option scope', () => {
  it.each([[false, undefined, '/enterprise'], [true, 7, '/admin/enterprises/7']] as const)('reads key and model options in the correct scope', async (admin, id, base) => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: [] })
    const api = createEnterpriseUsageOptionsAPI(admin, id)
    const signal = new AbortController().signal
    await api.keys('Production', signal)
    expect(apiClient.get).toHaveBeenLastCalledWith(`${base}/usage/api-key-options`, { params: { search: 'Production', limit: 50 }, signal })
    await api.models('gpt', signal, { start_date: '2026-10-01', end_date: '2026-10-18' })
    expect(apiClient.get).toHaveBeenLastCalledWith(`${base}/usage/model-options`, { params: { search: 'gpt', limit: 50, start_date: '2026-10-01', end_date: '2026-10-18' }, signal })
    const times = { start_time: '2026-10-17T12:34:56+08:00', end_time: '2026-10-18T12:34:56+08:00' }
    await api.models('gpt', signal, times)
    expect(apiClient.get).toHaveBeenLastCalledWith(`${base}/usage/model-options`, { params: { search: 'gpt', limit: 50, ...times }, signal })
  })
  it('does not fall back to the current user enterprise if an admin scope is missing', async () => {
    await expect(createEnterpriseUsageOptionsAPI(true).keys('', new AbortController().signal)).rejects.toThrow('Missing enterprise usage scope')
    expect(apiClient.get).not.toHaveBeenCalled()
  })
})
