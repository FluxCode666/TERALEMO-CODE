<template>
  <section class="relative min-w-0" :style="{ '--usage-header-height': `${headerHeight}px` }">
    <div ref="sentinel" class="pointer-events-none absolute left-0 top-0 h-px w-px" aria-hidden="true" />
    <div class="space-y-5">
      <form class="usage-filters sticky z-20 rounded-xl border border-gray-200 bg-white p-3 shadow-sm dark:border-dark-700 dark:bg-dark-900 sm:p-4" :aria-label="t('enterprise.usageAnalysis.filters')" @submit.prevent="searchUsage">
        <div v-if="isSticky" class="flex min-w-0 items-center gap-3">
          <p data-test="usage-filter-summary" class="min-w-0 flex-1 truncate text-sm text-gray-700 dark:text-gray-200" :title="filterSummary" aria-live="polite">{{ filterSummary }}</p>
          <span v-if="busy" role="status" class="sr-only">{{ t('common.loading') }}</span>
          <button type="button" data-test="usage-expand-filters" class="shrink-0 text-sm font-medium text-primary-600 dark:text-primary-400" :aria-expanded="showFilters" aria-controls="enterprise-usage-filters" @click="expanded = !expanded">{{ t(showFilters ? 'enterprise.usageAnalysis.collapse' : 'enterprise.usageAnalysis.expand') }}</button>
        </div>
        <div v-if="showFilters" id="enterprise-usage-filters" :class="{ 'mt-3': isSticky }">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('enterprise.usageAnalysis.filters') }}</span>
            <button v-for="period in periods" :key="period" type="button" class="rounded-md border border-gray-200 px-2.5 py-1.5 text-xs text-gray-600 hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:text-gray-300 dark:hover:text-primary-400" :data-test="`usage-period-${period}`" @click="setPeriod(period)">{{ t(`enterprise.usageAnalysis.${period}`) }}</button>
          </div>
          <div class="mt-3 grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-6">
            <div class="min-w-0 sm:col-span-2">
              <span id="enterprise-usage-dates-label" class="input-label">{{ t('admin.dashboard.timeRange') }}</span>
              <DateRangePicker v-model:start-date="filters.start_date" v-model:end-date="filters.end_date" data-test="usage-date-range" aria-labelledby="enterprise-usage-dates-label" />
            </div>
            <div v-if="canFilterMembers" class="min-w-0"><label class="input-label" for="enterprise-usage-member">{{ t('enterprise.members') }}</label><Select id="enterprise-usage-member" v-model="filters.user_id" :options="memberOptions" :aria-label="t('enterprise.members')" data-test="usage-member" :dropdown-z-index="20" searchable /></div>
            <div v-if="canFilterDepartments" class="min-w-0"><label class="input-label" for="enterprise-usage-department">{{ t('enterprise.department') }}</label><Select id="enterprise-usage-department" v-model="filters.department_id" :options="departmentOptions" :aria-label="t('enterprise.department')" data-test="usage-department" :dropdown-z-index="20" searchable /></div>
            <label class="min-w-0"><span class="input-label">{{ t('enterprise.model') }}</span><input v-model.trim="filters.model" class="input w-full" /></label>
            <label class="min-w-0"><span class="input-label">{{ t('enterprise.apiKey') }} ID</span><input v-model="filters.api_key_id" class="input w-full" type="number" min="1" step="1" /></label>
          </div>
          <div class="mt-3 flex flex-wrap items-center gap-3"><button class="btn btn-primary">{{ t('enterprise.search') }}</button><button type="button" class="btn btn-secondary" @click="resetFilters">{{ t('enterprise.reset') }}</button><span class="text-xs text-gray-500 dark:text-gray-400">{{ t('enterprise.usageAnalysis.snapshotHint') }}</span><span v-if="busy && !isSticky" role="status" class="text-xs text-primary-600 dark:text-primary-400">{{ t('common.loading') }}</span></div>
          <p v-if="validation" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
        </div>
      </form>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button type="button" class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
      <div v-if="summaryAction.busy.value && !stats" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4" :aria-label="t('common.loading')"><div v-for="index in 4" :key="index" class="h-28 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-800" /></div>
      <div v-if="stats" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4" :aria-busy="summaryAction.busy.value"><StatCard :title="t('enterprise.requests')" :value="stats.total_requests" :icon="RequestsIcon" /><StatCard :title="t('enterprise.tokens')" :value="stats.total_tokens" :icon="TokensIcon" icon-variant="warning" /><StatCard :title="t('enterprise.cost')" :value="usageMoney(stats.total_actual_cost)" :icon="CostIcon" icon-variant="success" /><StatCard data-test="usage-average-cost" :title="t('enterprise.usageAnalysis.averageCost')" :value="usageMoney(stats.total_requests ? stats.total_actual_cost / stats.total_requests : 0)" :icon="AverageCostIcon" /></div>
      <EnterpriseUsageTrend :items="trend" :loading="summaryAction.busy.value" />
      <EnterpriseUsageDistribution :items="models" :loading="summaryAction.busy.value" />
      <div v-if="canFilterMembers || canFilterDepartments" class="grid min-w-0 gap-5 xl:grid-cols-2">
        <EnterpriseUsageRanking v-if="canFilterMembers" data-test="usage-member-ranking" :title="t('enterprise.memberRanking')" :items="memberChartItems" :loading="summaryAction.busy.value" />
        <EnterpriseUsageRanking v-if="canFilterDepartments" data-test="usage-department-ranking" :title="t('enterprise.departmentRanking')" :items="departmentChartItems" :loading="summaryAction.busy.value" />
      </div>
      <EnterpriseUsageDetails :rows="rows" :departments="departments" :total="total" :loading="rowAction.busy.value" :admin="admin" @sort="sortRows" />
      <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; loadRows()" @update:page-size="pageSize = $event; page = 1; loadRows()" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseUsageLog, EnterpriseUsageStats, EnterpriseUsageTrend as UsageTrend, EnterpriseUsageModel, EnterpriseMemberUsage, EnterpriseDepartmentUsage, EnterpriseQuery } from '@/types/enterprise'
