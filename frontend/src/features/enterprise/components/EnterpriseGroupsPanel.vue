<template>
  <section class="space-y-6">
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <div class="card space-y-4 p-4 sm:p-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(admin ? 'enterprise.groupPool' : 'enterprise.availableGroups') }}</h2>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.groupPoolHint') }}</p>
      <div v-if="busy" class="h-12 animate-pulse rounded bg-gray-100 dark:bg-dark-700"></div>
      <div v-else-if="admin && !readonly" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label v-for="group in allGroups" :key="group.id" class="flex items-center gap-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-700 dark:text-gray-200"><input v-model="poolSelection" type="checkbox" :value="group.id" />{{ group.name }}<span class="text-xs text-gray-500">{{ group.platform }}</span></label>
      </div>
      <div v-else class="flex flex-wrap gap-2"><span v-for="group in pool" :key="group.id" class="badge-primary">{{ group.name }}</span><p v-if="!pool.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.empty') }}</p></div>
      <button v-if="admin && !readonly" class="btn btn-primary" :disabled="busy" @click="savePool">{{ t('enterprise.save') }}</button>
    </div>
    <div v-if="showWhitelists" class="card space-y-4 p-4 sm:p-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('enterprise.whitelist') }}</h2>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.whitelistHint') }}</p>
      <label class="block"><span class="input-label">{{ t('enterprise.quotaScope') }}</span><select v-model="selectedScope" class="input" @change="selectScope"><option v-for="scope in scopes" :key="scope.key" :value="scope.key">{{ scope.label }}</option></select></label>
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label v-for="group in candidates" :key="group.id" class="flex items-center gap-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-700 dark:text-gray-200"><input v-model="selection" type="checkbox" :value="group.id" :disabled="!canEditWhitelist" />{{ group.name }}</label>
      </div>
      <p v-if="!candidates.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.empty') }}</p>
      <p v-if="outOfScopeNames.length" class="text-sm text-amber-700 dark:text-amber-400">{{ t('enterprise.inactiveWhitelistGroups', { groups: outOfScopeNames.join(', ') }) }}</p>
      <button v-if="canEditWhitelist" class="btn btn-primary" :disabled="busy || !selectedScope" @click="saveWhitelist">{{ t('enterprise.save') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseGroup, EnterpriseGroupWhitelist, EnterpriseDepartment, EnterpriseMember, EnterpriseScope, EnterpriseOverview } from '@/types/enterprise'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { loadMemberOptions } from '../memberOptions'
const props = defineProps<{ api: EnterpriseResourceAPI; enterpriseId: number; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run } = useEnterpriseAction()
const pool = ref<EnterpriseGroup[]>([]), allGroups = ref<EnterpriseGroup[]>([]), whitelists = ref<EnterpriseGroupWhitelist[]>([])
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const poolSelection = ref<number[]>([]), selection = ref<number[]>([]), selectedScope = ref('')
const showWhitelists = computed(() => props.admin || props.overview?.me.role === 'owner' || props.overview?.me.role === 'manager')
const scopes = computed(() => {
  const result: { key: string; label: string; type: EnterpriseScope; id: number }[] = []
  if (props.admin || props.overview?.me.role === 'owner') {
    result.push({ key: `enterprise:${props.enterpriseId}`, type: 'enterprise', id: props.enterpriseId, label: t('enterprise.enterpriseScope') })
    result.push(...departments.value.map(item => ({ key: `department:${item.id}`, type: 'department' as const, id: item.id, label: `${t('enterprise.departmentScope')} · ${item.name}` })))
  }
  result.push(...members.value.filter(item => props.admin || props.overview?.me.role === 'owner' || (item.role === 'member' && item.department_id === props.overview?.me.department_id)).map(item => ({ key: `member:${item.id}`, type: 'member' as const, id: item.id, label: `${t('enterprise.memberScope')} · ${item.username || item.email}` })))
  return result
})
const scope = computed(() => scopes.value.find(item => item.key === selectedScope.value))
const canEditWhitelist = computed(() => !props.readonly && !!scope.value && (props.admin || props.overview?.me.role === 'owner' || (scope.value.type === 'member' && props.overview?.permissions.edit_member_groups)))
const candidates = computed(() => {
  let result = props.admin ? (pool.value.length ? pool.value : allGroups.value) : pool.value
  const target = scope.value
  if (!target) return result
  const ancestors = [{ type: 'enterprise', id: props.enterpriseId }]
  if (target.type === 'enterprise') return result
  if (target.type === 'member') {
    const departmentId = members.value.find(item => item.id === target.id)?.department_id
    if (departmentId) ancestors.push({ type: 'department', id: departmentId })
  }
  for (const ancestor of ancestors) {
    const list = whitelists.value.find(item => item.scope_type === ancestor.type && item.scope_id === ancestor.id)?.group_ids
    if (list?.length) result = result.filter(group => list.includes(group.id))
  }
  return result
})
const outOfScopeNames = computed(() => selection.value.filter(id => !candidates.value.some(group => group.id === id)).map(id => allGroups.value.find(group => group.id === id)?.name || `#${id}`))
function selectScope() { selection.value = [...(whitelists.value.find(item => item.scope_type === scope.value?.type && item.scope_id === scope.value?.id)?.group_ids ?? [])] }
async function load() {
  await run(async () => {
    const [groups, lists, departmentRows, memberRows, catalog] = await Promise.all([
      props.admin ? props.api.allowedGroups() : props.api.groups(),
      showWhitelists.value ? props.api.groupWhitelists() : Promise.resolve([]),
      showWhitelists.value ? props.api.departments() : Promise.resolve([]),
      showWhitelists.value && (props.admin || props.overview?.permissions.view_members) ? loadMemberOptions(props.api) : Promise.resolve([]),
      props.admin ? adminAPI.groups.getAll() : Promise.resolve([])
    ])
    pool.value = groups; poolSelection.value = groups.map(item => item.id); whitelists.value = lists; departments.value = departmentRows; members.value = memberRows
    allGroups.value = props.admin ? catalog : groups
    if (!scopes.value.some(item => item.key === selectedScope.value)) selectedScope.value = scopes.value[0]?.key ?? ''
    selectScope()
  })
}
async function savePool() {
  if (await run(() => props.api.updateAllowedGroups(poolSelection.value), t('enterprise.saved'))) { emit('updated'); await load() }
}
async function saveWhitelist() {
  const target = scope.value
  if (!target || !canEditWhitelist.value) return
  const ids = selection.value.filter(id => candidates.value.some(group => group.id === id))
  if (await run(() => props.api.updateGroupWhitelist(target.type, target.id, ids), t('enterprise.saved'))) { emit('updated'); await load() }
}
onMounted(load)
</script>
