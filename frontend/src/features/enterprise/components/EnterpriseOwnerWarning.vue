<template>
  <div v-if="activeOwnerCount === 0" role="status" class="space-y-1" data-test="enterprise-owner-warning">
    <span class="badge badge-warning">
      <Icon name="exclamationTriangle" size="sm" aria-hidden="true" />
      {{ t('admin.enterprises.noActiveOwner') }}
    </span>
    <p class="text-xs text-amber-800 dark:text-amber-300">{{ t('admin.enterprises.noActiveOwnerHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

defineProps<{ activeOwnerCount?: number }>()
const { t } = useI18n()
</script>
