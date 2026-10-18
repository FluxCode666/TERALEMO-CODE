<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw, Search, FolderPlus, UserPlus, Upload } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { useMotion } from '@/composables/useMotion'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, UpdateEnterpriseMemberRequest, CreateEnterpriseMemberRequest, CreateEnterpriseAccountMemberRequest } from '@/types/enterprise'
import { buildOrganizationTree, departmentSubtreeIds } from '../departmentTree'
import { loadMemberOptions } from '../memberOptions'
import { useEnterpriseAction } from '../useEnterpriseAction'
import EnterpriseTree from './EnterpriseTree.vue'
import EnterpriseOrganizationDetail from './EnterpriseOrganizationDetail.vue'
import EnterpriseDepartmentPicker from './EnterpriseDepartmentPicker.vue'
import EnterpriseDialog from './EnterpriseDialog.vue'
import EnterpriseConfirm from './EnterpriseConfirm.vue'
import EnterpriseMemberForm from './EnterpriseMemberForm.vue'
import EnterpriseImportWizard from './EnterpriseImportWizard.vue'
import EnterprisePasswordResetDialog from './EnterprisePasswordResetDialog.vue'
import { canResetMemberPassword } from '../memberPassword'
const props = defineProps<{ api: EnterpriseResourceAPI; enterpriseName?: string; memberCount?: number; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; refreshKey?: number; mode?: string }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, read, run } = useEnterpriseAction()
const root = ref<HTMLElement>()
const motion = useMotion(root)
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const search = ref(''), selected = ref('all'), loaded = ref(false)
const limitedManager = computed(() => !props.admin && props.overview?.me.role === 'manager')
const managerIds = computed(() => departmentSubtreeIds(departments.value, props.overview?.me.department_id))
const visibleDepartments = computed(() => departments.value.filter(row => !limitedManager.value || managerIds.value.has(row.id)))
const visibleMembers = computed(() => members.value.filter(row => !limitedManager.value || managerIds.value.has(row.department_id ?? 0)))
const canViewMembers = computed(() => props.mode !== 'departments' && !!(props.admin || props.overview?.permissions.view_members))
const canManageDepartments = computed(() => !props.readonly && !!(props.admin || props.overview?.me.role === 'owner' && props.overview.permissions.manage_departments))
const managerManagementEnabled = computed(() => !limitedManager.value || props.overview?.settings?.manager_can_manage_members !== false)
const canCreateMembers = computed(() => managerManagementEnabled.value && canViewMembers.value && !props.readonly && !!(props.admin || props.overview?.permissions.create_members))
const canManageMembers = computed(() => managerManagementEnabled.value && !props.readonly && !!(props.admin || props.overview?.permissions.manage_members))
const canImport = computed(() => canCreateMembers.value && canManageMembers.value)
function canManageMember(member: EnterpriseMember) { return canManageMembers.value && (!limitedManager.value || member.role === 'member' && managerIds.value.has(member.department_id ?? 0)) }
const passwordTarget = ref<EnterpriseMember>()
function canResetPassword(member: EnterpriseMember) { return canResetMemberPassword(member, { admin: props.admin, readonly: props.readonly, overview: props.overview, departments: departments.value }) }
function openPasswordReset(member: EnterpriseMember) { if (canResetPassword(member)) passwordTarget.value = member }
const selectedMember = computed(() => visibleMembers.value.find(row => `member-${row.id}` === selected.value))
const directoryCount = computed(() => props.memberCount ?? (canViewMembers.value ? visibleMembers.value.length : undefined))
const nodes = computed(() => {
  const tree = buildOrganizationTree(visibleDepartments.value, visibleMembers.value, props.enterpriseName || props.overview?.enterprise.name || t('enterprise.enterpriseScope'), t('enterprise.orgTree.unassigned'), t(canViewMembers.value ? 'enterprise.orgTree.noMembers' : 'enterprise.orgTree.membersHidden'), directoryCount.value)
  tree[0]!.count = directoryCount.value
  if (!canViewMembers.value) { const unassigned = tree[0]!.children.find(node => node.kind === 'unassigned'); if (unassigned) unassigned.count = undefined }
  return limitedManager.value ? tree[0]!.children.filter(row => row.kind === 'department') : tree
})
const formOpen = ref(false), formKind = ref<'member' | 'department'>('member'), importOpen = ref(false)
const editingMember = ref<EnterpriseMember>(), editingDepartment = ref<EnterpriseDepartment>()
const defaultDepartment = ref(0), moving = ref(false)
const departmentForm = reactive({ name: '', parent_id: 0, sort_order: 0 })
const blockedParents = computed(() => new Set([...departmentSubtreeIds(departments.value, editingDepartment.value?.id)].map(String)))
const deleting = ref<{ kind: 'member'; row: EnterpriseMember } | { kind: 'department'; row: EnterpriseDepartment } | null>(null)
const departmentNotEmpty = computed(() => deleting.value?.kind === 'department' && (deleting.value.row.member_count > 0 || departments.value.some(row => row.parent_id === deleting.value?.row.id)))
async function load() {
  await read(signal => Promise.all([props.api.departments({ signal }), canViewMembers.value ? loadMemberOptions(props.api, signal) : Promise.resolve([])]), ([departmentRows, memberRows]) => {
    departments.value = departmentRows; members.value = memberRows; loaded.value = true
    const valid = selected.value === 'all' || selected.value === '0' || visibleDepartments.value.some(row => String(row.id) === selected.value) || visibleMembers.value.some(row => `member-${row.id}` === selected.value)
    if (!valid || limitedManager.value && (selected.value === 'all' || selected.value === '0')) selected.value = limitedManager.value ? String(props.overview?.me.department_id ?? 0) : 'all'
  })
}
async function changed() { emit('updated'); await load() }
function openMember(row?: EnterpriseMember, departmentId?: number) {
  if (row ? !canManageMember(row) : !canCreateMembers.value) return
  const contextDepartment = selectedMember.value ? selectedMember.value.department_id ?? 0 : visibleDepartments.value.find(department => String(department.id) === selected.value)?.id ?? (limitedManager.value ? props.overview?.me.department_id ?? 0 : 0)
  editingMember.value = row; defaultDepartment.value = departmentId ?? contextDepartment; formKind.value = 'member'; error.value = ''; formOpen.value = true
}
function openDepartment(row?: EnterpriseDepartment, move = false, parentId = 0) {
  if (!canManageDepartments.value) return
  editingDepartment.value = row; moving.value = move; Object.assign(departmentForm, { name: row?.name ?? '', parent_id: row?.parent_id ?? parentId, sort_order: row?.sort_order ?? 0 }); formKind.value = 'department'; error.value = ''; formOpen.value = true
}
async function saveMember(payload: Record<string, unknown>) {
  if (editingMember.value ? !canManageMember(editingMember.value) : !canCreateMembers.value) return
  if (limitedManager.value && (!managerIds.value.has(Number(payload.department_id ?? editingMember.value?.department_id)) || payload.role && payload.role !== 'member')) return
  if (await run(async () => {
    if (editingMember.value) await props.api.updateMember(editingMember.value.id, payload as UpdateEnterpriseMemberRequest)
    else if (props.admin && 'allowedGroups' in props.api) await props.api.createMember(payload as CreateEnterpriseMemberRequest)
    else await props.api.createMember(payload as CreateEnterpriseAccountMemberRequest)
  }, t('enterprise.saved'))) { formOpen.value = false; await changed() }
}
async function saveDepartment() {
  if (!canManageDepartments.value || !departmentForm.name.trim() || !Number.isInteger(departmentForm.sort_order) || blockedParents.value.has(String(departmentForm.parent_id))) return
  if (await run(() => editingDepartment.value ? props.api.updateDepartment(editingDepartment.value.id, { ...departmentForm, name: departmentForm.name.trim() }) : props.api.createDepartment({ ...departmentForm, name: departmentForm.name.trim(), parent_id: departmentForm.parent_id || null }), t('enterprise.saved'))) { formOpen.value = false; await changed() }
}
async function toggleMember(row: EnterpriseMember) { if (canManageMember(row) && await run(() => props.api.updateMember(row.id, { status: row.status === 'active' ? 'disabled' : 'active' }), t('enterprise.saved'))) await changed() }
async function remove() {
  const target = deleting.value
  if (!target || departmentNotEmpty.value || (target.kind === 'department' ? !canManageDepartments.value : !canManageMember(target.row))) return
  if (await run(() => target.kind === 'member' ? props.api.deleteMember(target.row.id) : props.api.deleteDepartment(target.row.id), t('enterprise.deleted'))) { deleting.value = null; selected.value = 'all'; await changed() }
}
watch(selected, async () => { await nextTick(); motion.stop(); motion.fadeIn('[data-test="organization-detail"]') })
onMounted(load)
watch(() => props.refreshKey, load)
</script>
<template>
  <section ref="root" class="shadcn-scope space-y-4" :aria-busy="busy">
    <div class="flex flex-wrap items-center justify-between gap-3" data-test="organization-toolbar">
      <div class="relative w-full sm:max-w-sm"><Search class="pointer-events-none absolute left-3 top-3 size-4 text-muted-foreground" /><Input v-model="search" type="search" class="pl-9" :placeholder="t('enterprise.orgTree.search')" :aria-label="t('enterprise.orgTree.search')" /></div>
      <div class="flex flex-wrap gap-2"><Button variant="outline" :disabled="busy" data-test="organization-refresh" @click="load"><RefreshCw class="mr-2 size-4" />{{ t('enterprise.refresh') }}</Button><Button v-if="canManageDepartments" variant="outline" data-test="department-create" @click="openDepartment()"><FolderPlus class="mr-2 size-4" />{{ t('enterprise.orgTree.newDepartment') }}</Button><Button v-if="canCreateMembers" variant="outline" data-test="organization-create" @click="openMember()"><UserPlus class="mr-2 size-4" />{{ t('enterprise.orgTree.newMember') }}</Button><Button v-if="canImport" data-test="organization-import" @click="importOpen = true"><Upload class="mr-2 size-4" />{{ t('enterprise.importUi.title') }}</Button></div>
    </div>
    <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
    <div v-if="!loaded && busy" class="grid gap-4 lg:grid-cols-[minmax(18rem,2fr)_3fr]"><Skeleton class="h-96" /><Skeleton class="h-96" /></div>
    <div v-else class="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(18rem,2fr)_3fr]">
      <Card class="min-w-0 overflow-hidden"><div class="border-b px-4 py-3 text-sm font-semibold">{{ t('enterprise.organization') }}</div><ScrollArea class="h-[26rem] lg:h-[min(68dvh,48rem)]"><div class="w-full overflow-x-auto p-2"><div class="min-w-max"><EnterpriseTree v-model="selected" :nodes="nodes" :search="search" :label="t('enterprise.organization')" /></div></div></ScrollArea></Card>
      <EnterpriseOrganizationDetail :selected="selected" :enterprise-name="enterpriseName || overview?.enterprise.name || t('enterprise.enterpriseScope')" :departments="visibleDepartments" :members="visibleMembers" :member-count="directoryCount" :can-manage-departments="canManageDepartments" :can-create-members="canCreateMembers" :can-view-members="canViewMembers" :can-edit-member="!!selectedMember && canManageMember(selectedMember)" :can-reset-password="!!selectedMember && canResetPassword(selectedMember)" @reset-password="openPasswordReset" :busy="busy" @select="selected = $event" @edit-member="openMember" @toggle-member="toggleMember" @remove-member="row => deleting = { kind: 'member', row }" @create-member="id => openMember(undefined, id)" @create-department="id => openDepartment(undefined, false, id)" @edit-department="openDepartment" @delete-department="row => deleting = { kind: 'department', row }" />
    </div>
    <EnterprisePasswordResetDialog v-if="passwordTarget" :api="api" :member="passwordTarget" @close="passwordTarget = undefined" @updated="changed" />
    <EnterpriseDialog :show="formOpen" :title="t(formKind === 'member' ? editingMember ? 'enterprise.editMember' : 'enterprise.orgTree.newMember' : moving ? 'enterprise.orgTree.move' : editingDepartment ? 'enterprise.editDepartment' : 'enterprise.orgTree.newDepartment')" @close="formOpen = false">
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
      <EnterpriseMemberForm v-if="formOpen && formKind === 'member'" :admin="admin" :member="editingMember" :departments="visibleDepartments" :limited-manager="limitedManager" :department-id="overview?.me.department_id" :default-department-id="defaultDepartment" :busy="busy" @submit="saveMember" />
      <form v-if="formOpen && formKind === 'department'" class="space-y-4" @submit.prevent="saveDepartment"><Label class="block">{{ t('enterprise.name') }}<Input v-model="departmentForm.name" required maxlength="100" data-test="department-name" :disabled="moving" class="mt-2" /></Label><Label class="block">{{ t('enterprise.sortOrder') }}<Input v-model.number="departmentForm.sort_order" type="number" step="1" data-test="department-sort" class="mt-2" /></Label><div><Label class="mb-2 block">{{ t('enterprise.orgTree.parentDepartment') }}</Label><EnterpriseDepartmentPicker v-model="departmentForm.parent_id" :departments="departments" :disabled-keys="blockedParents" :root-label="t('enterprise.orgTree.enterpriseRoot')" /></div><p class="text-xs text-muted-foreground">{{ t('enterprise.orgTree.depthHint') }}</p><Button :disabled="busy">{{ t('enterprise.save') }}</Button></form>
    </EnterpriseDialog>
    <EnterpriseConfirm :show="!!deleting" :title="t(deleting?.kind === 'department' ? 'enterprise.deleteDepartment' : 'enterprise.removeMember')" :message="t(departmentNotEmpty ? 'enterprise.orgTree.notEmpty' : 'enterprise.confirmRemove')" :disabled="!!departmentNotEmpty" :busy="busy" danger @cancel="deleting = null" @confirm="remove" />
    <EnterpriseImportWizard v-if="importOpen && canImport" :api="api" :limited-manager="limitedManager" @close="importOpen = false" @imported="changed" />
  </section>
</template>
