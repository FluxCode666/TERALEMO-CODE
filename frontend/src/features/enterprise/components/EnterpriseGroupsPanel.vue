<template>
  <section class="shadcn-scope space-y-6" :aria-busy="busy">
    <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }} <Button variant="ghost" class="underline" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <Card class="rounded-ui-lg border bg-card text-card-foreground shadow-sm space-y-4 p-4 sm:p-6">
      <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold text-foreground">{{ t('enterprise.groupAccess.availableTitle') }}</h2><Button variant="outline" v-if="admin && !readonly"  @click="poolOpen = true">{{ t('enterprise.groupAccess.editAvailable') }}</Button></div>
      <p class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.availableHint') }}</p>
      <Skeleton v-if="busy && !loaded" class="h-24" />
      <template v-else><EnterpriseGroupCards v-if="poolConfigured !== false && availableGroups.length" :groups="availableGroups" /><p v-if="poolConfigured === false" class="rounded-lg bg-gray-50 p-4 text-sm text-gray-600 dark:bg-dark-900 dark:text-gray-300">{{ t('enterprise.groupAccess.personalRules') }}</p><p v-else-if="!availableGroups.length" class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.noAvailable') }}</p></template>
    </Card>
    <template v-if="showWhitelists">
      <section v-for="kind in scopeKinds" :key="kind" class="space-y-3" :data-test="`group-section-${kind}`">
        <div><h2 class="text-lg font-semibold text-foreground">{{ t(`enterprise.groupAccess.sections.${kind}`) }}</h2><p class="mt-1 text-sm text-muted-foreground">{{ t(kind === 'department' ? 'enterprise.orgTree.groupInheritance' : 'enterprise.groupAccess.configureHint') }}</p></div>
        <EnterpriseSelect v-if="kind === 'member' && scopeOptions(kind).length" v-model="selectedTargets[kind]" :options="scopeOptions(kind)" :aria-label="t(`enterprise.groupAccess.sections.${kind}`)" :data-test="`group-target-${kind}`" class="sm:max-w-md" />
        <EnterpriseDepartmentPicker v-if="kind === 'department' && departments.length" v-model="selectedTargets.department" :departments="departments" :allow-root="false" :label="t('enterprise.groupAccess.sections.department')" data-test="group-target-department" />
        <EnterpriseGroupEditor v-if="currentScope(kind)" :key="currentScope(kind)!.key" :title="currentScope(kind)!.label" :group-ids="whitelistIds(kind, currentScope(kind)!.id)" :groups="candidates(currentScope(kind)!)" :editable="canEdit(currentScope(kind)!)" :busy="busy" @save="saveWhitelist(currentScope(kind)!, $event)" />
        <Card v-else class="rounded-ui-lg border bg-card text-card-foreground shadow-sm"><EnterpriseEmpty :title="t('enterprise.empty')" /></Card>
      </section>
    </template>
    <EnterpriseDialog :show="poolOpen" :title="t('enterprise.groupAccess.editAvailable')" width="wide" @close="poolOpen = false"><div class="space-y-4"><p class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.adminHint') }}</p><p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p><EnterpriseGroupCards v-model="poolSelection" :groups="allGroups.filter(group => group.status === 'active')" selectable :disabled="busy" /><Button variant="default"  :disabled="busy" @click="savePool">{{ t('enterprise.save') }}</Button></div></EnterpriseDialog>
  </section>
</template>

<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'

