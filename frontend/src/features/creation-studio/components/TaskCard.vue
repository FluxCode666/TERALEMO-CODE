<template>
  <article class="card cs-task" :data-status="task.status" :class="{ 'cs-enter': task.fresh }">
    <header class="cs-task-header">
      <span class="cs-task-kind">
        <Film v-if="task.kind === 'video'" aria-hidden="true" />
        <ImageIcon v-else aria-hidden="true" />
      </span>
      <h3 class="cs-task-title">{{ task.model || task.taskId }}</h3>
      <span class="cs-task-meta">
        <span>{{ task.protocol }}</span>
        <span class="cs-dot-sep" aria-hidden="true" />
        <span>{{ formatTime(task.createdAt) }}</span>
      </span>
      <span class="cs-status" :class="`cs-status--${task.status}`">
        <component :is="statusIcon" :class="{ 'animate-spin': isPending }" aria-hidden="true" />
        <span>{{ statusLabel }}</span>
      </span>
    </header>

    <p
      v-if="task.prompt"
      class="cs-task-prompt"
      :data-expanded="expanded"
      :title="task.prompt"
      @click="expanded = !expanded"
    >
      {{ task.prompt }}
    </p>

    <div v-if="paramPills.length" class="cs-task-params">
      <span v-for="pill in paramPills" :key="pill.key" class="cs-param-pill">
        <span>{{ pill.label }}</span>
        <span>{{ pill.value }}</span>
      </span>
    </div>

    <div class="cs-task-body">
      <p v-if="task.status === 'failed'" class="cs-task-error" :id="errorId">
        {{ task.error || t('creationStudio.task.failed') }}
      </p>

      <p v-else-if="task.status === 'cancelled'" class="text-xs text-gray-500 dark:text-dark-400">
        {{ t('creationStudio.task.cancelledHint') }}
      </p>

      <div v-else-if="task.status === 'expired'" class="cs-media-grid" data-count="1">
        <div class="cs-media-tile">
          <div class="cs-media-placeholder" data-tone="warning">
            <Link2Off aria-hidden="true" />
            <span>{{ t('creationStudio.task.expiredHint') }}</span>
          </div>
        </div>
      </div>

      <template v-else-if="isPending">
        <MediaGrid
          v-if="task.kind === 'image'"
          skeleton
          :skeleton-count="task.imageCount"
          :aspect-w="aspect.w"
          :aspect-h="aspect.h"
          :alt-prefix="altPrefix"
        />
        <div v-else class="cs-media-grid" data-count="1">
          <div class="cs-media-tile cs-media-skeleton" aria-hidden="true"><Film /></div>
        </div>
        <div
          v-if="task.status === 'processing'"
          class="cs-progress mt-2"
          data-indeterminate="true"
          role="progressbar"
          :aria-label="statusLabel"
        >
          <div class="cs-progress-bar" />
        </div>
        <div class="cs-progress-meta">
          <span>{{ statusLabel }}</span>
          <span>{{ t('creationStudio.task.elapsed', { time: elapsed }) }}</span>
        </div>
      </template>

      <template v-else>
        <div v-if="isCompare" class="cs-compare">
          <div class="cs-compare-body">
            <figure class="cs-compare-source">
              <div
                class="cs-media-tile"
                :style="{ '--cs-aspect-w': String(sourceImage?.width ?? 1), '--cs-aspect-h': String(sourceImage?.height ?? 1) }"
              >
                <img
                  v-if="sourceUrl"
                  :src="sourceUrl"
                  :alt="t('creationStudio.compare.sourceAlt')"
                />
                <div v-else class="cs-media-placeholder" data-tone="warning">
                  <Link2Off aria-hidden="true" />
                  <span>{{ t('creationStudio.compare.sourceExpired') }}</span>
                </div>
                <button
                  v-if="sourceUrl"
                  type="button"
                  class="cs-media-open"
                  :aria-label="t('creationStudio.compare.openSource')"
                  @click="openSource"
                />
                <span class="cs-media-tag">{{ t('creationStudio.compare.source') }}</span>
              </div>
              <figcaption class="cs-compare-label">
                {{ t('creationStudio.compare.sourceMeta', { w: sourceImage?.width ?? 0, h: sourceImage?.height ?? 0 }) }}
              </figcaption>
            </figure>
            <span class="cs-compare-arrow" aria-hidden="true"><ArrowRight /></span>
            <div class="cs-compare-results">
              <p class="cs-compare-label">
                {{ t('creationStudio.compare.results', { count: task.urls.length }) }}
              </p>
              <MediaGrid
                :urls="task.urls"
                :aspect-w="aspect.w"
                :aspect-h="aspect.h"
                :alt-prefix="altPrefix"
                @open="openResult"
              />
            </div>
          </div>
        </div>

        <VideoPlayer
          v-else-if="task.kind === 'video' && task.catalogId"
          :catalog-id="task.catalogId"
          :aspect-w="aspect.w"
          :aspect-h="aspect.h"
        />

        <MediaGrid
          v-else
          :urls="task.urls"
          :aspect-w="aspect.w"
          :aspect-h="aspect.h"
          :alt-prefix="altPrefix"
          @open="openResult"
        />
      </template>
    </div>

    <footer class="cs-task-footer">
      <span class="cs-task-meta">
        <span v-if="task.completedAt">{{ t('creationStudio.task.completedAt', { time: formatTime(task.completedAt) }) }}</span>
        <span v-else>{{ t('creationStudio.task.createdAt', { time: formatTime(task.createdAt) }) }}</span>
      </span>
      <div class="cs-task-actions">
        <button
          v-if="task.kind === 'video' && task.status === 'processing'"
          type="button"
          class="btn btn-ghost btn-sm"
          @click="emit('refresh', task)"
        >
          {{ t('common.refresh') }}
        </button>
        <a
          v-if="!task.catalogId"
          :href="taskListHref"
          class="btn btn-ghost btn-sm"
        >
          {{ t('creationStudio.action.openTaskList') }}
        </a>
        <button type="button" class="btn btn-ghost btn-sm" @click="emit('reuse', task)">
          <RotateCcw class="h-3.5 w-3.5" aria-hidden="true" />
          {{ t('creationStudio.action.reuse') }}
        </button>
      </div>
    </footer>
  </article>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowRight,
  Ban,
  CircleCheck,
  CircleX,
  Clock,
  Film,
  ImageIcon,
  Link2Off,
  LoaderCircle,
  RotateCcw,
} from '@lucide/vue'
import MediaGrid from './MediaGrid.vue'
import VideoPlayer from './VideoPlayer.vue'
import { safeURL } from '../safeURL'
import type { CreationLightboxItem, CreationTaskView } from '../types'

