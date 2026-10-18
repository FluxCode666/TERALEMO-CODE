import { apiClient } from '@/api/client'
import type { EnterpriseQuery } from '@/types/enterprise'

export interface EnterpriseUsageOption { value: string; label: string; detail?: string }
interface UsageKeyOption { id: number; name: string; user: { id: number; username: string; email: string } }
interface UsageModelOption { model: string; requests: number }

/** Option endpoints share the route scope with the panel without changing its public props. */
export function createEnterpriseUsageOptionsAPI(admin: boolean, enterpriseId?: number) {
  const base = admin && enterpriseId ? `/admin/enterprises/${enterpriseId}` : '/enterprise'
  async function get<T>(resource: string, search: string, signal: AbortSignal, dates = {}) {
    // An admin panel must never accidentally query the signed-in user's enterprise.
    if (admin && !enterpriseId) throw new Error('Missing enterprise usage scope')
    return (await apiClient.get<T>(`${base}/usage/${resource}-options`, { params: { search, limit: 50, ...dates }, signal })).data
  }
  return {
    keys: (search: string, signal: AbortSignal) => get<UsageKeyOption[]>('api-key', search, signal),
    models: (search: string, signal: AbortSignal, dates: Pick<EnterpriseQuery, 'start_date' | 'end_date' | 'start_time' | 'end_time'>) => get<UsageModelOption[]>('model', search, signal, dates)
  }
}
