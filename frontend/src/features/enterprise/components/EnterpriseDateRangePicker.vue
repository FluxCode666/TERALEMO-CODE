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
import { usageDateRange, type UsagePeriod } from '../useEnterpriseUsage'

const props = defineProps<{ startDate: string; endDate: string }>()
const emit = defineEmits<{ 'update:startDate': [value: string]; 'update:endDate': [value: string] }>()
const { t, locale } = useI18n()
const open = ref(false)
const wide = useMediaQuery('(min-width: 768px)')
const range = shallowRef<DateRange>({ start: undefined, end: undefined })
const periods: UsagePeriod[] = ['today', 'last7', 'last30', 'month', 'lastMonth']
const calendarLocale = computed(() => locale.value === 'zh' ? 'zh-CN' : locale.value)
const label = computed(() => props.startDate || props.endDate ? `${props.startDate || '…'} – ${props.endDate || '…'}` : t('enterprise.usageAnalysis.allTime'))
watch([() => props.startDate, () => props.endDate, open], () => {
  range.value = { start: props.startDate ? parseDate(props.startDate) : undefined, end: props.endDate ? parseDate(props.endDate) : undefined }
}, { immediate: true })
function update(value: DateRange | undefined) {
  range.value = value ?? { start: undefined, end: undefined }
  if (value?.start && value.end) commit(value.start.toString(), value.end.toString())
}
function commit(start: string, end: string) {
  emit('update:startDate', start)
  emit('update:endDate', end)
  open.value = false
}
function preset(period: UsagePeriod) {
  const value = usageDateRange(period)
  commit(value.start_date, value.end_date)
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <Button type="button" variant="outline" class="w-full min-w-0 justify-start gap-2 text-left font-normal" data-test="usage-date-range" :aria-label="`${t('enterprise.usageAnalysis.dateRange')}: ${label}`">
        <CalendarDays class="size-4 shrink-0 text-muted-foreground" />
        <span class="min-w-0 flex-1 truncate">{{ label }}</span>
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
