import { describe, expect, it } from 'vitest'
import { auditReferenceIds, auditReferenceLabel, auditReferenceType, auditTargetLabel, type AuditReferenceCatalog } from '../auditReferences'
import type { EnterpriseAuditLog } from '@/types/enterprise'
const catalog: AuditReferenceCatalog = {
  enterprise: { id: 1, name: 'Acme' }, departments: [{ id: 3, name: '研发部' }],
  members: [{ id: 7, user_id: 70, username: 'Alice', email: 'alice@example.com' }, { id: 70, user_id: 700, username: '', email: 'bob@example.com' }],
  groups: [{ id: 2, name: 'Claude' }]
}
const t = (key: string) => ({ 'enterprise.auditDetails.references.enterprise': '企业', 'enterprise.auditDetails.references.department': '部门', 'enterprise.auditDetails.references.member': '成员', 'enterprise.auditDetails.references.user': '用户', 'enterprise.auditDetails.references.group': '分组', 'enterprise.target': '目标' }[key] ?? key)
const log = (target_type: string, target_id: number | null, detail = {}): EnterpriseAuditLog => ({ id: 1, target_type, target_id, detail, actor_user_id: 1, actor_email: '', actor_role: 'owner', action: '', created_at: '' })
describe('审计名称解析', () => {
  it.each([['enterprise', 1, 'Acme'], ['department', 3, '研发部'], ['member', 7, 'Alice'], ['member', 70, 'bob@example.com'], ['user', 70, 'Alice'], ['group', 2, 'Claude']])('映射 %s #%i 并区分成员与用户 ID', (type, id, expected) => {
    expect(auditTargetLabel(log(String(type), Number(id)), catalog, t)).toBe(expected)
  })
  it('从删除记录的 detail 和前后快照取名称，不混用其他 ID 的名称', () => {
    expect(auditTargetLabel(log('department', 4, { before: { id: 4, name: '已删部门' }, after: null }), catalog, t)).toBe('已删部门')
    expect(auditTargetLabel(log('member', 9, { email: 'removed@example.com' }), catalog, t)).toBe('removed@example.com')
    expect(auditTargetLabel(log('member', 9, { member: { id: 9, username: '已移出成员' } }), catalog, t)).toBe('已移出成员')
    expect(auditTargetLabel(log('member', 9, { user: { id: 90, username: '历史用户' } }), catalog, t)).toBe('历史用户')
    expect(auditTargetLabel(log('department', 4, { target: { id: 4, name: '历史目标' } }), catalog, t)).toBe('历史目标')
    expect(auditTargetLabel(log('department', 4, { department_id: 5, department_name: '另一部门' }), catalog, t)).toBe('部门 #4')
    expect(auditReferenceLabel('department', 5, catalog, t, log('member', 9, { department_id: 5, department_name: '已删部门' }))).toBe('已删部门')
  })
  it('取不到名称、目标为空或类型未知时安全回退', () => {
    expect(auditTargetLabel(log('enterprise', 99), catalog, t)).toBe('企业 #99')
    expect(auditTargetLabel(log('member', null), catalog, t)).toBe('成员 #—')
    expect(auditTargetLabel(log('future', 12), catalog, t)).toBe('目标 #12')
    expect(auditReferenceLabel('group', 9, catalog, t)).toBe('分组 #9')
  })
  it('识别嵌套与带前缀的 ID 字段，未知数组结构不强行映射', () => {
    expect(auditReferenceType('policy.allowed_group_ids')).toBe('group')
    expect(auditReferenceType('member_ids')).toBe('member')
    expect(auditReferenceType('department_id')).toBe('department')
    expect(auditReferenceType('ids')).toBeNull()
    expect(auditReferenceIds([2, '3'])).toEqual([2, 3])
    expect(auditReferenceIds([{ id: 2 }])).toBeNull()
    expect(auditReferenceIds(null)).toBeNull()
  })
})
