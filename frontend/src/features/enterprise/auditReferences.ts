import type { EnterpriseAuditLog, EnterpriseDepartment, EnterpriseGroup, EnterpriseMember } from '@/types/enterprise'

export type AuditReferenceType = 'enterprise' | 'department' | 'member' | 'user' | 'group'
export interface AuditReferenceCatalog {
  enterprise?: { id: number; name: string }
  departments?: Pick<EnterpriseDepartment, 'id' | 'name'>[]
  members?: Pick<EnterpriseMember, 'id' | 'user_id' | 'username' | 'email'>[]
  groups?: Pick<EnterpriseGroup, 'id' | 'name'>[]
}
export function auditObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
function identifier(value: unknown): number | null {
  if (typeof value !== 'number' && (typeof value !== 'string' || !/^\d+$/.test(value))) return null
  const id = Number(value)
  return Number.isSafeInteger(id) && id > 0 ? id : null
}
export function auditReferenceIds(value: unknown): number[] | null {
  const values = Array.isArray(value) ? value : [value]
  const ids = values.map(identifier)
  return ids.every((id): id is number => id !== null) ? ids : null
}
export function auditReferenceType(key: string): AuditReferenceType | null {
  return (key.split('.').at(-1)?.match(/(?:^|_)(enterprise|department|member|user|group)_ids?$/)?.[1] as AuditReferenceType) ?? null
}

/** Audit payloads are JSON: object key order is irrelevant; ordinary arrays keep order. */
export function auditValuesEqual(field: string, before: unknown, after: unknown): boolean {
  if (Object.is(before, after)) return true
  if (Array.isArray(before) && Array.isArray(after)) {
    if (auditReferenceType(field)) {
      const left = auditReferenceIds(before), right = auditReferenceIds(after)
      if (left && right) {
        const ids = new Set(left), others = new Set(right)
        return ids.size === others.size && [...ids].every(id => others.has(id))
      }
    }
    return before.length === after.length && before.every((value, index) => auditValuesEqual('', value, after[index]))
  }
  if (auditObject(before) && auditObject(after)) {
    const keys = Object.keys(before)
    return keys.length === Object.keys(after).length && keys.every(key => Object.prototype.hasOwnProperty.call(after, key) && auditValuesEqual(key, before[key], after[key]))
  }
  return false
}

// Historical names remain useful for deleted entities. Only use a descriptive
// field when its object identifies the same entity (or is the target snapshot).
function historicalName(value: unknown, type: AuditReferenceType, id: number, implicit = false, depth = 0): string | undefined {
  if (!auditObject(value) || depth > 5) return
  const ownId = value[`${type}_id`]
  if (identifier(ownId) === id || (implicit && (ownId === undefined || identifier(ownId) === id))) {
    const keys = type === 'member' || type === 'user' ? [`${type}_name`, 'username', 'email'] : [`${type}_name`, 'name']
    for (const key of keys) if (typeof value[key] === 'string' && value[key].trim()) return value[key]
  }
  // Prefer the latest snapshot when both sides contain names.
  const snapshots = ['after', 'new', 'to', 'new_value', 'before', 'old', 'from', 'old_value']
  for (const key of new Set([...snapshots, ...Object.keys(value)])) {
    const child = value[key]
    const entityMatches = auditObject(child) && identifier(child.id) === id && key === type
    const targetSnapshot = implicit && (snapshots.includes(key) || key === 'target' || (type === 'member' && key === 'user'))
    const name = historicalName(child, type, id, entityMatches || targetSnapshot, depth + 1)
    if (name) return name
  }
}
export function auditReferenceName(type: AuditReferenceType, id: number, catalog: AuditReferenceCatalog, log?: EnterpriseAuditLog): string | undefined {
  let name: string | undefined
  if (type === 'enterprise' && catalog.enterprise?.id === id) name = catalog.enterprise.name
  if (type === 'department') name = catalog.departments?.find(item => item.id === id)?.name
  if (type === 'group') name = catalog.groups?.find(item => item.id === id)?.name
  if (type === 'member' || type === 'user') {
    const member = catalog.members?.find(item => (type === 'member' ? item.id : item.user_id) === id)
    name = member?.username || member?.email
  }
  if (name) return name
  if (!log) return
  const target = log.target_type === type && log.target_id === id
  if (target && typeof log.detail?.target_name === 'string' && log.detail.target_name.trim()) return log.detail.target_name
  return historicalName(log.detail, type, id, target)
}

export function auditReferenceLabel(type: AuditReferenceType, id: number | null, catalog: AuditReferenceCatalog, t: (key: string) => string, log?: EnterpriseAuditLog): string {
  const fallback = t(`enterprise.auditDetails.references.${type}`)
  return (id !== null && auditReferenceName(type, id, catalog, log)) || `${fallback} #${id ?? '—'}`
}
export function auditTargetLabel(log: EnterpriseAuditLog, catalog: AuditReferenceCatalog, t: (key: string) => string): string {
  if (['enterprise', 'department', 'member', 'user', 'group'].includes(log.target_type)) return auditReferenceLabel(log.target_type as AuditReferenceType, log.target_id, catalog, t, log)
  return `${t('enterprise.target')} #${log.target_id ?? '—'}`
}
