<template>
  <div class="cs-field">
    <div class="cs-field-head">
      <label class="cs-field-label" :for="inputId">{{ t('creationStudio.form.prompt') }}</label>
      <span class="cs-field-counter" :data-over="tooLong">{{ length }} / {{ max }}</span>
    </div>
    <textarea
      :id="inputId"
      class="input cs-prompt"
      :value="modelValue"
      :placeholder="t('creationStudio.form.promptPlaceholder')"
      :aria-invalid="Boolean(error)"
      :aria-describedby="error ? errorId : undefined"
      aria-keyshortcuts="Meta+Enter Control+Enter"
      rows="5"
      @input="onInput"
      @keydown.meta.enter.prevent="emit('submit')"
      @keydown.ctrl.enter.prevent="emit('submit')"
    />
    <div class="cs-prompt-footer">
      <p v-if="error" :id="errorId" class="cs-field-error">
        <CircleX aria-hidden="true" />
        <span>{{ error }}</span>
      </p>
      <span class="cs-kbd" aria-hidden="true">⌘↵</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleX } from '@lucide/vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    max: number
    error?: string
    inputId?: string
  }>(),
  { error: '', inputId: 'creation-prompt' },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'submit'): void
}>()

const { t } = useI18n()

const errorId = computed(() => `${props.inputId}-error`)
const length = computed(() => [...props.modelValue].length)
const tooLong = computed(() => length.value > props.max)

function onInput(event: Event) {
  emit('update:modelValue', (event.target as HTMLTextAreaElement).value)
}
</script>
