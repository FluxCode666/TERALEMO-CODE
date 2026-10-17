<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('enterprise.quotas') }}</h2><p class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.quotaHint') }}</p></div>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <DataTable :columns="columns" :data="quotas" :loading="busy">
      <template #cell-scope_type="{ row }">{{ scopeLabel(row) }}</template>
      <template v-for="window in windows" :key="window" #[`cell-${window}`]="{ row }"><span class="tabular-nums">${{ row[`${window}_usage_usd`].toFixed(4) }} / {{ row[`${window}_limit_usd`] === null ? t('enterprise.unlimited') : `$${row[`${window}_limit_usd`]}` }}</span></template>
      <template #cell-actions="{ row }"><div class="flex gap-3"><button v-if="canEdit(row)" class="text-primary-600 dark:text-primary-400" :data-test="`quota-edit-${row.scope_type}-${row.scope_id}`" @click="edit(row)">{{ t('enterprise.edit') }}</button><button v-if="canReset" class="text-red-600 dark:text-red-400" :data-test="`quota-reset-${row.scope_type}-${row.scope_id}`" @click="resetTarget = row; resetWindows = ['total']">{{ t('enterprise.resetUsage') }}</button></div></template>
    </DataTable>
    <BaseDialog :show="!!editing" :title="t('enterprise.quotas')" @close="editing = null">
      <form class="space-y-4" @submit.prevent="save">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.quotaHint') }}</p>
        <label v-for="window in windows" :key="window" class="block"><span class="input-label">{{ t(`enterprise.${window}`) }} (USD)</span><input v-model="limits[window]" type="number" min="0" step="any" class="input" :placeholder="t('enterprise.unlimited')" :data-test="`quota-${window}`" /></label>
        <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
        <button class="btn btn-primary" :disabled="busy">{{ t('enterprise.save') }}</button>
      </form>
    </BaseDialog>
    <ConfirmDialog :show="!!resetTarget" :title="t('enterprise.resetUsage')" :message="t('enterprise.resetConfirm')" danger @cancel="resetTarget = null" @confirm="reset">
      <div class="flex flex-wrap gap-4"><label v-for="window in windows" :key="window" class="flex items-center gap-2"><input v-model="resetWindows" type="checkbox" :value="window" />{{ t(`enterprise.${window}`) }}</label></div>
    </ConfirmDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseQuota, EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, QuotaWindow, QuotaLimits } from '@/types/enterprise'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { loadMemberOptions } from '../memberOptions'
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run } = useEnterpriseAction()
const quotas = ref<EnterpriseQuota[]>([]), departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const editing = ref<EnterpriseQuota | null>(null), resetTarget = ref<EnterpriseQuota | null>(null)
const resetWindows = ref<QuotaWindow[]>(['total'])
const windows: QuotaWindow[] = ['daily', 'weekly', 'monthly', 'total']
const limits = reactive<Record<QuotaWindow, string | number>>({ daily: '', weekly: '', monthly: '', total: '' })
const validation = ref('')
const canReset = computed(() => !props.readonly && (props.admin || props.overview?.permissions.edit_quotas))
const columns = computed(() => [{ key: 'scope_type', label: t('enterprise.quotaScope') }, ...windows.map(window => ({ key: window, label: `${t(`enterprise.${window}`)} (USD)` })), { key: 'actions', label: t('enterprise.actions') }])
function scopeLabel(quota: EnterpriseQuota) {
  const name = quota.scope_type === 'department' ? departments.value.find(item => item.id === quota.scope_id)?.name : quota.scope_type === 'member' ? members.value.find(item => item.id === quota.scope_id)?.username : ''
  return `${t(`enterprise.${quota.scope_type}Scope`)}${name ? ` · ${name}` : ` #${quota.scope_id}`}`
}
function canEdit(quota: EnterpriseQuota) {
  if (props.readonly) return false
  if (props.admin || props.overview?.permissions.edit_quotas) return true
  return quota.scope_type === 'member' && !!props.overview?.permissions.edit_member_quota && members.value.some(member => member.id === quota.scope_id && member.role === 'member' && member.department_id === props.overview?.me.department_id)
}
async function load() {
  await run(async () => {
    const [rows, departmentRows, memberRows] = await Promise.all([props.api.quotas(), props.api.departments(), props.admin || props.overview?.permissions.view_members ? loadMemberOptions(props.api) : Promise.resolve([])])
    quotas.value = rows
    departments.value = departmentRows
    members.value = memberRows
  })
}
function edit(quota: EnterpriseQuota) {
  editing.value = quota; validation.value = ''
  for (const window of windows) limits[window] = quota[`${window}_limit_usd`] === null ? '' : String(quota[`${window}_limit_usd`])
}
async function save() {
  if (!editing.value) return
  if (windows.some(window => String(limits[window]).trim() !== '' && (!Number.isFinite(Number(limits[window])) || Number(limits[window]) < 0))) { validation.value = t('enterprise.validationQuota'); return }
  const body = Object.fromEntries(windows.map(window => [`${window}_limit_usd`, String(limits[window]).trim() === '' ? null : Number(limits[window])])) as QuotaLimits
  const target = editing.value
  if (await run(() => props.api.updateQuota(target.scope_type, target.scope_id, body), t('enterprise.saved'))) { editing.value = null; emit('updated'); await load() }
}
async function reset() {
  if (!resetTarget.value || !resetWindows.value.length) return
  const target = resetTarget.value
  if (await run(() => props.api.resetQuota(target.scope_type, target.scope_id, resetWindows.value), t('enterprise.saved'))) { resetTarget.value = null; emit('updated'); await load() }
}
onMounted(load)
</script>
