/**
 * F11：结果列 / 历史流。
 *
 * - 分页用 `asyncTasksAPI.list` + 「加载更多」追加，加载更多不清空已有列表，失败保留旧数据。
 * - 已加载的页里存在 `processing` 时每 10 秒刷新；页面隐藏时暂停，回到前台补刷一次。
 * - 视频 `processing` 调 `refresh`，每个任务 30 秒节流；图片不调 `refresh`。
 */
import { computed, onUnmounted, ref, watch, type Ref } from 'vue'
import { asyncTasksAPI, type AsyncTask } from '@/api/asyncTasks'
import type { CreationSnapshot, CreationTaskStatus, CreationTaskView } from './types'

export type CreationFeedFilter = 'all' | 'image' | 'video'

export const FEED_PAGE_SIZE = 20
export const FEED_POLL_INTERVAL_MS = 10000
export const VIDEO_REFRESH_THROTTLE_MS = 30000

export interface UseCreationFeedDeps {
  snapshotFor: (catalogId: string, taskId: string) => CreationSnapshot | undefined
  autoRefresh: Ref<boolean>
}

function nowSeconds(): number {
  return Math.floor(Date.now() / 1000)
}

export function useCreationFeed(deps: UseCreationFeedDeps) {
  const items = ref<CreationTaskView[]>([])
  const loading = ref(false)
  const loadingMore = ref(false)
  const error = ref(false)
  const filter = ref<CreationFeedFilter>('all')
  const pagesLoaded = ref(0)
  const total = ref(0)

  let generation = 0
  let pollTimer: ReturnType<typeof setInterval> | null = null
  const lastVideoRefresh = new Map<string, number>()

  function toView(task: AsyncTask, fresh = false): CreationTaskView {
    const snapshot = deps.snapshotFor(task.id, task.task_id)
    const urls = task.result_urls?.length
      ? task.result_urls
      : task.result_url
        ? [task.result_url]
        : []
    const expiresAt = task.expires_at ?? 0
    const expired = task.status === 'completed' && expiresAt > 0 && expiresAt < nowSeconds()
    const status: CreationTaskStatus = expired ? 'expired' : (task.status as CreationTaskStatus)
    const requestedCount = Number(snapshot?.params?.n ?? 0)
    return {
      catalogId: task.id,
      taskId: task.task_id,
      kind: task.kind,
      protocol: task.protocol,
      model: task.model ?? '',
      status,
      prompt: snapshot?.prompt ?? '',
      params: snapshot?.params ?? {},
      imageCount: urls.length || (Number.isFinite(requestedCount) && requestedCount > 0 ? requestedCount : 1),
      urls,
      error: task.error ?? '',
      createdAt: task.created_at,
      completedAt: task.completed_at ?? 0,
      expiresAt,
      snapshot: snapshot ?? null,
      fresh,
    }
  }

  const hasProcessing = computed(() =>
    items.value.some(item => item.status === 'processing' || item.status === 'queued'),
  )

  function mergeFresh(next: CreationTaskView[], previous: CreationTaskView[]): CreationTaskView[] {
    const freshIds = new Set(previous.filter(item => item.fresh).map(item => item.catalogId))
    return next.map(item => (freshIds.has(item.catalogId) ? { ...item, fresh: true } : item))
  }

  async function fetchPage(page: number, signal: AbortSignal): Promise<AsyncTask[]> {
    const result = await asyncTasksAPI.list(
      {
        page,
        page_size: FEED_PAGE_SIZE,
        kind: filter.value === 'all' ? '' : filter.value,
        status: '',
        search: '',
      },
      signal,
    )
    total.value = result.total
    return result.items ?? []
  }

  async function load(): Promise<void> {
    const current = ++generation
    const controller = new AbortController()
    loading.value = true
    error.value = false
    try {
      const pageItems = await fetchPage(1, controller.signal)
      if (current !== generation) return
      items.value = pageItems.map(task => toView(task))
      pagesLoaded.value = 1
      await refreshVideos()
    } catch {
      if (current === generation) error.value = true
    } finally {
      if (current === generation) loading.value = false
    }
  }

  async function loadMore(): Promise<void> {
    if (loading.value || loadingMore.value) return
    const current = generation
    loadingMore.value = true
    const controller = new AbortController()
    try {
      const pageItems = await fetchPage(pagesLoaded.value + 1, controller.signal)
      if (current !== generation) return
      const known = new Set(items.value.map(item => item.catalogId))
      const appended = pageItems.filter(task => !known.has(task.id)).map(task => toView(task))
      items.value = [...items.value, ...appended]
      pagesLoaded.value += 1
    } catch {
      if (current === generation) error.value = true
    } finally {
      loadingMore.value = false
    }
  }

  /** 重新拉已加载的页（不清空列表；失败时保留旧数据）。 */
  async function refreshLoaded(): Promise<void> {
    if (loading.value || loadingMore.value) return
    const current = generation
    const controller = new AbortController()
    try {
      const pages = Math.max(1, pagesLoaded.value)
      const collected: AsyncTask[] = []
      for (let page = 1; page <= pages; page += 1) {
        collected.push(...(await fetchPage(page, controller.signal)))
        if (current !== generation) return
      }
      const seen = new Set<string>()
      const next = collected
        .filter(task => (seen.has(task.id) ? false : (seen.add(task.id), true)))
        .map(task => toView(task))
      items.value = mergeFresh(next, items.value)
      error.value = false
      await refreshVideos()
    } catch {
      if (current === generation) error.value = true
    }
  }

  /** 视频 processing 才刷新上游，每个任务 30 秒节流。 */
  async function refreshVideos(): Promise<void> {
    const timestamp = Date.now()
    const pending = items.value.filter(item => {
      if (item.kind !== 'video' || item.status !== 'processing' || !item.catalogId) return false
      const last = lastVideoRefresh.get(item.catalogId) ?? 0
      return timestamp - last >= VIDEO_REFRESH_THROTTLE_MS
    })
    for (let index = 0; index < pending.length; index += 3) {
      const slice = pending.slice(index, index + 3)
      // 最多并发 3 个上游刷新（与异步任务页一致）。
      await Promise.all(slice.map(item => refreshOneVideo(item)))
    }
  }

  async function refreshOneVideo(item: CreationTaskView): Promise<void> {
    lastVideoRefresh.set(item.catalogId, Date.now())
    try {
      const updated = await asyncTasksAPI.refresh(item.catalogId)
      const target = items.value.find(entry => entry.catalogId === item.catalogId)
      if (target) Object.assign(target, toView(updated, target.fresh))
    } catch {
      // 单条刷新失败不影响列表，等下一次轮询。
    }
  }

  /** 提交成功后插入到任务流顶部。 */
  function insert(view: CreationTaskView) {
    const rest = items.value.filter(item => item.catalogId !== view.catalogId)
    items.value = [{ ...view, fresh: true }, ...rest]
    total.value += 1
  }

  function setFilter(next: CreationFeedFilter) {
    if (filter.value === next) return
    filter.value = next
    void load()
  }

  function startPolling() {
    if (pollTimer !== null) return
    pollTimer = setInterval(() => {
      if (!deps.autoRefresh.value || document.hidden) return
      if (!hasProcessing.value) return
      void refreshLoaded()
    }, FEED_POLL_INTERVAL_MS)
  }

  function stopPolling() {
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  function handleVisibility() {
    if (document.hidden || !deps.autoRefresh.value) return
    if (hasProcessing.value) void refreshLoaded()
  }

  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', handleVisibility)
    startPolling()
  }

  watch(deps.autoRefresh, enabled => {
    if (enabled && hasProcessing.value) void refreshLoaded()
  })

  onUnmounted(() => {
    generation += 1
    stopPolling()
    if (typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  })

  return {
    items,
    loading,
    loadingMore,
    error,
    filter,
    total,
    hasProcessing,
    load,
    loadMore,
    refreshLoaded,
    insert,
    setFilter,
  }
}
