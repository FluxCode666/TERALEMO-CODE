<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-wrap items-center border-b border-gray-200 dark:border-dark-700">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          :data-testid="`async-tasks-tab-${tab.key}`"
          class="-mb-px inline-flex items-center border-b-2 px-3 py-3 text-sm font-medium transition-colors sm:px-4"
          :class="activeTab === tab.key
            ? 'border-primary-500 text-primary-600 dark:text-primary-400'
            : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-dark-500 dark:hover:text-gray-200'"
          @click="activeTab = tab.key"
        >
          {{ t(tab.labelKey) }}
        </button>
      </div>

      <div v-show="activeTab === 'tasks'" class="space-y-5">
      <div class="flex flex-wrap items-center justify-end gap-4">
        <div class="flex items-center gap-4">
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
            <input v-model="autoRefresh" type="checkbox" class="rounded border-gray-300 text-primary-600" />{{ t('asyncTasks.autoRefresh') }}
          </label>
          <button type="button" class="btn btn-primary" :disabled="loading" @click="load(true, true)">{{ t('common.refresh') }}</button>
        </div>
      </div>

      <form class="card grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-4" @submit.prevent="applyFilters">
        <div class="sm:col-span-2">
          <label for="admin-task-search" class="input-label">{{ t('asyncTasks.search') }}</label>
          <input id="admin-task-search" v-model="filters.search" type="search" maxlength="256" class="input" :placeholder="t('asyncTasks.searchPlaceholder')" />
        </div>
        <div>
          <label for="admin-task-kind" class="input-label">{{ t('asyncTasks.kind') }}</label>
          <select id="admin-task-kind" v-model="filters.kind" class="input" @change="applyFilters">
            <option value="">{{ t('common.all') }}</option>
            <option value="image">{{ t('asyncTasks.image') }}</option><option value="video">{{ t('asyncTasks.video') }}</option>
          </select>
        </div>
        <div>
          <label for="admin-task-status" class="input-label">{{ t('common.status') }}</label>
          <select id="admin-task-status" v-model="filters.status" class="input" @change="applyFilters">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="value in statuses" :key="value" :value="value">{{ t(`asyncTasks.${value}`) }}</option>
          </select>
        </div>
        <div v-for="field in identityFilters" :key="field">
          <label :for="`admin-task-${field}`" class="input-label">{{ t(`adminAsyncTasks.${field}`) }}</label>
          <input :id="`admin-task-${field}`" v-model="filters[field]" type="number" min="1" step="1" class="input" :placeholder="t('common.all')" />
        </div>
        <div>
          <label for="admin-task-protocol" class="input-label">{{ t('adminAsyncTasks.protocol') }}</label>
          <select id="admin-task-protocol" v-model="filters.protocol" class="input" @change="applyFilters">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="value in protocols" :key="value" :value="value">{{ value }}</option>
          </select>
        </div>
        <div class="flex justify-end gap-2 sm:col-span-2 lg:col-span-4">
          <button type="button" class="btn btn-secondary" @click="resetFilters">{{ t('common.reset') }}</button>
          <button type="submit" class="btn btn-primary">{{ t('common.search') }}</button>
        </div>
      </form>

      <div v-if="error" role="alert" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
        {{ error }} <button type="button" class="ml-2 underline" @click="load(true, true)">{{ t('common.tryAgain') }}</button>
      </div>
      <div v-if="actionError" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ actionError }}</div>

      <div class="card overflow-hidden" :aria-busy="loading">
        <div v-if="loading && !items.length" role="status" class="space-y-4 p-6">
          <span class="sr-only">{{ t('common.loading') }}</span>
          <div v-for="index in 4" :key="index" class="h-12 animate-pulse rounded bg-gray-100 dark:bg-dark-700" />
        </div>
        <div v-else-if="!items.length && !error" role="status" class="px-6 py-16 text-center">
          <h2 class="font-medium text-gray-900 dark:text-gray-100">{{ t('asyncTasks.empty') }}</h2>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('adminAsyncTasks.emptyHint') }}</p>
        </div>
        <div v-else-if="items.length" class="overflow-x-auto">
          <table class="w-full min-w-[1100px] text-left text-sm">
            <thead class="whitespace-nowrap border-b border-gray-200 bg-gray-50 text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th class="px-5 py-3">{{ t('asyncTasks.task') }}</th><th class="px-5 py-3">{{ t('adminAsyncTasks.owner') }}</th>
                <th class="px-5 py-3">{{ t('adminAsyncTasks.source') }}</th><th class="px-5 py-3">{{ t('common.status') }}</th>
                <th class="px-5 py-3">{{ t('asyncTasks.createdAt') }}</th><th class="px-5 py-3">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="task in items" :key="`${task.user_id}:${task.id}`">
                <td class="max-w-xs px-5 py-4">
                  <div class="break-all font-mono text-xs text-gray-800 dark:text-gray-200">{{ task.task_id }}</div>
                  <div class="mt-1 break-all text-gray-500 dark:text-gray-400">{{ task.model || '—' }}</div>
                  <div class="mt-1 text-xs text-gray-500">{{ t(`asyncTasks.${task.kind}`) }} · {{ task.protocol }}</div>
                </td>
                <td class="max-w-[220px] px-5 py-4">
                  <div class="break-all text-gray-800 dark:text-gray-200">{{ task.user_name || task.user_email || `#${task.user_id}` }}</div>
                  <div class="mt-1 text-xs text-gray-500">{{ t('adminAsyncTasks.user_id') }} #{{ task.user_id }}</div>
                </td>
                <td class="px-5 py-4 text-gray-600 dark:text-gray-300">
                  <div>{{ task.key_name || 'Key' }} #{{ task.api_key_id }}</div>
                  <div class="mt-1 text-xs text-gray-500">{{ task.group_id ? `${task.group_name || t('adminAsyncTasks.group')} #${task.group_id}` : '—' }}</div>
                </td>
                <td class="whitespace-nowrap px-5 py-4"><span class="rounded-md px-2 py-1 text-xs font-medium" :class="statusClass(task.status)">{{ t(`asyncTasks.${task.status}`) }}</span></td>
                <td class="whitespace-nowrap px-5 py-4 text-gray-500 dark:text-gray-400">
                  {{ formatTime(task.created_at) }}<div v-if="task.completed_at" class="mt-1 text-xs">{{ t('asyncTasks.finishedAt') }} {{ formatTime(task.completed_at) }}</div>
                </td>
                <td class="whitespace-nowrap px-5 py-4">
                  <button type="button" class="btn btn-ghost btn-sm" @click="showDetail(task)">{{ t('adminAsyncTasks.details') }}</button>
                  <button v-if="task.kind === 'video' && task.status === 'processing'" type="button" class="btn btn-ghost btn-sm" :disabled="refreshing.has(taskKey(task))" @click="refreshTask(task, generation, true)">{{ t('common.refresh') }}</button>
                  <button v-if="task.kind === 'video' && task.status === 'completed' && task.result_url" type="button" class="btn btn-ghost btn-sm" :disabled="downloading === taskKey(task)" @click="download(task)">{{ t('asyncTasks.download') }}</button>
                  <a v-else-if="safeURL(task.result_url)" :href="safeURL(task.result_url)" target="_blank" rel="noopener noreferrer" class="btn btn-ghost btn-sm">{{ t('asyncTasks.viewResult') }}</a>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination v-if="total > 0" :page="page" :page-size="20" :total="total" :show-page-size-selector="false" @update:page="changePage" />
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('adminAsyncTasks.retention') }}</p>
      </div>

      <VideoBillingReservationsPanel v-if="activeTab === 'reservations'" />
    </div>

    <BaseDialog :show="detailOpen" :title="t('adminAsyncTasks.details')" width="wide" @close="detailOpen = false">
      <div v-if="detailLoading" role="status">{{ t('common.loading') }}</div>
      <dl v-else-if="detail" class="grid gap-4 text-sm sm:grid-cols-2">
        <div v-for="field in detailFields" :key="field" class="min-w-0">
          <dt class="text-gray-500 dark:text-gray-400">{{ t(`adminAsyncTasks.detail.${field}`) }}</dt>
          <dd class="mt-1 break-all text-gray-900 dark:text-gray-100">{{ detailValue(field) }}</dd>
        </div>
        <div v-if="detail.error" class="sm:col-span-2">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('adminAsyncTasks.error') }}</dt>
          <dd class="mt-1 whitespace-pre-wrap break-words text-red-600 dark:text-red-400">{{ detail.error }}</dd>
        </div>
        <div v-if="safeURL(detail.result_url)" class="min-w-0 sm:col-span-2">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('adminAsyncTasks.resultURL') }}</dt>
          <dd class="mt-1 break-all"><a :href="safeURL(detail.result_url)" class="text-primary-600 underline dark:text-primary-400" target="_blank" rel="noopener noreferrer">{{ detail.result_url }}</a></dd>
        </div>
      </dl>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import VideoBillingReservationsPanel from '@/components/admin/asyncTasks/VideoBillingReservationsPanel.vue'
