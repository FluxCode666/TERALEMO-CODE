import { apiClient } from './client'
import type { FetchOptions } from '@/types'
import type {
  EnterpriseOverview, EnterpriseDepartment, EnterpriseMember, EnterpriseQuota, EnterpriseSettings,
  EnterpriseGroup, EnterpriseGroupWhitelist, EnterpriseUsageLog, EnterpriseUsageStats,
  EnterpriseUsageTrend, EnterpriseUsageModel, EnterpriseMemberUsage, EnterpriseDepartmentUsage,
  EnterpriseAuditLog, EnterpriseBalanceLog, EnterprisePage, EnterpriseQuery, EnterpriseScope,
  QuotaLimits, QuotaWindow, CreateEnterpriseMemberRequest, CreateEnterpriseAccountMemberRequest, UpdateEnterpriseMemberRequest
} from '@/types/enterprise'

/** Resources available on both enterprise and administrator routes. */
function createEnterpriseResourceAPI(base: string) {
  const get = <T>(path: string, params?: EnterpriseQuery, options?: FetchOptions): Promise<T> => readEnterpriseResource<T>(base, path, params, options)
  const post = async <T>(path: string, body: unknown): Promise<T> => (await apiClient.post<T>(`${base}${path}`, body)).data
  const put = async <T>(path: string, body: unknown): Promise<T> => (await apiClient.put<T>(`${base}${path}`, body)).data
  const remove = async (path: string): Promise<void> => { await apiClient.delete(`${base}${path}`) }
  return {
    departments: (options?: FetchOptions) => get<EnterpriseDepartment[]>('/departments', undefined, options),
    createDepartment: (body: { name: string; sort_order?: number }) => post<EnterpriseDepartment>('/departments', body),
    updateDepartment: (id: number, body: { name?: string; sort_order?: number }) => put<EnterpriseDepartment>(`/departments/${id}`, body),
    deleteDepartment: (id: number) => remove(`/departments/${id}`),
    members: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterprisePage<EnterpriseMember>>('/members', params, options),
    createMember: (body: CreateEnterpriseMemberRequest) => post<EnterpriseMember>('/members', body),
    updateMember: (id: number, body: UpdateEnterpriseMemberRequest) => put<EnterpriseMember>(`/members/${id}`, body),
    deleteMember: (id: number) => remove(`/members/${id}`),
    quotas: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterpriseQuota[]>('/quotas', params, options),
    updateQuota: (scope: EnterpriseScope, id: number, body: QuotaLimits) => put<EnterpriseQuota>(`/quotas/${scope}/${id}`, body),
    resetQuota: (scope: EnterpriseScope, id: number, windows: QuotaWindow[]) => post<EnterpriseQuota>(`/quotas/${scope}/${id}/reset`, { windows }),
    groups: (options?: FetchOptions) => get<EnterpriseGroup[]>('/groups', undefined, options),
    groupWhitelists: (options?: FetchOptions) => get<EnterpriseGroupWhitelist[]>('/group-whitelists', undefined, options),
    updateGroupWhitelist: (scope: EnterpriseScope, id: number, group_ids: number[]) => put<EnterpriseGroupWhitelist>(`/group-whitelists/${scope}/${id}`, { group_ids }),
    settings: (options?: FetchOptions) => get<EnterpriseSettings>('/settings', undefined, options),
    updateSettings: (body: EnterpriseSettings) => put<EnterpriseSettings>('/settings', body),
    usage: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterprisePage<EnterpriseUsageLog>>('/usage', params, options),
    usageStats: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterpriseUsageStats>('/usage/stats', params, options),
    usageTrend: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterpriseUsageTrend[]>('/usage/trend', params, options),
    usageModels: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterpriseUsageModel[]>('/usage/models', params, options),
    usageMembers: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterpriseMemberUsage[]>('/usage/members', params, options),
    usageDepartments: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterpriseDepartmentUsage[]>('/usage/departments', params, options),
    auditLogs: (params?: EnterpriseQuery, options?: FetchOptions) => get<EnterprisePage<EnterpriseAuditLog>>('/audit-logs', params, options)
  }
}
async function readEnterpriseResource<T>(base: string, path: string, params?: EnterpriseQuery, options?: FetchOptions): Promise<T> {
  return (await apiClient.get<T>(`${base}${path}`, { params, ...options })).data
}

