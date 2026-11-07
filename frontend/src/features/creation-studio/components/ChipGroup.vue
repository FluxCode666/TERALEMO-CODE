<template>
  <div
    class="cs-chip-group"
    :class="{ 'cs-chip-group--grid': grid }"
    role="radiogroup"
    :aria-labelledby="labelledby"
  >
    <label v-for="option in options" :key="option.value">
      <input
        type="radio"
        class="cs-chip-input"
        :name="name"
        :value="option.value"
        :checked="modelValue === option.value"
        :disabled="disabled || option.disabled"
        @change="emit('update:modelValue', option.value)"
      />
      <span class="cs-chip" :class="{ 'cs-chip-stack': Boolean(option.sub) }">
        <span>{{ option.label }}</span>
        <span v-if="option.sub" class="cs-chip-sub">{{ option.sub }}</span>
      </span>
    </label>
  </div>
</template>

<script setup lang="ts">
interface ChipOption {
  value: string
  label: string
  sub?: string
  disabled?: boolean
}

withDefaults(
  defineProps<{
    name: string
    modelValue: string
    options: ChipOption[]
    labelledby?: string
    disabled?: boolean
    grid?: boolean
  }>(),
  { labelledby: undefined, disabled: false, grid: false },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()
</script>
