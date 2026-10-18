<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import type { EnterpriseUsageModel } from '@/types/enterprise'
import { normalizeEnterpriseUsageModel, useEnterpriseUsageTheme } from '../useEnterpriseUsage'
const props = defineProps<{ items: EnterpriseUsageModel[]; loading: boolean }>()
const { t } = useI18n()
const metric = ref<'tokens' | 'actual_cost'>('tokens')
const theme = useEnterpriseUsageTheme()
const data = computed(() => props.items.map(normalizeEnterpriseUsageModel))
</script>

<template>
  <Card class="usage-chart-card min-w-0" :aria-busy="loading">
    <CardHeader class="flex-row flex-wrap items-center justify-between gap-3 space-y-0 pb-3">
      <CardTitle class="text-base">{{ t('enterprise.modelDistribution') }}</CardTitle>
      <Select v-model="metric"><SelectTrigger class="h-8 w-28" data-test="usage-distribution-metric" :aria-label="t('enterprise.usageAnalysis.distributionMetric')"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="tokens">{{ t('enterprise.tokens') }}</SelectItem><SelectItem value="actual_cost">{{ t('enterprise.cost') }}</SelectItem></SelectContent></Select>
    </CardHeader>
    <CardContent class="min-w-0 pt-0">
      <Skeleton v-if="loading" class="h-64 w-full" />
      <ModelDistributionChart v-else :key="theme.text" v-model:metric="metric" class="usage-shared-chart" :model-stats="data" :loading="false" :show-source-toggle="false" :show-metric-toggle="false" :enable-breakdown="false" :show-account-cost="false" />
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
