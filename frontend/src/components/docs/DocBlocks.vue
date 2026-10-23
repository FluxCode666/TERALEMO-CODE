<template>
  <div class="space-y-4">
    <template v-for="(block, index) in blocks" :key="index">
      <p v-if="block.type === 'p'" class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ block.text }}</p>
      <ul v-else-if="block.type === 'list'" class="list-disc space-y-1.5 pl-5 text-sm leading-6 text-gray-600 dark:text-gray-300">
        <li v-for="(item, itemIndex) in block.items" :key="itemIndex">{{ item }}</li>
      </ul>
      <div v-else-if="block.type === 'table'" class="overflow-x-auto">
        <table class="w-full min-w-[640px] text-left text-sm">
          <thead class="border-b border-gray-200 bg-gray-50 text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400">
            <tr><th v-for="header in block.headers" :key="header" class="whitespace-nowrap px-4 py-2">{{ header }}</th></tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="(row, rowIndex) in block.rows" :key="rowIndex">
              <td v-for="(cell, cellIndex) in row" :key="cellIndex" class="px-4 py-2 align-top text-gray-600 dark:text-gray-300" :class="cellIndex === 0 ? 'whitespace-nowrap font-mono text-xs' : ''">{{ cell }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else-if="block.type === 'code'" class="space-y-1">
        <div class="flex items-center justify-between gap-2">
          <h3 class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ block.title }}</h3>
          <button type="button" class="btn btn-ghost btn-sm shrink-0" @click="copyCode(index, block.code)">{{ copiedBlock === index ? t('apiDocs.copied') : t('apiDocs.copy') }}</button>
        </div>
        <pre class="overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs leading-5 text-gray-100"><code>{{ resolveCode(block.code) }}</code></pre>
      </div>
      <RouterLink v-else-if="block.type === 'link'" :to="block.to" class="inline-block text-sm text-primary-600 hover:underline dark:text-primary-400">{{ block.text }}</RouterLink>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import type { DocBlock } from '@/docs/video/types'

const props = defineProps<{
  blocks: DocBlock[]
  resolveCode: (code: string) => string
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const copiedBlock = ref<number | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | null = null

async function copyCode(index: number, code: string) {
  const success = await copyToClipboard(props.resolveCode(code), t('apiDocs.copied'))
  if (!success) return
  copiedBlock.value = index
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copiedBlock.value = null
    copiedTimer = null
  }, 2000)
}

onBeforeUnmount(() => {
  if (copiedTimer) clearTimeout(copiedTimer)
})
</script>
