<template>
  <section ref="root" class="shadcn-scope relative min-w-0" :style="{ '--usage-header-height': `${headerHeight}px` }">
    <div ref="sentinel" class="pointer-events-none absolute left-0 top-0 h-px w-px" aria-hidden="true" />
    <div class="space-y-5">
      <form class="usage-filters sticky z-20 rounded-xl border bg-card p-3 text-card-foreground shadow-sm sm:p-4" :aria-label="t('enterprise.usageAnalysis.filters')" @submit.prevent="searchUsage">
        <div v-if="isSticky" class="flex min-w-0 items-center gap-3">
          <Badge variant="secondary" class="shrink-0"><SlidersHorizontal class="mr-1 size-3" />{{ t('enterprise.usageAnalysis.filters') }}</Badge>
          <p data-test="usage-filter-summary" class="min-w-0 flex-1 truncate text-sm text-muted-foreground" :title="summaryTitle" aria-live="polite">{{ filterSummary }}</p>
          <span v-if="busy" role="status" class="sr-only">{{ t('common.loading') }}</span>
          <Button type="button" variant="ghost" size="sm" data-test="usage-expand-filters" class="shrink-0" :aria-expanded="showFilters" aria-controls="enterprise-usage-filters" @click="expanded = !expanded">{{ t(showFilters ? 'enterprise.usageAnalysis.collapse' : 'enterprise.usageAnalysis.expand') }}</Button>
        </div>
        <div v-if="showFilters" id="enterprise-usage-filters" :class="{ 'mt-3': isSticky }">
          <div class="flex min-w-0 flex-wrap items-center gap-2" data-test="usage-filter-controls">
            <div class="w-fit max-w-full shrink-0"><EnterpriseDateRangePicker v-model:start-date="filters.start_date" v-model:end-date="filters.end_date" v-model:period="datePeriod" :start-time="rollingRange.start_time" :end-time="rollingRange.end_time" /></div>
            <EnterpriseUsageCombobox v-if="canFilterMembers" v-model="filters.user_id" class="w-40 max-w-full shrink-0" data-test="usage-member" :label="t('enterprise.members')" :options="memberOptions" :selected-label="memberOptions.find(item => item.value === filters.user_id)?.label" />
            <div v-if="canFilterDepartments" class="w-44 max-w-full shrink-0"><EnterpriseUsageDepartmentTree v-model="filters.department_id" :departments="departments" :root-id="departmentRoot" /></div>
            <EnterpriseUsageCombobox v-model="filters.model" class="w-40 max-w-full shrink-0" data-test="usage-model" :label="t('enterprise.model')" :search-options="searchModels" :search-scope="JSON.stringify(draftDateQuery)" />
            <EnterpriseUsageCombobox v-model="filters.api_key_id" class="w-44 max-w-full shrink-0" data-test="usage-api-key" :label="t('enterprise.apiKey')" :search-options="searchKeys" :selected-label="selectedKey?.label" @select="selectedKey = $event" />
            <ToggleGroup type="single" :model-value="granularity" size="sm" class="h-10 shrink-0 gap-0 rounded-md border p-1" data-test="usage-granularity" :aria-label="t('admin.dashboard.granularity')" @update:model-value="changeGranularity">
              <ToggleGroupItem value="day" class="h-8 px-2.5" data-test="usage-granularity-day">{{ t('enterprise.usageAnalysis.byDay') }}</ToggleGroupItem>
              <ToggleGroupItem value="hour" class="h-8 px-2.5" data-test="usage-granularity-hour" :disabled="!allowsHourly" :title="!allowsHourly ? t('enterprise.usageAnalysis.hourLimit') : undefined" :aria-describedby="!allowsHourly ? 'usage-hour-limit' : undefined">{{ t('enterprise.usageAnalysis.byHour') }}</ToggleGroupItem>
            </ToggleGroup>
            <span v-if="!allowsHourly || hourNotice" id="usage-hour-limit" role="status" class="max-w-xs text-xs text-muted-foreground">{{ t('enterprise.usageAnalysis.hourLimit') }}<span v-if="hourNotice"> {{ t('enterprise.usageAnalysis.switchedToDay') }}</span></span>
            <span v-if="busy && !isSticky" role="status" class="sr-only">{{ t('common.loading') }}</span>
            <div class="ml-auto flex shrink-0 items-center gap-2">
              <TooltipProvider><Tooltip><TooltipTrigger as-child><Button type="submit" data-test="usage-search"><Search class="mr-1.5 size-4" />{{ t('enterprise.search') }}</Button></TooltipTrigger><TooltipContent>{{ t('enterprise.usageAnalysis.snapshotHint') }}</TooltipContent></Tooltip></TooltipProvider>
              <Button type="button" variant="outline" data-test="usage-reset" @click="resetFilters"><RotateCcw class="mr-1.5 size-4" />{{ t('enterprise.reset') }}</Button>
            </div>
          </div>
          <p v-if="validation" role="alert" class="mt-3 text-sm text-destructive">{{ validation }}</p>
        </div>
      </form>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }} <Button type="button" variant="link" size="sm" @click="load">{{ t('enterprise.retry') }}</Button></p>
      <div v-if="summaryAction.busy.value && !stats" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4" :aria-label="t('common.loading')"><Skeleton v-for="index in 4" :key="index" class="h-28 w-full rounded-xl" /></div>
      <div v-if="stats" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4" :aria-busy="summaryAction.busy.value">
        <EnterpriseUsageMetricCard :title="t('enterprise.requests')" :value="stats.total_requests" :icon="Activity" />
        <EnterpriseUsageMetricCard :title="t('enterprise.tokens')" :value="stats.total_tokens" :icon="Zap" />
        <EnterpriseUsageMetricCard :title="t('enterprise.cost')" :value="stats.total_actual_cost" :icon="Coins" money />
        <EnterpriseUsageMetricCard data-test="usage-average-cost" :title="t('enterprise.usageAnalysis.averageCost')" :value="stats.total_requests ? stats.total_actual_cost / stats.total_requests : 0" :icon="Calculator" money />
      </div>
      <div class="grid min-w-0 grid-cols-1 items-stretch gap-5 lg:grid-cols-2" data-test="usage-charts-row">
        <EnterpriseUsageTrend :granularity="granularity" :items="trend" :loading="trendAction.busy.value" />
        <EnterpriseUsageDistribution v-model:view="modelView" :items="models" :loading="summaryAction.busy.value" :trend-items="modelTrend" :trend-loading="breakdownAction.busy.value" />
      </div>
      <div v-if="canFilterMembers || canFilterDepartments" class="grid min-w-0 gap-5 lg:grid-cols-2">
        <EnterpriseUsageRanking v-if="canFilterMembers" v-model:view="memberView" :trend-items="memberTrend" :trend-loading="breakdownAction.busy.value" data-test="usage-member-ranking" :title="t('enterprise.memberRanking')" :items="memberChartItems" :loading="summaryAction.busy.value" />
        <EnterpriseUsageRanking v-if="canFilterDepartments" v-model:view="departmentView" :trend-items="departmentTrend" :trend-loading="breakdownAction.busy.value" data-test="usage-department-ranking" :title="t('enterprise.departmentRanking')" :items="departmentChartItems" :loading="summaryAction.busy.value" />
      </div>
      <EnterpriseUsageDetails :rows="rows" :departments="departments" :total="total" :loading="rowAction.busy.value" :admin="admin" @sort="sortRows">
        <template #actions="{ columns }"><Button type="button" variant="outline" size="sm" data-test="usage-export" :disabled="exporting" :aria-busy="exporting" @click="exportCsv(columns)"><Download class="mr-2 size-4" />{{ t(exporting ? 'common.exporting' : 'enterprise.usageAnalysis.exportCsv') }}</Button></template>
      </EnterpriseUsageDetails>
      <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; loadRows()" @update:page-size="pageSize = $event; page = 1; loadRows()" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, inject, nextTick, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import { useAppStore } from '@/stores/app'
