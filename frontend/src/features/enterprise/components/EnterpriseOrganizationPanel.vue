<template>
  <section class="space-y-4" :aria-busy="busy">
    <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <div class="grid items-start gap-5 lg:grid-cols-[16rem_minmax(0,1fr)]">
      <aside class="card min-w-0 p-3">
        <div class="flex items-center justify-between gap-2 px-2 py-2">
          <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('enterprise.organization') }}</h2>
          <button class="btn btn-secondary px-2 py-1 lg:hidden" :aria-expanded="treeOpen" @click="treeOpen = !treeOpen">{{ t(treeOpen ? 'enterprise.organizationUi.collapse' : 'enterprise.organizationUi.expand') }}</button>
        </div>
        <div :class="treeOpen ? 'block' : 'hidden lg:block'">
          <nav class="space-y-1" :aria-label="t('enterprise.departments')">
            <button v-for="node in nodes" :key="node.key" :data-test="`org-node-${node.key}`" class="flex w-full items-center justify-between gap-2 rounded-lg px-3 py-3 text-left text-sm transition-colors focus-visible:ring-2 focus-visible:ring-primary-500" :class="selected === node.key ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-current="selected === node.key ? 'true' : undefined" @click="selectNode(node.key)">
              <span class="min-w-0 break-words" :class="node.key !== 'all' && !limitedManager ? 'pl-3' : ''">{{ node.name }}</span><span class="shrink-0 rounded-full bg-gray-100 px-2 py-0.5 text-xs tabular-nums text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ node.count ?? '—' }}</span>
            </button>
          </nav>
          <button v-if="canManageDepartments" class="btn btn-secondary mt-3 w-full" data-test="department-create" @click="openDepartment()">+ {{ t('enterprise.addDepartment') }}</button>
        </div>
      </aside>
      <div class="min-w-0 space-y-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ selectedName }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(canViewMembers ? 'enterprise.organizationUi.membersHint' : 'enterprise.organizationUi.directoryHint') }}</p></div>
          <div class="flex flex-wrap gap-2">
            <template v-if="selectedDepartment && canManageDepartments"><button class="btn btn-secondary" data-test="department-edit" @click="openDepartment(selectedDepartment)">{{ t('enterprise.editDepartment') }}</button><button class="btn btn-secondary text-red-600 dark:text-red-400" data-test="department-delete" @click="deleting = { kind: 'department', row: selectedDepartment }">{{ t('enterprise.deleteDepartment') }}</button></template>
            <button v-if="canCreateMembers" class="btn btn-primary" data-test="organization-create" @click="openMember()">{{ t('enterprise.addMember') }}</button>
          </div>
        </div>
        <template v-if="canViewMembers">
          <form class="card grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-[minmax(10rem,1fr)_9rem_8rem_auto]" @submit.prevent="searchMembers">
            <SearchInput v-model="search" :placeholder="t('enterprise.organizationUi.searchMembers')" :aria-label="t('enterprise.search')" />
            <Select v-model="roleFilter" :options="roleOptions" :aria-label="t('enterprise.role')" />
            <Select v-model="statusFilter" :options="statusOptions" :aria-label="t('enterprise.status')" />
            <button class="btn btn-secondary" :disabled="busy">{{ t('enterprise.search') }}</button>
          </form>
          <DataTable :columns="columns" :data="visibleMembers" :loading="busy">
            <template #cell-username="{ row }"><span class="break-words font-medium">{{ row.username || row.email }}</span><p class="break-all text-xs text-gray-500 dark:text-gray-400">{{ row.email }}</p></template>
            <template #cell-role="{ value }">{{ t(`enterprise.roles.${value}`) }}</template>
            <template #cell-status="{ value }"><span class="badge" :class="value === 'active' ? 'badge-success' : 'badge-danger'">{{ t(value === 'active' ? 'enterprise.active' : 'enterprise.disabled') }}</span></template>
            <template #cell-department_name="{ value }">{{ value || t('enterprise.noDepartment') }}</template>
            <template #cell-joined_at="{ value }">{{ formatDateTime(value) }}</template>
            <template #cell-actions="{ row }"><div v-if="canManageMembers && canManageMember(row)" class="flex gap-3"><button class="text-primary-600 dark:text-primary-400" @click="openMember(row)">{{ t('enterprise.edit') }}</button><button class="text-red-600 dark:text-red-400" @click="deleting = { kind: 'member', row }">{{ t('enterprise.removeMember') }}</button></div></template>
            <template #empty><EmptyState :title="t('enterprise.empty')" :description="t('enterprise.organizationUi.emptyMembers')" /></template>
          </DataTable>
          <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; load()" @update:page-size="pageSize = $event; page = 1; load()" />
        </template>
        <template v-else><div v-if="busy" class="h-32 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-800" /><div v-else class="card p-5"><p class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.memberCount') }}</p><p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ nodes.find(node => node.key === selected)?.count ?? '—' }}</p></div></template>
      </div>
    </div>
    <BaseDialog :show="formOpen" :title="t(formKind === 'member' ? (editingMember ? 'enterprise.editMember' : 'enterprise.addMember') : (editingDepartment ? 'enterprise.editDepartment' : 'enterprise.addDepartment'))" @close="formOpen = false">
      <p v-if="error" role="alert" class="mb-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <EnterpriseMemberForm v-if="formOpen && formKind === 'member'" :admin="admin" :member="editingMember" :departments="departments" :limited-manager="limitedManager" :department-id="limitedManager ? overview?.me.department_id : selectedDepartment?.id" :default-department-id="selectedDepartment?.id" :busy="busy" @submit="saveMember" />
      <form v-else-if="formOpen" class="space-y-4" @submit.prevent="saveDepartment">
        <label class="block"><span class="input-label">{{ t('enterprise.name') }}</span><input v-model.trim="departmentForm.name" class="input" maxlength="100" required data-test="department-name" /></label>
        <label class="block"><span class="input-label">{{ t('enterprise.sortOrder') }}</span><input v-model.number="departmentForm.sort_order" class="input" type="number" step="1" required data-test="department-sort" /><span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('enterprise.organizationUi.sortHint') }}</span></label>
        <button class="btn btn-primary" :disabled="busy || !departmentForm.name">{{ t('enterprise.save') }}</button>
      </form>
    </BaseDialog>
    <ConfirmDialog :show="!!deleting" :title="t(deleting?.kind === 'member' ? 'enterprise.removeMember' : 'enterprise.deleteDepartment')" :message="t('enterprise.confirmRemove')" danger @cancel="deleting = null" @confirm="remove"><p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p></ConfirmDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseQuery, CreateEnterpriseAccountMemberRequest, CreateEnterpriseMemberRequest, UpdateEnterpriseMemberRequest } from '@/types/enterprise'
