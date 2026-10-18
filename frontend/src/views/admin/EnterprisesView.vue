<template>
  <AppLayout>
    <div class="shadcn-scope min-w-0 space-y-5 text-foreground">
      <header><div class="shadcn-scope flex flex-wrap items-center justify-between gap-4"><div><h1 class="text-2xl font-bold text-foreground">{{ t('admin.enterprises.title') }}</h1><p class="mt-1 text-sm text-muted-foreground">{{ t('admin.enterprises.description') }}</p></div><Button variant="default"  data-test="enterprise-create-open" @click="openCreate">{{ t('admin.enterprises.create') }}</Button></div></header>
      <section>
        <Card><form class="flex flex-wrap gap-3 p-4" @submit.prevent="searchEnterprises">
          <Input type="search" v-model="search" class="sm:max-w-sm" :aria-label="t('enterprise.search')" :placeholder="t('admin.enterprises.searchPlaceholder')" />
          <EnterpriseSelect v-model="status" :options="[{ value: '', label: t('enterprise.all') }, ...statusOptions]" class="w-full sm:w-40" :aria-label="t('enterprise.status')" data-test="enterprise-status-filter" />
          <Button variant="outline" data-test="enterprise-search">{{ t('enterprise.search') }}</Button>
          <p v-if="error" role="alert" class="w-full text-sm text-red-600 dark:text-red-400">{{ error }} <Button variant="ghost" type="button" class="underline" @click="load">{{ t('enterprise.retry') }}</Button></p>
        </form></Card>
      </section>
      <section class="min-w-0">
        <EnterpriseTable :columns="columns" :data="items" :loading="busy">
          <template #cell-name="{ row }"><RouterLink :to="`/admin/enterprises/${row.id}`" class="font-medium text-primary">{{ row.name }}</RouterLink><p class="text-xs text-gray-500">#{{ row.id }}</p><EnterpriseOwnerWarning class="mt-2" :active-owner-count="row.active_owner_count" /></template>
          <template #cell-status="{ value }"><Badge data-test="enterprise-status-badge" class="whitespace-nowrap" :variant="value === 'active' ? 'secondary' : 'destructive'">{{ t(value === 'active' ? 'enterprise.active' : 'enterprise.disabled') }}</Badge></template>
          <template #cell-balance="{ row }">${{ row.balance.toFixed(4) }}</template>
          <template #cell-frozen_balance="{ row }"><span :title="t('enterprise.frozenBalanceHint')" :class="row.frozen_balance > 0 ? 'text-amber-700 dark:text-amber-300' : 'text-muted-foreground'">${{ row.frozen_balance.toFixed(4) }}</span></template>
          <template #cell-created_at="{ row }">{{ formatDateTime(row.created_at) }}</template>
          <template #cell-actions="{ row }"><DropdownMenu><DropdownMenuTrigger as-child><Button variant="ghost" size="icon" :aria-label="`${row.name} · ${t('enterprise.actions')}`" :data-test="`enterprise-menu-${row.id}`"><MoreHorizontal class="size-4" /></Button></DropdownMenuTrigger><DropdownMenuContent align="end"><DropdownMenuItem as-child><RouterLink :to="`/admin/enterprises/${row.id}`">{{ t('admin.enterprises.open') }}</RouterLink></DropdownMenuItem><DropdownMenuItem :data-test="`enterprise-edit-${row.id}`" @select="editing = row">{{ t('enterprise.edit') }}</DropdownMenuItem><DropdownMenuItem :data-test="`enterprise-balance-${row.id}`" @select="balanceTarget = row">{{ t('enterprise.adjustBalance') }}</DropdownMenuItem><DropdownMenuItem :data-test="`enterprise-status-${row.id}`" @select="statusTarget = row">{{ t(row.status === 'active' ? 'enterprise.disabled' : 'enterprise.active') }}</DropdownMenuItem><DropdownMenuSeparator /><DropdownMenuItem class="text-destructive focus:text-destructive" :data-test="`enterprise-delete-${row.id}`" @select="deleting = row">{{ t('admin.enterprises.delete') }}</DropdownMenuItem></DropdownMenuContent></DropdownMenu></template>
          <template #empty><EnterpriseEmpty :title="t('enterprise.empty')" :description="t('admin.enterprises.description')" /></template>
        </EnterpriseTable>
      </section>
      <footer><EnterprisePagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; load()" @update:page-size="pageSize = $event; page = 1; load()" /></footer>
    </div>
    <EnterpriseDialog :show="createOpen" :title="t('admin.enterprises.createTitle')" width="wide" @close="createOpen = false">
      <form class="space-y-5" data-test="enterprise-create-form" @submit.prevent="create">
        <div class="grid gap-4 sm:grid-cols-2">
          <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.name') }}</span><Input v-model.trim="form.name"  maxlength="100" required data-test="enterprise-name" /></Label>
          <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.status') }}</span><EnterpriseSelect v-model="form.status" :options="statusOptions" :aria-label="t('enterprise.status')" data-test="enterprise-status-select" /></Label>
          <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.contactName') }}</span><Input v-model.trim="form.contact_name"  maxlength="100" /></Label>
          <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.contactEmail') }}</span><Input v-model.trim="form.contact_email"  type="email" maxlength="255" /></Label>
          <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.contactPhone') }}</span><Input v-model.trim="form.contact_phone"  maxlength="50" /></Label>
          <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.lowBalanceThreshold') }}</span><Input v-model="form.threshold"  type="number" min="0" step="any" :placeholder="t('enterprise.disabled')" /></Label>
        </div>
        <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.notes') }}</span><textarea v-model.trim="form.notes" class="flex w-full rounded-ui-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50" rows="2"></textarea></Label>
        <fieldset class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <legend class="px-2 text-sm font-medium text-foreground">{{ t('admin.enterprises.initialOwner') }}</legend>
          <RadioGroup v-model="ownerMode" class="flex flex-wrap gap-4"><Label v-for="mode in ownerModes" :key="mode.value" class="flex items-center gap-2"><RadioGroupItem :value="mode.value" />{{ mode.label }}</Label></RadioGroup>
          <div v-if="ownerMode === 'existing'" class="space-y-3">
            <div class="flex gap-2"><Input v-model.trim="ownerSearch"  :aria-label="t('enterprise.searchUsers')" :placeholder="t('enterprise.searchUsers')" @keydown.enter.prevent="searchUsers" /><Button variant="outline" type="button"  :disabled="searchingUsers" @click="searchUsers">{{ t('enterprise.search') }}</Button></div>
            <EnterpriseSelect v-model="ownerUserId" :options="ownerOptions" :aria-label="t('enterprise.selectUser')" data-test="owner-user" />
          </div>
          <div v-else-if="ownerMode === 'new'" class="grid gap-4 sm:grid-cols-2"><Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.email') }}</span><Input v-model.trim="owner.email"  type="email" autocomplete="off" data-test="owner-email" /></Label><Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.username') }}</span><Input v-model.trim="owner.username"  autocomplete="off" /></Label><Label class="sm:col-span-2"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.password') }}</span><Input v-model="owner.password"  type="password" autocomplete="new-password" data-test="owner-password" /></Label></div>
        </fieldset>
        <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
        <div class="flex justify-end gap-3"><Button variant="outline" type="button"  @click="createOpen = false">{{ t('enterprise.cancel') }}</Button><Button variant="default"  :disabled="busy" data-test="enterprise-create">{{ t('admin.enterprises.create') }}</Button></div>
      </form>
    </EnterpriseDialog>
    <EnterpriseDeleteDialog v-if="deleting" :key="deleting.id" :enterprise-id="deleting.id" @cancel="deleting = null" @deleted="removed" />
    <EnterpriseDialog :show="!!editing" :title="t('enterprise.edit')" width="wide" @close="editing = null"><EnterpriseBasicsForm v-if="editing" :key="editing.id" :enterprise="editing" @updated="editing = null; load()" /></EnterpriseDialog>
    <EnterpriseDialog :show="!!balanceTarget" :title="t('enterprise.adjustBalance')" @close="balanceTarget = null"><EnterpriseBalanceForm v-if="balanceTarget" :key="balanceTarget.id" :enterprise-id="balanceTarget.id" @updated="balanceTarget = null; load()" /></EnterpriseDialog>
    <EnterpriseConfirm :busy="busy" :show="!!statusTarget" :title="t(statusTarget?.status === 'active' ? 'enterprise.disabled' : 'enterprise.active')" :message="t('enterprise.confirmStatusChange')" :danger="statusTarget?.status === 'active'" @cancel="statusTarget = null" @confirm="changeStatus" />
  </AppLayout>
