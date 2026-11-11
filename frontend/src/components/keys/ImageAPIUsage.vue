<template>
  <div class="space-y-4" data-testid="image-api-usage">
    <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('imageAccounts.usage.intro') }}</p>
    <div v-for="example in examples" :key="example.key" class="space-y-2">
      <h3 class="font-medium text-gray-900 dark:text-gray-100">{{ example.title }}</h3>
      <pre class="overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs text-gray-100"><code>{{ example.code }}</code></pre>
    </div>
    <div class="space-y-1">
      <h3 class="font-medium text-gray-900 dark:text-gray-100">{{ t('imageAccounts.usage.notesTitle') }}</h3>
      <ul class="list-disc space-y-1 pl-5 text-sm text-gray-600 dark:text-gray-400">
        <li v-for="note in notes" :key="note">{{ t(`imageAccounts.usage.${note}`) }}</li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps<{ baseUrl: string; apiKey: string }>()
const { t } = useI18n()
const notes = ['noteN', 'noteSize', 'noteResponseFormat', 'noteUnsupported']
const examples = computed(() => {
  const base = (props.baseUrl || window.location.origin).replace(/\/$/, '').replace(/\/v1$/, '')
  const key = props.apiKey
  return [
    { key: 'generations', title: t('imageAccounts.usage.generations'), code: `curl ${base}/v1/images/generations \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"YOUR_IMAGE_MODEL","prompt":"A cat astronaut, watercolor","n":1,"size":"1024x1024","response_format":"b64_json"}'` },
    { key: 'editsMultipart', title: t('imageAccounts.usage.editsMultipart'), code: `curl ${base}/v1/images/edits \\
  -H "Authorization: Bearer ${key}" \\
  -F model=YOUR_IMAGE_MODEL \\
  -F prompt="Add a red hat" \\
  -F image=@input.png` },
    { key: 'editsJson', title: t('imageAccounts.usage.editsJson'), code: `curl ${base}/v1/images/edits \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"YOUR_IMAGE_MODEL","prompt":"Add a red hat","images":[{"image_url":"https://example.com/input.png"}]}'` },
    { key: 'async', title: t('imageAccounts.usage.async'), code: `curl ${base}/v1/images/generations/async \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"YOUR_IMAGE_MODEL","prompt":"A cat astronaut, watercolor"}'

curl ${base}/v1/images/tasks/{task_id} \\
  -H "Authorization: Bearer ${key}"` }
  ]
})
</script>