import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseGroup, EnterpriseGroupWhitelist, EnterpriseDepartment, EnterpriseMember, EnterpriseScope, EnterpriseOverview } from '@/types/enterprise'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
import EnterpriseDialog from '@/features/enterprise/components/EnterpriseDialog.vue'
import EnterpriseEmpty from '@/features/enterprise/components/EnterpriseEmpty.vue'
import EnterpriseGroupCards from './EnterpriseGroupCards.vue'
import EnterpriseGroupEditor from './EnterpriseGroupEditor.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { loadMemberOptions } from '../memberOptions'
import { departmentAncestors, departmentPath, departmentSubtreeIds } from '../departmentTree'
import EnterpriseDepartmentPicker from './EnterpriseDepartmentPicker.vue'
type Scope = { key: string; label: string; type: EnterpriseScope; id: number }
const props = defineProps<{ api: EnterpriseResourceAPI; enterpriseId: number; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const pool = ref<EnterpriseGroup[]>([]), allGroups = ref<EnterpriseGroup[]>([]), whitelists = ref<EnterpriseGroupWhitelist[]>([])
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const poolSelection = ref<number[]>([]), poolOpen = ref(false), loaded = ref(false)
const selectedTargets = reactive<Record<EnterpriseScope, number | null>>({ enterprise: props.enterpriseId, department: null, member: null })
const owner = computed(() => props.admin || props.overview?.me.role === 'owner')
const showWhitelists = computed(() => owner.value || props.overview?.me.role === 'manager')
const scopeKinds = computed<EnterpriseScope[]>(() => owner.value ? ['enterprise', 'department', 'member'] : ['member'])
const eligibleMembers = computed(() => members.value.filter(item => owner.value || departmentSubtreeIds(departments.value, props.overview?.me.department_id).has(item.department_id ?? 0)))
const scopes = computed<Scope[]>(() => [
  ...(owner.value ? [{ key: `enterprise:${props.enterpriseId}`, type: 'enterprise' as const, id: props.enterpriseId, label: props.overview?.enterprise.name || t('enterprise.enterpriseScope') }, ...departments.value.map(item => ({ key: `department:${item.id}`, type: 'department' as const, id: item.id, label: departmentPath(departments.value, item.id) }))] : []),
  ...eligibleMembers.value.filter(item => owner.value || item.role === 'member').map(item => ({ key: `member:${item.id}`, type: 'member' as const, id: item.id, label: `${item.username || item.email} · ${item.department_name || t('enterprise.noDepartment')}` }))
])
const poolConfigured = computed(() => props.admin ? pool.value.length > 0 : undefined)
const availableGroups = computed(() => pool.value)
function scopeOptions(kind: EnterpriseScope) { return scopes.value.filter(item => item.type === kind).map(item => ({ value: item.id, label: item.label })) }
function currentScope(kind: EnterpriseScope) { return scopes.value.find(item => item.type === kind && item.id === selectedTargets[kind]) }
const emptyGroupIds: number[] = []
function whitelistIds(kind: EnterpriseScope, id: number) { return whitelists.value.find(item => item.scope_type === kind && item.scope_id === id)?.group_ids ?? emptyGroupIds }
function canEdit(target: Scope) { return !props.readonly && (owner.value || target.type === 'member' && !!props.overview?.permissions.edit_member_groups && eligibleMembers.value.some(item => item.id === target.id && item.role === 'member')) }
function candidates(target: Scope) {
  let result = allGroups.value.filter(group => group.status === 'active')
  if (poolConfigured.value) result = result.filter(group => pool.value.map(item => item.id).includes(group.id))
  const ancestors: { type: EnterpriseScope; id: number }[] = []
  if (target.type !== 'enterprise') ancestors.push({ type: 'enterprise', id: props.enterpriseId })
  const departmentId = target.type === 'department' ? target.id : target.type === 'member' ? members.value.find(item => item.id === target.id)?.department_id : null
  if (departmentId) ancestors.push(...departmentAncestors(departments.value, departmentId).filter(row => target.type !== 'department' || row.id !== target.id).map(row => ({ type: 'department' as const, id: row.id })))
  for (const ancestor of ancestors) {
    const configured = whitelists.value.find(item => item.scope_type === ancestor.type && item.scope_id === ancestor.id)
    const ids = configured?.group_ids
    if (ids?.length) result = result.filter(group => ids.includes(group.id))
  }
  return result
}
async function load() {
  await read(signal => Promise.all([
    props.admin && 'allowedGroups' in props.api ? props.api.allowedGroups({ signal }) : props.api.groups({ signal }),
    showWhitelists.value ? props.api.groupWhitelists({ signal }) : Promise.resolve([]),
    showWhitelists.value ? props.api.departments({ signal }) : Promise.resolve([]),
    showWhitelists.value && (props.admin || props.overview?.permissions.view_members || props.overview?.permissions.edit_member_groups) ? loadMemberOptions(props.api, signal) : Promise.resolve([]),
    props.admin ? adminAPI.groups.getAll() : Promise.resolve([])
  ]), ([groups, lists, departmentRows, memberRows, catalog]) => {
    pool.value = groups; poolSelection.value = groups.map(item => item.id); whitelists.value = lists; departments.value = departmentRows; members.value = memberRows; allGroups.value = props.admin ? catalog : groups; loaded.value = true
    for (const kind of scopeKinds.value) if (!currentScope(kind)) selectedTargets[kind] = scopeOptions(kind)[0]?.value ?? null
  })
}
async function savePool() {
  const api = props.api
  if (!props.admin || props.readonly || !('updateAllowedGroups' in api)) return
  if (await run(() => api.updateAllowedGroups(poolSelection.value), t('enterprise.saved'))) { poolOpen.value = false; emit('updated'); await load() }
}
async function saveWhitelist(target: Scope, ids: number[]) {
  if (!canEdit(target)) return
  if (await run(() => props.api.updateGroupWhitelist(target.type, target.id, ids), t('enterprise.saved'))) { emit('updated'); await load() }
}
onMounted(load)
watch(() => props.refreshKey, load)
</script>
