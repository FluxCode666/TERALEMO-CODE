<template>
  <!-- Own member quota: daily / weekly / monthly / total limits with usage bars. -->
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
    <div v-for="row in rows" :key="row.key" :data-testid="`member-quota-${row.key}`" class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-cyan-100 p-2 dark:bg-cyan-900/30">
          <Icon name="chart" size="md" class="text-cyan-600 dark:text-cyan-400" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(`enterprise.quotaUsage.${row.key}`) }}</p>
          <p class="break-words text-xl font-bold text-gray-900 dark:text-white">
            ${{ formatUsd(row.usage) }}
            <span v-if="row.limit != null" class="text-sm font-normal text-gray-400 dark:text-gray-500">/ ${{ formatUsd(row.limit) }}</span>
            <span v-else class="text-sm font-normal text-gray-400 dark:text-gray-500">/ {{ t('enterprise.unlimited') }}</span>
          </p>
        </div>
      </div>
      <p v-if="row.limit === 0" class="mt-1.5 text-xs text-red-600 dark:text-red-400">{{ t('enterprise.quotaDisabled') }}</p>
      <div class="mt-3 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
        <div
          v-if="row.limit != null && row.limit > 0"
          class="h-full rounded-full transition-all"
          :class="barClass(percent(row.usage, row.limit))"
          :style="{ width: percent(row.usage, row.limit) + '%' }"
        />
        <div v-else-if="row.limit != null" class="h-full w-full rounded-full bg-red-500" />
        <div v-else class="h-full w-full rounded-full bg-green-500/30" />
      </div>
      <p v-if="row.resetsAt" class="mt-1.5 text-xs text-gray-400 dark:text-gray-500">
        {{ t('enterprise.quotaResetsAt', { time: formatResetTime(row.resetsAt) }) }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { EnterpriseQuota } from '@/types/enterprise'

// A missing quota row means unlimited with zero usage; expired daily/weekly/
// monthly windows already report zero usage from the backend.
const props = defineProps<{ quota: EnterpriseQuota | null | undefined }>()
const { t } = useI18n()

const rows = computed(() => {
  const q = props.quota
  return [
    { key: 'daily', usage: Number(q?.daily_usage_usd ?? 0), limit: q?.daily_limit_usd ?? null, resetsAt: q?.daily_resets_at ?? null },
    { key: 'weekly', usage: Number(q?.weekly_usage_usd ?? 0), limit: q?.weekly_limit_usd ?? null, resetsAt: q?.weekly_resets_at ?? null },
    { key: 'monthly', usage: Number(q?.monthly_usage_usd ?? 0), limit: q?.monthly_limit_usd ?? null, resetsAt: q?.monthly_resets_at ?? null },
    { key: 'total', usage: Number(q?.total_usage_usd ?? 0), limit: q?.total_limit_usd ?? null, resetsAt: null },
  ] as const
})

// Same thresholds as the platform quota bars in UserDashboardStats.
function percent(usage: number, limit: number): number {
  if (!limit || limit <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((usage / limit) * 100)))
}
function barClass(p: number): string {
  if (p >= 95) return 'bg-red-500'
  if (p >= 75) return 'bg-amber-500'
  return 'bg-green-500'
}

const usdFormatter = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
function formatUsd(n: number): string {
  if (!Number.isFinite(n)) return '0.00'
  return usdFormatter.format(n)
}
function formatResetTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })
}
</script>
