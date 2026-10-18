<template>
  <div v-if="activeOwnerCount === 0" role="status" class="shadcn-scope space-y-1" data-test="enterprise-owner-warning">
    <Badge variant="outline" class="gap-1 border-amber-500/40 bg-amber-500/10 text-amber-800 dark:text-amber-300">
      <TriangleAlert class="size-4" aria-hidden="true" />
      {{ t('admin.enterprises.noActiveOwner') }}
    </Badge>
    <p class="text-xs text-amber-800 dark:text-amber-300">{{ t('admin.enterprises.noActiveOwnerHint') }}</p>
  </div>
</template>

<script setup lang="ts">


import { useI18n } from 'vue-i18n'
import { TriangleAlert } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'

defineProps<{ activeOwnerCount?: number }>()
const { t } = useI18n()
</script>
