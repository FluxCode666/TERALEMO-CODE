<template>
  <div class="space-y-2">
    <label :for="id" class="input-label">{{ t('imageAccounts.protocol') }}</label>
    <select :id="id" class="input" :value="modelValue" @change="changeProtocol">
      <option value="openai">OpenAI</option>
      <option value="gemini">Google Gemini (Imagen / Nano Banana)</option>
      <option value="grok">xAI Grok</option>
    </select>
    <p class="input-hint">{{ t(`imageAccounts.${modelValue === 'gemini' ? 'geminiHint' : modelValue === 'grok' ? 'grokHint' : 'openaiHint'}`) }}</p>
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
