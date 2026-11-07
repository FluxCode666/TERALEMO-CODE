<template>
  <section class="cs-section">
    <div class="cs-section-head">
      <h2 class="cs-section-title">{{ t('creationStudio.feed.title') }}</h2>
      <span class="cs-section-count">{{ total }}</span>
      <div class="cs-segmented ml-auto" role="tablist" :aria-label="t('creationStudio.feed.filterAll')">
        <button
          v-for="option in filters"
          :key="option.value"
          type="button"
          class="cs-segmented-item"
          role="tab"
          :aria-selected="filter === option.value"
          @click="emit('update:filter', option.value)"
        >
          {{ t(option.labelKey) }}
        </button>
      </div>
    </div>

    <div v-if="loading && !items.length" class="cs-feed" aria-busy="true">
      <span class="sr-only">{{ t('common.loading') }}</span>
      <div v-for="index in 3" :key="index" class="card cs-task p-4">
        <div class="skeleton h-4 w-1/3" />
        <div class="skeleton mt-3 h-3 w-2/3" />
        <div class="skeleton mt-3 h-32 w-full" />
      </div>
    </div>

    <div v-else-if="!items.length && !error" class="cs-empty">
      <img class="cs-illu" :src="emptyStudio" alt="" aria-hidden="true" />
      <h3 class="cs-empty-title">{{ t('creationStudio.empty.title') }}</h3>
      <p class="cs-empty-desc">{{ t('creationStudio.empty.description') }}</p>
      <div class="cs-empty-actions">
        <span class="text-xs font-medium text-gray-500 dark:text-dark-400">
          {{ t('creationStudio.empty.suggestions') }}
        </span>
        <button
          v-for="index in 3"
          :key="index"
          type="button"
          class="cs-suggestion"
          @click="emit('apply-prompt', t(`creationStudio.empty.suggestion${index}`))"
        >
          <Sparkles aria-hidden="true" />
          <span>{{ t(`creationStudio.empty.suggestion${index}`) }}</span>
        </button>
      </div>
    </div>

    <div v-else class="cs-feed">
      <TaskCard
        v-for="task in items"
        :key="task.catalogId || task.taskId"
        :task="task"
        @reuse="emit('reuse', $event)"
        @refresh="emit('refresh', $event)"
        @preview="(lightboxItems, index) => emit('preview', lightboxItems, index)"
      />

      <p v-if="error" class="cs-feed-end">
        {{ t('creationStudio.feed.loadFailed') }}
        <button type="button" class="cs-field-action" @click="emit('retry')">
          {{ t('creationStudio.action.retry') }}
        </button>
      </p>
      <div v-else-if="hasMore" class="cs-load-more">
        <button type="button" class="btn btn-secondary" :disabled="loadingMore" @click="emit('load-more')">
          <LoaderCircle v-if="loadingMore" class="h-4 w-4 animate-spin" aria-hidden="true" />
          {{ t('creationStudio.feed.loadMore') }}
        </button>
      </div>
      <p v-else class="cs-feed-end">{{ t('creationStudio.feed.noMore') }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { LoaderCircle, Sparkles } from '@lucide/vue'
import TaskCard from './TaskCard.vue'
import emptyStudio from '@/assets/creation-studio/empty-studio.svg'
import type { CreationFeedFilter } from '../useCreationFeed'
import type { CreationLightboxItem, CreationTaskView } from '../types'

withDefaults(
  defineProps<{
    items: CreationTaskView[]
    loading?: boolean
    loadingMore?: boolean
    error?: boolean
    filter: CreationFeedFilter
    total?: number
    hasMore?: boolean
  }>(),
  { loading: false, loadingMore: false, error: false, total: 0, hasMore: false },
)

const emit = defineEmits<{
  (e: 'update:filter', value: CreationFeedFilter): void
  (e: 'load-more'): void
  (e: 'retry'): void
  (e: 'reuse', task: CreationTaskView): void
  (e: 'refresh', task: CreationTaskView): void
  (e: 'preview', items: CreationLightboxItem[], index: number): void
  (e: 'apply-prompt', prompt: string): void
}>()

const { t } = useI18n()

const filters: { value: CreationFeedFilter; labelKey: string }[] = [
  { value: 'all', labelKey: 'creationStudio.feed.filterAll' },
  { value: 'image', labelKey: 'creationStudio.feed.filterImage' },
  { value: 'video', labelKey: 'creationStudio.feed.filterVideo' },
]
</script>
