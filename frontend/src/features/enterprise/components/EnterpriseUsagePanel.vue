<template>
  <section class="min-w-0 space-y-5">
    <!-- Sticky is relative to the nearest scrolling ancestor, including AppLayout main. -->
    <form class="usage-filters sticky top-0 z-40 rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900" :aria-label="t('enterprise.usageAnalysis.filters')" @submit.prevent="searchUsage">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap items-center gap-2"><span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('enterprise.usageAnalysis.filters') }}</span><button v-for="period in periods" :key="period" type="button" class="rounded-md border border-gray-200 px-2.5 py-1.5 text-xs text-gray-600 hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:text-gray-300 dark:hover:text-primary-400" :data-test="`usage-period-${period}`" @click="setPeriod(period)">{{ t(`enterprise.usageAnalysis.${period}`) }}</button></div>
        <button type="button" class="text-sm font-medium text-primary-600 dark:text-primary-400 sm:hidden" :aria-expanded="filtersExpanded" aria-controls="enterprise-usage-filters" @click="filtersExpanded = !filtersExpanded">{{ t(filtersExpanded ? 'enterprise.usageAnalysis.collapse' : 'enterprise.usageAnalysis.expand') }}</button>
      </div>
      <div id="enterprise-usage-filters" class="mt-3 grid max-h-[40vh] grid-cols-1 gap-3 overflow-y-auto sm:max-h-[60vh] sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6" :class="{ 'hidden sm:grid': !filtersExpanded }">
        <label><span class="input-label">{{ t('enterprise.startDate') }}</span><input v-model="filters.start_date" class="input w-full" type="date" /></label>
        <label><span class="input-label">{{ t('enterprise.endDate') }}</span><input v-model="filters.end_date" class="input w-full" type="date" /></label>
        <label v-if="canFilterMembers"><span class="input-label">{{ t('enterprise.members') }}</span><select v-model="filters.user_id" class="input w-full"><option value="">{{ t('enterprise.all') }}</option><option v-for="member in members" :key="member.id" :value="member.user_id">{{ member.username || member.email }}</option></select></label>
        <label v-if="canFilterDepartments"><span class="input-label">{{ t('enterprise.department') }}</span><select v-model="filters.department_id" class="input w-full"><option value="">{{ t('enterprise.all') }}</option><option v-for="department in departments" :key="department.id" :value="department.id">{{ department.name }}</option></select></label>
        <label><span class="input-label">{{ t('enterprise.model') }}</span><input v-model.trim="filters.model" class="input w-full" /></label>
        <label><span class="input-label">{{ t('enterprise.apiKey') }} ID</span><input v-model="filters.api_key_id" class="input w-full" type="number" min="1" step="1" /></label>
      </div>
      <div class="mt-3 flex flex-wrap items-center gap-3"><button class="btn btn-primary">{{ t('enterprise.search') }}</button><button type="button" class="btn btn-secondary" @click="resetFilters">{{ t('enterprise.reset') }}</button><span class="text-xs text-gray-500 dark:text-gray-400">{{ t('enterprise.usageAnalysis.snapshotHint') }}</span><span v-if="busy" role="status" class="text-xs text-primary-600 dark:text-primary-400">{{ t('common.loading') }}</span></div>
      <p v-if="validation" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button type="button" class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <div v-if="summaryAction.busy.value && !stats" class="grid gap-4 sm:grid-cols-3" :aria-label="t('common.loading')"><div v-for="index in 3" :key="index" class="h-28 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-800" /></div>
    <div v-if="stats" class="grid gap-4 sm:grid-cols-3" :aria-busy="summaryAction.busy.value"><StatCard :title="t('enterprise.requests')" :value="stats.total_requests" :icon="RequestsIcon" /><StatCard :title="t('enterprise.tokens')" :value="stats.total_tokens" :icon="TokensIcon" icon-variant="warning" /><StatCard :title="t('enterprise.cost')" :value="usageMoney(stats.total_actual_cost)" :icon="CostIcon" icon-variant="success" /></div>
    <p v-if="stats" class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.usageAnalysis.averageCost') }}: <span class="font-medium tabular-nums text-gray-700 dark:text-gray-200">{{ usageMoney(stats.total_requests ? stats.total_actual_cost / stats.total_requests : 0) }}</span></p>
    <EnterpriseUsageTrend :items="trend" :loading="summaryAction.busy.value" />
    <EnterpriseUsageDistribution :items="models" :loading="summaryAction.busy.value" />
    <div v-if="canFilterMembers || canFilterDepartments" class="grid min-w-0 gap-5 xl:grid-cols-2">
      <EnterpriseUsageRanking v-if="canFilterMembers" data-test="usage-member-ranking" :title="t('enterprise.memberRanking')" :items="memberChartItems" :loading="summaryAction.busy.value" />
      <EnterpriseUsageRanking v-if="canFilterDepartments" data-test="usage-department-ranking" :title="t('enterprise.departmentRanking')" :items="departmentChartItems" :loading="summaryAction.busy.value" />
    </div>
    <EnterpriseUsageDetails :rows="rows" :departments="departments" :total="total" :loading="rowAction.busy.value" :admin="admin" @sort="sortRows" />
    <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; loadRows()" @update:page-size="pageSize = $event; page = 1; loadRows()" />
  </section>
