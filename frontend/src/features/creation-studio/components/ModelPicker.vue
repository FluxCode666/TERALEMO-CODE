<template>
  <div class="cs-field">
    <label class="cs-field-label" :for="selectId">{{ t('creationStudio.form.model') }}</label>
    <Select
      :id="selectId"
      :model-value="modelValue"
      :options="options"
      :disabled="disabled || options.length === 0"
      :placeholder="t('creationStudio.unavailable.noModel.title')"
      :empty-text="t('creationStudio.unavailable.noModel.title')"
      @update:model-value="onSelect"
    >
      <template #option="{ option }">
        <span class="cs-option">
          <span class="cs-option-main">
            <span class="cs-option-name">
              <ModelIcon :model="option.model.model" size="16px" />
              {{ option.label }}
            </span>
            <span v-if="option.model.partial" class="cs-option-sub">
              {{ t('creationStudio.model.partial') }}
            </span>
          </span>
          <span v-if="asideText(option.model)" class="cs-option-aside">{{ asideText(option.model) }}</span>
        </span>
      </template>
      <template #selected>
        <span class="cs-option-name">
          <ModelIcon v-if="selectedModel" :model="selectedModel.model" size="16px" />
          {{ selectedModel?.display_name ?? t('creationStudio.unavailable.noModel.title') }}
        </span>
      </template>
    </Select>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import type { CreationModelOption } from '../types'

const props = withDefaults(
  defineProps<{
    modelValue: string
    models: CreationModelOption[]
    disabled?: boolean
  }>(),
  { disabled: false },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const { t } = useI18n()

const selectId = 'creation-model-picker'

const options = computed(() =>
  props.models.map(model => ({
    value: model.id,
    label: model.display_name,
    model,
  })),
)

const selectedModel = computed(() => props.models.find(model => model.id === props.modelValue) ?? null)

function asideText(model: CreationModelOption): string {
  const mode = model.pricing.mode
  if (!mode) return ''
  return t(`creationStudio.pricing.${mode}`)
}

function onSelect(value: string | number | boolean | null) {
  emit('update:modelValue', typeof value === 'string' ? value : '')
}
</script>
