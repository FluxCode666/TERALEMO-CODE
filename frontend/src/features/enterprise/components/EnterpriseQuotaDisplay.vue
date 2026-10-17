<template>
  <div class="card p-4 sm:p-5">
    <h3 class="font-medium text-gray-900 dark:text-white">{{ title }}</h3>
    <dl class="mt-3 grid grid-cols-2 gap-4 sm:grid-cols-4">
      <div v-for="window in windows" :key="window"><dt class="text-xs text-gray-500 dark:text-gray-400">{{ t(`enterprise.${window}`) }}</dt><dd class="mt-1 text-sm tabular-nums text-gray-900 dark:text-gray-100">{{ usageAmount(window) }} / {{ quota[`${window}_limit_usd`] === null ? t('enterprise.unlimited') : `$${quota[`${window}_limit_usd`]}` }}</dd></div>
    </dl>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { EnterpriseQuota, QuotaWindow } from '@/types/enterprise'
const props = defineProps<{ quota: EnterpriseQuota; title: string }>()
const { t } = useI18n()
const windows: QuotaWindow[] = ['daily', 'weekly', 'monthly', 'total']
function usageAmount(window: QuotaWindow) {
  const usage = props.quota[`${window}_usage_usd`]
  return props.quota.usage_visible === false || usage === null ? t('enterprise.hidden') : `$${usage.toFixed(4)}`
}
</script>
