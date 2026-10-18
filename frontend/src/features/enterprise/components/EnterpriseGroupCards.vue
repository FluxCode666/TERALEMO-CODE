<template>
  <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    <component :is="selectable ? 'button' : 'div'" v-for="group in groups" :key="group.id" :type="selectable ? 'button' : undefined" :disabled="selectable && disabled" :aria-pressed="selectable ? modelValue.includes(group.id) : undefined" :data-test="`group-card-${group.id}`" class="min-w-0 rounded-xl border p-4 text-left transition-colors" :class="selectable && modelValue.includes(group.id) ? 'border-primary-500 bg-primary-50 ring-1 ring-primary-500 dark:bg-primary-900/20' : 'border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800'" @click="toggle(group.id)">
      <div class="flex items-start justify-between gap-2"><h4 class="min-w-0 break-words font-medium text-gray-900 dark:text-gray-100">{{ group.name }}</h4><span v-if="selectable" class="text-primary-600 dark:text-primary-400" aria-hidden="true">{{ modelValue.includes(group.id) ? '●' : '○' }}</span></div>
      <div class="mt-3 flex flex-wrap items-center gap-2 text-xs"><span class="rounded-md bg-gray-100 px-2 py-1 font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-200">{{ platformName(group.platform) }}</span><span class="text-gray-500 dark:text-gray-400">{{ group.rate_multiplier == null ? t('enterprise.groupAccess.rateUnknown') : `${group.rate_multiplier}×` }}</span><span v-if="group.is_exclusive" class="badge-primary">{{ t('enterprise.groupAccess.exclusive') }}</span><span v-if="group.subscription_type === 'subscription'" class="badge-success">{{ t('enterprise.groupAccess.subscription') }}</span></div>
    </component>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { EnterpriseGroup } from '@/types/enterprise'
const props = withDefaults(defineProps<{ groups: EnterpriseGroup[]; selectable?: boolean; disabled?: boolean; modelValue?: number[] }>(), { modelValue: () => [] })
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()
const { t } = useI18n()
function platformName(platform: string) { return ({ openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Gemini', antigravity: 'Antigravity' } as Record<string, string>)[platform] || platform }
function toggle(id: number) { if (props.selectable && !props.disabled) emit('update:modelValue', props.modelValue.includes(id) ? props.modelValue.filter(item => item !== id) : [...props.modelValue, id]) }
</script>
