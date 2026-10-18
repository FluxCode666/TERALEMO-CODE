<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Info, RefreshCw, Search } from '@lucide/vue'
import { Card, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import { Skeleton } from '@/components/ui/skeleton'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseQuota, EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, QuotaWindow, QuotaLimits, BatchMemberQuotaResult } from '@/types/enterprise'
import { getBrowserTimeZone } from '@/utils/format'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { loadMemberOptions } from '../memberOptions'
import { buildDepartmentTree, departmentAncestors, departmentSubtreeIds } from '../departmentTree'
import { quotaWindows as windows, quotaLimitBody, type QuotaTableRow } from '../quotaPresentation'
import EnterpriseDialog from './EnterpriseDialog.vue'
import EnterpriseConfirm from './EnterpriseConfirm.vue'
import EnterprisePagination from './EnterprisePagination.vue'
import EnterpriseQuotaTable from './EnterpriseQuotaTable.vue'
const props = defineProps<{ api: EnterpriseResourceAPI; enterpriseName?: string; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const timezone = getBrowserTimeZone()
const quotas = ref<EnterpriseQuota[]>([]), departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const loaded = ref(false), editing = ref<EnterpriseQuota | null>(null), resetTarget = ref<EnterpriseQuota | null>(null)
const resetWindows = ref<QuotaWindow[]>(['total'])
const limits = reactive<Record<QuotaWindow, string | number>>({ daily: '', weekly: '', monthly: '', total: '' })
const validation = ref(''), memberSearch = ref(''), page = ref(1), pageSize = ref(10)
const departmentOpen = reactive<Record<number, boolean>>({})
const selected = ref<number[]>([]), batchOpen = ref(false), batchFields = ref<QuotaWindow[]>(['daily']), batchResult = ref<BatchMemberQuotaResult>()
const canReset = computed(() => !props.readonly && !!(props.admin || props.overview?.permissions.edit_quotas))
function canEdit(quota: EnterpriseQuota) {
  if (props.readonly) return false
  if (props.admin || props.overview?.permissions.edit_quotas) return true
  return quota.scope_type === 'member' && !!props.overview?.permissions.edit_member_quota && members.value.some(member => member.id === quota.scope_id && member.role === 'member' && departmentSubtreeIds(departments.value, props.overview?.me.department_id).has(member.department_id ?? 0))
}
const enterpriseRows = computed<QuotaTableRow[]>(() => quotas.value.filter(row => row.scope_type === 'enterprise').map(quota => ({ id: quota.scope_id, label: props.enterpriseName || props.overview?.enterprise.name || t('enterprise.enterpriseScope'), quota })))
const departmentRows = computed(() => {
  const needed = new Set(quotas.value.filter(row => row.scope_type === 'department').flatMap(row => [row.scope_id, ...departmentAncestors(departments.value, row.scope_id).map(department => department.id)]))
  const result: (QuotaTableRow & { ancestors: number[] })[] = []
  const visit = (nodes: ReturnType<typeof buildDepartmentTree>, level = 0, ancestors: number[] = []) => nodes.forEach(node => {
    if (needed.has(node.department.id)) result.push({ id: node.department.id, label: node.department.name, level, ancestors, quota: quotas.value.find(row => row.scope_type === 'department' && row.scope_id === node.department.id), hasChildren: node.children.some(child => needed.has(child.department.id)), expanded: departmentOpen[node.department.id] !== false })
    visit(node.children, level + 1, [...ancestors, node.department.id])
  })
  visit(buildDepartmentTree(departments.value))
  for (const quota of quotas.value.filter(row => row.scope_type === 'department')) if (!result.some(row => row.id === quota.scope_id)) result.push({ id: quota.scope_id, label: props.overview?.me.department_id === quota.scope_id ? props.overview.me.department_name || `#${quota.scope_id}` : `#${quota.scope_id}`, quota, ancestors: [] })
  return result
})
const visibleDepartmentRows = computed(() => departmentRows.value.filter(row => row.ancestors.every(id => departmentOpen[id] !== false)))
function memberLabel(id: number) { const member = members.value.find(row => row.id === id); return member?.username || member?.email || (props.overview?.me.member_id === id ? t('enterprise.quotaTable.me') : `#${id}`) }
const memberRows = computed<QuotaTableRow[]>(() => quotas.value.filter(row => row.scope_type === 'member').map(quota => {
  const member = members.value.find(row => row.id === quota.scope_id)
  return { id: quota.scope_id, quota, label: memberLabel(quota.scope_id), username: member?.username ? `@${member.username}` : undefined, self: props.overview?.me.member_id === quota.scope_id }
}))
const filteredMembers = computed(() => memberRows.value.filter(row => `${row.label} ${row.username ?? ''}`.toLocaleLowerCase().includes(memberSearch.value.trim().toLocaleLowerCase())))
const pageMembers = computed(() => filteredMembers.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
const sections = computed(() => [
  { kind: 'enterprise', rows: enterpriseRows.value, total: enterpriseRows.value.length },
  { kind: 'department', rows: visibleDepartmentRows.value, total: departmentRows.value.length },
  { kind: 'member', rows: pageMembers.value, total: filteredMembers.value.length }
])
const selectedEditable = computed(() => selected.value.filter(id => memberRows.value.some(row => row.id === id && row.quota && canEdit(row.quota))))
function editableCount(rows: QuotaTableRow[]) { return rows.filter(row => row.quota && canEdit(row.quota)).length }
function select(id: number, checked: boolean) { selected.value = checked ? [...new Set([...selected.value, id])] : selected.value.filter(item => item !== id) }
function selectPage(checked: boolean) { pageMembers.value.filter(row => row.quota && canEdit(row.quota)).forEach(row => select(row.id, checked)) }
watch([memberSearch, pageSize], () => { page.value = 1 })
watch(filteredMembers, rows => { page.value = Math.min(page.value, Math.max(1, Math.ceil(rows.length / pageSize.value))) })
watch(selectedEditable, ids => { if (ids.length !== selected.value.length) selected.value = ids })
async function load() {
  await read(signal => Promise.all([props.api.quotas(undefined, { signal }), props.api.departments({ signal }), props.admin || props.overview?.permissions.view_members || props.overview?.permissions.edit_member_quota ? loadMemberOptions(props.api, signal) : Promise.resolve([])]), ([rows, departmentRows, memberRows]) => { quotas.value = rows; departments.value = departmentRows; members.value = memberRows; loaded.value = true })
}
async function changed() { emit('updated'); await load() }
function edit(quota: EnterpriseQuota) {
  if (!canEdit(quota)) return
  editing.value = quota; validation.value = ''
  for (const window of windows) limits[window] = quota[`${window}_limit_usd`] ?? ''
}
function openBatch() {
  if (!selectedEditable.value.length) return
  batchResult.value = undefined; batchFields.value = ['daily']; validation.value = ''; windows.forEach(window => { limits[window] = '' }); batchOpen.value = true
}
function setField(window: QuotaWindow, checked: boolean) { batchFields.value = checked ? [...new Set([...batchFields.value, window])] : batchFields.value.filter(item => item !== window) }
async function save() {
  const fields = batchOpen.value ? batchFields.value : windows
  if (!fields.length) { validation.value = t('enterprise.quotaTable.fieldsRequired'); return }
  const body = quotaLimitBody(limits, fields)
  if (!body) { validation.value = t('enterprise.validationQuota'); return }
  if (batchOpen.value) {
    if (!selectedEditable.value.length || batchResult.value) return
    if (await run(async () => { batchResult.value = await props.api.batchMemberQuotas({ member_ids: [...selectedEditable.value], fields: [...fields], ...body }) })) { selected.value = batchResult.value!.failed.map(row => row.member_id); await changed() }
  } else {
    const target = editing.value
    if (!target || !canEdit(target)) return
    if (await run(() => props.api.updateQuota(target.scope_type, target.scope_id, body as QuotaLimits), t('enterprise.saved'))) { editing.value = null; await changed() }
  }
}
async function reset() {
  const target = resetTarget.value
  if (!target || !resetWindows.value.length || !canReset.value) return
  if (await run(() => props.api.resetQuota(target.scope_type, target.scope_id, resetWindows.value), t('enterprise.saved'))) { resetTarget.value = null; await changed() }
}
function closeEdit() { if (!busy.value) { editing.value = null; batchOpen.value = false } }
onMounted(load)
watch(() => props.refreshKey, load)
</script>
<template>
  <section class="shadcn-scope min-w-0 space-y-6" :aria-busy="busy">
    <header class="flex flex-wrap items-start justify-between gap-4"><div class="min-w-0 flex-1"><p class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">QUOTAS</p><h2 class="mt-2 text-3xl font-semibold tracking-tight">{{ t('enterprise.quotaTable.title') }}</h2><p class="mt-3 text-sm leading-relaxed text-muted-foreground">{{ t('enterprise.quotaTable.description', { timezone }) }}</p></div><Button variant="outline" :disabled="busy" data-test="quota-refresh" @click="load"><RefreshCw class="mr-2 size-4" />{{ t('enterprise.refresh') }}</Button></header>
    <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }} <Button variant="ghost" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <div v-if="busy && !loaded" class="space-y-4"><Skeleton v-for="n in 3" :key="n" class="h-40" /></div>
    <template v-else>
      <Card v-for="section in sections" :key="section.kind" class="min-w-0 overflow-hidden" :data-test="`quota-section-${section.kind}`">
        <CardHeader class="flex flex-wrap flex-row items-start justify-between gap-3 space-y-0 p-4 sm:p-5"><div><CardTitle class="text-base">{{ t(`enterprise.quotaSections.${section.kind}.title`) }}</CardTitle><CardDescription class="mt-1">{{ t('enterprise.quotaTable.count', { total: section.total, editable: editableCount(section.rows) }) }}</CardDescription></div><div v-if="section.kind === 'member'" class="relative w-full sm:w-64"><Search class="pointer-events-none absolute left-3 top-3 size-4 text-muted-foreground" /><Input v-model="memberSearch" type="search" class="pl-9" :placeholder="t('enterprise.quotaTable.search')" :aria-label="t('enterprise.quotaTable.search')" /></div></CardHeader>
        <div v-if="section.kind === 'member' && selectedEditable.length" class="mx-4 mb-4 flex flex-wrap items-center gap-2 rounded-ui-md border border-primary/20 bg-primary/5 p-3 text-sm" data-test="quota-batch-toolbar"><span class="mr-auto">{{ t('enterprise.quotaTable.selected', { count: selectedEditable.length }) }}</span><Button variant="outline" size="sm" :disabled="busy" data-test="quota-batch-open" @click="openBatch">{{ t('enterprise.quotaTable.batch') }}</Button><Button variant="ghost" size="sm" :disabled="busy" @click="selected = []">{{ t('enterprise.quotaTable.deselect') }}</Button></div>
        <EnterpriseQuotaTable :kind="section.kind" :rows="section.rows" :can-edit="canEdit" :can-reset="canReset" :selectable="section.kind === 'member'" :selected="selectedEditable" :busy="busy" @edit="edit" @reset="row => { resetTarget = row; resetWindows = ['total'] }" @toggle="id => departmentOpen[id] = departmentOpen[id] === false" @select="select" @select-page="selectPage" />
        <div v-if="section.kind === 'member' && filteredMembers.length" class="border-t p-4"><EnterprisePagination v-model:page="page" v-model:page-size="pageSize" :total="filteredMembers.length" /></div>
      </Card>
    </template>
    <aside class="flex items-start gap-3 rounded-ui-lg border bg-muted/30 p-4 sm:p-5" data-test="quota-statistics-note"><Info class="mt-0.5 size-5 shrink-0 text-muted-foreground" /><div class="space-y-2 text-sm leading-relaxed text-muted-foreground"><h3 class="font-medium text-foreground">{{ t('enterprise.quotaTable.statistics') }}</h3><p>{{ t('enterprise.quotaTable.balanceOnly') }}</p><p>{{ t('enterprise.quotaTable.periods') }}</p><p>{{ t('enterprise.quotaTable.timezone', { timezone }) }}</p><p>{{ t('enterprise.quotaTable.subtree') }}</p><p>{{ t('enterprise.quotaTable.delay') }}</p></div></aside>
    <EnterpriseDialog :show="!!editing || batchOpen" :title="t(batchOpen ? 'enterprise.quotaTable.batch' : 'enterprise.quotaTable.edit')" @close="closeEdit">
      <div v-if="batchOpen && batchResult" class="space-y-4" data-test="quota-batch-result"><p role="status">{{ t('enterprise.quotaTable.result', { updated: batchResult.updated.length, failed: batchResult.failed.length }) }}</p><ul v-if="batchResult.failed.length" class="space-y-2"><li v-for="failure in batchResult.failed" :key="failure.member_id" class="rounded-ui-md border border-destructive/20 p-3 text-sm"><p class="font-medium">{{ memberLabel(failure.member_id) }}</p><p class="mt-1 break-words text-destructive">{{ failure.message || t('enterprise.errors.generic') }}</p></li></ul><Button :disabled="busy" @click="closeEdit">{{ t('enterprise.close') }}</Button></div>
      <form v-else class="space-y-4" data-test="quota-edit-form" @submit.prevent="save"><p class="text-sm text-muted-foreground">{{ t('enterprise.quotaTable.editHint') }}</p><div v-for="window in windows" :key="window" class="space-y-2"><Label class="flex items-center gap-2"><Checkbox v-if="batchOpen" :model-value="batchFields.includes(window)" :data-test="`quota-batch-field-${window}`" :disabled="busy" @update:model-value="setField(window, $event === true)" /><span>{{ t(`enterprise.${window}`) }} (USD)</span></Label><Input v-model="limits[window]" :disabled="busy || batchOpen && !batchFields.includes(window)" type="number" min="0" step="any" :aria-label="`${t(`enterprise.${window}`)} (USD)`" :placeholder="t('enterprise.unlimited')" :data-test="`quota-${window}`" /></div><p v-if="validation || error" role="alert" class="text-sm text-destructive">{{ validation || error }}</p><Button :disabled="busy" data-test="quota-save">{{ t('enterprise.save') }}</Button></form>
    </EnterpriseDialog>
    <EnterpriseConfirm :busy="busy" :show="!!resetTarget" :disabled="!resetWindows.length" :title="t('enterprise.resetUsage')" :message="t('enterprise.resetConfirm')" danger @cancel="resetTarget = null" @confirm="reset"><div class="flex flex-wrap gap-4"><Label v-for="window in windows" :key="window" class="flex items-center gap-2"><Switch :model-value="resetWindows.includes(window)" :aria-label="t(`enterprise.${window}`)" :data-test="`quota-reset-window-${window}`" @update:model-value="checked => resetWindows = checked ? [...new Set([...resetWindows, window])] : resetWindows.filter(item => item !== window)" />{{ t(`enterprise.${window}`) }}</Label></div></EnterpriseConfirm>
  </section>
</template>
