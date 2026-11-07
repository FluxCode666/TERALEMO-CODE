<template>
  <div class="cs-field">
    <label class="cs-field-label" :for="selectId">{{ t('creationStudio.form.apiKey') }}</label>
    <Select
      :id="selectId"
      :model-value="modelValue"
      :options="options"
      :disabled="disabled || options.length === 0"
      :loading="loading"
      :error="failed"
      searchable
      :placeholder="t('creationStudio.form.apiKeyPlaceholder')"
      :empty-text="t('creationStudio.unavailable.noKey.title')"
      @update:model-value="onSelect"
    >
      <template #option="{ option }">
        <span class="cs-option">
          <span class="cs-option-main">
            <span class="cs-option-name">{{ option.label }}</span>
            <span class="cs-option-sub">{{ maskKey(option.apiKey?.key) }}</span>
          </span>
          <GroupBadge
            v-if="option.apiKey?.group"
            class="cs-option-aside"
            :name="option.apiKey.group.name"
            :platform="option.apiKey.group.platform"
            :show-rate="false"
          />
        </span>
      </template>
      <template #selected="{ option }">
        <span class="cs-option-name">{{ option?.label ?? t('creationStudio.form.apiKeyPlaceholder') }}</span>
      </template>
    </Select>
    <p v-if="failed" class="cs-field-error">
      <CircleX aria-hidden="true" />
      <span>
        {{ t('creationStudio.feed.loadFailed') }}
        <button type="button" class="cs-field-action" @click="emit('retry')">
          {{ t('creationStudio.action.retry') }}
        </button>
      </span>
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleX } from '@lucide/vue'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import type { ApiKey } from '@/types'

const props = withDefaults(
  defineProps<{
    modelValue: number | null
    keys: ApiKey[]
    loading?: boolean
    failed?: boolean
    disabled?: boolean
  }>(),
  { loading: false, failed: false, disabled: false },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: number | null): void
  (e: 'retry'): void
}>()

const { t } = useI18n()

const selectId = 'creation-key-picker'

const options = computed(() =>
  props.keys.map(key => ({
    value: key.id,
    label: key.name,
    apiKey: key,
  })),
)

/** `sk-****1234`：只露出末 4 位，避免把 Key 明文铺在页面上。 */
function maskKey(value?: string): string {
  if (!value) return ''
  const tail = value.slice(-4)
  return value.startsWith('sk-') ? `sk-****${tail}` : `****${tail}`
}

function onSelect(value: string | number | boolean | null) {
  emit('update:modelValue', typeof value === 'number' ? value : null)
}
</script>
