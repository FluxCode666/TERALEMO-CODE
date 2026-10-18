import { defineStore } from 'pinia'
import { computed, ref, shallowRef, watch } from 'vue'
import { enterpriseAPI } from '@/api/enterprise'
import { canAccessEnterprise, getEnterpriseTabs } from '@/features/enterprise/navigation'
import type { EnterpriseOverview } from '@/types/enterprise'
import { useAuthStore } from './auth'

/** Session-scoped, shared by navigation guards, the sidebar and the workspace. */
export const useEnterpriseStore = defineStore('enterprise', () => {
  const auth = useAuthStore()
  const overview = shallowRef<EnterpriseOverview | null>(null)
  const loading = ref(false)
  const error = shallowRef<unknown>(null)
  const stale = ref(true)
  const canAccess = computed(() => canAccessEnterprise(auth.user?.enterprise, auth.isAdmin))
  const tabs = computed(() => canAccess.value ? getEnterpriseTabs(overview.value) : [])
  let requested = false
  let generation = 0
  let controller: AbortController | undefined
  let pending: Promise<EnterpriseOverview | null> | undefined
  const sessionKey = () => canAccess.value
    ? `${auth.user?.id}:${auth.user?.role}:${auth.user?.enterprise?.id}:${auth.user?.enterprise?.role}`
    : ''
  let currentSession = sessionKey()

  function invalidate(reload = true) {
    generation++
    controller?.abort()
    controller = undefined
    pending = undefined
    stale.value = true
    loading.value = false
    error.value = null
    const nextSession = sessionKey()
    if (nextSession !== currentSession || !canAccess.value) overview.value = null
    currentSession = nextSession
    if (reload && requested && canAccess.value) void ensureLoaded()
  }

  async function ensureLoaded(): Promise<EnterpriseOverview | null> {
    requested = true
    if (!canAccess.value) return null
    if (!stale.value && overview.value) return overview.value
    if (pending) return pending
    const version = generation
    const request = new AbortController()
    controller = request
    loading.value = true
    error.value = null
    pending = (async () => {
      try {
        const result = await enterpriseAPI.overview({ signal: request.signal })
        if (version !== generation) return ensureLoaded()
        overview.value = result
        stale.value = false
        return result
      } catch (reason) {
        if (version !== generation) return ensureLoaded()
        error.value = reason
        return null
      } finally {
        if (version === generation) {
          loading.value = false
          pending = undefined
        }
      }
    })()
    return pending
  }

  // User replacement also covers login and refreshUser; deep tracking covers
  // enterprise identity updates. Token rotation alone does not discard the cache.
  watch(() => auth.user, () => invalidate(), { deep: true, flush: 'sync' })

  return { overview, loading, error, stale, canAccess, tabs, ensureLoaded, invalidate }
})
