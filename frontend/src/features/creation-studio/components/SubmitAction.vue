<template>
  <div class="space-y-2">
    <button
      type="button"
      class="btn btn-primary cs-submit"
      :aria-busy="busy"
      aria-keyshortcuts="Meta+Enter Control+Enter"
      :disabled="disabled || busy"
      @click="emit('submit')"
    >
      <LoaderCircle v-if="busy" class="h-4 w-4 animate-spin" aria-hidden="true" />
      <Sparkles v-else class="h-4 w-4" aria-hidden="true" />
      <span>{{ busy ? t('creationStudio.submit.submitting') : label }}</span>
    </button>
    <p v-if="reason" class="cs-submit-reason">{{ reason }}</p>
    <p v-else class="cs-submit-reason">{{ t('creationStudio.submit.shortcutHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { LoaderCircle, Sparkles } from '@lucide/vue'
import type { CreationMode } from '../types'

const props = withDefaults(
  defineProps<{
    mode: CreationMode
    busy?: boolean
    disabled?: boolean
    reason?: string
  }>(),
  { busy: false, disabled: false, reason: '' },
)

const emit = defineEmits<{ (e: 'submit'): void }>()

const { t } = useI18n()

const label = computed(() =>
  t(props.mode === 'video' ? 'creationStudio.submit.video' : 'creationStudio.submit.image'),
)
</script>
