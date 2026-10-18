<template>
  <article class="card min-w-0 p-4 sm:p-5">
    <div class="flex flex-wrap items-center justify-between gap-3"><h3 class="break-words font-medium text-gray-900 dark:text-white">{{ title }}</h3><div v-if="$slots.actions" class="flex flex-wrap gap-3 text-sm"><slot name="actions" /></div></div>
    <dl class="mt-4 grid gap-4" :class="compact ? 'grid-cols-2' : 'grid-cols-2 xl:grid-cols-4'">
      <div v-for="window in windows" :key="window" class="min-w-0">
        <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t(`enterprise.${window}`) }}</dt>
        <dd class="mt-1 break-words text-sm tabular-nums text-gray-900 dark:text-gray-100">{{ limitAmount(window) }}</dd>
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
function limitAmount(window: QuotaWindow) { const limit = props.quota[`${window}_limit_usd`]; return limit === null ? t('enterprise.unlimited') : limit === 0 ? t('enterprise.quotaSections.disabled') : `$${limit}` }
</script>
