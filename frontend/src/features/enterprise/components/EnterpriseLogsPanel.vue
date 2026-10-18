<template>
  <section class="shadcn-scope space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold text-foreground">{{ t(kind === 'audit' ? 'enterprise.audit' : 'enterprise.balanceLogs') }}</h2><form v-if="kind === 'audit'" class="flex w-full flex-wrap gap-2 sm:w-auto" @submit.prevent="searchLogs"><EnterpriseSelect v-model="action" :options="actionOptions" class="min-w-0 flex-1 sm:w-56" :aria-label="t('enterprise.action')" /><Button variant="outline"  :disabled="busy">{{ t('enterprise.search') }}</Button></form></div>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <Button variant="ghost" class="underline" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <EnterpriseTable :columns="columns" :data="rows" :loading="busy">
      <template #cell-created_at="{ row }">{{ formatDateTime(row.created_at) }}</template>
      <template #cell-type="{ value }">{{ t(value === 'add' ? 'enterprise.addBalance' : 'enterprise.subtractBalance') }}</template>
      <template #cell-amount="{ row }">${{ ('amount' in row ? row.amount : 0).toFixed(4) }}</template>
      <template #cell-balance_before="{ row }">${{ ('balance_before' in row ? row.balance_before : 0).toFixed(4) }}</template>
      <template #cell-balance_after="{ row }">${{ ('balance_after' in row ? row.balance_after : 0).toFixed(4) }}</template>
      <template #cell-action="{ value }">{{ actionLabel(String(value)) }}</template>
      <template #cell-actor_role="{ value }">{{ t(value === 'admin' ? 'enterprise.auditUi.admin' : `enterprise.roles.${value}`) }}</template>
      <template #cell-target="{ row }">{{ targetLabel('target_type' in row ? row.target_type : '') }} #{{ ('target_id' in row ? row.target_id : null) ?? '—' }}</template>
      <template #empty><EnterpriseEmpty :title="t('enterprise.empty')" /></template>
      <template #cell-detail="{ row }"><Button variant="outline" size="sm" :data-test="`audit-detail-${row.id}`" @click="selectedLog = 'action' in row ? row : null">{{ t('enterprise.auditDetails.view') }}</Button></template>
    </EnterpriseTable>
    <EnterpriseAuditDetail :log="selectedLog" :action-label="selectedLog ? actionLabel(selectedLog.action) : ''" :target-label="selectedLog ? targetLabel(selectedLog.target_type) : ''" @close="selectedLog = null" />
    <EnterprisePagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; load()" @update:page-size="pageSize = $event; page = 1; load()" />
  </section>
</template>
<script setup lang="ts">
import { Button } from '@/components/ui/button'

import EnterpriseAuditDetail from './EnterpriseAuditDetail.vue'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseAuditLog, EnterpriseBalanceLog, EnterprisePage } from '@/types/enterprise'
import { formatDateTime } from '@/utils/format'
import EnterpriseTable from '@/features/enterprise/components/EnterpriseTable.vue'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
import EnterpriseEmpty from '@/features/enterprise/components/EnterpriseEmpty.vue'
import EnterprisePagination from '@/features/enterprise/components/EnterprisePagination.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; kind: 'audit' | 'balanceLogs'; refreshKey?: number }>()
const { t } = useI18n()
const { busy, error, read } = useEnterpriseAction()
const selectedLog = ref<EnterpriseAuditLog | null>(null)
const rows = ref<(EnterpriseAuditLog | EnterpriseBalanceLog)[]>([])
const page = ref(1), pageSize = ref(20), total = ref(0), action = ref('')
const auditActions = ['enterprise.create', 'enterprise.update', 'enterprise.delete', 'balance.adjust', 'department.create', 'department.update', 'department.delete', 'member.add', 'member.create', 'member.update', 'member.remove', 'member.disabled_by_moderation', 'quota.update', 'quota.reset', 'settings.update', 'group_pool.update', 'group_whitelist.update']
const actionOptions = computed(() => [{ value: '', label: t('enterprise.all') }, ...auditActions.map(value => ({ value, label: actionLabel(String(value)) }))])
function actionLabel(value: string) { return t(`enterprise.auditUi.actions.${auditActions.includes(value) ? value.replace('.', '_') : 'other'}`) }
function targetLabel(value: string) { return t(`enterprise.${['enterprise', 'department', 'member'].includes(value) ? `${value}Scope` : 'target'}`) }
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
