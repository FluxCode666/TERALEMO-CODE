<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.massEmails.detail.title') }}</h1>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" @click="router.push('/admin/mass-emails')">{{ t('admin.massEmails.actions.back') }}</button>
          <button v-if="campaign && isEditableStatus(campaign.status)" class="btn btn-secondary" @click="router.push(`/admin/mass-emails/${campaign.id}/edit`)">
            {{ t('admin.massEmails.actions.edit') }}
          </button>
          <button v-if="campaign && canSendStatus(campaign.status)" class="btn btn-primary" data-test="send" @click="doSend">
            {{ t('admin.massEmails.actions.send') }}
          </button>
          <button v-if="campaign && canCancelStatus(campaign.status)" class="btn btn-secondary" data-test="cancel" @click="showCancel = true">
            {{ t('admin.massEmails.actions.cancel') }}
          </button>
          <button v-if="stats && stats.failed > 0" class="btn btn-secondary" data-test="retry-failed" @click="doRetryFailed">
            {{ t('admin.massEmails.actions.retryFailed') }} ({{ stats.failed }})
          </button>
        </div>
      </div>

      <div v-if="loading && !campaign" class="py-12 text-center text-gray-500">{{ t('common.loading') }}</div>
      <template v-if="campaign && stats">
        <div class="card space-y-3 p-5">
          <div class="flex flex-wrap items-center gap-3">
            <span class="text-lg font-medium text-gray-900 dark:text-white">{{ campaign.subject }}</span>
            <span :class="['badge', statusBadge(campaign.status)]" data-test="status">{{ t(`admin.massEmails.status.${campaign.status}`) }}</span>
          </div>
          <div class="grid grid-cols-2 gap-4 sm:grid-cols-5">
            <div><div class="text-xs text-gray-500">{{ t('admin.massEmails.detail.total') }}</div><div class="text-xl font-semibold" data-test="stat-total">{{ stats.total }}</div></div>
            <div><div class="text-xs text-gray-500">{{ t('admin.massEmails.detail.pending') }}</div><div class="text-xl font-semibold">{{ stats.pending }}</div></div>
            <div><div class="text-xs text-gray-500">{{ t('admin.massEmails.detail.sent') }}</div><div class="text-xl font-semibold text-green-600" data-test="stat-sent">{{ stats.sent }}</div></div>
            <div><div class="text-xs text-gray-500">{{ t('admin.massEmails.detail.failed') }}</div><div class="text-xl font-semibold text-red-600" data-test="stat-failed">{{ stats.failed }}</div></div>
            <div><div class="text-xs text-gray-500">{{ t('admin.massEmails.detail.openRate') }}</div><div class="text-xl font-semibold" data-test="stat-open-rate">{{ stats.opened }} ({{ openRatePercent(stats.open_rate) }})</div></div>
          </div>
          <div>
            <div class="mb-1 text-xs text-gray-500">{{ t('admin.massEmails.detail.progress') }} {{ progress }}%</div>
            <div class="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
              <div class="h-full bg-primary-500 transition-all" :style="{ width: progress + '%' }"></div>
            </div>
          </div>
          <div class="grid gap-1 text-sm text-gray-600 dark:text-gray-300 sm:grid-cols-3">
            <div v-if="campaign.scheduled_at">{{ t('admin.massEmails.detail.scheduledAt') }}: {{ formatDateTime(campaign.scheduled_at) }}</div>
            <div v-if="campaign.started_at">{{ t('admin.massEmails.detail.startedAt') }}: {{ formatDateTime(campaign.started_at) }}</div>
            <div v-if="campaign.finished_at">{{ t('admin.massEmails.detail.finishedAt') }}: {{ formatDateTime(campaign.finished_at) }}</div>
          </div>
          <div v-if="campaign.last_error" class="text-sm text-red-600">{{ t('admin.massEmails.detail.lastError') }}: {{ campaign.last_error }}</div>
        </div>

        <div class="card p-5">
          <h2 class="mb-2 text-base font-medium text-gray-900 dark:text-white">{{ t('admin.massEmails.detail.content') }}</h2>
          <!-- body_html is sanitized server-side; DOMPurify again client-side as defense in depth -->
          <div class="prose prose-sm max-w-none dark:prose-invert" v-html="safeBody"></div>
        </div>

        <div class="card space-y-3 p-5">
          <h2 class="text-base font-medium text-gray-900 dark:text-white">{{ t('admin.massEmails.detail.recipients') }}</h2>
          <div class="flex flex-wrap items-center gap-3">
            <div class="inline-flex overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
              <button
                v-for="tab in tabs"
                :key="tab.key"
                type="button"
                :data-test="`tab-${tab.key}`"
                :class="['px-3 py-1.5 text-sm', activeTab === tab.key ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-dark-700']"
                @click="changeTab(tab.key)"
              >{{ tab.label }}</button>
            </div>
            <input v-model="search" type="text" class="input max-w-xs" :placeholder="t('admin.massEmails.detail.searchRecipients')" @input="handleSearch" />
          </div>

          <DataTable :columns="columns" :data="recipients" :loading="recipientsLoading">
            <template #cell-status="{ value }">
              <span :class="['badge', value === 'sent' ? 'badge-success' : value === 'failed' ? 'badge-danger' : 'badge-gray']">
                {{ t(`admin.massEmails.recipientStatus.${value}`) }}
              </span>
            </template>
            <template #cell-error="{ value }"><span class="text-xs text-red-600">{{ value || '-' }}</span></template>
            <template #cell-sent_at="{ value }">{{ value ? formatDateTime(value) : '-' }}</template>
            <template #cell-first_opened_at="{ value }">{{ value ? formatDateTime(value) : '-' }}</template>
            <template #cell-last_opened_at="{ value }">{{ value ? formatDateTime(value) : '-' }}</template>
            <template #cell-actions="{ row }">
              <button v-if="row.status === 'failed'" class="btn btn-secondary btn-sm" :data-test="`retry-${row.id}`" @click="doRetryOne(row)">
                {{ t('admin.massEmails.actions.retry') }}
              </button>
            </template>
            <template #empty>
              <EmptyState :title="t('admin.massEmails.detail.emptyRecipients')" />
            </template>
          </DataTable>
          <Pagination
            v-if="pagination.total > 0"
            :page="pagination.page"
            :total="pagination.total"
            :page-size="pagination.page_size"
            @update:page="(p: number) => { pagination.page = p; loadRecipients(false) }"
            @update:pageSize="(s: number) => { pagination.page_size = s; pagination.page = 1; loadRecipients(false) }"
          />
        </div>
      </template>
    </div>

    <ConfirmDialog
      :show="showCancel"
      :title="t('admin.massEmails.confirm.cancelTitle')"
      :message="t('admin.massEmails.confirm.cancelMessage')"
      :danger="true"
      @confirm="doCancel"
      @cancel="showCancel = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import DOMPurify from 'dompurify'
