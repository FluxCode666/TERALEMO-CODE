<template>
  <section class="space-y-5">
    <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('enterprise.usage') }}</h2>
    <form class="card flex flex-wrap items-end gap-3 p-4" @submit.prevent="searchUsage">
      <label><span class="input-label">{{ t('enterprise.startDate') }}</span><input v-model="filters.start_date" class="input" type="date" /></label>
      <label><span class="input-label">{{ t('enterprise.endDate') }}</span><input v-model="filters.end_date" class="input" type="date" /></label>
      <label v-if="canFilterMembers"><span class="input-label">{{ t('enterprise.members') }}</span><select v-model="filters.user_id" class="input"><option value="">{{ t('enterprise.all') }}</option><option v-for="member in members" :key="member.id" :value="member.user_id">{{ member.username || member.email }}</option></select></label>
      <label v-if="canFilterDepartments"><span class="input-label">{{ t('enterprise.department') }}</span><select v-model="filters.department_id" class="input"><option value="">{{ t('enterprise.all') }}</option><option v-for="department in departments" :key="department.id" :value="department.id">{{ department.name }}</option></select></label>
      <label><span class="input-label">{{ t('enterprise.model') }}</span><input v-model.trim="filters.model" class="input w-full sm:w-44" /></label>
      <label><span class="input-label">{{ t('enterprise.apiKey') }} ID</span><input v-model="filters.api_key_id" class="input w-full sm:w-32" type="number" min="1" step="1" /></label>
      <button class="btn btn-primary" :disabled="busy">{{ t('enterprise.search') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="resetFilters">{{ t('enterprise.reset') }}</button>
    </form>
    <p v-if="validation || error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation || error }} <button v-if="error" class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <div v-if="stats" class="grid gap-4 sm:grid-cols-3"><StatCard :title="t('enterprise.requests')" :value="stats.total_requests" /><StatCard :title="t('enterprise.tokens')" :value="stats.total_tokens" /><StatCard :title="t('enterprise.cost')" :value="money(stats.total_actual_cost)" /></div>
    <div class="card p-4 sm:p-5">
      <h3 class="mb-4 font-medium text-gray-900 dark:text-white">{{ t('enterprise.trend') }}</h3>
      <div v-if="trend.length" class="h-64"><Line :data="chartData" :options="chartOptions" :aria-label="t('enterprise.trend')" role="img" /></div>
      <p v-else class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.empty') }}</p>
      <details v-if="trend.length" class="mt-4 text-sm"><summary class="cursor-pointer text-gray-600 dark:text-gray-300">{{ t('enterprise.detail') }}</summary><DataTable :columns="trendColumns" :data="trend"><template #cell-actual_cost="{ value }">{{ money(value) }}</template></DataTable></details>
    </div>
    <div class="card overflow-hidden"><h3 class="p-4 font-medium text-gray-900 dark:text-white">{{ t('enterprise.modelDistribution') }}</h3><DataTable :columns="modelColumns" :data="models" :loading="busy"><template #cell-actual_cost="{ value }">{{ money(value) }}</template></DataTable></div>
    <div v-if="canFilterMembers" class="card overflow-hidden"><h3 class="p-4 font-medium text-gray-900 dark:text-white">{{ t('enterprise.memberRanking') }}</h3><DataTable :columns="memberColumns" :data="memberRanking" :loading="busy"><template #cell-actual_cost="{ value }">{{ money(value) }}</template></DataTable></div>
    <div v-if="canFilterDepartments" class="card overflow-hidden"><h3 class="p-4 font-medium text-gray-900 dark:text-white">{{ t('enterprise.departmentRanking') }}</h3><DataTable :columns="departmentColumns" :data="departmentRanking" :loading="busy"><template #cell-department_name="{ value }">{{ value || t('enterprise.noDepartment') }}</template><template #cell-actual_cost="{ value }">{{ money(value) }}</template></DataTable></div>
    <DataTable :columns="columns" :data="rows" :loading="busy">
      <template #cell-user="{ row }"><span>{{ row.user?.username || row.user?.email }}</span><p class="text-xs text-gray-500">{{ row.user?.email }}</p></template>
      <template #cell-api_key="{ row }">{{ row.api_key?.name || '—' }}</template>
      <template #cell-department_id="{ value }">{{ departments.find(item => item.id === value)?.name || (value ? `#${value}` : t('enterprise.noDepartment')) }}</template>
      <template #cell-actual_cost="{ value }">{{ money(value) }}</template>
      <template #cell-tokens="{ row }">{{ (row.input_tokens + row.output_tokens + row.cache_creation_tokens + row.cache_read_tokens).toLocaleString() }}</template>
    </DataTable>
    <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; loadRows()" @update:page-size="pageSize = $event; page = 1; loadRows()" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend } from 'chart.js'
import { Line } from 'vue-chartjs'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseUsageLog, EnterpriseUsageStats, EnterpriseUsageTrend, EnterpriseUsageModel, EnterpriseMemberUsage, EnterpriseDepartmentUsage, EnterpriseQuery } from '@/types/enterprise'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import StatCard from '@/components/common/StatCard.vue'
import { loadMemberOptions } from '../memberOptions'
import { useEnterpriseAction } from '../useEnterpriseAction'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const { t } = useI18n()
const rowAction = useEnterpriseAction()
const summaryAction = useEnterpriseAction()
const optionAction = useEnterpriseAction()
const busy = computed(() => rowAction.busy.value || summaryAction.busy.value || optionAction.busy.value)
const error = computed(() => rowAction.error.value || summaryAction.error.value || optionAction.error.value)
const page = ref(1), pageSize = ref(20), total = ref(0)
const rows = ref<EnterpriseUsageLog[]>([]), stats = ref<EnterpriseUsageStats | null>(null), trend = ref<EnterpriseUsageTrend[]>([]), models = ref<EnterpriseUsageModel[]>([])
const memberRanking = ref<EnterpriseMemberUsage[]>([]), departmentRanking = ref<EnterpriseDepartmentUsage[]>([])
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const filters = reactive({ start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' })
let appliedQuery: EnterpriseQuery = {}
const validation = ref('')
const canFilterMembers = computed(() => props.admin || !!props.overview?.permissions.view_members)
const canFilterDepartments = computed(() => props.admin || !!props.overview?.permissions.view_department_usage)
const money = (value: number) => `$${value.toFixed(4)}`
const metricColumns = computed(() => [{ key: 'requests', label: t('enterprise.requests') }, { key: 'total_tokens', label: t('enterprise.tokens') }, { key: 'actual_cost', label: t('enterprise.cost') }])
const modelColumns = computed(() => [{ key: 'model', label: t('enterprise.model') }, ...metricColumns.value])
const memberColumns = computed(() => [{ key: 'username', label: t('enterprise.username') }, { key: 'email', label: t('enterprise.email') }, { key: 'department_name', label: t('enterprise.department') }, ...metricColumns.value])
const departmentColumns = computed(() => [{ key: 'department_name', label: t('enterprise.department') }, ...metricColumns.value])
const trendColumns = computed(() => [{ key: 'date', label: t('enterprise.createdAt') }, ...metricColumns.value])
const columns = computed(() => [{ key: 'created_at', label: t('enterprise.createdAt') }, { key: 'user', label: t('enterprise.members') }, { key: 'department_id', label: t('enterprise.department') }, { key: 'api_key', label: t('enterprise.apiKey') }, { key: 'model', label: t('enterprise.model') }, { key: 'tokens', label: t('enterprise.tokens') }, { key: 'actual_cost', label: t('enterprise.cost') }])
const darkMode = ref(document.documentElement.classList.contains('dark'))
const themeObserver = new MutationObserver(() => { darkMode.value = document.documentElement.classList.contains('dark') })
onMounted(() => themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] }))
onUnmounted(() => themeObserver.disconnect())
const chartData = computed(() => ({ labels: trend.value.map(item => item.date), datasets: [{ label: t('enterprise.cost'), data: trend.value.map(item => item.actual_cost), borderColor: '#14b8a6', backgroundColor: '#14b8a6', tension: 0.25 }] }))
const chartOptions = computed(() => {
  const textColor = darkMode.value ? '#cbd5e1' : '#475569'
  const gridColor = darkMode.value ? 'rgba(148, 163, 184, 0.15)' : '#e5e7eb'
  return {
    responsive: true,
    maintainAspectRatio: false,
    plugins: { legend: { display: true, labels: { color: textColor } } },
    scales: {
      x: { ticks: { color: textColor }, grid: { color: gridColor } },
      y: { beginAtZero: true, ticks: { color: textColor }, grid: { color: gridColor } }
    }
  }
})
function query(): EnterpriseQuery { return { start_date: filters.start_date || undefined, end_date: filters.end_date || undefined, user_id: filters.user_id ? Number(filters.user_id) : undefined, department_id: filters.department_id ? Number(filters.department_id) : undefined, model: filters.model || undefined, api_key_id: filters.api_key_id ? Number(filters.api_key_id) : undefined } }
function validate() {
  validation.value = ''
  if (filters.start_date && filters.end_date) {
    const days = (Date.parse(filters.end_date) - Date.parse(filters.start_date)) / 86400000 + 1
    if (!Number.isFinite(days) || days < 1 || days > 366) validation.value = t('enterprise.validationDateRange')
  }
  return !validation.value
}
async function loadRows() {
  const params = { ...appliedQuery, page: page.value, page_size: pageSize.value }
  await rowAction.read(signal => props.api.usage(params, { signal }), result => { rows.value = result.items; total.value = result.total })
}
async function load() {
  const params = { ...appliedQuery }
  await Promise.all([
    loadRows(),
    summaryAction.read(signal => Promise.all([
      props.api.usageStats(params, { signal }), props.api.usageTrend({ ...params, granularity: 'day' }, { signal }), props.api.usageModels(params, { signal }),
      canFilterMembers.value ? props.api.usageMembers(params, { signal }) : Promise.resolve([]), canFilterDepartments.value ? props.api.usageDepartments(params, { signal }) : Promise.resolve([])
    ]), ([summary, trendRows, modelRows, memberRows, departmentRows]) => {
      stats.value = summary; trend.value = trendRows; models.value = modelRows; memberRanking.value = memberRows; departmentRanking.value = departmentRows
    })
  ])
}
function searchUsage() {
  if (!validate()) return
  appliedQuery = query()
  page.value = 1
  void load()
}
function resetFilters() { Object.assign(filters, { start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' }); searchUsage() }
async function reload() {
  await Promise.all([
    optionAction.read(signal => Promise.all([props.api.departments({ signal }), canFilterMembers.value ? loadMemberOptions(props.api, signal) : Promise.resolve([])]), ([departmentRows, memberRows]) => { departments.value = departmentRows; members.value = memberRows }),
    load()
  ])
}
onMounted(reload)
watch(() => props.refreshKey, reload)
</script>
