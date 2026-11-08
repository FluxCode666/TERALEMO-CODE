/**
 * F5：可选 API Key 列表 + 上次选择记忆。
 *
 * - 只取 `status=active` 的 Key，并过滤掉没有分组的 Key（创作页必须有分组才能判定能力）。
 * - 上次选择写在 `creationStudio:lastKeyId:v1:<user_id>`；该 Key 已失效时回落到第一个。
 */
import { computed, ref } from 'vue'
import { keysAPI } from '@/api/keys'
import { useAuthStore } from '@/stores/auth'
import type { ApiKey } from '@/types'

const LAST_KEY_PREFIX = 'creationStudio:lastKeyId:v1'

function lastKeyStorageKey(userId: number): string {
  return `${LAST_KEY_PREFIX}:${userId}`
}

function readRememberedKeyId(userId: number): number | null {
  if (!userId) return null
  try {
    const raw = localStorage.getItem(lastKeyStorageKey(userId))
    if (!raw) return null
    const parsed = Number.parseInt(raw, 10)
    return Number.isFinite(parsed) && parsed > 0 ? parsed : null
  } catch {
    return null
  }
}

function writeRememberedKeyId(userId: number, keyId: number | null): void {
  if (!userId) return
  try {
    if (keyId === null) localStorage.removeItem(lastKeyStorageKey(userId))
    else localStorage.setItem(lastKeyStorageKey(userId), String(keyId))
  } catch {
    // localStorage 不可用时只影响记忆，不影响主流程。
  }
}

/** 只有绑定了分组的 Key 才能进创作页（分组决定可用模式与模型）。 */
function hasGroup(key: ApiKey): boolean {
  return key.group_id !== null && key.group_id !== undefined
}

export function useCreationKeys() {
  const authStore = useAuthStore()

  const keys = ref<ApiKey[]>([])
  const loading = ref(false)
  const failed = ref(false)
  const selectedId = ref<number | null>(null)

  const userId = computed(() => authStore.user?.id ?? 0)
  const selectedKey = computed(() => keys.value.find(key => key.id === selectedId.value) ?? null)
  const hasKeys = computed(() => keys.value.length > 0)

  function remember(keyId: number | null) {
    writeRememberedKeyId(userId.value, keyId)
  }

  /** 选中的 Key 仍存在就保留，否则回落到第一个。 */
  function reconcileSelection(preferred?: number | null) {
    const available = keys.value
    if (!available.length) {
      selectedId.value = null
      remember(null)
      return
    }
    const wanted = preferred ?? selectedId.value
    const match = wanted !== null ? available.find(key => key.id === wanted) : undefined
    const next = match?.id ?? available[0].id
    if (next !== selectedId.value) {
      selectedId.value = next
      remember(next)
    }
  }

  async function load(signal?: AbortSignal) {
    loading.value = true
    failed.value = false
    try {
      const page = await keysAPI.list(1, 100, { status: 'active' }, { signal })
      keys.value = (page.items ?? []).filter(hasGroup)
      reconcileSelection(readRememberedKeyId(userId.value) ?? selectedId.value)
    } catch (error) {
      if (signal?.aborted) return
      keys.value = []
      selectedId.value = null
      failed.value = true
    } finally {
      loading.value = false
    }
  }

  function select(keyId: number | null) {
    if (keyId === null) {
      selectedId.value = null
      remember(null)
      return
    }
    if (!keys.value.some(key => key.id === keyId)) return
    selectedId.value = keyId
    remember(keyId)
  }

  return { keys, loading, failed, selectedId, selectedKey, hasKeys, load, select, reconcileSelection }
}
