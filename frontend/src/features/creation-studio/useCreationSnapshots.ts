/**
 * F12：复用参数的本地快照（契约 §8）。
 *
 * - key：`creationStudio:snapshots:v1:<user_id>`，结构 `{ v: 1, items: [...] }`，按 `created_at` 倒序。
 * - 上限 100 条，7 天过期，读和写都清理过期项。
 * - 写入失败（QuotaExceededError 等）：删掉最旧的一半后重试一次，仍失败就放弃写入，只告警，不影响提交。
 * - 读失败 / JSON 损坏 / `v` 不为 1：当作空，并覆盖写回空结构。
 */
import { computed, ref, watch, type Ref } from 'vue'
import type { CreationSnapshot } from './types'

const STORAGE_VERSION = 1
const MAX_ITEMS = 100
const TTL_SECONDS = 7 * 24 * 60 * 60
const SNAPSHOT_PREFIX = 'creationStudio:snapshots:v1'

interface SnapshotEnvelope {
  v: number
  items: CreationSnapshot[]
}

function storageKey(userId: number): string {
  return `${SNAPSHOT_PREFIX}:${userId}`
}

function nowSeconds(): number {
  return Math.floor(Date.now() / 1000)
}

function isSnapshot(value: unknown): value is CreationSnapshot {
  if (typeof value !== 'object' || value === null) return false
  const record = value as Record<string, unknown>
  return (
    typeof record.catalog_id === 'string' &&
    typeof record.task_id === 'string' &&
    typeof record.created_at === 'number' &&
    typeof record.prompt === 'string' &&
    typeof record.model === 'string'
  )
}

function prune(items: CreationSnapshot[]): CreationSnapshot[] {
  const cutoff = nowSeconds() - TTL_SECONDS
  return items
    .filter(item => item.created_at >= cutoff)
    .sort((left, right) => right.created_at - left.created_at)
    .slice(0, MAX_ITEMS)
}

export function useCreationSnapshots(userId: Ref<number>) {
  const items = ref<CreationSnapshot[]>([])

  const key = computed(() => storageKey(userId.value))

  function writeEmpty() {
    try {
      localStorage.setItem(key.value, JSON.stringify({ v: STORAGE_VERSION, items: [] }))
    } catch {
      // 存储不可用时忽略。
    }
  }

  function read(): CreationSnapshot[] {
    if (!userId.value) return []
    let raw: string | null = null
    try {
      raw = localStorage.getItem(key.value)
    } catch {
      // 读异常也按契约 §8.1 覆盖写回空结构（R1-16）；写失败不影响提交。
      writeEmpty()
      return []
    }
    if (!raw) return []
    try {
      const parsed = JSON.parse(raw) as Partial<SnapshotEnvelope>
      if (parsed.v !== STORAGE_VERSION || !Array.isArray(parsed.items)) {
        writeEmpty()
        return []
      }
      const valid = parsed.items.filter(isSnapshot)
      const pruned = prune(valid)
      if (pruned.length !== valid.length) persist(pruned)
      return pruned
    } catch {
      writeEmpty()
      return []
    }
  }

  function persist(next: CreationSnapshot[]) {
    if (!userId.value) return
    const payload: SnapshotEnvelope = { v: STORAGE_VERSION, items: next }
    try {
      localStorage.setItem(key.value, JSON.stringify(payload))
      return
    } catch {
      // 配额溢出：删掉最旧的一半后重试一次。
      const trimmed = next.slice(0, Math.max(0, Math.floor(next.length / 2)))
      try {
        localStorage.setItem(key.value, JSON.stringify({ v: STORAGE_VERSION, items: trimmed }))
        items.value = trimmed
      } catch {
        console.warn('[creation-studio] snapshot storage unavailable, skipping write')
      }
    }
  }

  function load() {
    items.value = read()
  }

  /** 提交成功后写入一条。 */
  function add(snapshot: CreationSnapshot) {
    const next = prune([snapshot, ...items.value])
    items.value = next
    persist(next)
  }

  /** 先按 `catalog_id`，找不到再按 `task_id`（契约 §8.1）。 */
  function find(catalogId: string, taskId: string): CreationSnapshot | undefined {
    if (catalogId) {
      const byCatalog = items.value.find(item => item.catalog_id === catalogId)
      if (byCatalog) return byCatalog
    }
    if (!taskId) return undefined
    return items.value.find(item => item.task_id === taskId)
  }

  function clear() {
    items.value = []
    writeEmpty()
  }

  watch(userId, load, { immediate: true })

  return { items, load, add, find, clear }
}
