<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions><div class="flex flex-wrap items-center justify-between gap-4"><div><h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.enterprises.title') }}</h1><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.enterprises.description') }}</p></div><button class="btn btn-primary" data-test="enterprise-create-open" @click="openCreate">{{ t('admin.enterprises.create') }}</button></div></template>
      <template #filters>
        <form class="card flex flex-wrap gap-3 p-4" @submit.prevent="searchEnterprises">
          <input v-model.trim="search" class="input w-full sm:w-72" :aria-label="t('enterprise.search')" :placeholder="t('admin.enterprises.searchPlaceholder')" />
          <select v-model="status" class="input w-full sm:w-40" :aria-label="t('enterprise.status')"><option value="">{{ t('enterprise.all') }}</option><option value="active">{{ t('enterprise.active') }}</option><option value="disabled">{{ t('enterprise.disabled') }}</option></select>
          <button class="btn btn-secondary">{{ t('enterprise.search') }}</button>
          <p v-if="error" role="alert" class="w-full text-sm text-red-600 dark:text-red-400">{{ error }} <button type="button" class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
        </form>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="items" :loading="busy">
          <template #cell-name="{ row }"><RouterLink :to="`/admin/enterprises/${row.id}`" class="font-medium text-primary-600 dark:text-primary-400">{{ row.name }}</RouterLink><p class="text-xs text-gray-500">#{{ row.id }}</p></template>
          <template #cell-status="{ value }"><span :class="value === 'active' ? 'badge-success' : 'badge-danger'">{{ t(value === 'active' ? 'enterprise.active' : 'enterprise.disabled') }}</span></template>
          <template #cell-balance="{ value }">${{ value.toFixed(4) }}</template>
          <template #cell-actions="{ row }"><div class="flex flex-wrap gap-3"><RouterLink :to="`/admin/enterprises/${row.id}`" class="text-primary-600 dark:text-primary-400">{{ t('admin.enterprises.open') }}</RouterLink><button class="text-primary-600 dark:text-primary-400" :data-test="`enterprise-edit-${row.id}`" @click="editing = row">{{ t('enterprise.edit') }}</button><button class="text-primary-600 dark:text-primary-400" :data-test="`enterprise-balance-${row.id}`" @click="balanceTarget = row">{{ t('enterprise.adjustBalance') }}</button><button class="text-amber-700 dark:text-amber-400" :data-test="`enterprise-status-${row.id}`" @click="statusTarget = row">{{ t(row.status === 'active' ? 'enterprise.disabled' : 'enterprise.active') }}</button><button class="text-red-600 dark:text-red-400" :data-test="`enterprise-delete-${row.id}`" @click="deleting = row">{{ t('admin.enterprises.delete') }}</button></div></template>
        </DataTable>
      </template>
      <template #pagination><Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; load()" @update:page-size="pageSize = $event; page = 1; load()" /></template>
    </TablePageLayout>
    <BaseDialog :show="createOpen" :title="t('admin.enterprises.createTitle')" width="wide" @close="createOpen = false">
      <form class="space-y-5" data-test="enterprise-create-form" @submit.prevent="create">
        <div class="grid gap-4 sm:grid-cols-2">
          <label><span class="input-label">{{ t('enterprise.name') }}</span><input v-model.trim="form.name" class="input" maxlength="100" required data-test="enterprise-name" /></label>
          <label><span class="input-label">{{ t('enterprise.status') }}</span><select v-model="form.status" class="input"><option value="active">{{ t('enterprise.active') }}</option><option value="disabled">{{ t('enterprise.disabled') }}</option></select></label>
          <label><span class="input-label">{{ t('enterprise.contactName') }}</span><input v-model.trim="form.contact_name" class="input" maxlength="100" /></label>
          <label><span class="input-label">{{ t('enterprise.contactEmail') }}</span><input v-model.trim="form.contact_email" class="input" type="email" maxlength="255" /></label>
          <label><span class="input-label">{{ t('enterprise.contactPhone') }}</span><input v-model.trim="form.contact_phone" class="input" maxlength="50" /></label>
          <label><span class="input-label">{{ t('enterprise.lowBalanceThreshold') }}</span><input v-model="form.threshold" class="input" type="number" min="0" step="any" :placeholder="t('enterprise.disabled')" /></label>
        </div>
        <label class="block"><span class="input-label">{{ t('enterprise.notes') }}</span><textarea v-model.trim="form.notes" class="input" rows="2"></textarea></label>
        <fieldset class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <legend class="px-2 text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.enterprises.initialOwner') }}</legend>
          <div class="flex flex-wrap gap-4"><label v-for="mode in ownerModes" :key="mode.value" class="flex items-center gap-2 text-sm dark:text-gray-200"><input v-model="ownerMode" type="radio" :value="mode.value" />{{ mode.label }}</label></div>
          <div v-if="ownerMode === 'existing'" class="space-y-3">
            <div class="flex gap-2"><input v-model.trim="ownerSearch" class="input" :aria-label="t('enterprise.searchUsers')" :placeholder="t('enterprise.searchUsers')" @keydown.enter.prevent="searchUsers" /><button type="button" class="btn btn-secondary" :disabled="searchingUsers" @click="searchUsers">{{ t('enterprise.search') }}</button></div>
            <select v-model="ownerUserId" class="input" :aria-label="t('enterprise.selectUser')" data-test="owner-user"><option :value="0">{{ t('enterprise.selectUser') }}</option><option v-for="user in users" :key="user.id" :value="user.id">{{ user.username || user.email }} · {{ user.email }}</option></select>
          </div>
          <div v-else-if="ownerMode === 'new'" class="grid gap-4 sm:grid-cols-2"><label><span class="input-label">{{ t('enterprise.email') }}</span><input v-model.trim="owner.email" class="input" type="email" autocomplete="off" data-test="owner-email" /></label><label><span class="input-label">{{ t('enterprise.username') }}</span><input v-model.trim="owner.username" class="input" autocomplete="off" /></label><label class="sm:col-span-2"><span class="input-label">{{ t('enterprise.password') }}</span><input v-model="owner.password" class="input" type="password" autocomplete="new-password" data-test="owner-password" /></label></div>
        </fieldset>
        <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
        <div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" @click="createOpen = false">{{ t('enterprise.cancel') }}</button><button class="btn btn-primary" :disabled="busy" data-test="enterprise-create">{{ t('admin.enterprises.create') }}</button></div>
      </form>
    </BaseDialog>
    <EnterpriseDeleteDialog v-if="deleting" :key="deleting.id" :enterprise-id="deleting.id" @cancel="deleting = null" @deleted="removed" />
    <BaseDialog :show="!!editing" :title="t('enterprise.edit')" width="wide" @close="editing = null"><EnterpriseBasicsForm v-if="editing" :key="editing.id" :enterprise="editing" @updated="editing = null; load()" /></BaseDialog>
    <BaseDialog :show="!!balanceTarget" :title="t('enterprise.adjustBalance')" @close="balanceTarget = null"><EnterpriseBalanceForm v-if="balanceTarget" :key="balanceTarget.id" :enterprise-id="balanceTarget.id" @updated="balanceTarget = null; load()" /></BaseDialog>
    <ConfirmDialog :show="!!statusTarget" :title="t(statusTarget?.status === 'active' ? 'enterprise.disabled' : 'enterprise.active')" :message="t('enterprise.confirmStatusChange')" :danger="statusTarget?.status === 'active'" @cancel="statusTarget = null" @confirm="changeStatus" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { enterprisesAPI } from '@/api/admin/enterprises'
