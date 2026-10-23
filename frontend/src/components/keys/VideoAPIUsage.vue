<template>
  <div class="space-y-4">
    <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('videoAccounts.usageHint') }}</p>
    <div v-for="example in examples" :key="example.protocol" class="space-y-2">
      <h3 class="font-medium text-gray-900 dark:text-gray-100">{{ example.protocol }}</h3>
      <pre class="overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs text-gray-100"><code>{{ example.code }}</code></pre>
    </div>
    <RouterLink to="/async-tasks" class="text-sm text-primary-600 hover:underline dark:text-primary-400">{{ t('asyncTasks.title') }}</RouterLink>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps<{ baseUrl: string; apiKey: string }>()
const { t } = useI18n()
const examples = computed(() => {
  const base = (props.baseUrl || window.location.origin).replace(/\/$/, '').replace(/\/v1$/, '')
  return [
    { protocol: 'Seedance (Ark)', code: `POST ${base}/api/v3/contents/generations/tasks
Authorization: Bearer ${props.apiKey}
Content-Type: application/json

{"model":"YOUR_VIDEO_MODEL","content":[{"type":"text","text":"Ocean waves at sunset"}]}

GET ${base}/api/v3/contents/generations/tasks/{task_id}
Authorization: Bearer ${props.apiKey}` },
    { protocol: 'Google Gemini (Veo)', code: `POST ${base}/v1beta/models/YOUR_VIDEO_MODEL:predictLongRunning
Authorization: Bearer ${props.apiKey}
Content-Type: application/json

{"instances":[{"prompt":"Ocean waves at sunset"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}

GET ${base}/v1beta/{operation_name}
Authorization: Bearer ${props.apiKey}` },
    { protocol: 'MiniMax v2 (H3)', code: `POST ${base}/v2/video_generation
Authorization: Bearer ${props.apiKey}
Content-Type: application/json

{"model":"MiniMax-H3","content":[{"type":"text","text":"Ocean waves at sunset"}],"duration":6,"resolution":"768P","ratio":"16:9"}

GET ${base}/v2/query/video_generation/{task_id}
Authorization: Bearer ${props.apiKey}` },
    { protocol: 'MiniMax v1 (Hailuo)', code: `POST ${base}/v1/video_generation
Authorization: Bearer ${props.apiKey}
Content-Type: application/json

{"model":"YOUR_VIDEO_MODEL","prompt":"Ocean waves at sunset","duration":6,"resolution":"1080P"}

GET ${base}/v1/query/video_generation?task_id={task_id}
Authorization: Bearer ${props.apiKey}

GET ${base}/v1/files/retrieve?file_id={file_id}
Authorization: Bearer ${props.apiKey}` }
  ]
})
</script>
