<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, BarElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Bar } from 'vue-chartjs'
import { Skeleton } from '@/components/ui/skeleton'
import { usageColors, usageMoney, useEnterpriseUsageTheme } from '../useEnterpriseUsage'
import { usageMetricValue, usageStackedSeries, type UsageMetric, type UsageSeriesPoint } from '../EnterpriseUsageSeries'
ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend)
const props = defineProps<{ title: string; items: UsageSeriesPoint[]; metric: UsageMetric; loading: boolean }>()
const { t } = useI18n()
const theme = useEnterpriseUsageTheme()
const mapped = computed(() => usageStackedSeries(props.items, t('enterprise.usageAnalysis.other')))
const data = computed(() => ({
  labels: mapped.value.dates,
  datasets: mapped.value.series.map((item, index) => ({ label: item.label, data: item.points.map(point => usageMetricValue(point, props.metric)), backgroundColor: item.id === '__other__' ? '#94a3b8' : usageColors[index % usageColors.length], maxBarThickness: 48 }))
}))
const options = computed<ChartOptions<'bar'>>(() => ({
  responsive: true, maintainAspectRatio: false, interaction: { mode: 'index', intersect: false },
  plugins: {
    legend: { position: 'bottom', labels: { color: theme.value.text, boxWidth: 10, generateLabels: chart => ChartJS.defaults.plugins.legend.labels.generateLabels(chart).map(item => ({ ...item, text: item.text.length > 24 ? `${item.text.slice(0, 22)}…` : item.text })) } },
    tooltip: { callbacks: { label: context => {
      const series = mapped.value.series[context.datasetIndex]
      const point = series?.points[context.dataIndex]
      return point ? [series.label, `${t('enterprise.requests')}: ${point.requests.toLocaleString()}`, `${t('enterprise.tokens')}: ${point.total_tokens.toLocaleString()}`, `${t('enterprise.cost')}: ${usageMoney(point.actual_cost)}`] : []
    } } }
  },
  scales: {
    x: { stacked: true, ticks: { color: theme.value.text, maxTicksLimit: 8, maxRotation: 0 }, grid: { display: false } },
    y: { stacked: true, beginAtZero: true, ticks: { color: theme.value.text }, grid: { color: theme.value.grid } }
  }
}))
</script>

<template>
  <Skeleton v-if="loading" class="h-64 w-full" />
  <template v-else-if="items.length">
    <p class="mb-2 text-xs text-muted-foreground">{{ t('enterprise.usageAnalysis.trendTop') }}</p>
    <div class="relative h-64 min-w-0"><Bar :data="data" :options="options" :aria-label="title" role="img" /></div>
  </template>
  <p v-else class="flex h-64 items-center justify-center text-sm text-muted-foreground">{{ t('enterprise.empty') }}</p>
</template>