import { adminAsyncTasksAPI, type AdminAsyncTask } from '@/api/admin/asyncTasks'

const { t } = useI18n()
const tabs = [
  { key: 'tasks', labelKey: 'adminAsyncTasks.tabs.tasks' },
  { key: 'reservations', labelKey: 'adminAsyncTasks.tabs.reservations' }
] as const
const activeTab = ref<(typeof tabs)[number]['key']>('tasks')
const identityFilters = ['user_id', 'api_key_id', 'group_id'] as const
const blankFilters = () => ({ search: '', kind: '', status: '', protocol: '', user_id: '', api_key_id: '', group_id: '' })
const filters = reactive(blankFilters())
const statuses = ['processing', 'completed', 'failed', 'cancelled']
const protocols = ['seedance', 'gemini', 'minimax', 'grok', 'openai']
const items = ref<AdminAsyncTask[]>([])
const page = ref(1), total = ref(0), loading = ref(false), autoRefresh = ref(true), error = ref(''), actionError = ref(''), downloading = ref('')
const refreshing = ref(new Set<string>())
const detail = ref<AdminAsyncTask | null>(null), detailOpen = ref(false), detailLoading = ref(false)
const detailFields = ['task_id', 'id', 'model', 'status', 'user_id', 'user_email', 'api_key_id', 'group_id', 'protocol', 'created_at', 'completed_at', 'expires_at'] as const
const controller = new AbortController()
let timer: ReturnType<typeof setInterval> | undefined
let generation = 0
let detailGeneration = 0

