<script setup lang="ts">
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
defineProps<{ show: boolean; title: string; width?: string }>()
defineEmits<{ close: [] }>()
</script>
<template>
  <Dialog :open="show" @update:open="open => { if (!open) $emit('close') }">
    <DialogContent class="max-h-[90dvh] w-[calc(100%-2rem)] overflow-y-auto rounded-ui-lg" :class="width === 'wide' ? 'sm:max-w-3xl' : ''">
      <DialogHeader><DialogTitle class="break-words pr-6">{{ title }}</DialogTitle><DialogDescription class="sr-only">{{ title }}</DialogDescription></DialogHeader>
      <slot />
    </DialogContent>
  </Dialog>
</template>
