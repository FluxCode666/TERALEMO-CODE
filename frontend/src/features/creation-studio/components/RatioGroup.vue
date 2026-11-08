<template>
  <div class="cs-chip-group cs-chip-group--grid" role="radiogroup" :aria-labelledby="labelledby">
    <label v-for="option in options" :key="option.value">
      <input
        type="radio"
        class="cs-chip-input"
        :name="name"
        :value="option.value"
        :checked="modelValue === option.value"
        :disabled="disabled"
        @change="emit('update:modelValue', option.value)"
      />
      <span class="cs-chip">
        <span class="cs-ratio-glyph" :style="glyphStyle(option)" aria-hidden="true" />
        <span>{{ option.value }}</span>
      </span>
    </label>
  </div>
</template>

<script setup lang="ts">
import type { CreationParamRatioOption } from '../types'

withDefaults(
  defineProps<{
    name: string
    modelValue: string
    options: CreationParamRatioOption[]
    labelledby?: string
    disabled?: boolean
  }>(),
  { labelledby: undefined, disabled: false },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

function glyphStyle(option: CreationParamRatioOption): Record<string, string> {
  return { '--cs-ratio-w': String(option.w), '--cs-ratio-h': String(option.h) }
}
</script>