</template>

<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseUsageLog, EnterpriseUsageStats, EnterpriseUsageTrend as UsageTrend, EnterpriseUsageModel, EnterpriseMemberUsage, EnterpriseDepartmentUsage, EnterpriseQuery } from '@/types/enterprise'
import Pagination from '@/components/common/Pagination.vue'
import StatCard from '@/components/common/StatCard.vue'
import Icon from '@/components/icons/Icon.vue'
import EnterpriseUsageTrend from './EnterpriseUsageTrend.vue'
import EnterpriseUsageDistribution from './EnterpriseUsageDistribution.vue'
import EnterpriseUsageRanking from './EnterpriseUsageRanking.vue'
import EnterpriseUsageDetails from './EnterpriseUsageDetails.vue'
import { loadMemberOptions } from '../memberOptions'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { usageDateRange, usageMoney, type UsagePeriod } from '../useEnterpriseUsage'
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const { t } = useI18n()
const RequestsIcon = () => h(Icon, { name: 'chart', size: 'lg' })
const TokensIcon = () => h(Icon, { name: 'bolt', size: 'lg' })
const CostIcon = () => h(Icon, { name: 'dollar', size: 'lg' })
const rowAction = useEnterpriseAction(), summaryAction = useEnterpriseAction(), optionAction = useEnterpriseAction()
const busy = computed(() => rowAction.busy.value || summaryAction.busy.value || optionAction.busy.value)
const error = computed(() => rowAction.error.value || summaryAction.error.value || optionAction.error.value)
const page = ref(1), pageSize = ref(20), total = ref(0)
const rows = ref<EnterpriseUsageLog[]>([]), stats = ref<EnterpriseUsageStats | null>(null), trend = ref<UsageTrend[]>([]), models = ref<EnterpriseUsageModel[]>([])
const memberRanking = ref<EnterpriseMemberUsage[]>([]), departmentRanking = ref<EnterpriseDepartmentUsage[]>([])
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const filters = reactive({ start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' })
const filtersExpanded = ref(false)
const periods: UsagePeriod[] = ['today', 'last7', 'last30', 'month']
const sort = reactive<{ sort_by: 'created_at' | 'model'; sort_order: 'asc' | 'desc' }>({ sort_by: 'created_at', sort_order: 'desc' })
let appliedQuery: EnterpriseQuery = {}
const validation = ref('')
const canFilterMembers = computed(() => props.admin || !!props.overview?.permissions.view_members)
const canFilterDepartments = computed(() => props.admin || !!props.overview?.permissions.view_department_usage)
const memberChartItems = computed(() => memberRanking.value.map(item => ({ ...item, id: item.user_id, label: item.username || item.email || `#${item.user_id}` })))
const departmentChartItems = computed(() => departmentRanking.value.map(item => ({ ...item, id: item.department_id ?? 'none', label: item.department_name || t('enterprise.noDepartment') })))
function setPeriod(period: UsagePeriod) { Object.assign(filters, usageDateRange(period)); validation.value = '' }
function query(): EnterpriseQuery {
  return { start_date: filters.start_date || undefined, end_date: filters.end_date || undefined, user_id: canFilterMembers.value && filters.user_id ? Number(filters.user_id) : undefined, department_id: canFilterDepartments.value && filters.department_id ? Number(filters.department_id) : undefined, model: filters.model || undefined, api_key_id: filters.api_key_id ? Number(filters.api_key_id) : undefined }
}
function validate() {
  validation.value = ''
  if (filters.start_date && filters.end_date) {
    const days = (Date.parse(filters.end_date) - Date.parse(filters.start_date)) / 86400000 + 1
    if (!Number.isFinite(days) || days < 1 || days > 366) validation.value = t('enterprise.validationDateRange')
  }
  return !validation.value
}
async function loadRows() {
  const params = { ...appliedQuery, ...sort, page: page.value, page_size: pageSize.value }
  await rowAction.read(signal => props.api.usage(params, { signal }), result => { rows.value = result.items; total.value = result.total })
}
function sortRows(key: string, order: 'asc' | 'desc') {
  if (key !== 'model' && key !== 'created_at') return
  sort.sort_by = key; sort.sort_order = order; page.value = 1
  void loadRows()
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
  appliedQuery = query(); page.value = 1
  // A failed new search must not combine previous statistics with new rows.
  rows.value = []; total.value = 0; stats.value = null; trend.value = []; models.value = []
  memberRanking.value = []; departmentRanking.value = []
  void load()
}
function resetFilters() { Object.assign(filters, { start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' }); searchUsage() }
async function reload() {
  await Promise.all([
    optionAction.read(signal => Promise.all([canFilterDepartments.value ? props.api.departments({ signal }) : Promise.resolve([]), canFilterMembers.value ? loadMemberOptions(props.api, signal) : Promise.resolve([])]), ([departmentRows, memberRows]) => { departments.value = departmentRows; members.value = memberRows }),
    load()
  ])
}
onMounted(reload)
watch(() => props.refreshKey, reload)
</script>
