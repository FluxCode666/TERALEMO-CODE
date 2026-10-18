import type { BasePaginationResponse, UsageLog } from '@/types'

export type EnterpriseRole = 'owner' | 'manager' | 'member'
export type EnterpriseStatus = 'active' | 'disabled'
export type EnterpriseScope = 'enterprise' | 'department' | 'member'
export type QuotaWindow = 'daily' | 'weekly' | 'monthly' | 'total'
export interface EnterpriseSettings {
  balance_visible_to_manager: boolean
  balance_visible_to_member: boolean
  member_quota_visible_to_member: boolean
  member_quota_visible_to_manager: boolean
  department_quota_visible_to_manager: boolean
  manager_can_manage_members: boolean
  manager_can_edit_member_quota: boolean
  manager_can_edit_member_groups: boolean
}
export const defaultEnterpriseSettings: EnterpriseSettings = {
  balance_visible_to_manager: true,
  balance_visible_to_member: false,
  member_quota_visible_to_member: true,
  member_quota_visible_to_manager: true,
  department_quota_visible_to_manager: true,
  manager_can_manage_members: false,
  manager_can_edit_member_quota: false,
  manager_can_edit_member_groups: false
}
export interface EnterpriseSummary {
  id: number
  name: string
  role: EnterpriseRole
}
export interface EnterpriseIdentity extends EnterpriseSummary {
  department_id: number | null
  department_name: string | null
  member_status: EnterpriseStatus
  enterprise_status: EnterpriseStatus
  balance_visible: boolean
  balance: number | null
  frozen_balance: number | null
}
export type QuotaLimits = Record<`${QuotaWindow}_limit_usd`, number | null>
export interface EnterpriseQuota extends QuotaLimits {
  scope_type: EnterpriseScope
  scope_id: number
  daily_usage_usd: number | null
  weekly_usage_usd: number | null
  monthly_usage_usd: number | null
  total_usage_usd: number | null
  daily_resets_at?: string | null
  weekly_resets_at?: string | null
  monthly_resets_at?: string | null
}
export interface Enterprise {
  id: number
  name: string
  status: EnterpriseStatus
  balance: number
  frozen_balance: number
  total_recharged: number
  low_balance_threshold: number | null
  contact_name: string
  contact_email: string
  contact_phone: string
  notes: string
  settings: EnterpriseSettings
  member_count: number
  department_count: number
  active_owner_count?: number
  quota?: EnterpriseQuota | null
  created_at: string
  updated_at: string
}
export interface EnterpriseDepartment {
  id: number
  enterprise_id: number
  name: string
  sort_order: number
  member_count: number
  created_at: string
}
export interface EnterpriseMember {
  id: number
  enterprise_id: number
  user_id: number
  email: string
  username: string
  department_id: number | null
  department_name: string | null
  role: EnterpriseRole
  status: EnterpriseStatus
  title: string
  joined_at: string
  user_status: EnterpriseStatus
}
export interface EnterprisePermissions {
  manage_departments: boolean
  manage_members: boolean
  create_members: boolean
  edit_member_quota: boolean
  edit_member_groups: boolean
  edit_quotas: boolean
  edit_settings: boolean
  view_members: boolean
  view_audit_logs: boolean
  view_department_usage: boolean
}
export interface EnterpriseOverview {
  enterprise: Pick<Enterprise, 'id' | 'name' | 'status'>
  me: { member_id: number; user_id: number; role: EnterpriseRole; status: EnterpriseStatus; department_id: number | null; department_name: string | null; title: string }
  balance_visible: boolean
  balance: number | null
  frozen_balance: number | null
  enterprise_quota: EnterpriseQuota | null
  department_quota_visible: boolean
  department_quota: EnterpriseQuota | null
  member_quota_visible: boolean
  member_quota: EnterpriseQuota | null
  settings: EnterpriseSettings | null
  permissions: EnterprisePermissions
  stats: { today_cost: number; month_cost: number }
}
export interface EnterpriseGroup {
  id: number
  name: string
  platform: string
  is_exclusive: boolean
  subscription_type: string
  status: string
  rate_multiplier?: number
}
export interface EnterpriseGroupWhitelist { scope_type: EnterpriseScope; scope_id: number; group_ids: number[] }
export interface EnterpriseBalanceLog {
  id: number; type: 'add' | 'subtract'; amount: number; balance_before: number; balance_after: number
  operator_user_id: number | null; operator_email: string; notes: string; created_at: string
}
export interface EnterpriseAuditLog {
  id: number; actor_user_id: number; actor_email: string; actor_role: 'admin' | EnterpriseRole
  action: string; target_type: string; target_id: number | null; detail: Record<string, unknown>; created_at: string
}
// New fields remain optional for compatibility with older enterprise backends.
export interface EnterpriseUsageLog extends Partial<Omit<UsageLog, 'user' | 'api_key' | 'group' | 'subscription'>> {
  id: number
  model: string
  created_at: string
  user?: { id: number; username: string; email: string } | null
  department_id?: number | null
  department_name?: string | null
  api_key?: { id: number; name: string } | null
  group?: { id: number; name: string; platform?: string } | null
  media_type?: string | null
}
// Aggregates follow the existing usage statistics DTOs; D6 specifies ranking fields explicitly.
export interface EnterpriseUsageStats {
  total_requests: number; total_tokens: number; total_actual_cost: number
  total_cost?: number; total_input_tokens?: number; total_output_tokens?: number
}
export interface EnterpriseUsageTrend { date: string; requests: number; actual_cost: number; total_tokens: number }
export interface EnterpriseUsageModel { model: string; requests: number; total_tokens: number; actual_cost: number }
export interface EnterpriseMemberUsage { user_id: number; username: string; email: string; department_id: number | null; department_name: string | null; requests: number; total_tokens: number; actual_cost: number }
export interface EnterpriseDepartmentUsage { department_id: number | null; department_name: string | null; requests: number; total_tokens: number; actual_cost: number }
export interface EnterpriseQuery { member_id?: number; page?: number; page_size?: number; search?: string; status?: string; role?: EnterpriseRole; department_id?: number; user_id?: number; api_key_id?: number; model?: string; start_date?: string; end_date?: string; granularity?: 'day' | 'hour'; action?: string; scope_type?: EnterpriseScope; sort_by?: 'created_at' | 'model'; sort_order?: 'asc' | 'desc' }
export type EnterpriseOwnerInput = { user_id: number; email?: never; password?: never; username?: never } | { user_id?: never; email: string; password: string; username?: string }
export type CreateEnterpriseRequest = Pick<Enterprise, 'name'> & Partial<Pick<Enterprise, 'status' | 'notes' | 'contact_name' | 'contact_email' | 'contact_phone' | 'low_balance_threshold' | 'settings'>> & { owner?: EnterpriseOwnerInput }
export type UpdateEnterpriseRequest = Omit<Partial<CreateEnterpriseRequest>, 'owner'>
export type CreateEnterpriseMemberRequest = EnterpriseOwnerInput & { department_id?: number | null; role: EnterpriseRole; title?: string }
export type CreateEnterpriseAccountMemberRequest = { user_id?: never; email: string; password: string; username?: string; department_id?: number | null; role: EnterpriseRole; title?: string }
export interface UpdateEnterpriseMemberRequest { department_id?: number; role?: EnterpriseRole; status?: EnterpriseStatus; title?: string }
export type EnterprisePage<T> = BasePaginationResponse<T>
