import { apiClient } from './client'
import type {
  EnterpriseOverview, EnterpriseDepartment, EnterpriseMember, EnterpriseQuota, EnterpriseSettings,
  EnterpriseGroup, EnterpriseGroupWhitelist, EnterpriseUsageLog, EnterpriseUsageStats,
  EnterpriseUsageTrend, EnterpriseUsageModel, EnterpriseMemberUsage, EnterpriseDepartmentUsage,
  EnterpriseAuditLog, EnterpriseBalanceLog, EnterprisePage, EnterpriseQuery, EnterpriseScope,
  QuotaLimits, QuotaWindow, CreateEnterpriseMemberRequest, UpdateEnterpriseMemberRequest
} from '@/types/enterprise'

/** Shared resource methods. The supplied base fixes the enterprise scope for every request. */
export function createEnterpriseAPI(base: string) {
  const get = async <T>(path: string, params?: EnterpriseQuery): Promise<T> => (await apiClient.get<T>(`${base}${path}`, { params })).data
  const post = async <T>(path: string, body: unknown): Promise<T> => (await apiClient.post<T>(`${base}${path}`, body)).data
  const put = async <T>(path: string, body: unknown): Promise<T> => (await apiClient.put<T>(`${base}${path}`, body)).data
  const remove = async (path: string): Promise<void> => { await apiClient.delete(`${base}${path}`) }
  return {
    departments: () => get<EnterpriseDepartment[]>('/departments'),
    createDepartment: (body: { name: string; sort_order?: number }) => post<EnterpriseDepartment>('/departments', body),
    updateDepartment: (id: number, body: { name?: string; sort_order?: number }) => put<EnterpriseDepartment>(`/departments/${id}`, body),
    deleteDepartment: (id: number) => remove(`/departments/${id}`),
    members: (params?: EnterpriseQuery) => get<EnterprisePage<EnterpriseMember>>('/members', params),
    createMember: (body: CreateEnterpriseMemberRequest) => post<EnterpriseMember>('/members', body),
    updateMember: (id: number, body: UpdateEnterpriseMemberRequest) => put<EnterpriseMember>(`/members/${id}`, body),
    deleteMember: (id: number) => remove(`/members/${id}`),
    quotas: (params?: EnterpriseQuery) => get<EnterpriseQuota[]>('/quotas', params),
    updateQuota: (scope: EnterpriseScope, id: number, body: QuotaLimits) => put<EnterpriseQuota>(`/quotas/${scope}/${id}`, body),
    resetQuota: (scope: EnterpriseScope, id: number, windows: QuotaWindow[]) => post<EnterpriseQuota>(`/quotas/${scope}/${id}/reset`, { windows }),
    groups: () => get<EnterpriseGroup[]>('/groups'),
    allowedGroups: () => get<EnterpriseGroup[]>('/allowed-groups'),
    updateAllowedGroups: (group_ids: number[]) => put<EnterpriseGroup[]>('/allowed-groups', { group_ids }),
    groupWhitelists: () => get<EnterpriseGroupWhitelist[]>('/group-whitelists'),
    updateGroupWhitelist: (scope: EnterpriseScope, id: number, group_ids: number[]) => put<EnterpriseGroupWhitelist>(`/group-whitelists/${scope}/${id}`, { group_ids }),
    settings: () => get<EnterpriseSettings>('/settings'),
    updateSettings: (body: EnterpriseSettings) => put<EnterpriseSettings>('/settings', body),
    usage: (params?: EnterpriseQuery) => get<EnterprisePage<EnterpriseUsageLog>>('/usage', params),
    usageStats: (params?: EnterpriseQuery) => get<EnterpriseUsageStats>('/usage/stats', params),
    usageTrend: (params?: EnterpriseQuery) => get<EnterpriseUsageTrend[]>('/usage/trend', params),
    usageModels: (params?: EnterpriseQuery) => get<EnterpriseUsageModel[]>('/usage/models', params),
    usageMembers: (params?: EnterpriseQuery) => get<EnterpriseMemberUsage[]>('/usage/members', params),
    usageDepartments: (params?: EnterpriseQuery) => get<EnterpriseDepartmentUsage[]>('/usage/departments', params),
    auditLogs: (params?: EnterpriseQuery) => get<EnterprisePage<EnterpriseAuditLog>>('/audit-logs', params),
    balanceLogs: (params?: EnterpriseQuery) => get<EnterprisePage<EnterpriseBalanceLog>>('/balance-logs', params)
  }
}
export type EnterpriseResourceAPI = ReturnType<typeof createEnterpriseAPI>
export const enterpriseAPI = {
  ...createEnterpriseAPI('/enterprise'),
  overview: async (): Promise<EnterpriseOverview> => (await apiClient.get<EnterpriseOverview>('/enterprise/overview')).data
}

const enterpriseErrorCodes = new Set([
  'ENTERPRISE_NOT_FOUND', 'ENTERPRISE_DEPARTMENT_NOT_FOUND', 'ENTERPRISE_MEMBER_NOT_FOUND',
  'ENTERPRISE_NAME_EXISTS', 'ENTERPRISE_DEPARTMENT_NAME_EXISTS', 'ENTERPRISE_MEMBER_EXISTS',
  'ENTERPRISE_MEMBER_ADMIN_FORBIDDEN', 'ENTERPRISE_MANAGER_REQUIRES_DEPARTMENT', 'ENTERPRISE_LAST_OWNER',
  'ENTERPRISE_DEPARTMENT_NOT_EMPTY', 'ENTERPRISE_NOT_EMPTY', 'ENTERPRISE_NOT_MEMBER', 'ENTERPRISE_PERMISSION_DENIED',
  'ENTERPRISE_DISABLED', 'ENTERPRISE_MEMBER_DISABLED', 'ENTERPRISE_BALANCE_INSUFFICIENT', 'ENTERPRISE_QUOTA_EXCEEDED',
  'DEPARTMENT_QUOTA_EXCEEDED', 'MEMBER_QUOTA_EXCEEDED', 'ENTERPRISE_QUOTA_EXCEEDS_PARENT',
  'ENTERPRISE_GROUP_OUT_OF_SCOPE', 'ENTERPRISE_GROUP_NOT_ALLOWED', 'ENTERPRISE_MEMBER_WALLET_DISABLED',
  'ENTERPRISE_FEATURE_UNSUPPORTED', 'ENTERPRISE_INVALID_SCOPE'
])
export function enterpriseErrorMessage(error: unknown, t: (key: string) => string): string {
  const value = error as { code?: string; reason?: string; response?: { data?: { code?: string; reason?: string } } } | null
  const code = [value?.reason, value?.code, value?.response?.data?.reason, value?.response?.data?.code]
    .find(item => typeof item === 'string' && enterpriseErrorCodes.has(item))
  return t(code ? `enterprise.errors.${code}` : 'enterprise.errors.generic')
}
export default enterpriseAPI
