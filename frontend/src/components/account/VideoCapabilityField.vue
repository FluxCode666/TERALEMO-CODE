<template>
  <div class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <div class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('videoAccounts.capability.title') }}</div>
    <div>
      <span class="input-label">{{ t('videoAccounts.capability.resolutions') }}</span>
      <div class="mt-2 flex flex-wrap gap-3">
        <label
          v-for="option in resolutionOptions"
          :key="option"
          class="flex cursor-pointer items-center gap-1.5 text-sm text-gray-700 dark:text-gray-300"
        >
          <input
            type="checkbox"
            class="rounded border-gray-300 text-primary-600"
            :data-testid="`${idPrefix}-resolution-${option}`"
            :checked="resolutions.includes(option)"
            @change="toggleResolution(option, $event)"
          />
          {{ option === '2k' ? '2K' : option }}
        </label>
      </div>
      <p class="input-hint mt-1">{{ t('videoAccounts.capability.resolutionsHint') }}</p>
    </div>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label :for="`${idPrefix}-min-duration`" class="input-label">{{ t('videoAccounts.capability.minDuration') }}</label>
        <input
          :id="`${idPrefix}-min-duration`"
          type="number"
          min="1"
          max="60"
          step="1"
          class="input"
          :placeholder="t('videoAccounts.capability.durationPlaceholder')"
          :value="minDuration"
          @input="emit('update:minDuration', ($event.target as HTMLInputElement).value)"
        />
      </div>
      <div>
        <label :for="`${idPrefix}-max-duration`" class="input-label">{{ t('videoAccounts.capability.maxDuration') }}</label>
        <input
          :id="`${idPrefix}-max-duration`"
          type="number"
          min="1"
          max="60"
          step="1"
          class="input"
          :placeholder="t('videoAccounts.capability.durationPlaceholder')"
          :value="maxDuration"
          @input="emit('update:maxDuration', ($event.target as HTMLInputElement).value)"
        />
      </div>
    </div>
    <p class="input-hint">{{ t('videoAccounts.capability.hint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { VIDEO_RESOLUTION_OPTIONS, normalizeVideoResolutions } from '@/utils/videoCapabilities'

const { t } = useI18n()

const props = defineProps<{
  resolutions: string[]
  minDuration: string | number
  maxDuration: string | number
  idPrefix: string
}>()

const emit = defineEmits<{
  'update:resolutions': [value: string[]]
  'update:minDuration': [value: string]
  'update:maxDuration': [value: string]
}>()

const resolutionOptions = VIDEO_RESOLUTION_OPTIONS

function toggleResolution(option: string, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  const next = checked ? [...props.resolutions, option] : props.resolutions.filter(value => value !== option)
  emit('update:resolutions', normalizeVideoResolutions(next))
}
</script>
