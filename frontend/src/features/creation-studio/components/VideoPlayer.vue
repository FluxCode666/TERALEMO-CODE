<template>
  <div class="cs-video" :data-orientation="orientation" :style="style">
    <video v-if="src" :src="src" controls preload="metadata" playsinline />
    <div v-else class="cs-media-placeholder">
      <LoaderCircle v-if="loading" class="animate-spin" aria-hidden="true" />
      <TriangleAlert v-else aria-hidden="true" />
      <span>{{ loading ? t('common.loading') : t('creationStudio.task.loadFailed') }}</span>
      <button v-if="!loading" type="button" class="cs-field-action" @click="load">
        {{ t('creationStudio.action.retry') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 视频一律走 `asyncTasksAPI.download` 取字节后转 blob URL 播放，
 * 不把上游链接写进 `<video src>`（契约 §7.4）。
 */
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { LoaderCircle, TriangleAlert } from '@lucide/vue'
import { asyncTasksAPI } from '@/api/asyncTasks'

const props = withDefaults(
  defineProps<{
    catalogId: string
    aspectW?: number
    aspectH?: number
  }>(),
  { aspectW: 16, aspectH: 9 },
)

const { t } = useI18n()

const src = ref('')
const loading = ref(false)

const orientation = computed(() => (props.aspectW < props.aspectH ? 'portrait' : undefined))
const style = computed(() => ({ '--cs-aspect': `${props.aspectW} / ${props.aspectH}` }))

function release() {
  if (src.value) {
    URL.revokeObjectURL(src.value)
    src.value = ''
  }
}

async function load() {
  if (!props.catalogId) return
  release()
  loading.value = true
  try {
    const blob = await asyncTasksAPI.download(props.catalogId)
    src.value = URL.createObjectURL(blob)
  } catch {
    src.value = ''
  } finally {
    loading.value = false
  }
}

watch(
  () => props.catalogId,
  () => {
    void load()
  },
  { immediate: true },
)

onUnmounted(release)
</script>
