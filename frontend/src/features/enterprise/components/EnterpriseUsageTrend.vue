<template>
  <div class="card min-w-0 p-4 sm:p-5" :aria-busy="loading">
    <h3 class="mb-4 font-medium text-gray-900 dark:text-white">{{ t('enterprise.trend') }}</h3>
    <div v-if="loading" class="h-64 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
    <div v-else-if="items.length" class="relative h-64 w-full min-w-0"><Line :data="chartData" :options="chartOptions" :height="256" :aria-label="t('enterprise.trend')" role="img" /></div>
    <p v-else class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.empty') }}</p>
    <details v-if="items.length && !loading" class="mt-4 text-sm"><summary class="cursor-pointer text-gray-600 dark:text-gray-300">{{ t('enterprise.usageAnalysis.viewTable') }}</summary><DataTable :columns="columns" :data="items"><template #cell-actual_cost="{ value }">{{ usageMoney(value) }}</template></DataTable></details>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend } from 'chart.js'
import type { ChartData, ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import DataTable from '@/components/common/DataTable.vue'
import type { EnterpriseUsageTrend } from '@/types/enterprise'
import { usageMoney, useEnterpriseUsageTheme } from '../useEnterpriseUsage'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)
const props = defineProps<{ items: EnterpriseUsageTrend[]; loading: boolean }>()
const { t } = useI18n()
const theme = useEnterpriseUsageTheme()
const columns = computed(() => [{ key: 'date', label: t('usage.time') }, { key: 'requests', label: t('enterprise.requests') }, { key: 'total_tokens', label: t('enterprise.tokens') }, { key: 'actual_cost', label: t('enterprise.cost') }])
const chartData = computed<ChartData<'line'>>(() => ({
  labels: props.items.map(item => item.date),
  datasets: [
    { label: t('enterprise.cost'), data: props.items.map(item => item.actual_cost), yAxisID: 'y', borderColor: '#14b8a6', backgroundColor: '#14b8a6', borderWidth: 2, pointRadius: props.items.length === 1 ? 5 : 3, pointHoverRadius: 7, pointHitRadius: 12, tension: 0.25 },
    { label: t('enterprise.requests'), data: props.items.map(item => item.requests), yAxisID: 'requests', borderColor: '#3b82f6', backgroundColor: '#3b82f6', borderDash: [4, 4], borderWidth: 2, pointRadius: props.items.length === 1 ? 5 : 3, tension: 0.25 }
  ]
}))
const chartOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true, maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' },
  plugins: {
    legend: { labels: { color: theme.value.text, usePointStyle: true } },
    tooltip: { callbacks: {
      label: context => `${context.dataset.label}: ${context.dataset.yAxisID === 'requests' ? (context.parsed.y ?? 0).toLocaleString() : usageMoney(context.parsed.y)}`,
      afterBody: items => {
        const point = props.items[items[0]?.dataIndex ?? -1]
        return point ? [`${t('enterprise.requests')}: ${point.requests.toLocaleString()}`, `${t('enterprise.tokens')}: ${point.total_tokens.toLocaleString()}`] : []
      }
    } }
  },
  scales: {
    x: { offset: true, ticks: { color: theme.value.text }, grid: { color: theme.value.grid } },
    y: { beginAtZero: true, grace: '10%', title: { display: true, text: t('enterprise.cost'), color: theme.value.text }, ticks: { color: theme.value.text }, grid: { color: theme.value.grid } },
    requests: { position: 'right', beginAtZero: true, grace: '10%', title: { display: true, text: t('enterprise.requests'), color: theme.value.text }, ticks: { color: theme.value.text, precision: 0 }, grid: { drawOnChartArea: false } }
  }
}))
</script>
