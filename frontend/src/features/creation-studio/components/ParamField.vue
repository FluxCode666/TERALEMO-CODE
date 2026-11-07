<template>
  <div v-if="isKnown" class="cs-field">
    <div class="cs-field-head">
      <span :id="labelId" class="cs-field-label">{{ t(descriptor.labelKey) }}</span>
      <span v-if="descriptor.hintKey" class="cs-field-hint">{{ t(descriptor.hintKey) }}</span>
    </div>

    <ChipGroup
      v-if="selectDescriptor && selectDescriptor.options.length <= 4"
      :name="`param-${descriptor.key}`"
      :model-value="stringValue"
      :options="chipOptions"
      :labelledby="labelId"
      :disabled="disabled"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <Select
      v-else-if="selectDescriptor"
      :id="`param-${descriptor.key}`"
      :model-value="stringValue"
      :options="chipOptions"
      :disabled="disabled"
      :aria-label="t(descriptor.labelKey)"
      @update:model-value="onSelect"
    />
    <IntegerStepper
      v-else-if="integerDescriptor"
      :model-value="numberValue"
      :min="integerDescriptor.min"
      :max="integerDescriptor.max"
      :step="integerDescriptor.step"
      :unit="integerDescriptor.unitKey ? t(integerDescriptor.unitKey) : ''"
      :disabled="disabled"
      :invalid="!valid"
      :input-id="`param-${descriptor.key}`"
      :label="t(descriptor.labelKey)"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <RatioGroup
      v-else-if="ratioDescriptor"
      :name="`param-${descriptor.key}`"
      :model-value="stringValue"
      :options="ratioDescriptor.options"
      :labelledby="labelId"
      :disabled="disabled"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <ImageDropzone
      v-else-if="imageDescriptor && images"
      :descriptor="imageDescriptor"
      :entries="images.entries"
      :errors="images.errors"
      :available="imageAvailable"
      :uploading="images.uploading"
      :limit-reached="images.limitReached"
      :removed-unavailable="images.removedUnavailable"
      @pick="emit('pick', $event)"
      @retry="emit('retry', $event)"
      @replace="(id: string, files: File[]) => emit('replace', id, files)"
      @remove="emit('remove', $event)"
      @expire="emit('expire', $event)"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import ChipGroup from './ChipGroup.vue'
import RatioGroup from './RatioGroup.vue'
import IntegerStepper from './IntegerStepper.vue'
import ImageDropzone from './ImageDropzone.vue'
import { isKnownParamKind, isParamValueValid } from '../useCreationForm'
import type { ImageFieldBinding } from '../useCreationUploads'
import type { CreationParamDescriptor } from '../types'

const props = withDefaults(
  defineProps<{
    descriptor: CreationParamDescriptor
    modelValue?: string | number
    images?: ImageFieldBinding | null
    disabled?: boolean
  }>(),
  { modelValue: '', images: null, disabled: false },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string | number): void
  (e: 'pick', files: File[]): void
  (e: 'retry', id: string): void
  (e: 'replace', id: string, files: File[]): void
  (e: 'remove', id: string): void
  (e: 'expire', id: string): void
}>()

const { t } = useI18n()

/** 未知 kind 连标签一起跳过（R1-02）：后端将来加 kind 时不能把整块表单卡死。 */
const isKnown = computed(() => isKnownParamKind(props.descriptor.kind))
const labelId = computed(() => `param-label-${props.descriptor.key}`)
const stringValue = computed(() => String(props.modelValue ?? ''))
const numberValue = computed(() => {
  const parsed = typeof props.modelValue === 'number' ? props.modelValue : Number.parseInt(String(props.modelValue ?? ''), 10)
  return Number.isFinite(parsed) ? parsed : 0
})
const valid = computed(() => isParamValueValid(props.descriptor, props.modelValue))

const selectDescriptor = computed(() => (props.descriptor.kind === 'select' ? props.descriptor : null))
const integerDescriptor = computed(() => (props.descriptor.kind === 'integer' ? props.descriptor : null))
const ratioDescriptor = computed(() => (props.descriptor.kind === 'ratio' ? props.descriptor : null))
const imageDescriptor = computed(() => (props.descriptor.kind === 'image' ? props.descriptor : null))

/** 描述符自身被标记 disabled（对象存储关闭）时，入口一律不可用（R1-17）。 */
const imageAvailable = computed(
  () => Boolean(props.images?.available) && !imageDescriptor.value?.disabled,
)

const chipOptions = computed(() => {
  const descriptor = selectDescriptor.value
  if (!descriptor) return []
  return descriptor.options.map(option => ({
    value: option.value,
    label: option.labelKey ? t(option.labelKey) : (option.label ?? option.value),
    disabled: option.disabled ?? false,
  }))
})

function onSelect(value: string | number | boolean | null) {
  emit('update:modelValue', typeof value === 'string' ? value : String(value ?? ''))
}
</script>
