<script setup lang="ts">
import { nextTick, onMounted, ref, watch, type Component } from 'vue'
import { Card, CardContent } from '@/components/ui/card'
import { useMotion } from '@/composables/useMotion'
import { usageMoney } from '../useEnterpriseUsage'
const props = defineProps<{ title: string; value: number; icon: Component; money?: boolean }>()
const root = ref<HTMLElement>()
const { countUp, stop } = useMotion(root)
const format = (value: number) => props.money ? usageMoney(value) : Math.round(value).toLocaleString()
async function animate() { await nextTick(); stop(); countUp('[data-usage-value]', props.value, { format }) }
onMounted(animate)
watch(() => props.value, animate)
</script>

<template>
  <Card class="min-w-0" data-usage-card>
    <CardContent class="p-5">
      <div ref="root" class="flex min-w-0 items-start justify-between gap-3">
        <div class="min-w-0"><p class="text-sm text-muted-foreground">{{ title }}</p><p class="mt-3 break-all text-2xl font-semibold tracking-tight tabular-nums" data-usage-value>{{ format(value) }}</p></div>
        <div class="rounded-lg bg-primary/10 p-2.5 text-primary"><component :is="icon" class="size-5" /></div>
      </div>
    </CardContent>
  </Card>
</template>
