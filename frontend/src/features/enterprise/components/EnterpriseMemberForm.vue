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
      <Select v-model="userId" :options="userOptions" :aria-label="t('enterprise.selectUser')" data-test="member-user" />
    </div>
    <template v-if="!member && mode === 'new'">
      <label class="block"><span class="input-label">{{ t('enterprise.email') }}</span><input v-model.trim="form.email" class="input" type="email" autocomplete="off" data-test="member-email" /></label>
      <label class="block"><span class="input-label">{{ t('enterprise.username') }}</span><input v-model.trim="form.username" class="input" autocomplete="off" /></label>
      <label class="block"><span class="input-label">{{ t('enterprise.password') }}</span><input v-model="form.password" class="input" type="password" autocomplete="new-password" data-test="member-password" /></label>
    </template>
    <label v-if="!limitedManager" class="block"><span class="input-label">{{ t('enterprise.department') }}</span>
      <Select v-model="form.department_id" :options="departmentOptions" :aria-label="t('enterprise.department')" data-test="member-department" />
    </label>
    <label v-if="!limitedManager" class="block"><span class="input-label">{{ t('enterprise.role') }}</span>
      <Select v-model="form.role" :options="roleOptions" :aria-label="t('enterprise.role')" data-test="member-role" />
    </label>
    <label class="block"><span class="input-label">{{ t('enterprise.titleField') }}</span><input v-model.trim="form.title" class="input" maxlength="100" /></label>
    <label v-if="member" class="block"><span class="input-label">{{ t('enterprise.status') }}</span><Select v-model="form.status" :options="statusOptions" :aria-label="t('enterprise.status')" data-test="member-status" /></label>
    <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400" data-test="member-validation">{{ validation }}</p>
    <button type="submit" class="btn btn-primary" :disabled="busy" data-test="member-submit">{{ t('enterprise.save') }}</button>
  </form>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
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
