import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview } from '@/types/enterprise'
import { departmentSubtreeIds } from './departmentTree'

export function canResetMemberPassword(member: EnterpriseMember, context: { admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; departments: EnterpriseDepartment[] }): boolean {
  if (context.readonly || member.password_resettable !== true) return false
  if (context.admin) return true
  const me = context.overview?.me
  if (!me || member.id === me.member_id || member.user_id === me.user_id) return false
  if (me.role === 'owner') return member.role !== 'owner'
  return me.role === 'manager' && !!context.overview?.permissions.manage_members
    && context.overview.settings?.manager_can_manage_members !== false && member.role === 'member'
    && departmentSubtreeIds(context.departments, me.department_id).has(member.department_id ?? 0)
}
