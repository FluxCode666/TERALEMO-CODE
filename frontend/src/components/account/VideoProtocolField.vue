<template>
  <div class="space-y-2">
    <label :for="id" class="input-label">{{ t('videoAccounts.protocol') }}</label>
    <select :id="id" class="input" :value="modelValue" @change="changeProtocol">
      <option value="seedance">Seedance (Ark)</option>
      <option value="gemini">Google Gemini (Veo)</option>
      <option value="minimax">MiniMax (Hailuo / H3)</option>
    </select>
    <p class="input-hint">{{ t(modelValue === 'minimax' ? 'videoAccounts.minimaxHint' : modelValue === 'gemini' ? 'videoAccounts.geminiHint' : 'videoAccounts.seedanceHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
defineProps<{ modelValue: string; id: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
function changeProtocol(event: Event) {
  emit('update:modelValue', (event.target as HTMLSelectElement).value)
}
</script>
