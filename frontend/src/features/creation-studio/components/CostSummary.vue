<template>
  <div class="space-y-2">
    <div class="cs-cost" :data-state="costState">
      <span class="cs-cost-label">{{ t('creationStudio.cost.estimate') }}</span>
      <div class="text-right">
        <span v-if="loading" class="skeleton inline-block h-5 w-20 align-middle" aria-hidden="true" />
        <span v-else class="cs-cost-value">{{ displayValue }}</span>
        <div class="cs-cost-detail">{{ detailText }}</div>
      </div>
    </div>
    <div v-if="showBalance" class="cs-balance">
      <span>{{ t('creationStudio.cost.balance') }}</span>
      <span>{{ formatMoney(estimate?.available_balance) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CreationEstimateResponse } from '../types'

const props = withDefaults(
  defineProps<{
    estimate: CreationEstimateResponse | null
    loading?: boolean
    balanceHidden?: boolean
    /** 提交返回余额类错误时由调用方强制切到 insufficient（规范 §7.2、R1-06）。 */
    stateOverride?: 'insufficient' | 'unavailable' | null
  }>(),
  { loading: false, balanceHidden: false, stateOverride: null },
)

const { t } = useI18n()

const costState = computed<'insufficient' | 'unavailable' | undefined>(() => {
  if (props.stateOverride) return props.stateOverride
  if (props.loading) return undefined
  if (!props.estimate || !props.estimate.priced) return 'unavailable'
  if (props.estimate.sufficient === false) return 'insufficient'
  return undefined
})

const displayValue = computed(() => {
  if (!props.estimate || !props.estimate.priced) return '—'
  return formatMoney(props.estimate.estimated_cost)
})

const detailText = computed(() => {
  const estimate = props.estimate
  if (!estimate || !estimate.priced) return t('creationStudio.cost.unavailable')
  if (estimate.sufficient === false && estimate.available_balance !== null) {
    const shortfall = Math.max(0, estimate.estimated_cost - estimate.available_balance)
    return t('creationStudio.cost.insufficient', { amount: formatMoney(shortfall) })
  }
  if (!estimate.detail.length) return ''
  return estimate.detail
    .map(item => `${t(`creationStudio.cost.detail.${item.label}`)} ${item.value}`)
    .join(' · ')
})

const showBalance = computed(() => {
  if (props.balanceHidden || props.loading) return false
  const estimate = props.estimate
  return Boolean(estimate && estimate.balance_scope === 'personal' && estimate.available_balance !== null)
})

function formatMoney(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  const absolute = Math.abs(value)
  const digits = absolute > 0 && absolute < 0.01 ? 4 : 2
  return `$${value.toFixed(digits)}`
}
</script>
