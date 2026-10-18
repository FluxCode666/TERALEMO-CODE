<template>
  <article class="card min-w-0 p-4 sm:p-5">
    <div class="flex flex-wrap items-center justify-between gap-3"><h3 class="break-words font-medium text-gray-900 dark:text-white">{{ title }}</h3><div v-if="$slots.actions" class="flex flex-wrap gap-3 text-sm"><slot name="actions" /></div></div>
    <dl class="mt-4 grid gap-4" :class="compact ? 'grid-cols-2' : 'grid-cols-2 xl:grid-cols-4'">
      <div v-for="window in windows" :key="window" class="min-w-0">
        <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t(`enterprise.${window}`) }}</dt>
        <dd class="mt-1 break-words text-sm tabular-nums text-gray-900 dark:text-gray-100">{{ usageAmount(window) }} / {{ limitAmount(window) }}</dd>
        <div v-if="usageVisible(window) && quota[`${window}_limit_usd`] !== null" role="progressbar" :aria-label="`${title} · ${t(`enterprise.${window}`)}`" :aria-valuenow="percentage(window)" :aria-valuemin="0" :aria-valuemax="100" class="mt-2 h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"><div class="h-full rounded-full transition-all" :class="percentage(window) >= 100 ? 'bg-red-500' : percentage(window) >= 80 ? 'bg-amber-500' : 'bg-primary-500'" :style="{ width: `${percentage(window)}%` }" /></div>
      </div>
    </dl>
  </article>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { EnterpriseQuota, QuotaWindow } from '@/types/enterprise'
const props = defineProps<{ quota: EnterpriseQuota; title: string; compact?: boolean }>()
const { t } = useI18n()
const windows: QuotaWindow[] = ['daily', 'weekly', 'monthly', 'total']
function usageVisible(window: QuotaWindow) { return props.quota.usage_visible !== false && props.quota[`${window}_usage_usd`] != null }
function usageAmount(window: QuotaWindow) { return usageVisible(window) ? `$${props.quota[`${window}_usage_usd`]!.toFixed(4)}` : t('enterprise.hidden') }
function limitAmount(window: QuotaWindow) { const limit = props.quota[`${window}_limit_usd`]; return limit === null ? t('enterprise.unlimited') : limit === 0 ? t('enterprise.quotaSections.disabled') : `$${limit}` }
function percentage(window: QuotaWindow) {
  const limit = props.quota[`${window}_limit_usd`], usage = props.quota[`${window}_usage_usd`] ?? 0
  return limit === 0 ? 100 : limit ? Math.min(100, Math.max(0, usage / limit * 100)) : 0
}
</script>
