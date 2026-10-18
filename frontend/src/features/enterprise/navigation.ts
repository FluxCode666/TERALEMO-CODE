import type { EnterpriseIdentity, EnterpriseOverview } from '@/types/enterprise'

export const enterpriseSections = ['overview', 'organization', 'quotas', 'groups', 'usage', 'audit', 'settings'] as const
export type EnterpriseSection = typeof enterpriseSections[number]
export type EnterpriseTab = EnterpriseSection | 'balanceLogs'

export function canAccessEnterprise(identity: Pick<EnterpriseIdentity, 'role'> | null | undefined, admin = false): boolean {
  return !!identity && (admin || identity.role === 'owner' || identity.role === 'manager')
}

/** Keep user navigation and administrator tabs aligned with server permissions. */
export function getEnterpriseTabs(overview?: EnterpriseOverview | null, admin = false): EnterpriseTab[] {
  const permissions = overview?.permissions
  const tabs: EnterpriseTab[] = ['overview', 'organization']
  if (admin || overview?.enterprise_quota ||
    (overview?.department_quota_visible && overview.department_quota) ||
    (overview?.member_quota_visible && overview.member_quota) ||
    permissions?.edit_quotas || permissions?.edit_member_quota) tabs.push('quotas')
  tabs.push('groups', 'usage')
  if (admin) tabs.push('balanceLogs')
  if (admin || permissions?.view_audit_logs) tabs.push('audit')
  if (admin || permissions?.edit_settings) tabs.push('settings')
  return tabs
}

export function enterpriseSectionPath(section: EnterpriseTab): string {
  return `/enterprise/${section}`
}