import { useAppStore } from '@/stores/app'
import { usePolling } from '@/composables/usePolling'
import { adminAPI } from '@/api/admin'
import type { MassEmailCampaign, MassEmailRecipient, MassEmailStats } from '@/api/admin/massEmails'
import { formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import {
  canCancelStatus,
  canSendStatus,
  isEditableStatus,
  openRatePercent,
  progressPercent,
  tabToFilters,
  type RecipientTab
} from './recipientForm'
import { massEmailErrorMessage } from './massEmailErrors'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()

const id = computed(() => Number(route.params.id))
const campaign = ref<MassEmailCampaign | null>(null)
const stats = ref<MassEmailStats | null>(null)
const loading = ref(false)
const recipients = ref<MassEmailRecipient[]>([])
const recipientsLoading = ref(false)
const activeTab = ref<RecipientTab>('all')
const search = ref('')
const showCancel = ref(false)
const pagination = reactive({ page: 1, page_size: 50, total: 0 })

const progress = computed(() =>
  campaign.value ? progressPercent(campaign.value) : 0
)
const safeBody = computed(() => DOMPurify.sanitize(campaign.value?.body_html ?? ''))

const tabs = computed(() => [
  { key: 'all' as RecipientTab, label: t('admin.massEmails.detail.tabAll') },
  { key: 'opened' as RecipientTab, label: t('admin.massEmails.detail.tabOpened') },
  { key: 'unopened' as RecipientTab, label: t('admin.massEmails.detail.tabUnopened') },
  { key: 'failed' as RecipientTab, label: t('admin.massEmails.detail.tabFailed') }
])

const columns = computed<Column[]>(() => [
  { key: 'email', label: t('admin.massEmails.detail.email') },
  { key: 'name', label: t('admin.massEmails.detail.name') },
  { key: 'status', label: t('admin.massEmails.detail.status') },
  { key: 'error', label: t('admin.massEmails.detail.error') },
  { key: 'attempts', label: t('admin.massEmails.detail.attempts') },
  { key: 'sent_at', label: t('admin.massEmails.detail.sentAt') },
  { key: 'first_opened_at', label: t('admin.massEmails.detail.firstOpened') },
  { key: 'last_opened_at', label: t('admin.massEmails.detail.lastOpened') },
  { key: 'open_count', label: t('admin.massEmails.detail.openCount') },
  { key: 'actions', label: t('admin.massEmails.columns.actions') }
])

function statusBadge(status: string): string {
  switch (status) {
    case 'completed': return 'badge-success'
    case 'sending': return 'badge-primary'
    case 'scheduled': return 'badge-warning'
    case 'failed': return 'badge-danger'
    default: return 'badge-gray'
  }
}

let disposed = false
let detailController: AbortController | null = null
let recipientsController: AbortController | null = null
async function loadDetail(silent: boolean) {
  if (disposed || (silent && loading.value)) return
  detailController?.abort()
  const controller = new AbortController()
  detailController = controller
  if (!silent) loading.value = true
  try {
    const res = await adminAPI.massEmails.getById(id.value, { signal: controller.signal })
    if (disposed || controller.signal.aborted) return
    campaign.value = res.campaign
    stats.value = res.stats
  } catch (err) {
    if (!disposed && !controller.signal.aborted && !silent) appStore.showError(massEmailErrorMessage(err, t))
  } finally {
    if (detailController === controller) loading.value = false
  }
}

async function loadRecipients(silent: boolean) {
  if (disposed || (silent && recipientsLoading.value)) return
  recipientsController?.abort()
  const controller = new AbortController()
  recipientsController = controller
  if (!silent) recipientsLoading.value = true
  try {
    const res = await adminAPI.massEmails.listRecipients(id.value, pagination.page, pagination.page_size, {
      ...tabToFilters(activeTab.value),
      search: search.value.trim() || undefined
    }, { signal: controller.signal })
    if (disposed || controller.signal.aborted) return
    recipients.value = res.items
    pagination.total = res.total
  } catch (err) {
    if (!disposed && !controller.signal.aborted && !silent) appStore.showError(massEmailErrorMessage(err, t))
  } finally {
    if (recipientsController === controller) recipientsLoading.value = false
  }
}

// Poll every 2s while sending; stop when status leaves sending or on unmount.
const poller = usePolling(async () => {
  await Promise.all([loadDetail(true), loadRecipients(true)])
}, 2000)
watch(
  () => campaign.value?.status,
  (status) => {
    if (status === 'sending') poller.start()
    else poller.stop()
  }
)

function changeTab(tab: RecipientTab) {
  activeTab.value = tab
  pagination.page = 1
  loadRecipients(false)
}
let searchTimer: number | null = null
function handleSearch() {
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => {
    pagination.page = 1
    loadRecipients(false)
  }, 300)
}

