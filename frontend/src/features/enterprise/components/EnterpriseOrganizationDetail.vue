<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Building2, Crown, FolderTree, UserRound } from '@lucide/vue'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { EnterpriseDepartment, EnterpriseMember } from '@/types/enterprise'
import { departmentPath, departmentSubtreeIds } from '../departmentTree'
import { formatDateTime } from '@/utils/format'
const props = defineProps<{ selected: string; enterpriseName: string; departments: EnterpriseDepartment[]; members: EnterpriseMember[]; memberCount?: number; canManageDepartments: boolean; canCreateMembers: boolean; canEditMember: boolean; canResetPassword?: boolean; canViewMembers: boolean; busy?: boolean }>()
const emit = defineEmits<{ resetPassword: [member: EnterpriseMember]; editMember: [member: EnterpriseMember]; toggleMember: [member: EnterpriseMember]; removeMember: [member: EnterpriseMember]; createMember: [departmentId?: number]; createDepartment: [parentId?: number]; editDepartment: [department: EnterpriseDepartment, move?: boolean]; deleteDepartment: [department: EnterpriseDepartment]; select: [key: string] }>()
const { t } = useI18n()
const member = computed(() => props.members.find(row => `member-${row.id}` === props.selected))
const department = computed(() => props.departments.find(row => String(row.id) === props.selected))
const path = computed(() => member.value ? member.value.department_path_names?.join(' / ') || (member.value.department_id ? departmentPath(props.departments, member.value.department_id) || member.value.department_name : '') || t('enterprise.orgTree.unassigned') : department.value ? departmentPath(props.departments, department.value.id) : '')
const children = computed(() => props.departments.filter(row => row.parent_id === department.value?.id && !!department.value))
const managers = computed(() => props.members.filter(row => row.role === 'manager' && row.department_id === department.value?.id))
const owners = computed(() => props.members.filter(row => row.role === 'owner'))
const total = computed(() => {
  const row = department.value
  if (!row) return 0
  const ids = departmentSubtreeIds(props.departments, row.id)
  return row.total_member_count ?? props.departments.filter(department => ids.has(department.id)).reduce((sum, department) => sum + (department.member_count ?? 0), 0)
})
const memberFields = computed(() => member.value ? [
  [t('enterprise.username'), member.value.username || '—'], [t('enterprise.email'), member.value.email],
  [t('enterprise.status'), t(member.value.status === 'active' ? 'enterprise.active' : 'enterprise.disabled')],
  [t('enterprise.titleField'), member.value.title || '—'], [t('enterprise.joinedAt'), member.value.joined_at ? formatDateTime(member.value.joined_at) : '—'],
  [t('enterprise.orgTree.lastLogin'), member.value.last_login_at ? formatDateTime(member.value.last_login_at) : t('enterprise.orgTree.neverLoggedIn')]
] : [])
</script>
<template>
  <Card class="shadcn-scope min-w-0 lg:sticky lg:top-4" data-test="organization-detail">
    <CardHeader class="border-b">
      <CardTitle class="flex flex-wrap items-center gap-2 break-words"><Crown v-if="member?.role === 'owner'" class="size-5 text-amber-600" /><UserRound v-else-if="member" class="size-5" /><FolderTree v-else-if="department" class="size-5" /><Building2 v-else class="size-5" />{{ member ? member.username || member.email : department?.name || (selected === '0' ? t('enterprise.orgTree.unassigned') : enterpriseName) }}<Badge v-if="member" variant="secondary">{{ t(`enterprise.roles.${member.role}`) }}</Badge></CardTitle>
      <CardDescription class="break-words">{{ member ? t('enterprise.orgTree.memberNode', { path }) : department ? path : t('enterprise.orgTree.overview') }}</CardDescription>
    </CardHeader>
    <CardContent class="space-y-6 pt-6">
      <template v-if="member">
        <dl class="grid gap-5 sm:grid-cols-2"><div v-for="[label, value] in memberFields" :key="label" class="min-w-0"><dt class="text-sm text-muted-foreground">{{ label }}</dt><dd class="mt-1 break-words text-sm font-medium">{{ value }}</dd></div></dl>
        <div v-if="canEditMember || canResetPassword" class="flex flex-wrap gap-2 border-t pt-5" data-test="member-actions"><template v-if="canEditMember"><Button variant="outline" :data-test="`member-edit-${member.id}`" @click="emit('editMember', member)">{{ t('enterprise.orgTree.editTransfer') }}</Button><Button variant="outline" :disabled="busy" data-test="member-toggle" @click="emit('toggleMember', member)">{{ t(member.status === 'active' ? 'enterprise.disabled' : 'enterprise.active') }}</Button><Button variant="destructive" :data-test="`member-remove-${member.id}`" @click="emit('removeMember', member)">{{ t('enterprise.removeMember') }}</Button></template><Button v-if="canResetPassword" variant="outline" :disabled="busy" data-test="member-reset-password" @click="emit('resetPassword', member)">{{ t('enterprise.passwordReset.title') }}</Button></div>
      </template>
      <template v-else-if="department">
        <dl class="grid gap-4 sm:grid-cols-2"><div><dt class="text-sm text-muted-foreground">{{ t('enterprise.orgTree.directCount') }}</dt><dd class="mt-1 text-2xl font-semibold tabular-nums">{{ department.member_count }}</dd></div><div><dt class="text-sm text-muted-foreground">{{ t('enterprise.orgTree.subtreeCount') }}</dt><dd class="mt-1 text-2xl font-semibold tabular-nums">{{ total }}</dd></div></dl>
        <div><h3 class="mb-2 text-sm font-medium">{{ t('enterprise.orgTree.departmentManagers') }}</h3><div class="flex flex-wrap gap-2"><Button v-for="row in managers" :key="row.id" variant="secondary" size="sm" @click="emit('select', `member-${row.id}`)">{{ row.username || row.email }}</Button><p v-if="!managers.length" class="text-sm text-muted-foreground">{{ t(canViewMembers ? 'enterprise.empty' : 'enterprise.orgTree.membersHidden') }}</p></div></div>
        <div><h3 class="mb-2 text-sm font-medium">{{ t('enterprise.orgTree.childDepartments') }}</h3><div class="flex flex-wrap gap-2"><Button v-for="row in children" :key="row.id" variant="outline" size="sm" @click="emit('select', String(row.id))">{{ row.name }}</Button><p v-if="!children.length" class="text-sm text-muted-foreground">{{ t('enterprise.empty') }}</p></div></div>
        <div class="flex flex-wrap gap-2 border-t pt-5"><template v-if="canManageDepartments"><Button variant="outline" data-test="department-child" @click="emit('createDepartment', department.id)">{{ t('enterprise.orgTree.newChild') }}</Button><Button variant="outline" data-test="department-rename" @click="emit('editDepartment', department)">{{ t('enterprise.orgTree.rename') }}</Button><Button variant="outline" data-test="department-move" @click="emit('editDepartment', department, true)">{{ t('enterprise.orgTree.move') }}</Button><Button variant="destructive" data-test="department-delete" @click="emit('deleteDepartment', department)">{{ t('enterprise.deleteDepartment') }}</Button></template><Button v-if="canCreateMembers" variant="default" data-test="department-create-member" @click="emit('createMember', department.id)">{{ t('enterprise.orgTree.newMemberHere') }}</Button></div>
      </template>
      <template v-else>
        <dl class="grid gap-4 sm:grid-cols-2"><div><dt class="text-sm text-muted-foreground">{{ t('enterprise.memberCount') }}</dt><dd class="mt-1 text-3xl font-semibold tabular-nums">{{ selected === '0' ? members.filter(row => !row.department_id).length : memberCount ?? (canViewMembers ? members.length : '—') }}</dd></div><div v-if="selected !== '0'"><dt class="text-sm text-muted-foreground">{{ t('enterprise.departmentCount') }}</dt><dd class="mt-1 text-3xl font-semibold tabular-nums">{{ departments.length }}</dd></div></dl>
        <div v-if="selected !== '0'"><h3 class="mb-2 text-sm font-medium">{{ t('enterprise.orgTree.owners') }}</h3><div class="flex flex-wrap gap-2"><Button v-for="row in owners" :key="row.id" variant="secondary" size="sm" @click="emit('select', `member-${row.id}`)"><Crown class="mr-1 size-3.5 text-amber-600" />{{ row.username || row.email }}</Button><p v-if="!owners.length" class="text-sm text-muted-foreground">{{ t(canViewMembers ? 'enterprise.empty' : 'enterprise.orgTree.membersHidden') }}</p></div></div>
        <div class="flex flex-wrap gap-2 border-t pt-5"><Button v-if="canManageDepartments" variant="outline" @click="emit('createDepartment')">{{ t('enterprise.orgTree.newDepartment') }}</Button><Button v-if="canCreateMembers" @click="emit('createMember')">{{ t('enterprise.orgTree.newMember') }}</Button></div>
      </template>
    </CardContent>
  </Card>
</template>
