<template>
  <section ref="root" class="shadcn-scope relative min-w-0" :style="{ '--usage-header-height': `${headerHeight}px` }">
    <div ref="sentinel" class="pointer-events-none absolute left-0 top-0 h-px w-px" aria-hidden="true" />
    <div class="space-y-5">
      <form class="usage-filters sticky z-20 rounded-xl border bg-card p-3 text-card-foreground shadow-sm sm:p-4" :aria-label="t('enterprise.usageAnalysis.filters')" @submit.prevent="searchUsage">
        <div v-if="isSticky" class="flex min-w-0 items-center gap-3">
          <Badge variant="secondary" class="shrink-0"><SlidersHorizontal class="mr-1 size-3" />{{ t('enterprise.usageAnalysis.filters') }}</Badge>
          <p data-test="usage-filter-summary" class="min-w-0 flex-1 truncate text-sm text-muted-foreground" :title="filterSummary" aria-live="polite">{{ filterSummary }}</p>
          <span v-if="busy" role="status" class="sr-only">{{ t('common.loading') }}</span>
          <Button type="button" variant="ghost" size="sm" data-test="usage-expand-filters" class="shrink-0" :aria-expanded="showFilters" aria-controls="enterprise-usage-filters" @click="expanded = !expanded">{{ t(showFilters ? 'enterprise.usageAnalysis.collapse' : 'enterprise.usageAnalysis.expand') }}</Button>
        </div>
        <div v-if="showFilters" id="enterprise-usage-filters" :class="{ 'mt-3': isSticky }">
          <div class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-6">
            <div class="min-w-0 space-y-2 sm:col-span-2">
              <Label>{{ t('enterprise.usageAnalysis.dateRange') }}</Label>
              <EnterpriseDateRangePicker v-model:start-date="filters.start_date" v-model:end-date="filters.end_date" />
            </div>
            <div v-if="canFilterMembers" class="min-w-0 space-y-2"><Label>{{ t('enterprise.members') }}</Label><EnterpriseUsageCombobox v-model="filters.user_id" data-test="usage-member" :label="t('enterprise.members')" :options="memberOptions" :selected-label="memberOptions.find(item => item.value === filters.user_id)?.label" /></div>
            <div v-if="canFilterDepartments" class="min-w-0 space-y-2"><Label for="enterprise-usage-department">{{ t('enterprise.department') }}</Label><Select :model-value="filters.department_id || '__all__'" @update:model-value="value => filters.department_id = value === '__all__' ? '' : String(value)"><SelectTrigger id="enterprise-usage-department" data-test="usage-department" :aria-label="t('enterprise.department')"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="__all__">{{ t('enterprise.all') }}</SelectItem><SelectItem v-for="option in departmentOptions" :key="option.value" :value="option.value">{{ option.label }}</SelectItem></SelectContent></Select></div>
            <div class="min-w-0 space-y-2"><Label>{{ t('enterprise.model') }}</Label><EnterpriseUsageCombobox v-model="filters.model" data-test="usage-model" :label="t('enterprise.model')" :search-options="searchModels" :search-scope="`${filters.start_date}:${filters.end_date}`" /></div>
            <div class="min-w-0 space-y-2"><Label>{{ t('enterprise.apiKey') }}</Label><EnterpriseUsageCombobox v-model="filters.api_key_id" data-test="usage-api-key" :label="t('enterprise.apiKey')" :search-options="searchKeys" :selected-label="selectedKey?.label" @select="selectedKey = $event" /></div>
          </div>
          <div class="mt-4 flex flex-wrap items-center gap-2">
            <TooltipProvider><Tooltip><TooltipTrigger as-child><Button type="submit" data-test="usage-search"><Search class="mr-2 size-4" />{{ t('enterprise.search') }}</Button></TooltipTrigger><TooltipContent>{{ t('enterprise.usageAnalysis.snapshotHint') }}</TooltipContent></Tooltip></TooltipProvider>
            <Button type="button" variant="outline" data-test="usage-reset" @click="resetFilters"><RotateCcw class="mr-2 size-4" />{{ t('enterprise.reset') }}</Button>
            <span class="text-xs text-muted-foreground">{{ t('enterprise.usageAnalysis.snapshotHint') }}</span>
            <span v-if="busy && !isSticky" role="status" class="text-xs text-muted-foreground">{{ t('common.loading') }}</span>
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
        <EnterpriseUsageTrend v-model:granularity="granularity" :items="trend" :loading="trendAction.busy.value" />
        <EnterpriseUsageDistribution :items="models" :loading="summaryAction.busy.value" />
      </div>
      <div v-if="canFilterMembers || canFilterDepartments" class="grid min-w-0 gap-5 lg:grid-cols-2">
        <EnterpriseUsageRanking v-if="canFilterMembers" data-test="usage-member-ranking" :title="t('enterprise.memberRanking')" :items="memberChartItems" :loading="summaryAction.busy.value" />
        <EnterpriseUsageRanking v-if="canFilterDepartments" data-test="usage-department-ranking" :title="t('enterprise.departmentRanking')" :items="departmentChartItems" :loading="summaryAction.busy.value" />
      </div>
      <EnterpriseUsageDetails :rows="rows" :departments="departments" :total="total" :loading="rowAction.busy.value" :admin="admin" @sort="sortRows" />
      <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="page = $event; loadRows()" @update:page-size="pageSize = $event; page = 1; loadRows()" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, inject, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseOverview, EnterpriseUsageLog, EnterpriseUsageStats, EnterpriseUsageTrend as UsageTrend, EnterpriseUsageModel, EnterpriseMemberUsage, EnterpriseDepartmentUsage, EnterpriseQuery } from '@/types/enterprise'
