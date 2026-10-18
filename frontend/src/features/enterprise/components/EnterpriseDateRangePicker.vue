<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMediaQuery } from '@vueuse/core'
import { parseDate } from '@internationalized/date'
import type { DateRange } from 'reka-ui'
import { CalendarDays, ChevronDown } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { RangeCalendar } from '@/components/ui/range-calendar'
import { usageDateRange, usageTimeRange, type UsagePeriod } from '../useEnterpriseUsage'

const props = defineProps<{ startDate: string; endDate: string; startTime?: string; endTime?: string }>()
const emit = defineEmits<{ 'update:startDate': [value: string]; 'update:endDate': [value: string] }>()
const selectedPeriod = defineModel<UsagePeriod | null>('period', { default: null })
const { t, locale } = useI18n()
const open = ref(false)
const wide = useMediaQuery('(min-width: 768px)')
const range = shallowRef<DateRange>({ start: undefined, end: undefined })
const periods: UsagePeriod[] = ['last24', 'today', 'last7', 'last30', 'month', 'lastMonth']
const previewTimeRange = ref(usageTimeRange())
const calendarLocale = computed(() => locale.value === 'zh' ? 'zh-CN' : locale.value)
const fullLabel = computed(() => selectedPeriod.value === 'last24'
  ? `${props.startTime || previewTimeRange.value.start_time} – ${props.endTime || previewTimeRange.value.end_time}`
  : props.startDate || props.endDate ? `${props.startDate || '…'} – ${props.endDate || '…'}` : t('enterprise.usageAnalysis.allTime'))
const label = computed(() => {
  if (selectedPeriod.value) return t(`enterprise.usageAnalysis.${selectedPeriod.value}`)
  if (props.startDate && props.endDate && props.startDate.slice(0, 4) === props.endDate.slice(0, 4)) {
    return `${props.startDate.slice(5)} – ${props.endDate.slice(5)}`
  }
  return fullLabel.value
})
watch([() => props.startDate, () => props.endDate], () => {
  if (!selectedPeriod.value) return
  if (selectedPeriod.value === 'last24') {
    if (props.startDate || props.endDate) selectedPeriod.value = null
    return
  }
  const value = usageDateRange(selectedPeriod.value)
  if (props.startDate !== value.start_date || props.endDate !== value.end_date) selectedPeriod.value = null
})
watch([() => props.startDate, () => props.endDate, open], () => {
  range.value = { start: props.startDate ? parseDate(props.startDate) : undefined, end: props.endDate ? parseDate(props.endDate) : undefined }
}, { immediate: true })
function update(value: DateRange | undefined) {
  range.value = value ?? { start: undefined, end: undefined }
  if (value?.start && value.end) commit(value.start.toString(), value.end.toString())
}
function commit(start: string, end: string, period: UsagePeriod | null = null) {
  selectedPeriod.value = period
  emit('update:startDate', start)
  emit('update:endDate', end)
  open.value = false
}
function preset(period: UsagePeriod) {
  if (period === 'last24') {
    previewTimeRange.value = usageTimeRange()
    commit('', '', period)
    return
  }
  const value = usageDateRange(period)
  commit(value.start_date, value.end_date, period)
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <Button type="button" variant="outline" class="w-auto min-w-[10rem] max-w-[min(100%,20rem)] justify-start gap-1.5 px-2 text-left font-normal sm:gap-2 sm:px-3" data-test="usage-date-range" :title="fullLabel" :aria-label="`${t('enterprise.usageAnalysis.dateRange')}: ${fullLabel}`">
        <CalendarDays class="size-4 shrink-0 text-muted-foreground" />
        <span class="flex-1 whitespace-nowrap">{{ label }}</span>
        <ChevronDown class="size-4 shrink-0 text-muted-foreground" />
      </Button>
    </PopoverTrigger>
    <PopoverContent align="start" class="w-auto max-w-[calc(100vw-2rem)] p-0" data-test="usage-date-popover">
      <div class="flex flex-col md:flex-row">
        <div class="flex flex-wrap gap-1 border-b p-2 md:w-32 md:shrink-0 md:flex-col md:border-b-0 md:border-r">
          <Button v-for="period in periods" :key="period" type="button" variant="ghost" size="sm" class="justify-start" :data-test="`usage-period-${period}`" @click="preset(period)">{{ t(`enterprise.usageAnalysis.${period}`) }}</Button>
          <Button type="button" variant="ghost" size="sm" class="justify-start text-muted-foreground" data-test="usage-date-clear" @click="commit('', '')">{{ t('enterprise.usageAnalysis.clear') }}</Button>
        </div>
        <div class="min-w-0">
          <RangeCalendar :model-value="range" :locale="calendarLocale" :number-of-months="wide ? 2 : 1" @update:model-value="update" />
        </div>
      </div>
    </PopoverContent>
  </Popover>
</template>
