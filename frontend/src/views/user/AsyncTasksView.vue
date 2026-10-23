<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('asyncTasks.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('asyncTasks.description') }}</p>
        </div>
        <div class="flex items-center gap-4">
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
            <input v-model="autoRefresh" type="checkbox" class="rounded border-gray-300 text-primary-600" />{{ t('asyncTasks.autoRefresh') }}
          </label>
          <button type="button" class="btn btn-primary" :disabled="loading" @click="load(true, true)">{{ t('common.refresh') }}</button>
        </div>
      </div>

      <form class="card grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-4" @submit.prevent="applyFilters">
        <div class="sm:col-span-2">
          <label for="task-search" class="input-label">{{ t('asyncTasks.search') }}</label>
          <div class="flex gap-2">
            <input id="task-search" v-model="search" type="search" class="input min-w-0" :placeholder="t('asyncTasks.searchPlaceholder')" />
            <button type="submit" class="btn btn-secondary shrink-0">{{ t('common.search') }}</button>
          </div>
        </div>
        <div>
          <label for="task-kind" class="input-label">{{ t('asyncTasks.kind') }}</label>
          <select id="task-kind" v-model="kind" class="input" @change="applyFilters">
            <option value="">{{ t('common.all') }}</option>
            <option value="image">{{ t('asyncTasks.image') }}</option><option value="video">{{ t('asyncTasks.video') }}</option>
          </select>
        </div>
        <div>
          <label for="task-status" class="input-label">{{ t('common.status') }}</label>
          <select id="task-status" v-model="status" class="input" @change="applyFilters">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="value in statuses" :key="value" :value="value">{{ t(`asyncTasks.${value}`) }}</option>
          </select>
        </div>
      </form>

      <div v-if="error" role="alert" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
        {{ error }} <button type="button" class="ml-2 underline" @click="load(true, true)">{{ t('common.tryAgain') }}</button>
      </div>
      <div v-if="refreshError" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ refreshError }}</div>

      <div class="card overflow-hidden" :aria-busy="loading">
        <div v-if="loading && !items.length" role="status" class="space-y-4 p-6">
          <span class="sr-only">{{ t('common.loading') }}</span>
          <div v-for="index in 4" :key="index" class="h-12 animate-pulse rounded bg-gray-100 dark:bg-dark-700" />
        </div>
        <div v-else-if="!items.length && !error" role="status" class="px-6 py-16 text-center">
          <h2 class="font-medium text-gray-900 dark:text-gray-100">{{ t('asyncTasks.empty') }}</h2>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('asyncTasks.emptyHint') }}</p>
        </div>
        <div v-else-if="items.length" class="overflow-x-auto">
          <table class="w-full min-w-[900px] text-left text-sm">
            <thead class="whitespace-nowrap border-b border-gray-200 bg-gray-50 text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400">
              <tr><th class="px-5 py-3">{{ t('asyncTasks.task') }}</th><th class="px-5 py-3">{{ t('asyncTasks.kind') }}</th><th class="px-5 py-3">{{ t('common.status') }}</th><th class="whitespace-nowrap px-5 py-3">{{ t('asyncTasks.createdAt') }}</th><th class="px-5 py-3">{{ t('common.actions') }}</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="task in items" :key="task.id">
                <td class="max-w-xs px-5 py-4">
                  <div class="break-all font-mono text-xs text-gray-800 dark:text-gray-200">{{ task.task_id }}</div>
                  <div class="mt-1 break-all text-gray-500 dark:text-gray-400">{{ task.model || '—' }} · Key #{{ task.api_key_id }}</div>
                  <p v-if="task.error" class="mt-1 break-words text-xs text-red-600 dark:text-red-400">{{ task.error }}</p>
                </td>
                <td class="whitespace-nowrap px-5 py-4 text-gray-600 dark:text-gray-300">{{ t(`asyncTasks.${task.kind}`) }}<div class="mt-1 text-xs text-gray-500">{{ task.protocol || '—' }}</div></td>
                <td class="whitespace-nowrap px-5 py-4"><span class="rounded-md px-2 py-1 text-xs font-medium" :class="statusClass(task.status)">{{ t(`asyncTasks.${task.status}`) }}</span></td>
                <td class="whitespace-nowrap px-5 py-4 text-gray-500 dark:text-gray-400">{{ formatTime(task.created_at) }}<div v-if="task.completed_at" class="mt-1 text-xs">{{ t('asyncTasks.finishedAt') }} {{ formatTime(task.completed_at) }}</div></td>
                <td class="whitespace-nowrap px-5 py-4">
                  <button v-if="task.kind === 'video' && task.status === 'processing'" type="button" class="btn btn-ghost btn-sm" :disabled="refreshing.has(task.id)" @click="refreshTask(task, true)">{{ t('common.refresh') }}</button>
                  <button v-if="task.kind === 'video' && (task.protocol !== 'seedance' || task.result_url?.startsWith('/api/v1/user/async-tasks/')) && task.status === 'completed' && task.result_url" type="button" class="btn btn-ghost btn-sm" :disabled="downloading === task.id" @click="download(task)">{{ t('asyncTasks.download') }}</button>
                  <a v-else-if="safeURL(task.result_url)" :href="safeURL(task.result_url)" target="_blank" rel="noopener noreferrer" class="btn btn-ghost btn-sm">{{ t('asyncTasks.viewResult') }}</a>
                  <span v-else-if="task.status !== 'processing' || task.kind !== 'video'" class="text-gray-400">—</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination v-if="total > 0" :page="page" :page-size="20" :total="total" :show-page-size-selector="false" @update:page="changePage" />
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncTasks.retention') }}</p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import { asyncTasksAPI, type AsyncTask } from '@/api/asyncTasks'

