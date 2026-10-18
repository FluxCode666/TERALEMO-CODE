<template>
  <div class="card min-w-0 p-4 sm:p-5" :aria-busy="loading">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <h3 class="font-medium text-gray-900 dark:text-white">{{ t('enterprise.modelDistribution') }}</h3>
      <Select v-model="metric" class="w-40" :options="metricOptions" :aria-label="t('enterprise.usageAnalysis.distributionMetric')" data-test="usage-distribution-metric" />
    </div>
    <div v-if="loading" class="h-64 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
    <div v-else-if="items.length" class="flex flex-col items-center gap-5 sm:flex-row">
      <div class="relative h-56 w-56 shrink-0"><Doughnut :data="chartData" :options="options" :aria-label="t('enterprise.modelDistribution')" role="img" /></div>
      <ul class="max-h-64 w-full min-w-0 space-y-3 overflow-y-auto text-sm" :aria-label="t('enterprise.usageAnalysis.legend')">
        <li v-for="(item, index) in sorted" :key="item.model" class="flex items-center gap-2"><span class="h-2.5 w-2.5 shrink-0 rounded-full" :style="{ backgroundColor: usageColors[index % usageColors.length] }" /><span class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-200" :title="item.model">{{ item.model }}</span><span class="shrink-0 tabular-nums text-gray-500 dark:text-gray-400">{{ percentage(item[metric], totals[metric]) }}%</span></li>
      </ul>
    </div>
    <p v-else class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.empty') }}</p>
    <details v-if="items.length && !loading" class="mt-4 text-sm"><summary class="cursor-pointer text-gray-600 dark:text-gray-300">{{ t('enterprise.usageAnalysis.viewTable') }}</summary><DataTable :columns="columns" :data="sorted"><template #cell-actual_cost="{ value }">{{ usageMoney(value) }}</template></DataTable></details>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, ArcElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import type { EnterpriseUsageModel } from '@/types/enterprise'
import { usageColors, usageMoney, usageNumber } from '../useEnterpriseUsage'
ChartJS.register(ArcElement, Tooltip, Legend)
const props = defineProps<{ items: EnterpriseUsageModel[]; loading: boolean }>()
const { t } = useI18n()
const metric = ref<'actual_cost' | 'total_tokens' | 'requests'>('actual_cost')
const metricOptions = computed(() => [{ value: 'actual_cost', label: t('enterprise.cost') }, { value: 'total_tokens', label: t('enterprise.tokens') }, { value: 'requests', label: t('enterprise.requests') }])
const sorted = computed(() => [...props.items].sort((a, b) => usageNumber(b[metric.value]) - usageNumber(a[metric.value])))
const totals = computed(() => props.items.reduce((sum, item) => ({ requests: sum.requests + usageNumber(item.requests), total_tokens: sum.total_tokens + usageNumber(item.total_tokens), actual_cost: sum.actual_cost + usageNumber(item.actual_cost) }), { requests: 0, total_tokens: 0, actual_cost: 0 }))
const percentage = (value: number, total: number) => total > 0 ? (usageNumber(value) / total * 100).toFixed(1) : '0.0'
// Follow ModelDistributionChart's doughnut + HTML legend pattern, with three-metric tooltips.
const chartData = computed(() => ({ labels: sorted.value.map(item => item.model), datasets: [{ data: sorted.value.map(item => usageNumber(item[metric.value])), backgroundColor: sorted.value.map((_, index) => usageColors[index % usageColors.length]), borderWidth: 0 }] }))
const options = computed<ChartOptions<'doughnut'>>(() => ({
  responsive: true, maintainAspectRatio: false, cutout: '68%',
  plugins: { legend: { display: false }, tooltip: { callbacks: { label: context => {
    const item = sorted.value[context.dataIndex]
    if (!item) return []
    return [`${t('enterprise.requests')}: ${item.requests.toLocaleString()} (${percentage(item.requests, totals.value.requests)}%)`, `${t('enterprise.tokens')}: ${item.total_tokens.toLocaleString()} (${percentage(item.total_tokens, totals.value.total_tokens)}%)`, `${t('enterprise.cost')}: ${usageMoney(item.actual_cost)} (${percentage(item.actual_cost, totals.value.actual_cost)}%)`]
  } } } }
}))
const columns = computed(() => [{ key: 'model', label: t('enterprise.model') }, { key: 'requests', label: t('enterprise.requests') }, { key: 'total_tokens', label: t('enterprise.tokens') }, { key: 'actual_cost', label: t('enterprise.cost') }])
</script>
