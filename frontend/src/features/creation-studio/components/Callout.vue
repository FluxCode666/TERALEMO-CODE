<template>
  <div
    class="cs-callout"
    :class="`cs-callout--${tone}`"
    :role="tone === 'danger' ? 'alert' : 'status'"
  >
    <component :is="toneIcon" class="cs-callout-icon" aria-hidden="true" />
    <div class="cs-callout-content">
      <p class="cs-callout-title">{{ title }}</p>
      <p v-if="body" class="cs-callout-body">{{ body }}</p>
      <div v-if="$slots.actions" class="cs-callout-actions">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import { CircleX, Info, TriangleAlert } from '@lucide/vue'

const props = withDefaults(
  defineProps<{
    tone?: 'info' | 'warning' | 'danger' | 'neutral'
    title: string
    body?: string
    icon?: Component
  }>(),
  { tone: 'info', body: '' },
)

const toneIcon = computed<Component>(() => {
  if (props.icon) return props.icon
  if (props.tone === 'warning') return TriangleAlert
  if (props.tone === 'danger') return CircleX
  return Info
})
</script>
