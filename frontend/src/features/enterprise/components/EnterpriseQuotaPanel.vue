<template>
  <section class="shadcn-scope space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold text-foreground">{{ t('enterprise.quotas') }}</h2><p class="text-sm text-muted-foreground">{{ t('enterprise.quotaHint') }}</p></div>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <Button variant="ghost" class="underline" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <div v-if="busy && !quotas.length" class="grid gap-4" aria-busy="true"><Skeleton v-for="n in 3" :key="n" class="h-40" /></div>
    <template v-else>
      <section v-for="kind in ['enterprise', 'department'] as const" :key="kind" class="space-y-3" :data-test="`quota-section-${kind}`">
        <div><h2 class="text-lg font-semibold text-foreground">{{ t(`enterprise.quotaSections.${kind}.title`) }}</h2><p class="mt-1 text-sm text-muted-foreground">{{ t(`enterprise.quotaSections.${kind}.description`) }}</p></div>
        <EnterpriseQuotaDisplay v-for="row in quotas.filter(item => item.scope_type === kind)" :key="row.scope_id" :quota="row" :title="scopeLabel(row)">
          <template #actions><Button variant="ghost" v-if="canEdit(row)" class="text-primary" :data-test="`quota-edit-${row.scope_type}-${row.scope_id}`" @click="edit(row)">{{ t('enterprise.edit') }}</Button><Button variant="ghost" v-if="canReset" class="text-red-600 dark:text-red-400" :data-test="`quota-reset-${row.scope_type}-${row.scope_id}`" @click="resetTarget = row; resetWindows = ['total']">{{ t('enterprise.resetUsage') }}</Button></template>
        </EnterpriseQuotaDisplay>
        <Card v-if="!quotas.some(item => item.scope_type === kind)" class="rounded-ui-lg border bg-card text-card-foreground shadow-sm"><EnterpriseEmpty :title="t('enterprise.empty')" :description="t('enterprise.quotaSections.empty')" /></Card>
      </section>
      <section class="space-y-3" data-test="quota-section-member">
        <div class="flex flex-wrap items-center justify-between gap-3"><div><h2 class="text-lg font-semibold text-foreground">{{ t('enterprise.quotaSections.member.title') }}</h2><p class="mt-1 text-sm text-muted-foreground">{{ t('enterprise.quotaSections.member.description') }}</p></div><Input type="search" v-model="memberSearch" class="sm:max-w-xs" :aria-label="t('enterprise.organizationUi.searchMembers')" :placeholder="t('enterprise.organizationUi.searchMembers')" /></div>
        <Collapsible v-for="section in memberSections" :key="section.id" class="rounded-xl border border-gray-200 bg-gray-50/50 dark:border-dark-700 dark:bg-dark-900/30" :open="memberSectionOpen(section.id, section.rows.length)" @update:open="value => sectionOpen[section.id] = value">
          <CollapsibleTrigger data-test="quota-member-toggle" class="flex w-full items-center justify-between px-4 py-4 text-left text-sm font-medium text-foreground">{{ section.name }} <span class="ml-2 text-muted-foreground">{{ section.rows.length }}</span><ChevronDown class="size-4" /></CollapsibleTrigger>
          <CollapsibleContent class="space-y-3 overflow-hidden px-3 pb-3 data-[state=open]:animate-collapsible-down data-[state=closed]:animate-collapsible-up motion-reduce:animate-none"><EnterpriseQuotaDisplay v-for="row in section.rows" :key="row.scope_id" :quota="row" :title="scopeLabel(row)"><template #actions><Button variant="ghost" v-if="canEdit(row)" class="text-primary" :data-test="`quota-edit-member-${row.scope_id}`" @click="edit(row)">{{ t('enterprise.edit') }}</Button><Button variant="ghost" v-if="canReset" class="text-red-600 dark:text-red-400" :data-test="`quota-reset-member-${row.scope_id}`" @click="resetTarget = row; resetWindows = ['total']">{{ t('enterprise.resetUsage') }}</Button></template></EnterpriseQuotaDisplay></CollapsibleContent>
        </Collapsible>
        <Card v-if="!memberSections.length" class="rounded-ui-lg border bg-card text-card-foreground shadow-sm"><EnterpriseEmpty :title="t('enterprise.empty')" :description="t('enterprise.quotaSections.empty')" /></Card>
      </section>
    </template>
    <EnterpriseDialog :show="!!editing" :title="t('enterprise.quotas')" @close="editing = null">
      <form class="space-y-4" @submit.prevent="save">
        <p class="text-sm text-muted-foreground">{{ t('enterprise.quotaHint') }}</p>
        <Label v-for="window in windows" :key="window" class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t(`enterprise.${window}`) }} (USD)</span><Input v-model="limits[window]" type="number" min="0" step="any"  :placeholder="t('enterprise.unlimited')" :data-test="`quota-${window}`" /></Label>
        <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
        <Button variant="default"  :disabled="busy">{{ t('enterprise.save') }}</Button>
      </form>
    </EnterpriseDialog>
    <EnterpriseConfirm :busy="busy" :show="!!resetTarget" :disabled="!resetWindows.length" :title="t('enterprise.resetUsage')" :message="t('enterprise.resetConfirm')" danger @cancel="resetTarget = null" @confirm="reset">
      <div class="flex flex-wrap gap-4"><div v-for="window in windows" :key="window" class="flex items-center gap-2"><Switch :model-value="resetWindows.includes(window)" :aria-label="t(`enterprise.${window}`)" :data-test="`quota-reset-window-${window}`" @update:model-value="selected => setResetWindow(window, selected)" /><span>{{ t(`enterprise.${window}`) }}</span></div></div>
    </EnterpriseConfirm>
  </section>
