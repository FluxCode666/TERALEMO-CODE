<template>
  <div class="card min-w-0" data-test="usage-details">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 p-4 dark:border-dark-700">
      <div><h3 class="font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usageAnalysis.details') }}</h3><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('enterprise.usageAnalysis.recordCount', { count: total.toLocaleString() }) }}</p></div>
      <div ref="dropdown" class="relative" @keydown.esc="showColumns = false">
        <button type="button" data-test="usage-column-settings" class="btn btn-secondary" :aria-expanded="showColumns" @click="showColumns = !showColumns"><Icon name="grid" size="sm" />{{ t('admin.users.columnSettings') }}</button>
        <div v-if="showColumns" class="absolute right-0 top-full z-50 mt-1 max-h-80 w-56 overflow-y-auto rounded-lg border border-gray-200 bg-white py-2 shadow-lg dark:border-dark-600 dark:bg-dark-800">
          <label v-for="column in columns" :key="column.key" class="flex cursor-pointer items-center gap-3 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-dark-700"><input type="checkbox" :data-test="`usage-column-toggle-${column.key}`" :checked="column.key === 'created_at' || !hidden.includes(column.key)" :disabled="column.key === 'created_at'" @change="toggle(column.key)" /><span>{{ column.label }}</span></label>
        </div>
      </div>
    </div>
    <UsageTable :data="normalized" :columns="visible" :loading="loading" mobile-layout="table" :sticky-first-column="false" :server-side-sort="true" :show-account-billing="false" :show-upstream-endpoint="false" default-sort-key="created_at" default-sort-order="desc" flat @sort="(key, order) => emit('sort', key, order)" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { onClickOutside } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import type { EnterpriseDepartment, EnterpriseUsageLog } from '@/types/enterprise'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import Icon from '@/components/icons/Icon.vue'
import { getUsageServiceTierLabel } from '@/utils/usageServiceTier'
import { normalizeEnterpriseUsage, useEnterpriseUsageColumns } from '../useEnterpriseUsage'
const props = defineProps<{ rows: EnterpriseUsageLog[]; departments: EnterpriseDepartment[]; total: number; loading: boolean; admin?: boolean }>()
const emit = defineEmits<{ sort: [key: string, order: 'asc' | 'desc'] }>()
const { t } = useI18n()
const dropdown = ref<HTMLElement>()
const showColumns = ref(false)
onClickOutside(dropdown, () => { showColumns.value = false })
const columns = computed<Column[]>(() => [
  { key: 'member', label: t('enterprise.members') },
  { key: 'department_name', label: t('enterprise.department') },
  { key: 'api_key', label: t('usage.apiKeyFilter') },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort') },
  { key: 'endpoint', label: t('usage.endpoint') },
  { key: 'ip_address', label: 'IP' },
  { key: 'group', label: t('admin.usage.group') },
  { key: 'stream', label: t('usage.type') },
  { key: 'billing_mode', label: t('admin.usage.billingMode') },
  { key: 'service_tier', label: t('usage.serviceTier'), formatter: value => value ? getUsageServiceTierLabel(value, t) : '—' },
  { key: 'billing_type', label: t('admin.usage.billingType'), formatter: value => value === 1 ? t('admin.usage.billingTypeSubscription') : value === 0 ? t('admin.usage.billingTypeBalance') : '—' },
  { key: 'tokens', label: t('usage.tokens') },
  { key: 'cost', label: t('usage.cost') },
  { key: 'latency', label: t('usage.latency') },
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'request_id', label: t('enterprise.usageAnalysis.requestId') },
  { key: 'media_type', label: t('enterprise.usageAnalysis.mediaType') },
  { key: 'user_agent', label: t('usage.userAgent') }
])
const { hidden, toggle, visible } = useEnterpriseUsageColumns(computed(() => !!props.admin), columns)
const normalized = computed(() => props.rows.map(row => normalizeEnterpriseUsage(row, props.departments, t('enterprise.noDepartment'))))
</script>
