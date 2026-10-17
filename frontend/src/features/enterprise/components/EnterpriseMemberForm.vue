<template>
  <form id="enterprise-member-form" class="space-y-4" @submit.prevent="submit">
    <div v-if="admin && !member" class="flex gap-4">
      <label class="flex items-center gap-2"><input v-model="mode" type="radio" value="existing" />{{ t('enterprise.existingUser') }}</label>
      <label class="flex items-center gap-2"><input v-model="mode" type="radio" value="new" />{{ t('enterprise.newAccount') }}</label>
    </div>
    <div v-if="!member && mode === 'existing'" class="space-y-3">
      <label class="input-label" for="enterprise-user-search">{{ t('enterprise.searchUsers') }}</label>
      <div class="flex gap-2">
        <input id="enterprise-user-search" v-model.trim="search" class="input" @keydown.enter.prevent="searchUsers" />
        <button type="button" class="btn btn-secondary" :disabled="searching" @click="searchUsers">{{ t('enterprise.search') }}</button>
      </div>
      <select v-model="userId" class="input" :aria-label="t('enterprise.selectUser')" data-test="member-user">
        <option :value="0">{{ t('enterprise.selectUser') }}</option>
        <option v-for="user in users" :key="user.id" :value="user.id">{{ user.username || user.email }} · {{ user.email }}</option>
      </select>
    </div>
    <template v-if="!member && mode === 'new'">
      <label class="block"><span class="input-label">{{ t('enterprise.email') }}</span><input v-model.trim="form.email" class="input" type="email" autocomplete="off" data-test="member-email" /></label>
      <label class="block"><span class="input-label">{{ t('enterprise.username') }}</span><input v-model.trim="form.username" class="input" autocomplete="off" /></label>
      <label class="block"><span class="input-label">{{ t('enterprise.password') }}</span><input v-model="form.password" class="input" type="password" autocomplete="new-password" data-test="member-password" /></label>
    </template>
    <label v-if="!limitedManager" class="block"><span class="input-label">{{ t('enterprise.department') }}</span>
      <select v-model="form.department_id" class="input" data-test="member-department"><option :value="0">{{ t('enterprise.noDepartment') }}</option><option v-for="department in departments" :key="department.id" :value="department.id">{{ department.name }}</option></select>
    </label>
    <label v-if="!limitedManager" class="block"><span class="input-label">{{ t('enterprise.role') }}</span>
      <select v-model="form.role" class="input" data-test="member-role"><option v-for="role in roles" :key="role" :value="role">{{ t(`enterprise.roles.${role}`) }}</option></select>
    </label>
    <label class="block"><span class="input-label">{{ t('enterprise.titleField') }}</span><input v-model.trim="form.title" class="input" maxlength="100" /></label>
    <label v-if="member" class="block"><span class="input-label">{{ t('enterprise.status') }}</span><select v-model="form.status" class="input"><option value="active">{{ t('enterprise.active') }}</option><option value="disabled">{{ t('enterprise.disabled') }}</option></select></label>
    <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400" data-test="member-validation">{{ validation }}</p>
    <button type="submit" class="btn btn-primary" :disabled="busy" data-test="member-submit">{{ t('enterprise.save') }}</button>
  </form>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { enterpriseErrorMessage } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember } from '@/types/enterprise'

const props = defineProps<{ admin?: boolean; member?: EnterpriseMember; departments: EnterpriseDepartment[]; limitedManager?: boolean; departmentId?: number | null; busy?: boolean }>()
const emit = defineEmits<{ submit: [payload: Record<string, unknown>] }>()
const { t } = useI18n()
const roles = ['owner', 'manager', 'member'] as const
const mode = ref(props.admin ? 'existing' : 'new')
const search = ref('')
const searching = ref(false)
const users = ref<{ id: number; username: string; email: string }[]>([])
const userId = ref(0)
const validation = ref('')
const form = reactive({ email: '', username: '', password: '', department_id: props.member?.department_id ?? props.departmentId ?? 0, role: props.member?.role ?? 'member', title: props.member?.title ?? '', status: props.member?.status ?? 'active' })

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
  if (props.limitedManager) {
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
