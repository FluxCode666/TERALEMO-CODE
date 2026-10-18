<template>
  <div ref="root" class="shadcn-scope grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    <component :is="selectable ? Button : Card" :variant="selectable ? 'outline' : undefined" v-for="group in groups" :key="group.id" :type="selectable ? 'button' : undefined" :disabled="selectable && disabled" :aria-pressed="selectable ? modelValue.includes(group.id) : undefined" :data-test="`group-card-${group.id}`" class="h-auto min-w-0 flex-col items-stretch justify-start whitespace-normal p-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50" :class="selectable && modelValue.includes(group.id) ? 'border-primary bg-primary/5 ring-1 ring-primary' : 'border-border bg-card'" @click="toggle(group.id)">
      <div class="flex items-start justify-between gap-2"><h4 class="min-w-0 break-words font-medium text-foreground">{{ group.name }}</h4><span v-if="selectable" class="flex size-4 shrink-0 items-center justify-center rounded border" :class="modelValue.includes(group.id) ? 'border-primary bg-primary text-primary-foreground' : 'border-input'" aria-hidden="true"><Check v-if="modelValue.includes(group.id)" class="size-3" /></span></div>
      <div class="mt-3 flex flex-wrap items-center gap-2 text-xs"><Badge variant="secondary">{{ platformName(group.platform) }}</Badge><span class="text-muted-foreground">{{ group.rate_multiplier == null ? t('enterprise.groupAccess.rateUnknown') : `${group.rate_multiplier}×` }}</span><Badge v-if="group.is_exclusive" variant="outline">{{ t('enterprise.groupAccess.exclusive') }}</Badge><Badge v-if="group.subscription_type === 'subscription'" variant="secondary">{{ t('enterprise.groupAccess.subscription') }}</Badge></div>
    </component>
  </div>
</template>
<script setup lang="ts">


import { Button } from '@/components/ui/button'
import { nextTick, onMounted, ref, watch } from 'vue'
import { useMotion } from '@/composables/useMotion'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Check } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseGroup } from '@/types/enterprise'
const props = withDefaults(defineProps<{ groups: EnterpriseGroup[]; selectable?: boolean; disabled?: boolean; modelValue?: number[] }>(), { modelValue: () => [] })
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()
const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const motion = useMotion(root)
async function animate() { await nextTick(); motion.stop(); motion.stagger('[data-test^=group-card-]', { distance: 5 }) }
onMounted(animate)
watch(() => props.groups, animate)
function platformName(platform: string) { return ({ openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Gemini', antigravity: 'Antigravity' } as Record<string, string>)[platform] || platform }
function toggle(id: number) { if (props.selectable && !props.disabled) emit('update:modelValue', props.modelValue.includes(id) ? props.modelValue.filter(item => item !== id) : [...props.modelValue, id]) }
</script>
