<script setup lang="ts">
import { onMounted, nextTick, ref, watch } from 'vue'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useMotion } from '@/composables/useMotion'
const props = defineProps<{ title: string; value: number; currency?: boolean }>()
const root = ref<HTMLElement | null>(null)
const motion = useMotion(root)
const format = (value: number) => props.currency ? `$${value.toFixed(4)}` : Math.round(value).toLocaleString()
async function animate() { await nextTick(); motion.stop(); motion.countUp('[data-metric-value]', props.value, { format }) }
onMounted(animate)
watch(() => props.value, animate)
</script>
<template><div ref="root" class="min-w-0" data-metric><Card class="h-full"><CardHeader class="pb-2"><CardTitle class="text-sm font-medium text-muted-foreground">{{ title }}</CardTitle></CardHeader><CardContent><p data-metric-value class="break-words text-2xl font-semibold tracking-tight tabular-nums">{{ format(value) }}</p><slot name="footer" /></CardContent></Card></div></template>
