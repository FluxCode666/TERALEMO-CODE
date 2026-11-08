<template>
  <div class="cs-segmented cs-mode-switch" role="tablist" :aria-label="t('creationStudio.form.mode')">
    <button
      v-for="item in items"
      :key="item.mode"
      type="button"
      class="cs-segmented-item"
      role="tab"
      :aria-selected="modelValue === item.mode"
      :data-unavailable="item.reason ? 'true' : undefined"
      :title="item.reason ? t(reasonKey(item.reason)) : undefined"
      @click="emit('update:modelValue', item.mode)"
    >
      <component :is="item.icon" aria-hidden="true" />
      <span>{{ t(`creationStudio.mode.${item.mode}`) }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { Film, ImageIcon } from '@lucide/vue'
import type { CreationMode, CreationUnavailableReason } from '../types'

const props = withDefaults(
  defineProps<{
    modelValue: CreationMode
    imageReason?: CreationUnavailableReason | null
    videoReason?: CreationUnavailableReason | null
  }>(),
  { imageReason: null, videoReason: null },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: CreationMode): void }>()

const { t } = useI18n()

interface ModeItem {
  mode: CreationMode
  icon: Component
  reason: CreationUnavailableReason | null
}

const items = computed<ModeItem[]>(() => [
  { mode: 'image', icon: ImageIcon, reason: props.imageReason ?? null },
  { mode: 'video', icon: Film, reason: props.videoReason ?? null },
])

function reasonKey(reason: CreationUnavailableReason): string {
  switch (reason) {
    case 'storage_disabled':
      return 'creationStudio.unavailable.imageStorage.title'
    case 'permission_denied':
      return 'creationStudio.errors.imageForbidden'
    case 'key_unavailable':
      return 'creationStudio.errors.keyUnavailable'
    case 'no_group':
      return 'creationStudio.key.reason.noGroup'
    case 'no_accounts':
      return 'creationStudio.unavailable.noModel.title'
    case 'platform_unsupported':
    default:
      return 'creationStudio.mode.unsupported'
  }
}
</script>