</template>

<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { MoreHorizontal } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
import { Input } from '@/components/ui/input'
import EnterpriseEmpty from '@/features/enterprise/components/EnterpriseEmpty.vue'
import { adminAPI } from '@/api/admin'
import { enterprisesAPI } from '@/api/admin/enterprises'
import { enterpriseErrorMessage } from '@/api/enterprise'
import type { Enterprise, EnterpriseQuery, CreateEnterpriseRequest } from '@/types/enterprise'
import AppLayout from '@/components/layout/AppLayout.vue'
import EnterpriseTable from '@/features/enterprise/components/EnterpriseTable.vue'
import EnterpriseDialog from '@/features/enterprise/components/EnterpriseDialog.vue'
import EnterpriseConfirm from '@/features/enterprise/components/EnterpriseConfirm.vue'
import EnterprisePagination from '@/features/enterprise/components/EnterprisePagination.vue'
import { useEnterpriseAction } from '@/features/enterprise/useEnterpriseAction'
import EnterpriseBasicsForm from '@/features/enterprise/components/EnterpriseBasicsForm.vue'
import EnterpriseBalanceForm from '@/features/enterprise/components/EnterpriseBalanceForm.vue'
import EnterpriseDeleteDialog from '@/features/enterprise/components/EnterpriseDeleteDialog.vue'
import EnterpriseOwnerWarning from '@/features/enterprise/components/EnterpriseOwnerWarning.vue'
import { formatDateTime } from '@/utils/format'
const { t } = useI18n()
const statusOptions = computed(() => ['active', 'disabled'].map(value => ({ value, label: t(`enterprise.${value}`) })))
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
const ownerOptions = computed(() => [{ value: 0, label: t('enterprise.selectUser') }, ...users.value.map(user => ({ value: user.id, label: `${user.username || user.email} · ${user.email}` }))])
const ownerModes = computed(() => [{ value: 'none', label: t('admin.enterprises.noOwner') }, { value: 'existing', label: t('enterprise.existingUser') }, { value: 'new', label: t('enterprise.newAccount') }])
const columns = computed(() => [{ key: 'name', label: t('admin.enterprises.name') }, { key: 'status', label: t('admin.enterprises.status') }, { key: 'balance', label: t('admin.enterprises.balance') }, { key: 'frozen_balance', label: t('enterprise.frozenBalance') }, { key: 'member_count', label: t('admin.enterprises.members') }, { key: 'department_count', label: t('admin.enterprises.departments') }, { key: 'created_at', label: t('admin.enterprises.createdAt') }, { key: 'actions', label: t('enterprise.actions') }])
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