const { t } = useI18n()
const items = ref<AsyncTask[]>([])
const page = ref(1), total = ref(0), search = ref(''), kind = ref(''), status = ref('')
const loading = ref(false), autoRefresh = ref(true), error = ref(''), refreshError = ref(''), downloading = ref('')
const refreshing = ref(new Set<string>())
const statuses = ['processing', 'completed', 'failed', 'cancelled']
const controller = new AbortController()
let timer: ReturnType<typeof setInterval> | undefined
let generation = 0

function safeURL(value?: string): string | undefined {
  if (!value) return undefined
  try { const url = new URL(value); return ['http:', 'https:'].includes(url.protocol) ? url.href : undefined } catch { return undefined }
}
function formatTime(value: number) { return new Date(value * 1000).toLocaleString() }
function statusClass(value: string) {
  return ({ processing: 'bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300', completed: 'bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300', failed: 'bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300', cancelled: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400' })[value]
}
function downloadName(task: AsyncTask) {
  const id = (task.task_id ?? '').replace(/[^a-zA-Z0-9-_]/g, '-')
  if (!id) return 'video-task.mp4'
  return `${task.protocol || 'video'}-${id}.mp4`
}
async function refreshTask(task: AsyncTask, manual = false) {
  if (refreshing.value.has(task.id)) return
  if (manual) refreshError.value = ''
  refreshing.value.add(task.id)
  try {
    const updated = await asyncTasksAPI.refresh(task.id, controller.signal)
    const index = items.value.findIndex(item => item.id === task.id)
    if (index >= 0) items.value[index] = updated
  } catch { if (!controller.signal.aborted) refreshError.value = t('asyncTasks.refreshFailed') }
  finally { refreshing.value.delete(task.id) }
}
async function load(refreshUpstream = false, manual = false) {
  const current = ++generation
  loading.value = true
  error.value = ''
  if (manual) refreshError.value = ''
  try {
    const result = await asyncTasksAPI.list({ page: page.value, page_size: 20, kind: kind.value, status: status.value, search: search.value.trim() }, controller.signal)
    if (current !== generation) return
    items.value = result.items; total.value = result.total
    if (refreshUpstream) {
      const pending = items.value.filter(task => task.status === 'processing' && task.kind === 'video')
      // At most three upstream polls per user page at a time.
      for (let i = 0; i < pending.length && current === generation; i += 3) await Promise.all(pending.slice(i, i + 3).map(task => refreshTask(task)))
    }
  } catch { if (current === generation && !controller.signal.aborted) error.value = t('asyncTasks.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function applyFilters() { page.value = 1; void load() }
function changePage(value: number) { page.value = value; void load(true) }
async function download(task: AsyncTask) {
  refreshError.value = ''
  downloading.value = task.id
  try {
    const blob = await asyncTasksAPI.download(task.id)
    const url = URL.createObjectURL(blob), link = document.createElement('a')
    link.href = url; link.download = downloadName(task); link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch { refreshError.value = t('asyncTasks.downloadFailed') }
  finally { downloading.value = '' }
}
onMounted(() => { void load(true); timer = setInterval(() => { if (autoRefresh.value && !loading.value && !document.hidden) void load(true) }, 10000) })
onUnmounted(() => { generation++; controller.abort(); if (timer) clearInterval(timer) })
</script>