const props = defineProps<{ task: CreationTaskView }>()

const emit = defineEmits<{
  (e: 'reuse', task: CreationTaskView): void
  (e: 'refresh', task: CreationTaskView): void
  (e: 'preview', items: CreationLightboxItem[], index: number): void
}>()

const { t } = useI18n()

const expanded = ref(false)
const errorId = `task-error-${props.task.catalogId || props.task.taskId}`
const taskListHref = '/async-tasks'

const isPending = computed(
  () => props.task.status === 'submitting' || props.task.status === 'queued' || props.task.status === 'processing',
)

const statusLabel = computed(() => t(`creationStudio.task.${props.task.status}`))

const statusIcon = computed(() => {
  switch (props.task.status) {
    case 'completed':
      return CircleCheck
    case 'failed':
      return CircleX
    case 'cancelled':
      return Ban
    case 'queued':
      return Clock
    case 'expired':
      return Link2Off
    case 'submitting':
    case 'processing':
    default:
      return LoaderCircle
  }
})

const sourceImage = computed(() => props.task.snapshot?.images.find(image => image.role === 'source'))
const isCompare = computed(() => Boolean(sourceImage.value))
const sourceUrl = computed(() => {
  const image = sourceImage.value
  if (!image) return undefined
  // 卡片里的原图按真正到期判断（不是表单里的 15 分钟余量）。
  if (image.expires_at > 0 && image.expires_at < Math.floor(Date.now() / 1000)) return undefined
  return safeURL(image.url)
})

const aspect = computed(() => {
  const params = props.task.params ?? {}
  const ratio = params.ratio ?? params.aspect_ratio
  if (typeof ratio === 'string' && ratio.includes(':')) {
    const [w, h] = ratio.split(':').map(Number)
    if (w > 0 && h > 0) return { w, h }
  }
  const size = params.size
  if (typeof size === 'string' && size.includes('x')) {
    const [w, h] = size.split('x').map(Number)
    if (w > 0 && h > 0) return { w, h }
  }
  return props.task.kind === 'video' ? { w: 16, h: 9 } : { w: 1, h: 1 }
})

const altPrefix = computed(() => t('creationStudio.task.resultAlt', { model: props.task.model }))

const paramPills = computed(() =>
  Object.entries(props.task.params ?? {})
    .filter(([, value]) => value !== '' && value !== undefined && value !== null)
    .slice(0, 6)
    .map(([key, value]) => ({
      key,
      label: t(`creationStudio.params.${key}`),
      value: String(value),
    })),
)

const now = ref(Math.floor(Date.now() / 1000))
let timer: ReturnType<typeof setInterval> | null = null

const elapsed = computed(() => {
  const seconds = Math.max(0, now.value - props.task.createdAt)
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`
})

function syncTimer(pending: boolean) {
  if (pending && timer === null) {
    timer = setInterval(() => {
      now.value = Math.floor(Date.now() / 1000)
    }, 1000)
  } else if (!pending && timer !== null) {
    clearInterval(timer)
    timer = null
  }
}

watch(isPending, pending => syncTimer(pending), { immediate: true })
onUnmounted(() => {
  if (timer !== null) clearInterval(timer)
})

function formatTime(value: number): string {
  if (!value) return '—'
  return new Date(value * 1000).toLocaleString()
}

function buildItems(): CreationLightboxItem[] {
  const items: CreationLightboxItem[] = []
  if (isCompare.value && sourceUrl.value) {
    items.push({
      url: sourceUrl.value,
      kind: 'image',
      tag: t('creationStudio.compare.source'),
      alt: t('creationStudio.compare.sourceAlt'),
    })
  }
  // 灯箱必须与网格使用同一份过滤后的链接，否则 javascript: 项会污染索引（R1-18）。
  const safeUrls = props.task.urls
    .map(url => safeURL(url))
    .filter((url): url is string => Boolean(url))
  safeUrls.forEach((url, index) => {
    items.push({
      url,
      kind: props.task.kind === 'video' ? 'video' : 'image',
      alt: `${altPrefix.value} ${index + 1}`,
    })
  })
  return items
}

function openSource() {
  const items = buildItems()
  if (items.length) emit('preview', items, 0)
}

function openResult(index: number) {
  const items = buildItems()
  if (!items.length) return
  emit('preview', items, isCompare.value && sourceUrl.value ? index + 1 : index)
}
</script>
