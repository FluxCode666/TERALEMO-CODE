<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

const props = defineProps<{
  class?: HTMLAttributes["class"]
}>()
</script>

<template>
  <div
    :class="
      cn('shadcn-scope',
        'rounded-ui-xl border bg-card text-card-foreground shadow-card',
        props.class,
      )
    "
  >
    <slot />
  </div>
</template>