async function refreshAll() {
  await Promise.all([loadDetail(true), loadRecipients(true)])
}

async function doSend() {
  try {
    await adminAPI.massEmails.send(id.value)
    appStore.showSuccess(t('admin.massEmails.messages.started'))
    await refreshAll()
  } catch (err) {
    appStore.showError(massEmailErrorMessage(err, t))
  }
}
async function doCancel() {
  showCancel.value = false
  try {
    await adminAPI.massEmails.cancel(id.value)
    appStore.showSuccess(t('admin.massEmails.messages.cancelled'))
    await refreshAll()
  } catch (err) {
    appStore.showError(massEmailErrorMessage(err, t))
  }
}
async function doRetryFailed() {
  try {
    const res = await adminAPI.massEmails.retryFailed(id.value)
    appStore.showSuccess(
      res.reset > 0 ? t('admin.massEmails.detail.retryDone', { count: res.reset }) : t('admin.massEmails.detail.retryNone')
    )
    await refreshAll()
  } catch (err) {
    appStore.showError(massEmailErrorMessage(err, t))
  }
}
async function doRetryOne(row: MassEmailRecipient) {
  try {
    await adminAPI.massEmails.retryRecipient(id.value, row.id)
    appStore.showSuccess(t('admin.massEmails.detail.retryOneDone'))
    await refreshAll()
  } catch (err) {
    appStore.showError(massEmailErrorMessage(err, t))
  }
}

watch(id, () => {
  poller.stop()
  detailController?.abort()
  recipientsController?.abort()
  if (searchTimer !== null) window.clearTimeout(searchTimer)
  campaign.value = null
  stats.value = null
  recipients.value = []
  activeTab.value = 'all'
  search.value = ''
  pagination.page = 1
  pagination.total = 0
  void Promise.all([loadDetail(false), loadRecipients(false)])
}, { immediate: true })

onBeforeUnmount(() => {
  disposed = true
  detailController?.abort()
  recipientsController?.abort()
  if (searchTimer !== null) window.clearTimeout(searchTimer)
})
</script>
