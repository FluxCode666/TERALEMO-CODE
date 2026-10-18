<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { useI18n } from 'vue-i18n'
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel } from '@/components/ui/alert-dialog'
defineProps<{ show: boolean; title: string; message: string; danger?: boolean; busy?: boolean; disabled?: boolean }>()
defineEmits<{ cancel: []; confirm: [] }>()
const { t } = useI18n()
</script>
<template>
  <AlertDialog :open="show" @update:open="open => { if (!open && !busy) $emit('cancel') }">
    <AlertDialogContent class="max-h-[90dvh] w-[calc(100%-2rem)] overflow-y-auto rounded-ui-lg">
      <AlertDialogHeader><AlertDialogTitle>{{ title }}</AlertDialogTitle><AlertDialogDescription class="whitespace-pre-line leading-relaxed">{{ message }}</AlertDialogDescription></AlertDialogHeader>
      <slot />
      <AlertDialogFooter>
        <AlertDialogCancel :disabled="busy" data-test="enterprise-confirm-cancel">{{ t('enterprise.cancel') }}</AlertDialogCancel>
        <Button type="button" :disabled="busy || disabled" :class="danger ? 'bg-destructive text-destructive-foreground hover:bg-destructive/90' : ''" data-test="enterprise-confirm-action" @click.prevent="$emit('confirm')">{{ t('common.confirm') }}</Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
