<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { UsageMetric, UsageView } from '../EnterpriseUsageSeries'
defineProps<{ metric: UsageMetric; view: UsageView; title: string }>()
const emit = defineEmits<{ 'update:metric': [value: UsageMetric]; 'update:view': [value: UsageView] }>()
const { t } = useI18n()
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <ToggleGroup type="single" :model-value="view" size="sm" class="gap-0 rounded-md border p-0.5" :aria-label="`${title} ${t('enterprise.usageAnalysis.chartView')}`" @update:model-value="value => { if (value === 'share' || value === 'trend') emit('update:view', value) }">
      <ToggleGroupItem value="share" data-test="usage-view-share" class="h-7 px-2">{{ t('enterprise.usageAnalysis.share') }}</ToggleGroupItem>
      <ToggleGroupItem value="trend" data-test="usage-view-trend" class="h-7 px-2">{{ t('enterprise.usageAnalysis.trend') }}</ToggleGroupItem>
    </ToggleGroup>
    <Select :model-value="metric" @update:model-value="value => emit('update:metric', value as UsageMetric)">
      <SelectTrigger class="h-8 w-28" data-test="usage-chart-metric" :aria-label="`${title} ${t('enterprise.usageAnalysis.metric')}`"><SelectValue /></SelectTrigger>
      <SelectContent><SelectItem value="actual_cost">{{ t('enterprise.cost') }}</SelectItem><SelectItem value="tokens">{{ t('enterprise.tokens') }}</SelectItem><SelectItem value="requests">{{ t('enterprise.requests') }}</SelectItem></SelectContent>
    </Select>
  </div>
</template>