/** Administrator-only resources stay out of the enterprise-side client. */
export function createEnterpriseAPI(base: string) {
  return {
    ...createEnterpriseResourceAPI(base),
    allowedGroups: (options?: FetchOptions) => readEnterpriseResource<EnterpriseGroup[]>(base, '/allowed-groups', undefined, options),
    updateAllowedGroups: async (group_ids: number[]): Promise<EnterpriseGroup[]> => (await apiClient.put<EnterpriseGroup[]>(`${base}/allowed-groups`, { group_ids })).data,
    balanceLogs: (params?: EnterpriseQuery, options?: FetchOptions) => readEnterpriseResource<EnterprisePage<EnterpriseBalanceLog>>(base, '/balance-logs', params, options)
  }
}

const enterpriseResources = createEnterpriseResourceAPI('/enterprise')
export const enterpriseAPI = {
  ...enterpriseResources,
  createMember: (body: CreateEnterpriseAccountMemberRequest) => enterpriseResources.createMember(body),
  overview: async (options?: FetchOptions): Promise<EnterpriseOverview> => (await (options
    ? apiClient.get<EnterpriseOverview>('/enterprise/overview', options)
    : apiClient.get<EnterpriseOverview>('/enterprise/overview'))).data
}
export type EnterpriseAdminResourceAPI = ReturnType<typeof createEnterpriseAPI>
export type EnterpriseResourceAPI = EnterpriseAdminResourceAPI | typeof enterpriseAPI

const enterpriseErrorCodes = new Set([
  'ENTERPRISE_NOT_FOUND', 'ENTERPRISE_DEPARTMENT_NOT_FOUND', 'ENTERPRISE_MEMBER_NOT_FOUND',
  'ENTERPRISE_NAME_EXISTS', 'ENTERPRISE_DEPARTMENT_NAME_EXISTS', 'ENTERPRISE_MEMBER_EXISTS',
  'ENTERPRISE_MEMBER_ADMIN_FORBIDDEN', 'ENTERPRISE_MANAGER_REQUIRES_DEPARTMENT', 'ENTERPRISE_LAST_OWNER',
  'ENTERPRISE_DEPARTMENT_NOT_EMPTY', 'ENTERPRISE_NOT_EMPTY', 'ENTERPRISE_NOT_MEMBER', 'ENTERPRISE_PERMISSION_DENIED',
  'ENTERPRISE_DISABLED', 'ENTERPRISE_MEMBER_DISABLED', 'ENTERPRISE_BALANCE_INSUFFICIENT', 'ENTERPRISE_QUOTA_EXCEEDED',
  'DEPARTMENT_QUOTA_EXCEEDED', 'MEMBER_QUOTA_EXCEEDED', 'ENTERPRISE_QUOTA_EXCEEDS_PARENT',
  'ENTERPRISE_GROUP_OUT_OF_SCOPE', 'ENTERPRISE_GROUP_NOT_ALLOWED', 'ENTERPRISE_MEMBER_WALLET_DISABLED',
  'ENTERPRISE_FEATURE_UNSUPPORTED', 'ENTERPRISE_INVALID_SCOPE', 'INVALID_DATE_RANGE'
])
export function enterpriseErrorMessage(error: unknown, t: (key: string) => string): string {
  const value = error as { code?: string; reason?: string; response?: { data?: { code?: string; reason?: string } } } | null
  const code = [value?.reason, value?.code, value?.response?.data?.reason, value?.response?.data?.code]
    .find(item => typeof item === 'string' && enterpriseErrorCodes.has(item))
  return t(code ? `enterprise.errors.${code}` : 'enterprise.errors.generic')
}
export default enterpriseAPI
