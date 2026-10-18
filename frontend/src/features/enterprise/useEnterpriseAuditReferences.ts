import { onScopeDispose, ref, watch } from 'vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { AuditReferenceCatalog } from './auditReferences'
import { loadMemberOptions } from './memberOptions'

/** Optional labels must never block the audit log or bypass member visibility. */
export function useEnterpriseAuditReferences(source: () => { api: EnterpriseResourceAPI; enabled: boolean; canViewMembers: boolean; refreshKey?: number }) {
  const catalog = ref<AuditReferenceCatalog>({})
  let pending: AbortController | undefined
  watch(source, async ({ api, enabled, canViewMembers }) => {
    pending?.abort()
    const current = new AbortController()
    pending = current
    catalog.value = {}
    if (!enabled) return
    const results = await Promise.allSettled([
      Promise.resolve().then(() => api.departments({ signal: current.signal })),
      Promise.resolve().then(() => canViewMembers ? loadMemberOptions(api, current.signal) : []),
      Promise.resolve().then(() => 'allowedGroups' in api ? api.allowedGroups({ signal: current.signal }) : api.groups({ signal: current.signal }))
    ])
    if (current.signal.aborted || pending !== current) return
    catalog.value = {
      departments: results[0].status === 'fulfilled' ? results[0].value : [],
      members: results[1].status === 'fulfilled' ? results[1].value : [],
      groups: results[2].status === 'fulfilled' ? results[2].value : []
    }
  }, { immediate: true })
  onScopeDispose(() => pending?.abort())
  return catalog
}
