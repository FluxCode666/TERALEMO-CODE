<template>
  <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <label :for="id" class="flex cursor-pointer items-center justify-between gap-4">
      <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('videoAccounts.hideUpstreamURL') }}</span>
      <input :id="id" type="checkbox" role="switch" class="checkbox" :checked="modelValue" @change="emit('update:modelValue', ($event.target as HTMLInputElement).checked)" />
    </label>
    <p class="input-hint mt-2">{{ t('videoAccounts.hideUpstreamURLHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ modelValue: boolean; id: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const { t } = useI18n()
</script>