import { formatDateTime } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import EnterpriseMemberForm from './EnterpriseMemberForm.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; mode?: 'members' | 'departments'; enterpriseName?: string; memberCount?: number; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const page = ref(1), pageSize = ref(20), total = ref(0), allCount = ref<number>()
const search = ref(''), roleFilter = ref(''), statusFilter = ref('')
let appliedQuery: EnterpriseQuery = {}
const limitedManager = computed(() => !props.admin && props.overview?.me.role === 'manager')
const canViewMembers = computed(() => props.mode !== 'departments' && (props.admin || props.overview?.permissions.view_members))
const canManageDepartments = computed(() => !props.readonly && (props.admin || props.overview?.permissions.manage_departments))
const canManageMembers = computed(() => !props.readonly && (props.admin || props.overview?.permissions.manage_members))
const canCreateMembers = computed(() => canViewMembers.value && !props.readonly && (props.admin || props.overview?.permissions.create_members))
const selected = ref(limitedManager.value ? String(props.overview?.me.department_id ?? 0) : 'all'), treeOpen = ref(false)
const visibleDepartments = computed(() => departments.value.filter(item => !limitedManager.value || item.id === props.overview?.me.department_id).sort((a, b) => a.sort_order - b.sort_order || a.id - b.id))
const nodes = computed(() => {
  const rows = visibleDepartments.value.map(item => ({ key: String(item.id), name: item.name, count: item.member_count }))
  if (limitedManager.value) return rows
  const count = props.memberCount ?? allCount.value
  return [{ key: 'all', name: props.enterpriseName || props.overview?.enterprise.name || t('enterprise.enterpriseScope'), count }, { key: '0', name: t('enterprise.organizationUi.directMembers'), count: count === undefined ? undefined : Math.max(0, count - departments.value.reduce((sum, item) => sum + item.member_count, 0)) }, ...rows]
})
const selectedDepartment = computed(() => visibleDepartments.value.find(item => String(item.id) === selected.value))
const selectedName = computed(() => nodes.value.find(item => item.key === selected.value)?.name || t('enterprise.departments'))
const visibleMembers = computed(() => members.value.filter(item => (!limitedManager.value || item.department_id === props.overview?.me.department_id) && (selected.value === 'all' || (item.department_id ?? 0) === Number(selected.value))))
const roleOptions = computed(() => [{ value: '', label: t('enterprise.organizationUi.allRoles') }, ...['owner', 'manager', 'member'].map(value => ({ value, label: t(`enterprise.roles.${value}`) }))])
const statusOptions = computed(() => [{ value: '', label: t('enterprise.organizationUi.allStatuses') }, ...['active', 'disabled'].map(value => ({ value, label: t(`enterprise.${value}`) }))])
const columns = computed(() => [{ key: 'username', label: t('enterprise.username') }, { key: 'department_name', label: t('enterprise.department') }, { key: 'role', label: t('enterprise.role') }, { key: 'status', label: t('enterprise.status') }, { key: 'title', label: t('enterprise.titleField') }, { key: 'joined_at', label: t('enterprise.joinedAt') }, { key: 'actions', label: t('enterprise.actions') }])
const formOpen = ref(false), formKind = ref<'member' | 'department'>('member')
const editingMember = ref<EnterpriseMember>(), editingDepartment = ref<EnterpriseDepartment>()
const deleting = ref<{ kind: 'member'; row: EnterpriseMember } | { kind: 'department'; row: EnterpriseDepartment } | null>(null)
const departmentForm = reactive({ name: '', sort_order: 0 })
function canManageMember(member: EnterpriseMember) { return !limitedManager.value || (member.role === 'member' && member.department_id === props.overview?.me.department_id) }
async function load() {
  const params = { ...appliedQuery, department_id: selected.value === 'all' ? undefined : Number(selected.value), page: page.value, page_size: pageSize.value }
  await read(signal => Promise.all([props.api.departments({ signal }), canViewMembers.value ? props.api.members(params, { signal }) : Promise.resolve(null)]), ([departmentRows, memberPage]) => {
    departments.value = departmentRows
    if (memberPage) { members.value = memberPage.items; total.value = memberPage.total; if (selected.value === 'all' && !appliedQuery.search && !appliedQuery.role && !appliedQuery.status) allCount.value = memberPage.total }
  })
}
function selectNode(key: string) { selected.value = key; page.value = 1; void load() }
function searchMembers() { appliedQuery = { search: search.value.trim() || undefined, role: (roleFilter.value || undefined) as EnterpriseQuery['role'], status: statusFilter.value || undefined }; page.value = 1; void load() }
function openMember(row?: EnterpriseMember) { editingMember.value = row; formKind.value = 'member'; error.value = ''; formOpen.value = true }
function openDepartment(row?: EnterpriseDepartment) { editingDepartment.value = row; departmentForm.name = row?.name ?? ''; departmentForm.sort_order = row?.sort_order ?? 0; formKind.value = 'department'; error.value = ''; formOpen.value = true }
async function saveMember(payload: Record<string, unknown>) {
  if ((!canCreateMembers.value && !editingMember.value) || (editingMember.value && (!canManageMembers.value || !canManageMember(editingMember.value)))) return
  if (await run(async () => {
    if (editingMember.value) await props.api.updateMember(editingMember.value.id, payload as UpdateEnterpriseMemberRequest)
    else if (props.admin && 'allowedGroups' in props.api) await props.api.createMember(payload as CreateEnterpriseMemberRequest)
    else await props.api.createMember(payload as CreateEnterpriseAccountMemberRequest)
  }, t('enterprise.saved'))) { if (!editingMember.value && allCount.value !== undefined) allCount.value++; formOpen.value = false; emit('updated'); await load() }
}
async function saveDepartment() {
  if (!canManageDepartments.value || !departmentForm.name || !Number.isInteger(departmentForm.sort_order)) return
  if (await run(() => editingDepartment.value ? props.api.updateDepartment(editingDepartment.value.id, { ...departmentForm }) : props.api.createDepartment({ ...departmentForm }), t('enterprise.saved'))) { formOpen.value = false; emit('updated'); await load() }
}
async function remove() {
  const target = deleting.value
  if (!target || busy.value || (target.kind === 'department' ? !canManageDepartments.value : !canManageMembers.value || !canManageMember(target.row))) return
  if (await run(() => target.kind === 'member' ? props.api.deleteMember(target.row.id) : props.api.deleteDepartment(target.row.id), t('enterprise.deleted'))) {
    deleting.value = null
    if (target.kind === 'department') selected.value = 'all'
    if (target.kind === 'member' && allCount.value !== undefined) allCount.value--
    if (target.kind === 'member' && members.value.length === 1 && page.value > 1) page.value--
    emit('updated'); await load()
  }
}
onMounted(load)
watch(() => props.refreshKey, load)
</script>