import Pagination from '@/components/common/Pagination.vue'
import { routeLocationKey } from 'vue-router'
import { Activity, Coins, Calculator, Zap, Search, RotateCcw, SlidersHorizontal } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useMotion } from '@/composables/useMotion'
import EnterpriseDateRangePicker from './EnterpriseDateRangePicker.vue'
import EnterpriseUsageCombobox from './EnterpriseUsageCombobox.vue'
import EnterpriseUsageMetricCard from './EnterpriseUsageMetricCard.vue'
import { createEnterpriseUsageOptionsAPI, type EnterpriseUsageOption } from '../EnterpriseUsageOptionsAPI'
import EnterpriseUsageTrend from './EnterpriseUsageTrend.vue'
import EnterpriseUsageDistribution from './EnterpriseUsageDistribution.vue'
import EnterpriseUsageRanking from './EnterpriseUsageRanking.vue'
import EnterpriseUsageDetails from './EnterpriseUsageDetails.vue'
import { useEnterpriseStickyFilters } from '../useEnterpriseStickyFilters'
import { loadMemberOptions } from '../memberOptions'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const { t } = useI18n()
const route = inject(routeLocationKey, undefined)
const root = ref<HTMLElement>()
const motion = useMotion(root)
const granularity = ref<'day' | 'hour'>('day')
const selectedKey = ref<EnterpriseUsageOption>()
const appliedKeyLabel = ref('')
const usageOptions = computed(() => createEnterpriseUsageOptionsAPI(!!props.admin, Number(route?.params.id) || undefined))
async function searchKeys(search: string, signal: AbortSignal): Promise<EnterpriseUsageOption[]> {
  return (await usageOptions.value.keys(search, signal)).map(item => ({ value: String(item.id), label: `${item.name} · ${item.user.username || item.user.email}` }))
}
async function searchModels(search: string, signal: AbortSignal): Promise<EnterpriseUsageOption[]> {
  return (await usageOptions.value.models(search, signal, { start_date: filters.start_date || undefined, end_date: filters.end_date || undefined })).map(item => ({ value: item.model, label: item.model, detail: t('enterprise.usageAnalysis.optionRequests', { count: item.requests.toLocaleString() }) }))
}
const rowAction = useEnterpriseAction(), summaryAction = useEnterpriseAction(), trendAction = useEnterpriseAction(), optionAction = useEnterpriseAction()
const busy = computed(() => rowAction.busy.value || summaryAction.busy.value || trendAction.busy.value || optionAction.busy.value)
const error = computed(() => rowAction.error.value || summaryAction.error.value || trendAction.error.value || optionAction.error.value)
const page = ref(1), pageSize = ref(20), total = ref(0)
const rows = ref<EnterpriseUsageLog[]>([]), stats = ref<EnterpriseUsageStats | null>(null), trend = ref<UsageTrend[]>([]), models = ref<EnterpriseUsageModel[]>([])
const memberRanking = ref<EnterpriseMemberUsage[]>([]), departmentRanking = ref<EnterpriseDepartmentUsage[]>([])
const departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const filters = reactive({ start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' })
const { sentinel, headerHeight, isSticky, expanded, showFilters } = useEnterpriseStickyFilters()
const sort = reactive<{ sort_by: 'created_at' | 'model'; sort_order: 'asc' | 'desc' }>({ sort_by: 'created_at', sort_order: 'desc' })
const appliedQuery = ref<EnterpriseQuery>({})
const validation = ref('')
const canFilterMembers = computed(() => props.admin || !!props.overview?.permissions.view_members)
const canFilterDepartments = computed(() => props.admin || !!props.overview?.permissions.view_department_usage)
const memberChartItems = computed(() => memberRanking.value.map(item => ({ ...item, id: item.user_id, label: item.username || item.email || `#${item.user_id}` })))
const departmentChartItems = computed(() => departmentRanking.value.map(item => ({ ...item, id: item.department_id ?? 'none', label: item.department_name || t('enterprise.noDepartment') })))
const memberOptions = computed(() => members.value.map(member => ({ value: String(member.user_id), label: member.username || member.email, detail: member.username ? member.email : undefined })))
const departmentOptions = computed(() => departments.value.map(department => ({ value: String(department.id), label: department.name })))
const filterSummary = computed(() => {
  const query = appliedQuery.value
  const dates = query.start_date || query.end_date ? `${query.start_date || '…'} – ${query.end_date || '…'}` : t('enterprise.usageAnalysis.allTime')
  return [dates,
    query.user_id ? members.value.find(member => member.user_id === query.user_id)?.username || members.value.find(member => member.user_id === query.user_id)?.email || `#${query.user_id}` : '',
    query.department_id ? departments.value.find(department => department.id === query.department_id)?.name || `#${query.department_id}` : '',
    query.model, query.api_key_id ? appliedKeyLabel.value || `${t('enterprise.apiKey')} #${query.api_key_id}` : ''
  ].filter(Boolean).join(' · ')
})
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
  appliedQuery.value = query(); page.value = 1
  appliedKeyLabel.value = selectedKey.value?.label ?? ''
  expanded.value = false
  // A failed new search must not combine previous statistics with new rows.
  rows.value = []; total.value = 0; stats.value = null; trend.value = []; models.value = []
  memberRanking.value = []; departmentRanking.value = []
  void load()
}
function resetFilters() { selectedKey.value = undefined; Object.assign(filters, { start_date: '', end_date: '', user_id: '', department_id: '', model: '', api_key_id: '' }); searchUsage() }
async function reload() {
  await Promise.all([
    optionAction.read(signal => Promise.all([canFilterDepartments.value ? props.api.departments({ signal }) : Promise.resolve([]), canFilterMembers.value ? loadMemberOptions(props.api, signal) : Promise.resolve([])]), ([departmentRows, memberRows]) => { departments.value = departmentRows; members.value = memberRows }),
    load()
  ])
}
async function loadTrend() {
  const params = { ...appliedQuery.value, granularity: granularity.value }
  trend.value = []
  await trendAction.read(signal => props.api.usageTrend(params, { signal }), result => { trend.value = result })
}
watch(granularity, () => { void loadTrend() })
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
