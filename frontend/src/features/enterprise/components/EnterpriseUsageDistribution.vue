<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Doughnut } from 'vue-chartjs'
import type { ChartOptions } from 'chart.js'
import EnterpriseUsageChartControls from './EnterpriseUsageChartControls.vue'
import EnterpriseUsageStackedTrend from './EnterpriseUsageStackedTrend.vue'
import type { UsageMetric, UsageView, UsageSeriesPoint } from '../EnterpriseUsageSeries'
import { Skeleton } from '@/components/ui/skeleton'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import type { EnterpriseUsageModel } from '@/types/enterprise'
import { normalizeEnterpriseUsageModel, useEnterpriseUsageTheme, usageColors, usageNumber } from '../useEnterpriseUsage'
const props = withDefaults(defineProps<{ items: EnterpriseUsageModel[]; loading: boolean; trendItems?: UsageSeriesPoint[]; trendLoading?: boolean }>(), { trendItems: () => [], trendLoading: false })
const view = defineModel<UsageView>('view', { default: 'share' })
const { t } = useI18n()
const metric = ref<UsageMetric>('tokens')
const theme = useEnterpriseUsageTheme()
const data = computed(() => props.items.map(normalizeEnterpriseUsageModel))
const requestItems = computed(() => [...data.value].sort((a, b) => b.requests - a.requests))
const requestData = computed(() => ({ labels: requestItems.value.map(item => item.model), datasets: [{ data: requestItems.value.map(item => item.requests), backgroundColor: usageColors }] }))
const requestOptions = computed<ChartOptions<'doughnut'>>(() => ({ responsive: true, maintainAspectRatio: false, plugins: { legend: { position: 'bottom', labels: { color: theme.value.text } }, tooltip: { callbacks: { label: context => {
  const total = requestItems.value.reduce((sum, item) => sum + item.requests, 0)
  const count = usageNumber(context.raw)
  return `${context.label}: ${count.toLocaleString()} (${total ? (count / total * 100).toFixed(1) : '0.0'}%)`
} } } } }))
</script>

<template>
  <Card class="usage-chart-card min-w-0" :aria-busy="view === 'trend' ? trendLoading : loading">
    <CardHeader class="flex-row flex-wrap items-center justify-between gap-3 space-y-0 pb-3">
      <CardTitle class="text-base">{{ t('enterprise.modelDistribution') }}</CardTitle>
      <EnterpriseUsageChartControls v-model:metric="metric" v-model:view="view" :title="t('enterprise.modelDistribution')" />
    </CardHeader>
    <CardContent class="min-w-0 pt-0">
      <EnterpriseUsageStackedTrend v-if="view === 'trend'" :title="t('enterprise.modelDistribution')" :items="trendItems" :loading="trendLoading" :metric="metric" />
      <Skeleton v-else-if="loading" class="h-64 w-full" />
      <ModelDistributionChart v-else-if="metric !== 'requests'" :key="theme.text" v-model:metric="metric" class="usage-shared-chart" :model-stats="data" :loading="false" :show-source-toggle="false" :show-metric-toggle="false" :enable-breakdown="false" :show-account-cost="false" />
      <div v-else-if="items.length" class="relative h-64"><Doughnut :data="requestData" :options="requestOptions" :aria-label="t('enterprise.modelDistribution')" role="img" /></div>
      <p v-else class="flex h-64 items-center justify-center text-sm text-muted-foreground">{{ t('enterprise.empty') }}</p>
    </CardContent>
  </Card>
</template>

<style scoped>
.usage-shared-chart { border: 0; background: transparent; box-shadow: none; padding: 0; }
.usage-shared-chart :deep(> div:first-child) { display: none; }
.usage-shared-chart :deep(.h-48), .usage-shared-chart :deep(.max-h-48) { height: 16rem; max-height: 16rem; }
.usage-shared-chart :deep(.w-48) { width: 12rem; }
@media (min-width: 1024px) and (max-width: 1279px) {
  .usage-shared-chart :deep(.sm\:flex-row) { flex-direction: column; }
  .usage-shared-chart :deep(.max-h-48) { height: auto; max-height: 10rem; }
}
</style>
