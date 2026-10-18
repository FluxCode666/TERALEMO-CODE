<template>
  <form id="enterprise-member-form" class="shadcn-scope space-y-4" @submit.prevent="submit">
    <RadioGroup v-if="admin && !member" v-model="mode" class="flex flex-wrap gap-4"><Label class="flex items-center gap-2"><RadioGroupItem value="existing" />{{ t('enterprise.existingUser') }}</Label><Label class="flex items-center gap-2"><RadioGroupItem value="new" />{{ t('enterprise.newAccount') }}</Label></RadioGroup>
    <div v-if="!member && mode === 'existing'" class="space-y-3">
      <Label class="mb-2 block text-sm font-medium text-foreground" for="enterprise-user-search">{{ t('enterprise.searchUsers') }}</Label>
      <div class="flex gap-2">
        <Input id="enterprise-user-search" v-model.trim="search"  @keydown.enter.prevent="searchUsers" />
        <Button variant="outline" type="button"  :disabled="searching" data-test="member-search" @click="searchUsers">{{ t('enterprise.search') }}</Button>
      </div>
      <EnterpriseSelect v-model="userId" :options="userOptions" :aria-label="t('enterprise.selectUser')" data-test="member-user" />
    </div>
    <template v-if="!member && mode === 'new'">
      <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.email') }}</span><Input v-model.trim="form.email"  type="email" autocomplete="off" data-test="member-email" /></Label>
      <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.username') }}</span><Input v-model.trim="form.username"  autocomplete="off" /></Label>
      <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.password') }}</span><Input v-model="form.password"  type="password" autocomplete="new-password" data-test="member-password" /></Label>
    </template>
    <Label v-if="!limitedManager" class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.department') }}</span>
      <EnterpriseSelect v-model="form.department_id" :options="departmentOptions" :aria-label="t('enterprise.department')" data-test="member-department" />
    </Label>
    <Label v-if="!limitedManager" class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.role') }}</span>
      <EnterpriseSelect v-model="form.role" :options="roleOptions" :aria-label="t('enterprise.role')" data-test="member-role" />
    </Label>
    <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.titleField') }}</span><Input v-model.trim="form.title"  maxlength="100" /></Label>
    <Label v-if="member" class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.status') }}</span><EnterpriseSelect v-model="form.status" :options="statusOptions" :aria-label="t('enterprise.status')" data-test="member-status" /></Label>
    <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400" data-test="member-validation">{{ validation }}</p>
    <Button variant="default" type="submit"  :disabled="busy" data-test="member-submit">{{ t('enterprise.save') }}</Button>
  </form>
</template>

<script setup lang="ts">
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
import { adminAPI } from '@/api/admin'
import { enterpriseErrorMessage } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember } from '@/types/enterprise'

const props = defineProps<{ admin?: boolean; member?: EnterpriseMember; departments: EnterpriseDepartment[]; limitedManager?: boolean; departmentId?: number | null; defaultDepartmentId?: number; busy?: boolean }>()
const emit = defineEmits<{ submit: [payload: Record<string, unknown>] }>()
const { t } = useI18n()
const roles = ['owner', 'manager', 'member'] as const
const mode = ref(props.admin ? 'existing' : 'new')
const search = ref('')
const searching = ref(false)
const users = ref<{ id: number; username: string; email: string }[]>([])
const userId = ref(0)
const validation = ref('')
const initialDepartment = props.member
  ? props.member.department_id ?? 0
  : props.limitedManager ? props.departmentId ?? 0 : props.defaultDepartmentId ?? 0
const form = reactive({ email: '', username: '', password: '', department_id: initialDepartment, role: props.member?.role ?? 'member', title: props.member?.title ?? '', status: props.member?.status ?? 'active' })

const userOptions = computed(() => [{ value: 0, label: t('enterprise.selectUser') }, ...users.value.map(user => ({ value: user.id, label: `${user.username || user.email} · ${user.email}` }))])
const departmentOptions = computed(() => [{ value: 0, label: t('enterprise.noDepartment') }, ...props.departments.map(item => ({ value: item.id, label: item.name }))])
const roleOptions = computed(() => roles.map(value => ({ value, label: t(`enterprise.roles.${value}`) })))
const statusOptions = computed(() => ['active', 'disabled'].map(value => ({ value, label: t(`enterprise.${value}`) })))

async function searchUsers() {
  searching.value = true
  try {
    const result = await adminAPI.users.list(1, 50, { search: search.value, role: 'user' })
    users.value = result.items.filter(user => !user.enterprise)
  } catch (error) {
    validation.value = enterpriseErrorMessage(error, t)
  } finally { searching.value = false }
}

function submit() {
  validation.value = ''
  if (props.limitedManager && !props.member) {
    form.role = 'member'
    form.department_id = props.departmentId ?? 0
  }
  if (form.role === 'manager' && !form.department_id) { validation.value = t('enterprise.validationManagerDepartment'); return }
  if (props.member) {
    emit('submit', props.limitedManager ? { status: form.status, title: form.title } : { department_id: form.department_id, role: form.role, status: form.status, title: form.title })
    return
  }
  let identity: Record<string, unknown>
  if (mode.value === 'existing') {
    if (!userId.value) { validation.value = t('enterprise.validationUser'); return }
    identity = { user_id: userId.value }
  } else {
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email)) { validation.value = t('enterprise.validationEmail'); return }
    if (form.password.length < 8) { validation.value = t('enterprise.validationPassword'); return }
    identity = { email: form.email, password: form.password, username: form.username || undefined }
  }
  emit('submit', { ...identity, department_id: form.department_id || undefined, role: form.role, title: form.title })
}
</script>
