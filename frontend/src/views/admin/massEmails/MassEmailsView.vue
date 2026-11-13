<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex-1 sm:max-w-64">
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.massEmails.searchPlaceholder')"
              class="input"
              @input="handleSearch"
            />
          </div>
          <Select v-model="filters.status" :options="statusFilterOptions" class="w-40" @change="handleFilterChange" />
          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button class="btn btn-secondary" :disabled="loading" :title="t('common.refresh')" @click="loadCampaigns">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button class="btn btn-primary" data-test="create-btn" @click="router.push('/admin/mass-emails/create')">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.massEmails.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="campaigns" :loading="loading">
          <template #cell-subject="{ value, row }">
            <div class="min-w-0">
              <router-link :to="`/admin/mass-emails/${row.id}`" class="truncate font-medium text-gray-900 hover:text-primary-600 dark:text-white">
                {{ value }}
              </router-link>
              <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">#{{ row.id }}</div>
            </div>
          </template>
          <template #cell-status="{ value }">
            <span :class="['badge', statusBadge(value)]">{{ t(`admin.massEmails.status.${value}`) }}</span>
          </template>
          <template #cell-progress="{ row }">
            <div class="w-36">
              <div class="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                <div class="h-full bg-primary-500 transition-all" :style="{ width: progressPercent(row) + '%' }"></div>
              </div>
              <div class="mt-1 text-xs text-gray-500">{{ row.sent_count + row.failed_count }} / {{ row.total_count }} ({{ progressPercent(row) }}%)</div>
            </div>
          </template>
          <template #cell-sent_count="{ value }"><span class="text-green-600">{{ value }}</span></template>
          <template #cell-failed_count="{ value }"><span :class="value > 0 ? 'text-red-600' : 'text-gray-500'">{{ value }}</span></template>
          <template #cell-opened_count="{ row }">
            <span>{{ row.opened_count }} <span class="text-xs text-gray-500">({{ openRatePercent(row.open_rate) }})</span></span>
          </template>
          <template #cell-scheduled_at="{ value }">
            <span class="text-sm text-gray-500">{{ value ? formatDateTime(value) : '-' }}</span>
          </template>
          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500">{{ formatDateTime(value) }}</span>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex items-center space-x-1">
              <button class="btn-icon" :title="t('admin.massEmails.actions.view')" @click="router.push(`/admin/mass-emails/${row.id}`)">
                <Icon name="eye" size="sm" />
              </button>
              <button v-if="isEditableStatus(row.status)" class="btn-icon" :title="t('admin.massEmails.actions.edit')" @click="router.push(`/admin/mass-emails/${row.id}/edit`)">
                <Icon name="edit" size="sm" />
              </button>
              <button v-if="canCancelStatus(row.status)" class="btn-icon" :title="t('admin.massEmails.actions.cancel')" @click="askCancel(row)">
                <Icon name="x" size="sm" />
              </button>
              <button v-if="row.status !== 'sending'" class="btn-icon text-red-500" :title="t('admin.massEmails.actions.delete')" @click="askDelete(row)">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState :title="t('admin.massEmails.empty')" :description="t('admin.massEmails.emptyDescription')" />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <ConfirmDialog
      :show="confirm.show"
      :title="confirm.kind === 'delete' ? t('admin.massEmails.confirm.deleteTitle') : t('admin.massEmails.confirm.cancelTitle')"
      :message="confirm.kind === 'delete' ? t('admin.massEmails.confirm.deleteMessage') : t('admin.massEmails.confirm.cancelMessage')"
      :danger="true"
      @confirm="runConfirm"
      @cancel="confirm.show = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { usePolling } from '@/composables/usePolling'
import { adminAPI } from '@/api/admin'
import type { MassEmailCampaign } from '@/api/admin/massEmails'
import { formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { canCancelStatus, isEditableStatus, openRatePercent, progressPercent } from './recipientForm'
import { massEmailErrorMessage } from './massEmailErrors'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const campaigns = ref<MassEmailCampaign[]>([])
const loading = ref(false)
const searchQuery = ref('')
const filters = reactive({ status: '' })
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const confirm = reactive<{ show: boolean; kind: 'delete' | 'cancel'; target: MassEmailCampaign | null }>({
  show: false,
  kind: 'delete',
  target: null
})

const statusFilterOptions = computed(() => [
  { value: '', label: t('common.all') },
  ...(['draft', 'scheduled', 'sending', 'completed', 'cancelled', 'failed'] as const).map((s) => ({
    value: s,
    label: t(`admin.massEmails.status.${s}`)
  }))
])

const columns = computed<Column[]>(() => [
  { key: 'subject', label: t('admin.massEmails.columns.subject') },
  { key: 'status', label: t('admin.massEmails.columns.status') },
  { key: 'progress', label: t('admin.massEmails.columns.progress') },
  { key: 'sent_count', label: t('admin.massEmails.columns.sent') },
  { key: 'failed_count', label: t('admin.massEmails.columns.failed') },
  { key: 'opened_count', label: t('admin.massEmails.columns.opened') },
  { key: 'scheduled_at', label: t('admin.massEmails.columns.scheduledAt') },
  { key: 'created_at', label: t('admin.massEmails.columns.createdAt') },
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
let listController: AbortController | null = null
async function fetchCampaigns(silent: boolean) {
  if (disposed || (silent && loading.value)) return
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  if (!silent) loading.value = true
  try {
    const res = await adminAPI.massEmails.list(pagination.page, pagination.page_size, {
      status: filters.status || undefined,
      search: searchQuery.value.trim() || undefined
    }, { signal: controller.signal })
    if (disposed || controller.signal.aborted) return
    campaigns.value = res.items
    pagination.total = res.total
  } catch (err) {
    if (!disposed && !controller.signal.aborted && !silent) appStore.showError(massEmailErrorMessage(err, t, 'admin.massEmails.messages.loadFailed'))
  } finally {
    if (listController === controller) loading.value = false
  }
}
const loadCampaigns = () => fetchCampaigns(false)

// Poll every 3s while any row is sending; stops when none are and on unmount.
const poller = usePolling(() => fetchCampaigns(true), 3000)
watch(
  campaigns,
  (rows) => {
    if (rows.some((r) => r.status === 'sending')) poller.start()
    else poller.stop()
  },
  { deep: false }
)

function handleFilterChange() {
  pagination.page = 1
  loadCampaigns()
}
function handlePageChange(page: number) {
  pagination.page = page
  loadCampaigns()
}
function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  loadCampaigns()
}
let searchTimer: number | null = null
function handleSearch() {
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(handleFilterChange, 300)
}

function askDelete(row: MassEmailCampaign) {
  confirm.kind = 'delete'
  confirm.target = row
  confirm.show = true
}
function askCancel(row: MassEmailCampaign) {
  confirm.kind = 'cancel'
  confirm.target = row
  confirm.show = true
}
async function runConfirm() {
  const target = confirm.target
  confirm.show = false
  if (!target) return
  try {
    if (confirm.kind === 'delete') {
      await adminAPI.massEmails.remove(target.id)
      appStore.showSuccess(t('admin.massEmails.messages.deleted'))
    } else {
      await adminAPI.massEmails.cancel(target.id)
      appStore.showSuccess(t('admin.massEmails.messages.cancelled'))
    }
    await loadCampaigns()
  } catch (err) {
    appStore.showError(massEmailErrorMessage(err, t))
  }
}

onMounted(loadCampaigns)
onBeforeUnmount(() => {
  disposed = true
  listController?.abort()
  if (searchTimer !== null) window.clearTimeout(searchTimer)
})
</script>
