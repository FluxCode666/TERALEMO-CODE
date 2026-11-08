<template>
  <div class="cs-media-grid" :data-count="gridCount" :style="gridStyle">
    <template v-if="skeleton">
      <div
        v-for="index in skeletonCount"
        :key="`skeleton-${index}`"
        class="cs-media-tile cs-media-skeleton"
        aria-hidden="true"
      >
        <ImageIcon />
      </div>
    </template>
    <template v-else>
      <div v-for="(url, index) in validUrls" :key="`${url}-${index}`" class="cs-media-tile">
        <img :src="url" :alt="`${altPrefix} ${index + 1}`" loading="lazy" />
        <button
          type="button"
          class="cs-media-open"
          :aria-label="t('creationStudio.action.previewCount', { index: index + 1, total: validUrls.length })"
          @click="emit('open', index)"
        />
        <span v-if="validUrls.length > 1" class="cs-media-index">{{ index + 1 }}</span>
        <div class="cs-media-overlay">
          <a
            class="cs-media-action"
            :href="url"
            :download="`${downloadPrefix}-${index + 1}`"
            target="_blank"
            rel="noopener noreferrer"
            :aria-label="t('creationStudio.action.download')"
          >
            <Download aria-hidden="true" />
          </a>
          <button
            type="button"
            class="cs-media-action"
            :data-state="copiedIndex === index ? 'done' : undefined"
            :aria-label="t('creationStudio.action.copyLink')"
            @click="copy(url, index)"
          >
            <CircleCheck v-if="copiedIndex === index" aria-hidden="true" />
            <Copy v-else aria-hidden="true" />
          </button>
          <a
            class="cs-media-action"
            :href="url"
            target="_blank"
            rel="noopener noreferrer"
            :aria-label="t('creationStudio.action.openInNewTab')"
          >
            <ExternalLink aria-hidden="true" />
          </a>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleCheck, Copy, Download, ExternalLink, ImageIcon } from '@lucide/vue'
import { useClipboard } from '@/composables/useClipboard'
import { safeURL } from '../safeURL'

const props = withDefaults(
  defineProps<{
    urls?: string[]
    skeleton?: boolean
    skeletonCount?: number
    aspectW?: number
    aspectH?: number
    altPrefix: string
    downloadPrefix?: string
  }>(),
  {
    urls: () => [],
    skeleton: false,
    skeletonCount: 1,
    aspectW: 1,
    aspectH: 1,
    downloadPrefix: 'creation-studio',
  },
)

const emit = defineEmits<{ (e: 'open', index: number): void }>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const copiedIndex = ref<number | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | null = null

const validUrls = computed(() => props.urls.map(url => safeURL(url)).filter((url): url is string => Boolean(url)))

/** 1–4 用固定排布；更多走 auto-fill 兜底（`data-count` 不设值）。 */
const gridCount = computed(() => {
  const count = props.skeleton ? props.skeletonCount : validUrls.value.length
  return count >= 1 && count <= 4 ? count : undefined
})

const gridStyle = computed(() => ({
  '--cs-aspect-w': String(props.aspectW || 1),
  '--cs-aspect-h': String(props.aspectH || 1),
}))

async function copy(url: string, index: number) {
  const ok = await copyToClipboard(url)
  if (!ok) return
  copiedIndex.value = index
  if (copiedTimer !== null) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copiedIndex.value = null
    copiedTimer = null
  }, 1500)
}

onUnmounted(() => {
  if (copiedTimer !== null) clearTimeout(copiedTimer)
})
</script>
