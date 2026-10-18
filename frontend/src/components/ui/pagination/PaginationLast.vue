<script setup lang="ts">
import type { PaginationLastProps } from 'reka-ui'
import type { HTMLAttributes } from 'vue'
import type { ButtonVariants } from '@/components/ui/button'
import { reactiveOmit } from '@vueuse/core'
import { ChevronRightIcon } from '@lucide/vue'
import { PaginationLast, useForwardProps } from 'reka-ui'
import { useI18n } from 'vue-i18n'
import { cn } from '@/lib/utils'
import { buttonVariants } from '@/components/ui/button'

const props = withDefaults(defineProps<PaginationLastProps & {
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
  <PaginationLast
    :aria-label="t('pagination.last')"
    data-slot="pagination-last"
    :class="cn('shadcn-scope', buttonVariants({ variant: 'ghost', size }), 'gap-1 px-2.5 sm:pr-2.5', props.class)"
    v-bind="forwarded"
  >
    <slot>
      <span class="hidden sm:block">{{ t('pagination.last') }}</span>
      <ChevronRightIcon />
    </slot>
  </PaginationLast>
</template>
