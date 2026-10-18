<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import type { EnterpriseUsageTrend } from '@/types/enterprise'
import { usageMoney, normalizeEnterpriseUsageTrend, useEnterpriseUsageTheme } from '../useEnterpriseUsage'
const props = defineProps<{ items: EnterpriseUsageTrend[]; loading: boolean; granularity?: 'day' | 'hour' }>()
const emit = defineEmits<{ 'update:granularity': [value: 'day' | 'hour'] }>()
const { t } = useI18n()
const theme = useEnterpriseUsageTheme()
const data = computed(() => props.items.map(normalizeEnterpriseUsageTrend))
</script>

<template>
  <Card class="usage-chart-card min-w-0" :aria-busy="loading">
    <CardHeader class="flex-row flex-wrap items-center justify-between gap-3 space-y-0 pb-3">
      <CardTitle class="text-base">{{ t('admin.dashboard.tokenUsageTrend') }}</CardTitle>
      <Select :model-value="granularity ?? 'day'" @update:model-value="value => emit('update:granularity', value as 'day' | 'hour')">
        <SelectTrigger class="h-8 w-28" data-test="usage-granularity" :aria-label="t('admin.dashboard.granularity')"><SelectValue /></SelectTrigger>
        <SelectContent><SelectItem value="day">{{ t('admin.dashboard.day') }}</SelectItem><SelectItem value="hour">{{ t('admin.dashboard.hour') }}</SelectItem></SelectContent>
      </Select>
    </CardHeader>
    <CardContent class="min-w-0 pt-0">
      <Skeleton v-if="loading" class="h-64 w-full" />
      <TokenUsageTrend v-else :key="theme.text" class="usage-shared-chart" :trend-data="data" :loading="false" />
      <Collapsible v-if="items.length && !loading" class="mt-3">
        <CollapsibleTrigger as-child><Button type="button" size="sm" variant="ghost">{{ t('enterprise.usageAnalysis.viewTable') }}</Button></CollapsibleTrigger>
        <CollapsibleContent>
          <Table><TableHeader><TableRow><TableHead>{{ t('usage.time') }}</TableHead><TableHead>{{ t('enterprise.requests') }}</TableHead><TableHead>{{ t('enterprise.tokens') }}</TableHead><TableHead>{{ t('enterprise.cost') }}</TableHead></TableRow></TableHeader><TableBody><TableRow v-for="item in items" :key="item.date"><TableCell>{{ item.date }}</TableCell><TableCell>{{ item.requests.toLocaleString() }}</TableCell><TableCell>{{ item.total_tokens.toLocaleString() }}</TableCell><TableCell>{{ usageMoney(item.actual_cost) }}</TableCell></TableRow></TableBody></Table>
        </CollapsibleContent>
      </Collapsible>
    </CardContent>
  </Card>
</template>

<style scoped>
.usage-shared-chart { border: 0; background: transparent; box-shadow: none; padding: 0; }
.usage-shared-chart :deep(h3) { display: none; }
.usage-shared-chart :deep(.h-48) { height: 16rem; }
</style>
