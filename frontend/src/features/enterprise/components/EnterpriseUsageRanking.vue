<template>
  <Card class="min-w-0 p-4 sm:p-5" :aria-busy="loading">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 class="font-medium text-gray-900 dark:text-white">{{ title }}</h3><Button v-if="items.length" type="button" variant="outline" size="sm" :aria-pressed="table" @click="table = !table">{{ t(table ? 'enterprise.usageAnalysis.viewChart' : 'enterprise.usageAnalysis.viewTable') }}</Button></div>
    <Skeleton v-if="loading" class="h-64 w-full" />
    <template v-else-if="items.length">
      <Table v-if="table" data-test="usage-ranking-table"><TableHeader><TableRow><TableHead>{{ title }}</TableHead><TableHead>{{ t('enterprise.requests') }}</TableHead><TableHead>{{ t('enterprise.tokens') }}</TableHead><TableHead>{{ t('enterprise.cost') }}</TableHead></TableRow></TableHeader><TableBody><TableRow v-for="item in sorted" :key="item.id"><TableCell>{{ item.label }}</TableCell><TableCell>{{ item.requests.toLocaleString() }}</TableCell><TableCell>{{ item.total_tokens.toLocaleString() }}</TableCell><TableCell>{{ usageMoney(item.actual_cost) }}</TableCell></TableRow></TableBody></Table>
      <template v-else><p class="mb-3 text-xs text-gray-500 dark:text-gray-400">{{ t('enterprise.usageAnalysis.topRanking', { count: Math.min(limit, items.length), total: items.length }) }}</p><div class="relative w-full" :style="{ height: `${Math.max(180, displayed.length * 36)}px` }"><Bar :data="chartData" :options="options" :aria-label="title" role="img" /></div></template>
    </template>
    <p v-else class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.empty') }}</p>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, BarElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Bar } from 'vue-chartjs'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { usageMoney, usageNumber, useEnterpriseUsageTheme } from '../useEnterpriseUsage'
ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend)
export interface UsageRankingItem { id: string | number; label: string; requests: number; total_tokens: number; actual_cost: number }
const props = withDefaults(defineProps<{ title: string; items: UsageRankingItem[]; loading: boolean; limit?: number }>(), { limit: 10 })
const { t } = useI18n()
const theme = useEnterpriseUsageTheme()
const table = ref(false)
const sorted = computed(() => [...props.items].sort((a, b) => usageNumber(b.actual_cost) - usageNumber(a.actual_cost)))
const displayed = computed(() => sorted.value.slice(0, props.limit))
const chartData = computed(() => ({ labels: displayed.value.map(item => item.label), datasets: [{ label: t('enterprise.cost'), data: displayed.value.map(item => usageNumber(item.actual_cost)), backgroundColor: '#14b8a6', borderRadius: 4, maxBarThickness: 22 }] }))
const options = computed<ChartOptions<'bar'>>(() => ({
  indexAxis: 'y', responsive: true, maintainAspectRatio: false,
  plugins: { legend: { display: false }, tooltip: { callbacks: { label: context => {
    const item = displayed.value[context.dataIndex]
    return item ? [`${t('enterprise.requests')}: ${item.requests.toLocaleString()}`, `${t('enterprise.tokens')}: ${item.total_tokens.toLocaleString()}`, `${t('enterprise.cost')}: ${usageMoney(item.actual_cost)}`] : []
  } } } },
  scales: { x: { beginAtZero: true, ticks: { color: theme.value.text }, grid: { color: theme.value.grid } }, y: { ticks: { color: theme.value.text, callback: function(value) { const label = this.getLabelForValue(Number(value)); return label.length > 24 ? `${label.slice(0, 22)}…` : label } }, grid: { display: false } } }
}))
</script>
