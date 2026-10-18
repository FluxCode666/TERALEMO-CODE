<script setup lang="ts">
import type { PaginationPrevProps } from 'reka-ui'
import type { HTMLAttributes } from 'vue'
import type { ButtonVariants } from '@/components/ui/button'
import { reactiveOmit } from '@vueuse/core'
import { ChevronLeftIcon } from '@lucide/vue'
import { PaginationPrev, useForwardProps } from 'reka-ui'
import { useI18n } from 'vue-i18n'
import { cn } from '@/lib/utils'
import { buttonVariants } from '@/components/ui/button'

const props = withDefaults(defineProps<PaginationPrevProps & {
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
  <PaginationPrev
    :aria-label="t('pagination.previous')"
    data-slot="pagination-previous"
    :class="cn('shadcn-scope', buttonVariants({ variant: 'ghost', size }), 'gap-1 px-2.5 sm:pr-2.5', props.class)"
    v-bind="forwarded"
  >
    <slot>
      <ChevronLeftIcon />
      <span class="hidden sm:block">{{ t('pagination.previous') }}</span>
    </slot>
  </PaginationPrev>
</template>
