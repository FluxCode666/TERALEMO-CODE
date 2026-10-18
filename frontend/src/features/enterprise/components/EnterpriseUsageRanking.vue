<template>
  <Card class="min-w-0 p-4 sm:p-5" :aria-busy="view === 'trend' ? trendLoading : loading">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 class="font-medium text-foreground">{{ title }}</h3><div class="flex flex-wrap items-center gap-2"><EnterpriseUsageChartControls v-model:metric="metric" v-model:view="view" :title="title" /></div></div>
    <EnterpriseUsageStackedTrend v-if="view === 'trend'" :title="title" :items="trendItems" :loading="trendLoading" :metric="metric" />
    <Skeleton v-else-if="loading" class="h-64 w-full" />
    <template v-else-if="items.length">
      <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">{{ t('enterprise.usageAnalysis.topRanking', { count: Math.min(limit, items.length), total: items.length, metric: t(metric === 'actual_cost' ? 'enterprise.cost' : metric === 'tokens' ? 'enterprise.tokens' : 'enterprise.requests') }) }}</p><div class="relative w-full" :style="{ height: `${Math.max(180, displayed.length * 36)}px` }"><Bar :data="chartData" :options="options" :aria-label="title" role="img" /></div>
    </template>
    <p v-else class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.empty') }}</p>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import EnterpriseUsageChartControls from './EnterpriseUsageChartControls.vue'
import EnterpriseUsageStackedTrend from './EnterpriseUsageStackedTrend.vue'
import { usageMetricValue, type UsageMetric, type UsageView, type UsageSeriesPoint } from '../EnterpriseUsageSeries'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, BarElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Bar } from 'vue-chartjs'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { usageMoney, useEnterpriseUsageTheme } from '../useEnterpriseUsage'
ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend)
export interface UsageRankingItem { id: string | number; label: string; requests: number; total_tokens: number; actual_cost: number }
const props = withDefaults(defineProps<{ title: string; items: UsageRankingItem[]; loading: boolean; limit?: number; trendItems?: UsageSeriesPoint[]; trendLoading?: boolean }>(), { limit: 10, trendItems: () => [], trendLoading: false })
const view = defineModel<UsageView>('view', { default: 'share' })
const metric = ref<UsageMetric>('actual_cost')
const { t } = useI18n()
const theme = useEnterpriseUsageTheme()
const sorted = computed(() => [...props.items].sort((a, b) => usageMetricValue(b, metric.value) - usageMetricValue(a, metric.value)))
const displayed = computed(() => sorted.value.slice(0, props.limit))
const chartData = computed(() => ({ labels: displayed.value.map(item => item.label), datasets: [{ label: t(metric.value === 'actual_cost' ? 'enterprise.cost' : metric.value === 'tokens' ? 'enterprise.tokens' : 'enterprise.requests'), data: displayed.value.map(item => usageMetricValue(item, metric.value)), backgroundColor: '#14b8a6', borderRadius: 4, maxBarThickness: 22 }] }))
const options = computed<ChartOptions<'bar'>>(() => ({
  indexAxis: 'y', responsive: true, maintainAspectRatio: false,
  plugins: { legend: { display: false }, tooltip: { callbacks: { label: context => {
    const item = displayed.value[context.dataIndex]
    return item ? [`${t('enterprise.requests')}: ${item.requests.toLocaleString()}`, `${t('enterprise.tokens')}: ${item.total_tokens.toLocaleString()}`, `${t('enterprise.cost')}: ${usageMoney(item.actual_cost)}`] : []
  } } } },
  scales: { x: { beginAtZero: true, ticks: { color: theme.value.text }, grid: { color: theme.value.grid } }, y: { ticks: { color: theme.value.text, callback: function(value) { const label = this.getLabelForValue(Number(value)); return label.length > 24 ? `${label.slice(0, 22)}…` : label } }, grid: { display: false } } }
}))
</script>