</template>

<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { ChevronDown } from '@lucide/vue'
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseQuota, EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, QuotaWindow, QuotaLimits } from '@/types/enterprise'
import EnterpriseDialog from '@/features/enterprise/components/EnterpriseDialog.vue'
import EnterpriseConfirm from '@/features/enterprise/components/EnterpriseConfirm.vue'
import EnterpriseQuotaDisplay from './EnterpriseQuotaDisplay.vue'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import EnterpriseEmpty from '@/features/enterprise/components/EnterpriseEmpty.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { loadMemberOptions } from '../memberOptions'
const props = defineProps<{ api: EnterpriseResourceAPI; enterpriseName?: string; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const quotas = ref<EnterpriseQuota[]>([]), departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const editing = ref<EnterpriseQuota | null>(null), resetTarget = ref<EnterpriseQuota | null>(null)
const resetWindows = ref<QuotaWindow[]>(['total'])
const windows: QuotaWindow[] = ['daily', 'weekly', 'monthly', 'total']
const limits = reactive<Record<QuotaWindow, string | number>>({ daily: '', weekly: '', monthly: '', total: '' })
const validation = ref('')
const canReset = computed(() => !props.readonly && (props.admin || props.overview?.permissions.edit_quotas))
const memberSearch = ref('')
const sectionOpen = reactive<Record<number, boolean>>({})
function memberSectionOpen(id: number, length: number) { return !!memberSearch.value || (sectionOpen[id] ?? length <= 5) }
const memberSections = computed(() => {
  const sections = new Map<number, { id: number; name: string; rows: EnterpriseQuota[] }>()
  for (const row of quotas.value.filter(item => item.scope_type === 'member')) {
    const member = members.value.find(item => item.id === row.scope_id)
    const name = member ? `${member.username} ${member.email}` : props.overview?.me.member_id === row.scope_id ? props.overview.me.title : `#${row.scope_id}`
    if (memberSearch.value && !name.toLowerCase().includes(memberSearch.value.toLowerCase())) continue
    const id = member?.department_id ?? (props.overview?.me.member_id === row.scope_id ? props.overview.me.department_id : null) ?? 0
    if (!sections.has(id)) sections.set(id, { id, name: departments.value.find(item => item.id === id)?.name || (id ? props.overview?.me.department_name : null) || t('enterprise.noDepartment'), rows: [] })
    sections.get(id)!.rows.push(row)
  }
  return [...sections.values()]
})
function setResetWindow(window: QuotaWindow, selected: boolean) {
  resetWindows.value = selected ? [...new Set([...resetWindows.value, window])] : resetWindows.value.filter(item => item !== window)
}
function scopeLabel(quota: EnterpriseQuota) {
  if (quota.scope_type === 'enterprise') return props.enterpriseName || props.overview?.enterprise?.name || t('enterprise.enterpriseScope')
  const name = quota.scope_type === 'department' ? departments.value.find(item => item.id === quota.scope_id)?.name : quota.scope_type === 'member' ? (members.value.find(item => item.id === quota.scope_id)?.username || members.value.find(item => item.id === quota.scope_id)?.email) : ''
  return `${t(`enterprise.${quota.scope_type}Scope`)}${name ? ` · ${name}` : ` #${quota.scope_id}`}`
}
function canEdit(quota: EnterpriseQuota) {
  if (props.readonly) return false
  if (props.admin || props.overview?.permissions.edit_quotas) return true
  return quota.scope_type === 'member' && !!props.overview?.permissions.edit_member_quota && members.value.some(member => member.id === quota.scope_id && member.role === 'member' && member.department_id === props.overview?.me.department_id)
}
async function load() {
  await read(signal => Promise.all([
    props.api.quotas(undefined, { signal }),
    props.api.departments({ signal }),
    props.admin || props.overview?.permissions.view_members || props.overview?.permissions.edit_member_quota ? loadMemberOptions(props.api, signal) : Promise.resolve([])
  ]), ([rows, departmentRows, memberRows]) => {
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
  if (!editing.value || !canEdit(editing.value)) return
  if (windows.some(window => String(limits[window]).trim() !== '' && (!Number.isFinite(Number(limits[window])) || Number(limits[window]) < 0))) { validation.value = t('enterprise.validationQuota'); return }
  const body = Object.fromEntries(windows.map(window => [`${window}_limit_usd`, String(limits[window]).trim() === '' ? null : Number(limits[window])])) as QuotaLimits
  const target = editing.value
  if (await run(() => props.api.updateQuota(target.scope_type, target.scope_id, body), t('enterprise.saved'))) { editing.value = null; emit('updated'); await load() }
}
async function reset() {
  if (!resetTarget.value || !resetWindows.value.length || !canReset.value) return
  const target = resetTarget.value
  if (await run(() => props.api.resetQuota(target.scope_type, target.scope_id, resetWindows.value), t('enterprise.saved'))) { resetTarget.value = null; emit('updated'); await load() }
}
onMounted(load)
watch(() => props.refreshKey, load)
</script>