import { enterpriseErrorMessage } from '@/api/enterprise'
import { buildEnterpriseUsageCsv, enterpriseUsageCsvFilename, enterpriseUsageExportLimit, enterpriseUsageExportPageSize } from '../usageCsv'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseUsageLog, EnterpriseUsageStats, EnterpriseUsageTrend as UsageTrend, EnterpriseUsageModel, EnterpriseMemberUsage, EnterpriseDepartmentUsage, EnterpriseQuery } from '@/types/enterprise'
import Pagination from '@/components/common/Pagination.vue'
import { routeLocationKey } from 'vue-router'
import { Activity, Coins, Calculator, Zap, Search, RotateCcw, SlidersHorizontal, Download } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useMotion } from '@/composables/useMotion'
import EnterpriseDateRangePicker from './EnterpriseDateRangePicker.vue'
import EnterpriseUsageCombobox from './EnterpriseUsageCombobox.vue'
import EnterpriseUsageDepartmentTree from './EnterpriseUsageDepartmentTree.vue'
import { usageAllowsHourly, usageDepartmentTree, type UsageView, type UsageSeriesPoint, type UsageDepartmentNode } from '../EnterpriseUsageSeries'
import EnterpriseUsageMetricCard from './EnterpriseUsageMetricCard.vue'
import { createEnterpriseUsageOptionsAPI, type EnterpriseUsageOption } from '../EnterpriseUsageOptionsAPI'
import EnterpriseUsageTrend from './EnterpriseUsageTrend.vue'
import EnterpriseUsageDistribution from './EnterpriseUsageDistribution.vue'
import EnterpriseUsageRanking from './EnterpriseUsageRanking.vue'
import EnterpriseUsageDetails from './EnterpriseUsageDetails.vue'
import { useEnterpriseStickyFilters } from '../useEnterpriseStickyFilters'
import { usageTimeRange, type UsagePeriod } from '../useEnterpriseUsage'
import { loadMemberOptions } from '../memberOptions'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; overview?: EnterpriseOverview; enterpriseName?: string; refreshKey?: number }>()
const { t } = useI18n()
const app = useAppStore()
const exporting = ref(false)
let exportController: AbortController | undefined
function cancelExport() { exportController?.abort(); exportController = undefined; exporting.value = false }
onBeforeUnmount(cancelExport)
watch(() => props.api, cancelExport)
async function exportCsv(columns: Column[]) {
  if (exporting.value) return
  const request = new AbortController()
  exportController = request
  exporting.value = true
  const params = { ...appliedQuery.value, ...sort }
  const api = props.api, name = props.enterpriseName || props.overview?.enterprise?.name || 'enterprise'
  const snapshotColumns = columns.map(column => ({ ...column })), snapshotDepartments = [...departments.value]
  try {
    const records: EnterpriseUsageLog[] = []
    let expected = 0
    for (let page = 1; ; page++) {
      const result = await api.usage({ ...params, page, page_size: enterpriseUsageExportPageSize }, { signal: request.signal })
      if (request.signal.aborted) return
      if (result.total > enterpriseUsageExportLimit) { app.showWarning(t('enterprise.usageAnalysis.exportLimit', { count: enterpriseUsageExportLimit.toLocaleString() })); return }
      if (page === 1) expected = result.total
      if (!expected) { app.showWarning(t('enterprise.usageAnalysis.exportEmpty')); return }
      if (!result.items.length || result.total !== expected) { app.showWarning(t('enterprise.usageAnalysis.exportIncomplete')); return }
      records.push(...result.items.slice(0, expected - records.length))
      if (records.length >= expected) break
    }
    const blob = new Blob([buildEnterpriseUsageCsv(records, snapshotColumns, snapshotDepartments, t)], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    try {
      const link = document.createElement('a')
      link.href = url; link.download = enterpriseUsageCsvFilename(name, params)
      link.click()
    } finally { URL.revokeObjectURL(url) }
    app.showSuccess(t('enterprise.usageAnalysis.exportSuccess', { count: records.length.toLocaleString() }))
  } catch (cause) {
    if (!request.signal.aborted) app.showError(`${t('enterprise.usageAnalysis.exportFailed')}: ${enterpriseErrorMessage(cause, t)}`)
  } finally {
    if (exportController === request) { exporting.value = false; exportController = undefined }
  }
}
const route = inject(routeLocationKey, undefined)
const root = ref<HTMLElement>()
const motion = useMotion(root)
const granularity = ref<'day' | 'hour'>('hour')
const hourNotice = ref(false)
const modelView = ref<UsageView>('share'), memberView = ref<UsageView>('share'), departmentView = ref<UsageView>('share')
const modelTrend = ref<UsageSeriesPoint[]>([]), memberTrend = ref<UsageSeriesPoint[]>([]), departmentTrend = ref<UsageSeriesPoint[]>([])
const selectedKey = ref<EnterpriseUsageOption>()
const appliedKeyLabel = ref('')
const usageOptions = computed(() => createEnterpriseUsageOptionsAPI(!!props.admin, Number(route?.params.id) || undefined))
async function searchKeys(search: string, signal: AbortSignal): Promise<EnterpriseUsageOption[]> {
  return (await usageOptions.value.keys(search, signal)).map(item => ({ value: String(item.id), label: `${item.name} · ${item.user.username || item.user.email}` }))
}
async function searchModels(search: string, signal: AbortSignal): Promise<EnterpriseUsageOption[]> {
  return (await usageOptions.value.models(search, signal, draftDateQuery.value)).map(item => ({ value: item.model, label: item.model, detail: t('enterprise.usageAnalysis.optionRequests', { count: item.requests.toLocaleString() }) }))
}
const rowAction = useEnterpriseAction(), summaryAction = useEnterpriseAction(), trendAction = useEnterpriseAction(), optionAction = useEnterpriseAction(), breakdownAction = useEnterpriseAction()
const busy = computed(() => rowAction.busy.value || summaryAction.busy.value || trendAction.busy.value || optionAction.busy.value || breakdownAction.busy.value)
const error = computed(() => rowAction.error.value || summaryAction.error.value || trendAction.error.value || optionAction.error.value || breakdownAction.error.value)
const page = ref(1), pageSize = ref(20), total = ref(0)
const rows = ref<EnterpriseUsageLog[]>([]), stats = ref<EnterpriseUsageStats | null>(null), trend = ref<UsageTrend[]>([]), models = ref<EnterpriseUsageModel[]>([])
const memberRanking = ref<EnterpriseMemberUsage[]>([]), departmentRanking = ref<EnterpriseDepartmentUsage[]>([])
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const filters = reactive({ start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' })
const datePeriod = ref<UsagePeriod | null>('last24')
const rollingRange = ref(usageTimeRange())
const isRollingRange = computed(() => datePeriod.value === 'last24' && !filters.start_date && !filters.end_date)
const draftDateQuery = computed(() => isRollingRange.value ? rollingRange.value : { ...(filters.start_date ? { start_date: filters.start_date } : {}), ...(filters.end_date ? { end_date: filters.end_date } : {}) })
watch(datePeriod, period => { if (period === 'last24') rollingRange.value = usageTimeRange() }, { flush: 'sync' })
const { sentinel, headerHeight, isSticky, expanded, showFilters } = useEnterpriseStickyFilters()
const sort = reactive<{ sort_by: 'created_at' | 'model'; sort_order: 'asc' | 'desc' }>({ sort_by: 'created_at', sort_order: 'desc' })
const appliedQuery = ref<EnterpriseQuery>({ ...rollingRange.value })
const appliedRollingRange = ref(true)
const validation = ref('')
const allowsHourly = computed(() => usageAllowsHourly(appliedQuery.value))
const departmentRoot = computed(() => !props.admin && props.overview?.me?.role === 'manager' ? props.overview.me.department_id : undefined)
const allowedDepartmentIds = computed(() => {
  const ids = new Set<number>()
  function collect(nodes: UsageDepartmentNode[]) { nodes.forEach(node => { ids.add(node.department.id); collect(node.children) }) }
  collect(usageDepartmentTree(departments.value, departmentRoot.value))
  return ids
})
const canFilterMembers = computed(() => props.admin || !!props.overview?.permissions.view_members)
const canFilterDepartments = computed(() => props.admin || !!props.overview?.permissions.view_department_usage)
const memberChartItems = computed(() => memberRanking.value.map(item => ({ ...item, id: item.user_id, label: item.username || item.email || `#${item.user_id}` })))
const departmentChartItems = computed(() => departmentRanking.value.map(item => ({ ...item, id: item.department_id ?? 'none', label: item.department_name || t('enterprise.noDepartment') })))
const memberOptions = computed(() => members.value.map(member => ({ value: String(member.user_id), label: member.username || member.email, detail: member.username ? member.email : undefined })))
const filterSummary = computed(() => {
  const query = appliedQuery.value
  const dates = appliedRollingRange.value ? t('enterprise.usageAnalysis.last24') : query.start_date || query.end_date ? `${query.start_date || '…'} – ${query.end_date || '…'}` : t('enterprise.usageAnalysis.allTime')
  return [dates, t(granularity.value === 'hour' ? 'enterprise.usageAnalysis.byHour' : 'enterprise.usageAnalysis.byDay'),
    query.user_id ? members.value.find(member => member.user_id === query.user_id)?.username || members.value.find(member => member.user_id === query.user_id)?.email || `#${query.user_id}` : '',
    query.department_id ? departmentLabel(query.department_id) : '',
    query.model, query.api_key_id ? appliedKeyLabel.value || `${t('enterprise.apiKey')} #${query.api_key_id}` : ''
  ].filter(Boolean).join(' · ')
})
const summaryTitle = computed(() => appliedRollingRange.value ? `${filterSummary.value} (${appliedQuery.value.start_time} – ${appliedQuery.value.end_time})` : filterSummary.value)
function departmentLabel(id: number) {
  const department = departments.value.find(item => item.id === id)
  return department?.path_names?.join(' / ') || department?.name || `#${id}`
}
function query(): EnterpriseQuery {
  return { ...draftDateQuery.value, user_id: canFilterMembers.value && filters.user_id ? Number(filters.user_id) : undefined, department_id: canFilterDepartments.value && allowedDepartmentIds.value.has(Number(filters.department_id)) ? Number(filters.department_id) : undefined, model: filters.model || undefined, api_key_id: filters.api_key_id ? Number(filters.api_key_id) : undefined }
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
  cancelExport()
  sort.sort_by = key; sort.sort_order = order; page.value = 1
  void loadRows()
}
async function load() {
  const params = { ...appliedQuery.value }
  await Promise.all([
    loadRows(), loadTrend(),
    summaryAction.read(signal => Promise.all([
      props.api.usageStats(params, { signal }), props.api.usageModels(params, { signal }),
      canFilterMembers.value ? props.api.usageMembers(params, { signal }) : Promise.resolve([]), canFilterDepartments.value ? props.api.usageDepartments(params, { signal }) : Promise.resolve([])
    ]), ([summary, modelRows, memberRows, departmentRows]) => {
      stats.value = summary; models.value = modelRows; memberRanking.value = memberRows; departmentRanking.value = departmentRows
    })
  ])
}
function searchUsage() {
  if (!validate()) return
  cancelExport()
  if (isRollingRange.value) rollingRange.value = usageTimeRange()
  appliedRollingRange.value = isRollingRange.value
  appliedQuery.value = query(); page.value = 1
  hourNotice.value = granularity.value === 'hour' && !allowsHourly.value
  if (hourNotice.value) granularity.value = 'day'
  appliedKeyLabel.value = selectedKey.value?.label ?? ''
  expanded.value = false
  // A failed new search must not combine previous statistics with new rows.
  rows.value = []; total.value = 0; stats.value = null; trend.value = []; models.value = []
  memberRanking.value = []; departmentRanking.value = []
  modelTrend.value = []; memberTrend.value = []; departmentTrend.value = []
  void load()
}
function resetFilters() { selectedKey.value = undefined; datePeriod.value = 'last24'; granularity.value = 'hour'; Object.assign(filters, { start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' }); searchUsage() }
async function reload() {
  cancelExport()
  // Refresh the applied rolling window without applying any staged filter edits.
  if (appliedRollingRange.value) {
    const times = usageTimeRange()
    appliedQuery.value = { ...appliedQuery.value, ...times }
    if (isRollingRange.value) rollingRange.value = times
  }
  await Promise.all([
    optionAction.read(signal => Promise.all([canFilterDepartments.value ? props.api.departments({ signal }) : Promise.resolve([]), canFilterMembers.value ? loadMemberOptions(props.api, signal) : Promise.resolve([])]), ([departmentRows, memberRows]) => { departments.value = departmentRows; members.value = memberRows }),
    load()
  ])
}
async function loadTrend() {
  const params = { ...appliedQuery.value, granularity: granularity.value }
  trend.value = []
  await Promise.all([
    trendAction.read(signal => props.api.usageTrend(params, { signal }), result => { trend.value = result }),
    loadBreakdownTrends()
  ])
}
async function loadBreakdownTrends() {
  const params = { ...appliedQuery.value, granularity: granularity.value, top: 8 }
  modelTrend.value = []; memberTrend.value = []; departmentTrend.value = []
  await breakdownAction.read(signal => Promise.all([
    modelView.value === 'trend' ? props.api.usageModelsTrend(params, { signal }) : Promise.resolve([]),
    memberView.value === 'trend' && canFilterMembers.value ? props.api.usageMembersTrend(params, { signal }) : Promise.resolve([]),
    departmentView.value === 'trend' && canFilterDepartments.value ? props.api.usageDepartmentsTrend(params, { signal }) : Promise.resolve([])
  ]), ([modelRows, memberRows, departmentRows]) => {
    modelTrend.value = modelRows.map(item => ({ ...item, id: item.model, label: item.model }))
    memberTrend.value = memberRows.map(item => ({ ...item, id: item.user_id, label: item.username || `#${item.user_id}` }))
    departmentTrend.value = departmentRows.map(item => ({ ...item, id: item.department_id, label: item.department_name || t('enterprise.noDepartment') }))
  })
}
function changeGranularity(value: unknown) {
  if ((value !== 'day' && value !== 'hour') || value === granularity.value || (value === 'hour' && !allowsHourly.value)) return
  granularity.value = value; hourNotice.value = false
  void loadTrend()
}
watch([modelView, memberView, departmentView], () => { void loadBreakdownTrends() })
watch(stats, async () => { await nextTick(); motion.stop(); motion.stagger('[data-usage-card]') })
onMounted(() => { motion.fadeIn(); void reload() })
watch(() => props.refreshKey, reload)
</script>

<style scoped>
.usage-filters {
  top: var(--usage-header-height);
  max-height: calc(100dvh - var(--usage-header-height) - 1rem);
  overflow-y: auto;
}
</style>
