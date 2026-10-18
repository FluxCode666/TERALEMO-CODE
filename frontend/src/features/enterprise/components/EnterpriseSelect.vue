<script setup lang="ts">
import { computed } from 'vue'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
defineOptions({ inheritAttrs: false })
const props = defineProps<{ modelValue?: string | number | null; options: { value: string | number; label: string }[]; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string | number] }>()
// Select reserves the empty string for clearing; opaque indices retain numeric and empty filter values.
const selected = computed(() => { const index = props.options.findIndex(item => item.value === props.modelValue); return index < 0 ? undefined : String(index) })
function update(value: unknown) { const option = props.options[Number(value)]; if (option) emit('update:modelValue', option.value) }
</script>
<template>
  <div class="shadcn-scope min-w-0" :class="$attrs.class" :data-test="$attrs['data-test']">
    <Select :model-value="selected" :disabled="disabled" @update:model-value="update">
      <SelectTrigger v-bind="Object.fromEntries(Object.entries($attrs).filter(([key]) => key !== 'class' && key !== 'data-test'))" class="w-full"><SelectValue>{{ options.find(option => option.value === modelValue)?.label }}</SelectValue></SelectTrigger>
      <SelectContent><SelectItem v-for="(option, index) in options" :key="index" :value="String(index)">{{ option.label }}</SelectItem></SelectContent>
    </Select>
  </div>
</template>