import Pagination from '@/components/common/Pagination.vue'
import StatCard from '@/components/common/StatCard.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Icon from '@/components/icons/Icon.vue'
import EnterpriseUsageTrend from './EnterpriseUsageTrend.vue'
import EnterpriseUsageDistribution from './EnterpriseUsageDistribution.vue'
import EnterpriseUsageRanking from './EnterpriseUsageRanking.vue'
import EnterpriseUsageDetails from './EnterpriseUsageDetails.vue'
import { useEnterpriseStickyFilters } from '../useEnterpriseStickyFilters'
import { loadMemberOptions } from '../memberOptions'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { usageDateRange, usageMoney, type UsagePeriod } from '../useEnterpriseUsage'
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const { t } = useI18n()
const RequestsIcon = () => h(Icon, { name: 'chart', size: 'lg' })
const TokensIcon = () => h(Icon, { name: 'bolt', size: 'lg' })
const CostIcon = () => h(Icon, { name: 'dollar', size: 'lg' })
const AverageCostIcon = () => h(Icon, { name: 'calculator', size: 'lg' })
const rowAction = useEnterpriseAction(), summaryAction = useEnterpriseAction(), optionAction = useEnterpriseAction()
const busy = computed(() => rowAction.busy.value || summaryAction.busy.value || optionAction.busy.value)
const error = computed(() => rowAction.error.value || summaryAction.error.value || optionAction.error.value)
const page = ref(1), pageSize = ref(20), total = ref(0)
const rows = ref<EnterpriseUsageLog[]>([]), stats = ref<EnterpriseUsageStats | null>(null), trend = ref<UsageTrend[]>([]), models = ref<EnterpriseUsageModel[]>([])
const memberRanking = ref<EnterpriseMemberUsage[]>([]), departmentRanking = ref<EnterpriseDepartmentUsage[]>([])
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const filters = reactive({ start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' })
const { sentinel, headerHeight, isSticky, expanded, showFilters } = useEnterpriseStickyFilters()
const periods: UsagePeriod[] = ['today', 'last7', 'last30', 'month']
const sort = reactive<{ sort_by: 'created_at' | 'model'; sort_order: 'asc' | 'desc' }>({ sort_by: 'created_at', sort_order: 'desc' })
const appliedQuery = ref<EnterpriseQuery>({})
const appliedPeriod = ref<UsagePeriod>()
const validation = ref('')
const canFilterMembers = computed(() => props.admin || !!props.overview?.permissions.view_members)
const canFilterDepartments = computed(() => props.admin || !!props.overview?.permissions.view_department_usage)
const memberChartItems = computed(() => memberRanking.value.map(item => ({ ...item, id: item.user_id, label: item.username || item.email || `#${item.user_id}` })))
const departmentChartItems = computed(() => departmentRanking.value.map(item => ({ ...item, id: item.department_id ?? 'none', label: item.department_name || t('enterprise.noDepartment') })))
const memberOptions = computed(() => [{ value: '', label: t('enterprise.all') }, ...members.value.map(member => ({ value: String(member.user_id), label: member.username || member.email }))])
const departmentOptions = computed(() => [{ value: '', label: t('enterprise.all') }, ...departments.value.map(department => ({ value: String(department.id), label: department.name }))])
const filterSummary = computed(() => {
  const query = appliedQuery.value
  const dates = appliedPeriod.value ? t(`enterprise.usageAnalysis.${appliedPeriod.value}`) : query.start_date || query.end_date ? `${query.start_date || '…'} – ${query.end_date || '…'}` : t('enterprise.usageAnalysis.allTime')
  return [dates,
    query.user_id ? members.value.find(member => member.user_id === query.user_id)?.username || members.value.find(member => member.user_id === query.user_id)?.email || `#${query.user_id}` : '',
    query.department_id ? departments.value.find(department => department.id === query.department_id)?.name || `#${query.department_id}` : '',
    query.model, query.api_key_id ? `${t('enterprise.apiKey')} #${query.api_key_id}` : ''
  ].filter(Boolean).join(' · ')
})
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
  const params = { ...appliedQuery.value, ...sort, page: page.value, page_size: pageSize.value }
  await rowAction.read(signal => props.api.usage(params, { signal }), result => { rows.value = result.items; total.value = result.total })
}
function sortRows(key: string, order: 'asc' | 'desc') {
  if (key !== 'model' && key !== 'created_at') return
  sort.sort_by = key; sort.sort_order = order; page.value = 1
  void loadRows()
}
async function load() {
  const params = { ...appliedQuery.value }
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
  appliedQuery.value = query(); page.value = 1
  appliedPeriod.value = periods.find(period => {
    const range = usageDateRange(period)
    return range.start_date === filters.start_date && range.end_date === filters.end_date
  })
  expanded.value = false
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

<style scoped>
.usage-filters {
  top: var(--usage-header-height);
  max-height: calc(100dvh - var(--usage-header-height) - 1rem);
  overflow-y: auto;
}
.usage-filters:has(.date-picker-dropdown) {
  overflow: visible;
}
.usage-filters :deep(.date-picker-trigger) {
  @apply w-full min-w-0 justify-between;
}
.usage-filters :deep(.date-picker-value) {
  @apply min-w-0 truncate;
}
.usage-filters :deep(.date-picker-dropdown) {
  min-width: 0;
  width: min(20rem, calc(100vw - 4rem));
}
.usage-filters :deep(.date-picker-field),
.usage-filters :deep(.date-picker-input) {
  @apply min-w-0;
}
</style>
