<template>
  <Card class="min-w-0 overflow-hidden" data-test="usage-details">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 p-4 dark:border-dark-700">
      <div><h3 class="font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usageAnalysis.details') }}</h3><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('enterprise.usageAnalysis.recordCount', { count: total.toLocaleString() }) }}</p></div>
      <div class="flex flex-wrap items-center gap-2"><slot name="actions" :columns="visible" /><DropdownMenu>
        <DropdownMenuTrigger as-child><Button type="button" variant="outline" size="sm" data-test="usage-column-settings"><Columns3 class="mr-2 size-4" />{{ t('admin.users.columnSettings') }}</Button></DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="max-h-80 w-60 overflow-y-auto">
          <DropdownMenuCheckboxItem v-for="column in columns" :key="column.key" :model-value="column.key === 'created_at' || !hidden.includes(column.key)" :disabled="column.key === 'created_at'" :data-test="`usage-column-toggle-${column.key}`" @select.prevent @update:model-value="toggle(column.key)">{{ column.label }}</DropdownMenuCheckboxItem>
        </DropdownMenuContent>
      </DropdownMenu></div>
    </div>
    <UsageTable :data="normalized" :columns="visible" :loading="loading" mobile-layout="table" :sticky-first-column="false" :server-side-sort="true" :show-account-billing="false" :show-upstream-endpoint="false" default-sort-key="created_at" default-sort-order="desc" flat @sort="(key, order) => emit('sort', key, order)">
      <template #cell-member="{ row }">
        <div class="max-w-40 leading-tight" data-test="usage-row-member">
          <p class="truncate font-medium" :title="row.user?.username || row.user?.email || row.member">{{ row.user?.username || row.user?.email || row.member }}</p>
          <p v-if="row.user?.username && row.user?.email" class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400" :title="row.user.email">{{ row.user.email }}</p>
        </div>
      </template>
    </UsageTable>
  </Card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import type { EnterpriseDepartment, EnterpriseUsageLog } from '@/types/enterprise'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import { Columns3 } from '@lucide/vue'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger, DropdownMenuCheckboxItem } from '@/components/ui/dropdown-menu'
import { getUsageServiceTierLabel } from '@/utils/usageServiceTier'
import { normalizeEnterpriseUsage, useEnterpriseUsageColumns } from '../useEnterpriseUsage'
const props = defineProps<{ rows: EnterpriseUsageLog[]; departments: EnterpriseDepartment[]; total: number; loading: boolean; admin?: boolean }>()
const emit = defineEmits<{ sort: [key: string, order: 'asc' | 'desc'] }>()
const { t } = useI18n()
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

<style scoped>
:deep(tbody td) {
  @apply px-3 py-2;
}
:deep(tbody td .space-y-0\.5) {
  @apply space-y-0 leading-tight;
}
:deep(tbody td .grid.text-xs) {
  @apply gap-y-0 leading-tight;
}
</style>
