<template>
  <section class="space-y-4" :aria-busy="busy">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(`enterprise.${mode}`) }}</h2>
      <button v-if="canCreate" class="btn btn-primary" data-test="organization-create" @click="openForm()">{{ t(mode === 'members' ? 'enterprise.addMember' : 'enterprise.addDepartment') }}</button>
    </div>
    <form v-if="mode === 'members'" class="flex flex-wrap gap-3" @submit.prevent="searchMembers">
      <input v-model.trim="search" class="input w-full sm:w-64" :aria-label="t('enterprise.search')" :placeholder="t('enterprise.search')" />
      <select v-if="!limitedManager" v-model="departmentFilter" class="input w-full sm:w-48" :aria-label="t('enterprise.department')"><option value="">{{ t('enterprise.all') }}</option><option v-for="department in departments" :key="department.id" :value="department.id">{{ department.name }}</option></select>
      <select v-model="roleFilter" class="input w-full sm:w-40" :aria-label="t('enterprise.role')"><option value="">{{ t('enterprise.all') }}</option><option v-for="role in roles" :key="role" :value="role">{{ t(`enterprise.roles.${role}`) }}</option></select>
      <select v-model="statusFilter" class="input w-full sm:w-40" :aria-label="t('enterprise.status')"><option value="">{{ t('enterprise.all') }}</option><option value="active">{{ t('enterprise.active') }}</option><option value="disabled">{{ t('enterprise.disabled') }}</option></select>
      <button class="btn btn-secondary" :disabled="busy">{{ t('enterprise.search') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <DataTable :columns="columns" :data="mode === 'members' ? members : departments" :loading="busy">
      <template #cell-username="{ row }"><span class="font-medium">{{ row.username || row.email }}</span><p class="text-xs text-gray-500 dark:text-gray-400">{{ row.email }}</p></template>
      <template #cell-role="{ value }">{{ t(`enterprise.roles.${value}`) }}</template>
      <template #cell-status="{ value }"><span :class="value === 'active' ? 'badge-success' : 'badge-danger'">{{ t(value === 'active' ? 'enterprise.active' : 'enterprise.disabled') }}</span></template>
      <template #cell-department_name="{ value }">{{ value || t('enterprise.noDepartment') }}</template>
      <template #cell-joined_at="{ value }">{{ formatDateTime(value) }}</template>
      <template #cell-actions="{ row }"><div v-if="canManage && (mode === 'departments' || canManageMember(row))" class="flex gap-3"><button class="text-primary-600 dark:text-primary-400" @click="openForm(row)">{{ t('enterprise.edit') }}</button><button class="text-red-600 dark:text-red-400" @click="deleting = row">{{ t('enterprise.remove') }}</button></div></template>
    </DataTable>
    <Pagination v-if="mode === 'members'" :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; load()" @update:page-size="pageSize = $event; page = 1; load()" />
    <BaseDialog :show="formOpen" :title="t(mode === 'members' ? (editingMember ? 'enterprise.editMember' : 'enterprise.addMember') : (editingDepartment ? 'enterprise.editDepartment' : 'enterprise.addDepartment'))" @close="formOpen = false">
      <EnterpriseMemberForm v-if="formOpen && mode === 'members'" :admin="admin" :member="editingMember" :departments="departments" :limited-manager="limitedManager" :department-id="overview?.me.department_id" :busy="busy" @submit="saveMember" />
      <form v-else-if="formOpen" class="space-y-4" @submit.prevent="saveDepartment">
        <label class="block"><span class="input-label">{{ t('enterprise.name') }}</span><input v-model.trim="departmentForm.name" class="input" maxlength="100" required /></label>
        <label class="block"><span class="input-label">{{ t('enterprise.sortOrder') }}</span><input v-model.number="departmentForm.sort_order" class="input" type="number" step="1" required /></label>
        <button class="btn btn-primary" :disabled="busy || !departmentForm.name">{{ t('enterprise.save') }}</button>
      </form>
    </BaseDialog>
    <ConfirmDialog :show="!!deleting" :title="t(mode === 'members' ? 'enterprise.removeMember' : 'enterprise.deleteDepartment')" :message="t('enterprise.confirmRemove')" danger @cancel="deleting = null" @confirm="remove" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseRole, EnterpriseQuery, CreateEnterpriseAccountMemberRequest, CreateEnterpriseMemberRequest, UpdateEnterpriseMemberRequest } from '@/types/enterprise'
import { formatDateTime } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import EnterpriseMemberForm from './EnterpriseMemberForm.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'

const props = defineProps<{ api: EnterpriseResourceAPI; mode: 'members' | 'departments'; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const departments = ref<EnterpriseDepartment[]>([])
const members = ref<EnterpriseMember[]>([])
const page = ref(1), pageSize = ref(20), total = ref(0)
const search = ref(''), departmentFilter = ref(''), roleFilter = ref<EnterpriseRole | ''>(''), statusFilter = ref('')
let appliedQuery: EnterpriseQuery = {}
const roles = ['owner', 'manager', 'member'] as const
const formOpen = ref(false)
const editingMember = ref<EnterpriseMember>()
const editingDepartment = ref<EnterpriseDepartment>()
const deleting = ref<EnterpriseMember | EnterpriseDepartment | null>(null)
const departmentForm = reactive({ name: '', sort_order: 0 })
const limitedManager = computed(() => !props.admin && props.overview?.me.role === 'manager')
const canManage = computed(() => !props.readonly && (props.admin || (props.mode === 'members' ? props.overview?.permissions.manage_members : props.overview?.permissions.manage_departments)))
const canCreate = computed(() => !props.readonly && (props.admin || (props.mode === 'members' ? props.overview?.permissions.create_members : props.overview?.permissions.manage_departments)))
const columns = computed(() => props.mode === 'members' ? [
  { key: 'username', label: t('enterprise.username') }, { key: 'department_name', label: t('enterprise.department') }, { key: 'role', label: t('enterprise.role') }, { key: 'status', label: t('enterprise.status') }, { key: 'title', label: t('enterprise.titleField') }, { key: 'joined_at', label: t('enterprise.joinedAt') }, { key: 'actions', label: t('enterprise.actions') }
] : [{ key: 'name', label: t('enterprise.name') }, { key: 'member_count', label: t('enterprise.memberCount') }, { key: 'sort_order', label: t('enterprise.sortOrder') }, { key: 'actions', label: t('enterprise.actions') }])

function canManageMember(member: EnterpriseMember) { return !limitedManager.value || (member.role === 'member' && member.department_id === props.overview?.me.department_id) }
async function load() {
  const params = { ...appliedQuery, page: page.value, page_size: pageSize.value }
  await read(signal => Promise.all([props.api.departments({ signal }), props.mode === 'members' ? props.api.members(params, { signal }) : Promise.resolve(null)]), ([departmentRows, memberPage]) => {
    departments.value = departmentRows
    if (memberPage) { members.value = memberPage.items; total.value = memberPage.total }
  })
}
function searchMembers() {
  appliedQuery = { search: search.value || undefined, department_id: departmentFilter.value ? Number(departmentFilter.value) : undefined, role: roleFilter.value || undefined, status: statusFilter.value || undefined }
  page.value = 1
  void load()
}
function openForm(row?: EnterpriseMember | EnterpriseDepartment) {
  editingMember.value = props.mode === 'members' ? row as EnterpriseMember | undefined : undefined
  editingDepartment.value = props.mode === 'departments' ? row as EnterpriseDepartment | undefined : undefined
  departmentForm.name = editingDepartment.value?.name ?? ''
  departmentForm.sort_order = editingDepartment.value?.sort_order ?? 0
  formOpen.value = true
}
async function saveMember(payload: Record<string, unknown>) {
  const success = await run(async () => {
    if (editingMember.value) await props.api.updateMember(editingMember.value.id, payload as UpdateEnterpriseMemberRequest)
    else if (props.admin && 'allowedGroups' in props.api) await props.api.createMember(payload as CreateEnterpriseMemberRequest)
    else await props.api.createMember(payload as CreateEnterpriseAccountMemberRequest)
  }, t('enterprise.saved'))
  if (success) { formOpen.value = false; emit('updated'); await load() }
}
async function saveDepartment() {
  if (!departmentForm.name || !Number.isInteger(departmentForm.sort_order)) return
  const success = await run(async () => {
    if (editingDepartment.value) await props.api.updateDepartment(editingDepartment.value.id, { ...departmentForm })
    else await props.api.createDepartment({ ...departmentForm })
  }, t('enterprise.saved'))
  if (success) { formOpen.value = false; emit('updated'); await load() }
}
async function remove() {
  if (!deleting.value || busy.value) return
  const id = deleting.value.id
  const success = await run(() => props.mode === 'members' ? props.api.deleteMember(id) : props.api.deleteDepartment(id), t('enterprise.deleted'))
  if (success) { deleting.value = null; emit('updated'); await load() }
}
onMounted(load)
watch(() => props.refreshKey, load)
</script>
