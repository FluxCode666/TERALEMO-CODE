<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="cs-lightbox-overlay" />
      <DialogContent class="cs-lightbox" @keydown="onKeydown">
        <DialogTitle class="sr-only">{{ title }}</DialogTitle>
        <div class="cs-lightbox-toolbar">
          <span v-if="current?.tag" class="cs-lightbox-tag">{{ current.tag }}</span>
          <span v-else class="cs-lightbox-counter">
            {{ t('creationStudio.lightbox.preview', { index: safeIndex + 1, total: items.length }) }}
          </span>
          <div class="flex items-center gap-1">
            <a
              v-if="currentUrl"
              class="cs-lightbox-btn cs-lightbox-btn--icon"
              :href="currentUrl"
              :download="`creation-studio-${safeIndex + 1}`"
              target="_blank"
              rel="noopener noreferrer"
              :aria-label="t('creationStudio.action.download')"
            >
              <Download aria-hidden="true" />
            </a>
            <button
              type="button"
              class="cs-lightbox-btn cs-lightbox-btn--icon"
              :aria-label="t('creationStudio.action.copyLink')"
              @click="copyCurrent"
            >
              <Copy aria-hidden="true" />
            </button>
            <button
              type="button"
              class="cs-lightbox-btn cs-lightbox-btn--icon"
              :aria-label="t('creationStudio.lightbox.close')"
              @click="emit('update:open', false)"
            >
              <X aria-hidden="true" />
            </button>
          </div>
        </div>

        <div
          class="cs-lightbox-stage"
          @touchstart.passive="onTouchStart"
          @touchend.passive="onTouchEnd"
        >
          <img v-if="current?.kind === 'image' && currentUrl" :src="currentUrl" :alt="current.alt ?? ''" />
          <video v-else-if="current?.kind === 'video' && currentUrl" :src="currentUrl" controls playsinline />
          <button
            type="button"
            class="cs-lightbox-nav cs-lightbox-nav--prev"
            :disabled="safeIndex <= 0"
            :aria-label="t('creationStudio.lightbox.prev')"
            @click="step(-1)"
          >
            <ChevronLeft aria-hidden="true" />
          </button>
          <button
            type="button"
            class="cs-lightbox-nav cs-lightbox-nav--next"
            :disabled="safeIndex >= items.length - 1"
            :aria-label="t('creationStudio.lightbox.next')"
            @click="step(1)"
          >
            <ChevronRight aria-hidden="true" />
          </button>
        </div>

        <p v-if="current?.caption" class="cs-lightbox-caption">{{ current.caption }}</p>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<script setup lang="ts">
/**
 * 灯箱：基于 reka-ui 的 Dialog 原语（自带焦点陷阱与 Esc 关闭），样式全用 `cs-lightbox*`。
 * 键盘 ←/→ 切换、Esc 关闭；触屏左右滑动切换；关闭后焦点由 Dialog 还给触发元素。
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight, Copy, Download, X } from '@lucide/vue'
import { DialogContent, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { useClipboard } from '@/composables/useClipboard'
import { safeURL } from '../safeURL'
import type { CreationLightboxItem } from '../types'

const props = withDefaults(
  defineProps<{
    open: boolean
    items: CreationLightboxItem[]
    index: number
    title?: string
  }>(),
  { title: 'Preview' },
)

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'update:index', value: number): void
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const touchStartX = ref<number | null>(null)

const safeIndex = computed(() => Math.min(Math.max(props.index, 0), Math.max(props.items.length - 1, 0)))
const current = computed<CreationLightboxItem | undefined>(() => props.items[safeIndex.value])
const currentUrl = computed(() => safeURL(current.value?.url))

function step(direction: 1 | -1) {
  const next = safeIndex.value + direction
  if (next < 0 || next >= props.items.length) return
  emit('update:index', next)
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowLeft') {
    event.preventDefault()
    step(-1)
  } else if (event.key === 'ArrowRight') {
    event.preventDefault()
    step(1)
  }
}

function onTouchStart(event: TouchEvent) {
  touchStartX.value = event.changedTouches[0]?.clientX ?? null
}

function onTouchEnd(event: TouchEvent) {
  if (touchStartX.value === null) return
  const endX = event.changedTouches[0]?.clientX ?? touchStartX.value
  const delta = endX - touchStartX.value
  touchStartX.value = null
  if (Math.abs(delta) < 40) return
  step(delta > 0 ? -1 : 1)
}

async function copyCurrent() {
  if (currentUrl.value) await copyToClipboard(currentUrl.value)
}
</script>
