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
          <button
            type="button"
            class="btn btn-ghost btn-sm shrink-0"
            :data-doc-copy="`code-${index}`"
            :aria-label="t('apiDocs.copy')"
            @click="copyCode(`code-${index}`, block.code)"
          >{{ copiedBlock === `code-${index}` ? t('apiDocs.copied') : t('apiDocs.copy') }}</button>
        </div>
        <pre class="overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs leading-5 text-gray-100"><code>{{ formatCode(block.code) }}</code></pre>
      </div>
      <article
        v-else-if="block.type === 'example'"
        class="scroll-mt-20 space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-700"
        :id="props.examplePrefix ? `api-docs-${props.examplePrefix}-${block.id}` : undefined"
        :data-doc-example="block.id"
        :data-doc-example-id="props.examplePrefix ? `${props.examplePrefix}-${block.id}` : block.id"
        :data-scrollspy-id="props.examplePrefix ? `${props.examplePrefix}-${block.id}` : block.id"
      >
        <div class="space-y-1">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ block.title }}</h3>
          <p v-if="block.description" class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ block.description }}</p>
        </div>

        <div class="space-y-1">
          <div class="flex items-center justify-between gap-2">
            <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ t('apiDocs.request') }}</h4>
            <button
              type="button"
              class="btn btn-ghost btn-sm shrink-0"
              :data-doc-copy="`example-${index}-request`"
              :aria-label="`${t('apiDocs.copy')} ${t('apiDocs.request')}`"
              @click="copyCode(`example-${index}-request`, block.request)"
            >{{ copiedBlock === `example-${index}-request` ? t('apiDocs.copied') : t('apiDocs.copy') }}</button>
          </div>
          <pre class="overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs leading-5 text-gray-100"><code>{{ formatCode(block.request) }}</code></pre>
        </div>

        <div v-for="(response, responseIndex) in block.responses" :key="responseIndex" class="space-y-1" :data-doc-response="responseIndex">
          <div class="flex items-center justify-between gap-2">
            <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ response.title }} · {{ t('apiDocs.response') }} · {{ responseLabel(response.status) }}</h4>
            <button
              type="button"
              class="btn btn-ghost btn-sm shrink-0"
              :data-doc-copy="`example-${index}-response-${responseIndex}`"
              :aria-label="`${t('apiDocs.copy')} ${t('apiDocs.response')}`"
              @click="copyCode(`example-${index}-response-${responseIndex}`, responseText(response))"
            >{{ copiedBlock === `example-${index}-response-${responseIndex}` ? t('apiDocs.copied') : t('apiDocs.copy') }}</button>
          </div>
          <p v-if="response.description" class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ response.description }}</p>
          <pre v-if="response.body || response.headers" class="overflow-x-auto rounded-lg bg-gray-900 p-4 text-xs leading-5 text-gray-100"><code>{{ formatCode(responseText(response)) }}</code></pre>
        </div>
      </article>
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
  /** Prefix used to give example blocks protocol-scoped scroll targets. */
  examplePrefix?: string
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const copiedBlock = ref<string | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | null = null

async function copyCode(key: string, code: string) {
  const success = await copyToClipboard(formatCode(code), t('apiDocs.copied'))
  if (!success) return
  copiedBlock.value = key
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copiedBlock.value = null
    copiedTimer = null
  }, 2000)
}

function formatCode(code: string): string {
  const resolved = props.resolveCode(code)
  try {
    return JSON.stringify(JSON.parse(resolved), null, 2)
  } catch {
    return resolved
  }
}

function responseLabel(status: number): string {
  return `HTTP ${status}`
}

function responseText(response: { body?: string; headers?: string; description?: string }): string {
  const parts = [response.headers, response.body].filter((part): part is string => Boolean(part))
  if (parts.length === 0 && response.description) return response.description
  return parts.join('\n\n')
}

onBeforeUnmount(() => {
  if (copiedTimer) clearTimeout(copiedTimer)
})
</script>
