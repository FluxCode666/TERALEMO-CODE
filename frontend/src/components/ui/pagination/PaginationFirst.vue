<script setup lang="ts">
import type { PaginationFirstProps } from 'reka-ui'
import type { HTMLAttributes } from 'vue'
import type { ButtonVariants } from '@/components/ui/button'
import { reactiveOmit } from '@vueuse/core'
import { ChevronLeftIcon } from '@lucide/vue'
import { PaginationFirst, useForwardProps } from 'reka-ui'
import { useI18n } from 'vue-i18n'
import { cn } from '@/lib/utils'
import { buttonVariants } from '@/components/ui/button'

const props = withDefaults(defineProps<PaginationFirstProps & {
  size?: ButtonVariants["size"]
  class?: HTMLAttributes["class"]
}>(), {
  size: 'default',
})

const delegatedProps = reactiveOmit(props, 'class', 'size')
const forwarded = useForwardProps(delegatedProps)
const { t } = useI18n()
</script>

<template>
  <PaginationFirst
    :aria-label="t('pagination.first')"
    data-slot="pagination-first"
    :class="cn('shadcn-scope', buttonVariants({ variant: 'ghost', size }), 'gap-1 px-2.5 sm:pr-2.5', props.class)"
    v-bind="forwarded"
  >
    <slot>
      <ChevronLeftIcon />
      <span class="hidden sm:block">{{ t('pagination.first') }}</span>
    </slot>
  </PaginationFirst>
</template>