import { enterpriseErrorMessage } from '@/api/enterprise'
import type { Enterprise, EnterpriseQuery, CreateEnterpriseRequest } from '@/types/enterprise'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useEnterpriseAction } from '@/features/enterprise/useEnterpriseAction'
import EnterpriseBasicsForm from '@/features/enterprise/components/EnterpriseBasicsForm.vue'
import EnterpriseBalanceForm from '@/features/enterprise/components/EnterpriseBalanceForm.vue'
import EnterpriseDeleteDialog from '@/features/enterprise/components/EnterpriseDeleteDialog.vue'
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const items = ref<Enterprise[]>([]), deleting = ref<Enterprise | null>(null)
const editing = ref<Enterprise | null>(null), balanceTarget = ref<Enterprise | null>(null), statusTarget = ref<Enterprise | null>(null)
const page = ref(1), pageSize = ref(20), total = ref(0), search = ref(''), status = ref('')
let appliedFilters: Pick<EnterpriseQuery, 'search' | 'status'> = {}
const createOpen = ref(false), validation = ref('')
const form = reactive({ name: '', status: 'active' as 'active' | 'disabled', notes: '', contact_name: '', contact_email: '', contact_phone: '', threshold: '' as string | number })
const ownerMode = ref('none'), ownerUserId = ref(0), ownerSearch = ref(''), searchingUsers = ref(false)
const owner = reactive({ email: '', password: '', username: '' })
const users = ref<{ id: number; username: string; email: string }[]>([])
const ownerModes = computed(() => [{ value: 'none', label: t('admin.enterprises.noOwner') }, { value: 'existing', label: t('enterprise.existingUser') }, { value: 'new', label: t('enterprise.newAccount') }])
const columns = computed(() => [{ key: 'name', label: t('admin.enterprises.name') }, { key: 'status', label: t('admin.enterprises.status') }, { key: 'balance', label: t('admin.enterprises.balance') }, { key: 'member_count', label: t('admin.enterprises.members') }, { key: 'department_count', label: t('admin.enterprises.departments') }, { key: 'created_at', label: t('admin.enterprises.createdAt') }, { key: 'actions', label: t('enterprise.actions') }])
async function load() {
  const params = { ...appliedFilters, page: page.value, page_size: pageSize.value }
  await read(signal => enterprisesAPI.list(params, { signal }), result => {
    items.value = result.items
    total.value = result.total
  })
}
function searchEnterprises() {
  appliedFilters = { search: search.value || undefined, status: status.value || undefined }
  page.value = 1
  void load()
}
function openCreate() { Object.assign(form, { name: '', status: 'active', notes: '', contact_name: '', contact_email: '', contact_phone: '', threshold: '' }); Object.assign(owner, { email: '', password: '', username: '' }); ownerMode.value = 'none'; ownerUserId.value = 0; ownerSearch.value = ''; users.value = []; validation.value = ''; createOpen.value = true }
async function searchUsers() {
  searchingUsers.value = true
  try { const result = await adminAPI.users.list(1, 50, { search: ownerSearch.value, role: 'user' }); users.value = result.items.filter(user => !user.enterprise) }
  catch (error) { validation.value = enterpriseErrorMessage(error, t) }
  finally { searchingUsers.value = false }
}
async function create() {
  validation.value = ''
  if (!form.name) { validation.value = t('enterprise.validationRequired'); return }
  if (String(form.threshold).trim() !== '' && (!Number.isFinite(Number(form.threshold)) || Number(form.threshold) < 0)) { validation.value = t('enterprise.validationQuota'); return }
  const payload: CreateEnterpriseRequest = { name: form.name, status: form.status, notes: form.notes, contact_name: form.contact_name, contact_email: form.contact_email, contact_phone: form.contact_phone, low_balance_threshold: String(form.threshold).trim() === '' ? null : Number(form.threshold) }
  if (ownerMode.value === 'existing') {
    if (!ownerUserId.value) { validation.value = t('enterprise.validationUser'); return }
    payload.owner = { user_id: ownerUserId.value }
  }
  if (ownerMode.value === 'new') {
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(owner.email)) { validation.value = t('enterprise.validationEmail'); return }
    if (owner.password.length < 8) { validation.value = t('enterprise.validationPassword'); return }
    payload.owner = { email: owner.email, password: owner.password, username: owner.username || undefined }
  }
  if (await run(() => enterprisesAPI.create(payload), t('enterprise.saved'))) { createOpen.value = false; page.value = 1; await load() }
}
async function removed() {
  deleting.value = null
  if (items.value.length === 1 && page.value > 1) page.value--
  await load()
}
async function changeStatus() {
  if (!statusTarget.value || busy.value) return
  const target = statusTarget.value
  if (await run(() => enterprisesAPI.update(target.id, { status: target.status === 'active' ? 'disabled' : 'active' }), t('enterprise.saved'))) { statusTarget.value = null; await load() }
}
onMounted(load)
</script>
