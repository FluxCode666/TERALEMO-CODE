<template>
  <BaseDialog
    v-if="apiKey"
    :show="!!apiKey"
    :title="t('keys.showOnce.title')"
    :close-on-escape="false"
    :close-on-click-outside="false"
    :show-close-button="false"
  >
    <div class="space-y-4">
      <p role="alert" class="rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm font-medium text-amber-950 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-200">
        {{ t('keys.showOnce.warning') }}
      </p>
      <code data-testid="created-api-key" class="block break-all rounded-lg bg-gray-100 p-4 font-mono text-sm text-gray-900 dark:bg-dark-800 dark:text-gray-100">{{ apiKey }}</code>
      <button type="button" class="btn btn-primary w-full" data-testid="copy-created-key" @click="copyKey">
        <Icon name="clipboard" size="sm" class="mr-2" />
        {{ copied ? t('keys.copied') : t('keys.showOnce.copy') }}
      </button>
      <label class="flex cursor-pointer items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
        <input v-model="saved" type="checkbox" class="mt-0.5 h-4 w-4 shrink-0" data-testid="confirm-key-saved" />
        {{ t('keys.showOnce.confirm') }}
      </label>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="!saved" data-testid="close-created-key" @click="finish">
        {{ t('keys.showOnce.done') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{ apiKey: string }>()
const emit = defineEmits<{ (event: 'saved'): void }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const saved = ref(false)
const copied = ref(false)

watch(() => props.apiKey, () => {
  saved.value = false
  copied.value = false
})

async function copyKey() {
  copied.value = await copyToClipboard(props.apiKey, t('keys.copied'))
}

function finish() {
  if (saved.value) emit('saved')
}
</script>
