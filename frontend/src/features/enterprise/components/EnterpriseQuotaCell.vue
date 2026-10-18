<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseQuota, QuotaWindow } from '@/types/enterprise'
import { cn } from '@/lib/utils'
import { quotaCell, quotaMoney, quotaPercentLabel } from '../quotaPresentation'
const props = defineProps<{ quota: EnterpriseQuota; window: QuotaWindow }>()
const { t } = useI18n()
const cell = computed(() => quotaCell(props.quota, props.window))
</script>
<template>
  <div class="min-w-36 space-y-2 text-xs tabular-nums" :data-test="`quota-cell-${window}`">
    <span v-if="cell.hidden" class="text-muted-foreground">{{ t('enterprise.quotaTable.hidden') }}</span>
    <span v-else-if="cell.limit === 0" class="text-muted-foreground">{{ t('enterprise.quotaSections.disabled') }}</span>
    <template v-else>
      <div v-if="cell.percent !== null" role="progressbar" :aria-label="t(`enterprise.${window}`)" :aria-valuenow="Math.min(100, cell.percent)" :aria-valuetext="quotaPercentLabel(cell.percent)" :aria-valuemin="0" :aria-valuemax="100" class="h-1 overflow-hidden rounded-full bg-muted"><div :class="cn('h-full rounded-full bg-primary', cell.tone === 'warning' && 'bg-amber-500', cell.tone === 'exceeded' && 'bg-destructive')" :style="{ width: `${Math.min(100, cell.percent)}%` }" /></div>
      <p class="whitespace-nowrap"><span class="font-medium">{{ quotaMoney(cell.usage!) }}</span><span class="text-muted-foreground"> / {{ cell.limit === null ? t('enterprise.unlimited') : quotaMoney(cell.limit) }}<template v-if="cell.percent !== null"> ({{ quotaPercentLabel(cell.percent) }})</template></span></p>
    </template>
  </div>
</template>