function safeURL(value?: string): string | undefined {
  if (!value) return undefined
  try { const url = new URL(value); return ['http:', 'https:'].includes(url.protocol) ? url.href : undefined } catch { return undefined }
}
function formatTime(value: number) { return new Date(value * 1000).toLocaleString() }
function statusClass(value: string) {
  return ({ processing: 'bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300', completed: 'bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300', failed: 'bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300', cancelled: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400' })[value]
}
function detailValue(field: typeof detailFields[number]) {
  const value = detail.value?.[field]
  if (value === undefined || value === null || value === '') return '—'
  if (field.endsWith('_at')) return formatTime(Number(value))
  if (field === 'status') return t(`asyncTasks.${value}`)
  return value
}
function taskKey(task: AdminAsyncTask) { return `${task.user_id}:${task.id}` }
function downloadName(task: AdminAsyncTask) {
  const id = (task.task_id ?? '').replace(/[^a-zA-Z0-9-_]/g, '-')
  if (!id) return 'video-task.mp4'
  return `${task.protocol || 'video'}-${id}.mp4`
}
async function refreshTask(task: AdminAsyncTask, current = generation, manual = false) {
  const key = taskKey(task)
  if (refreshing.value.has(key)) return
  if (manual) actionError.value = ''
  refreshing.value.add(key)
  try {
    const updated = await adminAsyncTasksAPI.refresh(task, controller.signal)
    if (current !== generation) return
    const index = items.value.findIndex(item => item.id === task.id && item.user_id === task.user_id)
    if (index >= 0) items.value[index] = updated
    if (detail.value?.id === task.id && detail.value?.user_id === task.user_id) detail.value = updated
  } catch { if (!controller.signal.aborted) actionError.value = t('asyncTasks.refreshFailed') }
  finally { refreshing.value.delete(key) }
}
function validIdentityFilters() {
  return identityFilters.every(field => {
    const raw = String(filters[field]).trim()
    if (!raw) return true
    const value = Number(raw)
    return Number.isInteger(value) && value > 0
  })
}
async function load(refreshUpstream = false, manual = false) {
  const current = ++generation
  loading.value = true
  error.value = ''
  if (manual) actionError.value = ''
  try {
    const result = await adminAsyncTasksAPI.list({
      page: page.value, page_size: 20, search: filters.search.trim(), kind: filters.kind, status: filters.status, protocol: filters.protocol,
      ...Object.fromEntries(identityFilters.map(field => [field, filters[field] ? Number(filters[field]) : undefined]))
    }, controller.signal)
    if (current !== generation) return
    items.value = result.items; total.value = result.total
    if (refreshUpstream) {
      const pending = items.value.filter(task => task.kind === 'video' && task.status === 'processing')
      for (let i = 0; i < pending.length && current === generation; i += 3) await Promise.all(pending.slice(i, i + 3).map(task => refreshTask(task, current)))
    }
  } catch { if (current === generation && !controller.signal.aborted) error.value = t('asyncTasks.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function applyFilters() {
  if (!validIdentityFilters()) { actionError.value = t('adminAsyncTasks.invalidIdFilter'); return }
  actionError.value = ''
  page.value = 1; void load()
}
function resetFilters() { Object.assign(filters, blankFilters()); applyFilters() }
function changePage(value: number) { page.value = value; void load(true) }
async function showDetail(task: AdminAsyncTask) {
  const current = ++detailGeneration
  detail.value = null; detailOpen.value = true; detailLoading.value = true
  try {
    const result = await adminAsyncTasksAPI.get(task, controller.signal)
    if (current === detailGeneration) detail.value = result
  } catch {
    if (!controller.signal.aborted && current === detailGeneration) { actionError.value = t('asyncTasks.loadFailed'); detailOpen.value = false }
  } finally { if (current === detailGeneration) detailLoading.value = false }
}
async function download(task: AdminAsyncTask) {
  actionError.value = ''
  downloading.value = taskKey(task)
  try {
    const blob = await adminAsyncTasksAPI.download(task)
    const url = URL.createObjectURL(blob), link = document.createElement('a')
    link.href = url; link.download = downloadName(task); link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch { actionError.value = t('asyncTasks.downloadFailed') }
  finally { downloading.value = '' }
}
onMounted(() => { void load(true); timer = setInterval(() => { if (activeTab.value === 'tasks' && autoRefresh.value && !loading.value && !document.hidden) void load(true) }, 10000) })
onUnmounted(() => { generation++; detailGeneration++; controller.abort(); if (timer) clearInterval(timer) })
</script>
