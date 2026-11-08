<template>
  <div class="cs-stepper" :data-invalid="invalid" :aria-disabled="disabled">
    <button
      type="button"
      class="cs-stepper-btn"
      :disabled="disabled || atMin"
      :aria-label="t('creationStudio.param.decrease')"
      @click="nudge(-1)"
    >
      <Minus aria-hidden="true" />
    </button>
    <input
      :id="inputId"
      class="cs-stepper-input"
      type="number"
      inputmode="numeric"
      :value="modelValue"
      :min="min"
      :max="max"
      :step="stepValue"
      :disabled="disabled"
      :aria-label="label"
      :aria-invalid="invalid"
      @change="onInput"
    />
    <button
      type="button"
      class="cs-stepper-btn"
      :disabled="disabled || atMax"
      :aria-label="t('creationStudio.param.increase')"
      @click="nudge(1)"
    >
      <Plus aria-hidden="true" />
    </button>
    <span v-if="unit" class="cs-stepper-unit">{{ unit }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Minus, Plus } from '@lucide/vue'

const props = withDefaults(
  defineProps<{
    modelValue: number
    min: number
    max: number
    step?: number
    unit?: string
    disabled?: boolean
    invalid?: boolean
    inputId?: string
    label?: string
  }>(),
  { step: 1, unit: '', disabled: false, invalid: false, inputId: undefined, label: undefined },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: number): void }>()

const { t } = useI18n()

const stepValue = computed(() => (props.step > 0 ? props.step : 1))
const atMin = computed(() => props.modelValue <= props.min)
const atMax = computed(() => props.modelValue >= props.max)

function clamp(value: number): number {
  const snapped = props.min + Math.round((value - props.min) / stepValue.value) * stepValue.value
  return Math.min(props.max, Math.max(props.min, snapped))
}

function nudge(direction: 1 | -1) {
  emit('update:modelValue', clamp(props.modelValue + direction * stepValue.value))
}

function onInput(event: Event) {
  const input = event.target as HTMLInputElement
  const parsed = Number.parseInt(input.value, 10)
  if (!Number.isFinite(parsed)) {
    input.value = String(props.modelValue)
    return
  }
  const next = clamp(parsed)
  input.value = String(next)
  emit('update:modelValue', next)
}
</script>
