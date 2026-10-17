<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(kind === 'audit' ? 'enterprise.audit' : 'enterprise.balanceLogs') }}</h2><form v-if="kind === 'audit'" class="flex gap-2" @submit.prevent="searchLogs"><input v-model.trim="action" class="input" :aria-label="t('enterprise.action')" :placeholder="t('enterprise.action')" /><button class="btn btn-secondary" :disabled="busy">{{ t('enterprise.search') }}</button></form></div>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <DataTable :columns="columns" :data="rows" :loading="busy">
      <template #cell-type="{ value }">{{ t(value === 'add' ? 'enterprise.addBalance' : 'enterprise.subtractBalance') }}</template>
      <template #cell-amount="{ value }">${{ value.toFixed(4) }}</template>
      <template #cell-balance_before="{ value }">${{ value.toFixed(4) }}</template>
      <template #cell-balance_after="{ value }">${{ value.toFixed(4) }}</template>
      <template #cell-target="{ row }">{{ row.target_type }} #{{ row.target_id ?? '—' }}</template>
      <template #cell-detail="{ value }"><details><summary class="cursor-pointer text-primary-600 dark:text-primary-400">{{ t('enterprise.detail') }}</summary><pre class="mt-2 max-w-lg overflow-auto whitespace-pre-wrap break-all text-xs">{{ JSON.stringify(value, null, 2) }}</pre></details></template>
    </DataTable>
    <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; load()" @update:page-size="pageSize = $event; page = 1; load()" />
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseAuditLog, EnterpriseBalanceLog, EnterprisePage } from '@/types/enterprise'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; kind: 'audit' | 'balanceLogs'; refreshKey?: number }>()
const { t } = useI18n()
const { busy, error, read } = useEnterpriseAction()
const rows = ref<(EnterpriseAuditLog | EnterpriseBalanceLog)[]>([])
const page = ref(1), pageSize = ref(20), total = ref(0), action = ref('')
let appliedAction = ''
const columns = computed(() => [{ key: 'created_at', label: t('enterprise.createdAt') }, ...(props.kind === 'audit' ? [{ key: 'actor_email', label: t('enterprise.operator') }, { key: 'actor_role', label: t('enterprise.role') }, { key: 'action', label: t('enterprise.action') }, { key: 'target', label: t('enterprise.target') }, { key: 'detail', label: t('enterprise.detail') }] : [{ key: 'type', label: t('enterprise.operation') }, { key: 'amount', label: t('enterprise.amount') }, { key: 'balance_before', label: t('enterprise.balanceBefore') }, { key: 'balance_after', label: t('enterprise.balanceAfter') }, { key: 'operator_email', label: t('enterprise.operator') }, { key: 'notes', label: t('enterprise.notes') }])])
async function load() {
  const params = { page: page.value, page_size: pageSize.value, action: appliedAction || undefined }
  await read<EnterprisePage<EnterpriseAuditLog | EnterpriseBalanceLog>>(signal => {
    if (props.kind === 'audit') return props.api.auditLogs(params, { signal })
    if ('balanceLogs' in props.api) return props.api.balanceLogs(params, { signal })
    return Promise.reject({ code: 'ENTERPRISE_PERMISSION_DENIED' })
  }, result => { rows.value = result.items; total.value = result.total })
}
function searchLogs() { appliedAction = action.value; page.value = 1; void load() }
onMounted(load)
watch(() => props.refreshKey, load)
</script>
