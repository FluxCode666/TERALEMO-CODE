<script setup lang="ts">
import { Inbox } from '@lucide/vue'
defineProps<{ title: string; description?: string }>()
</script>
<template><div role="status" class="shadcn-scope flex flex-col items-center gap-2 p-8 text-center"><Inbox class="mb-1 size-8 text-muted-foreground/60" aria-hidden="true" /><p class="text-sm font-medium text-foreground">{{ title }}</p><p v-if="description" class="max-w-sm text-sm text-muted-foreground">{{ description }}</p></div></template>
