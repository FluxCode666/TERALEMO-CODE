<template>
  <section class="shadcn-scope space-y-4" :aria-busy="busy">
    <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }} <Button variant="ghost" class="underline" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <div class="grid items-start gap-5 lg:grid-cols-[16rem_minmax(0,1fr)]">
      <Card class="rounded-ui-lg border bg-card p-3">
        <Collapsible v-model:open="treeOpen">
          <div class="flex items-center justify-between gap-2 px-2 py-2"><h2 class="font-semibold">{{ t('enterprise.organization') }}</h2><CollapsibleTrigger as-child><Button variant="ghost" size="icon" :aria-label="t(treeOpen ? 'enterprise.organizationUi.collapse' : 'enterprise.organizationUi.expand')"><ChevronDown class="size-4 transition-transform" :class="treeOpen ? '' : '-rotate-90'" /></Button></CollapsibleTrigger></div>
          <CollapsibleContent class="overflow-hidden data-[state=open]:animate-collapsible-down data-[state=closed]:animate-collapsible-up motion-reduce:animate-none">
            <nav class="space-y-1" :aria-label="t('enterprise.departments')">
              <div v-for="node in nodes" :key="node.key" class="flex min-w-0 items-center gap-1">
                <Button variant="ghost" :data-test="`org-node-${node.key}`" class="h-auto min-w-0 flex-1 justify-between gap-2 px-3 py-3 text-left" :class="selected === node.key ? 'bg-primary/10 text-primary' : ''" :aria-current="selected === node.key ? 'true' : undefined" @click="selectNode(node.key)"><span class="flex min-w-0 items-center gap-2" :class="node.key !== 'all' && !limitedManager ? 'pl-3' : ''"><Building2 class="size-4 shrink-0 text-muted-foreground" /><span class="break-words whitespace-normal">{{ node.name }}</span></span><Badge variant="secondary" class="shrink-0 tabular-nums">{{ node.count ?? '—' }}</Badge></Button>
                <DropdownMenu v-if="canManageDepartments && node.key !== 'all' && node.key !== '0'"><DropdownMenuTrigger as-child><Button variant="ghost" size="icon" class="size-8 shrink-0" :aria-label="`${node.name} · ${t('enterprise.actions')}`" :data-test="`org-menu-${node.key}`"><MoreHorizontal class="size-4" /></Button></DropdownMenuTrigger><DropdownMenuContent align="start"><DropdownMenuItem :data-test="`org-edit-${node.key}`" @select="openDepartment(departments.find(row => String(row.id) === node.key))">{{ t('enterprise.editDepartment') }}</DropdownMenuItem><DropdownMenuItem class="text-destructive focus:text-destructive" :data-test="`org-delete-${node.key}`" @select="deleteDepartment(node.key)">{{ t('enterprise.deleteDepartment') }}</DropdownMenuItem></DropdownMenuContent></DropdownMenu>
              </div>
            </nav>
            <Button v-if="canManageDepartments" variant="outline" class="mt-3 w-full" data-test="department-create" @click="openDepartment()">+ {{ t('enterprise.addDepartment') }}</Button>
          </CollapsibleContent>
        </Collapsible>
      </Card>
      <div class="min-w-0 space-y-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div><h2 class="text-lg font-semibold text-foreground">{{ selectedName }}</h2><p class="mt-1 text-sm text-muted-foreground">{{ t(canViewMembers ? 'enterprise.organizationUi.membersHint' : 'enterprise.organizationUi.directoryHint') }}</p></div>
          <div class="flex flex-wrap gap-2">
            <Button variant="default" v-if="canCreateMembers"  data-test="organization-create" @click="openMember()">{{ t('enterprise.addMember') }}</Button>
          </div>
        </div>
        <template v-if="canViewMembers">
          <Card><form class="rounded-ui-lg border bg-card text-card-foreground shadow-sm grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-[minmax(10rem,1fr)_9rem_8rem_auto]" @submit.prevent="searchMembers">
            <Input type="search" v-model="search" :placeholder="t('enterprise.organizationUi.searchMembers')" :aria-label="t('enterprise.search')" />
            <EnterpriseSelect v-model="roleFilter" :options="roleOptions" :aria-label="t('enterprise.role')" />
            <EnterpriseSelect v-model="statusFilter" :options="statusOptions" :aria-label="t('enterprise.status')" />
            <Button variant="outline"  :disabled="busy">{{ t('enterprise.search') }}</Button>
          </form></Card>
          <EnterpriseTable :columns="columns" :data="visibleMembers" :loading="busy">
            <template #cell-username="{ row }"><span class="break-words font-medium">{{ row.username || row.email }}</span><p class="break-all text-xs text-muted-foreground">{{ row.email }}</p></template>
            <template #cell-role="{ value }">{{ t(`enterprise.roles.${value}`) }}</template>
            <template #cell-status="{ value }"><Badge data-test="enterprise-status-badge" class="whitespace-nowrap" :variant="value === 'active' ? 'secondary' : 'destructive'">{{ t(value === 'active' ? 'enterprise.active' : 'enterprise.disabled') }}</Badge></template>
            <template #cell-department_name="{ value }">{{ value || t('enterprise.noDepartment') }}</template>
            <template #cell-joined_at="{ row }">{{ formatDateTime(row.joined_at) }}</template>
            <template #cell-actions="{ row }">
              <DropdownMenu v-if="canManageMembers && canManageMember(row)">
                <DropdownMenuTrigger as-child><Button variant="ghost" size="icon" :aria-label="`${row.username || row.email} · ${t('enterprise.actions')}`" :data-test="`member-menu-${row.id}`"><MoreHorizontal class="size-4" /></Button></DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem :data-test="`member-edit-${row.id}`" @select="openMember(row)">{{ t('enterprise.edit') }}</DropdownMenuItem>
                  <DropdownMenuItem class="text-destructive focus:text-destructive" :data-test="`member-remove-${row.id}`" @select="deleting = { kind: 'member', row }">{{ t('enterprise.removeMember') }}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </template>
            <template #empty><EnterpriseEmpty :title="t('enterprise.empty')" :description="t('enterprise.organizationUi.emptyMembers')" /></template>
          </EnterpriseTable>
          <EnterprisePagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; load()" @update:page-size="pageSize = $event; page = 1; load()" />
        </template>
        <template v-else><Skeleton v-if="busy" class="h-32" /><Card v-else class="rounded-ui-lg border bg-card text-card-foreground shadow-sm p-5"><p class="text-sm text-muted-foreground">{{ t('enterprise.memberCount') }}</p><p class="mt-2 text-2xl font-semibold text-foreground">{{ nodes.find(node => node.key === selected)?.count ?? '—' }}</p></Card></template>
      </div>
    </div>
    <EnterpriseDialog :show="formOpen" :title="t(formKind === 'member' ? (editingMember ? 'enterprise.editMember' : 'enterprise.addMember') : (editingDepartment ? 'enterprise.editDepartment' : 'enterprise.addDepartment'))" @close="formOpen = false">
      <p v-if="error" role="alert" class="mb-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <EnterpriseMemberForm v-if="formOpen && formKind === 'member'" :admin="admin" :member="editingMember" :departments="departments" :limited-manager="limitedManager" :department-id="limitedManager ? overview?.me.department_id : selectedDepartment?.id" :default-department-id="selectedDepartment?.id" :busy="busy" @submit="saveMember" />
      <form v-else-if="formOpen" class="space-y-4" @submit.prevent="saveDepartment">
        <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.name') }}</span><Input v-model.trim="departmentForm.name"  maxlength="100" required data-test="department-name" /></Label>
        <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.sortOrder') }}</span><Input v-model.number="departmentForm.sort_order"  type="number" step="1" required data-test="department-sort" /><span class="mt-1 block text-xs text-muted-foreground">{{ t('enterprise.organizationUi.sortHint') }}</span></Label>
        <Button variant="default"  :disabled="busy || !departmentForm.name">{{ t('enterprise.save') }}</Button>
      </form>
    </EnterpriseDialog>
    <EnterpriseConfirm :show="!!deleting" :busy="busy" :title="t(deleting?.kind === 'member' ? 'enterprise.removeMember' : 'enterprise.deleteDepartment')" :message="t('enterprise.confirmRemove')" danger @cancel="deleting = null" @confirm="remove"><p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p></EnterpriseConfirm>
  </section>
</template>

<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

import { Badge } from '@/components/ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { MoreHorizontal, ChevronDown, Building2 } from '@lucide/vue'
import { Skeleton } from '@/components/ui/skeleton'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseQuery, CreateEnterpriseAccountMemberRequest, CreateEnterpriseMemberRequest, UpdateEnterpriseMemberRequest } from '@/types/enterprise'
import { formatDateTime } from '@/utils/format'
import EnterpriseDialog from '@/features/enterprise/components/EnterpriseDialog.vue'
import EnterpriseConfirm from '@/features/enterprise/components/EnterpriseConfirm.vue'
import EnterpriseTable from '@/features/enterprise/components/EnterpriseTable.vue'
import EnterprisePagination from '@/features/enterprise/components/EnterprisePagination.vue'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
import { Input } from '@/components/ui/input'
import EnterpriseEmpty from '@/features/enterprise/components/EnterpriseEmpty.vue'
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
const selected = ref(limitedManager.value ? String(props.overview?.me.department_id ?? 0) : 'all'), treeOpen = ref(true)
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
function deleteDepartment(key: string) { const row = departments.value.find(item => String(item.id) === key); if (row) deleting.value = { kind: 'department', row } }
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
