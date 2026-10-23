<template>
  <AppLayout>
    <div class="space-y-5">
      <div>
        <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('apiDocs.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('apiDocs.description') }}</p>
      </div>

      <div class="card p-4">
        <label for="api-docs-key" class="input-label">{{ t('apiDocs.keySelectLabel') }}</label>
        <select id="api-docs-key" v-model="selectedKeyId" class="input max-w-xl">
          <option :value="0">{{ t('apiDocs.keyPlaceholderOption') }}</option>
          <option v-for="key in apiKeys" :key="key.id" :value="key.id">{{ key.name }} ({{ maskKey(key.key) }})</option>
        </select>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('apiDocs.keySelectHint') }}</p>
      </div>

      <div class="lg:flex lg:items-start lg:gap-6">
        <nav :aria-label="t('apiDocs.contents')" class="mb-4 flex gap-2 overflow-x-auto pb-1 lg:mb-0 lg:w-56 lg:shrink-0 lg:flex-col lg:overflow-visible">
          <span class="shrink-0 rounded-md bg-primary-50 px-3 py-1.5 text-sm font-medium text-primary-700 dark:bg-primary-950 dark:text-primary-300">{{ docs.category }}</span>
          <a
            v-for="entry in tocEntries"
            :key="entry.id"
            :href="`#api-docs-${entry.id}`"
            class="shrink-0 rounded-md px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
          >{{ entry.title }}</a>
        </nav>

        <div class="min-w-0 flex-1 space-y-8">
          <section v-for="section in docs.beforeProtocols" :id="`api-docs-${section.id}`" :key="section.id" class="card scroll-mt-20 p-5">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ section.title }}</h2>
            <DocBlocks class="mt-3" :blocks="section.blocks" :resolve-code="resolveCode" />
          </section>

          <section id="api-docs-protocols" class="card scroll-mt-20 p-5">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ docs.protocolsTitle }}</h2>
            <select id="api-docs-protocol" v-model="activeProtocol" class="input mt-4 md:hidden" :aria-label="docs.protocolsTitle">
              <option v-for="protocol in docs.protocols" :key="protocol.id" :value="protocol.id">{{ protocol.name }}</option>
            </select>
            <div class="mt-4 hidden gap-1 overflow-x-auto border-b border-gray-200 dark:border-dark-700 md:flex" role="tablist">
              <button
                v-for="protocol in docs.protocols"
                :key="protocol.id"
                type="button"
                role="tab"
                :aria-selected="activeProtocol === protocol.id"
                class="whitespace-nowrap border-b-2 px-4 py-2 text-sm font-medium"
                :class="activeProtocol === protocol.id
                  ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
                  : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
                @click="activeProtocol = protocol.id"
              >{{ protocol.name }}</button>
            </div>
            <div v-for="protocol in docs.protocols" v-show="activeProtocol === protocol.id" :key="protocol.id" :data-protocol="protocol.id" class="mt-5 space-y-6">
              <div v-for="section in protocol.sections" :key="section.id">
                <h3 class="font-medium text-gray-900 dark:text-gray-100">{{ section.title }}</h3>
                <DocBlocks class="mt-3" :blocks="section.blocks" :resolve-code="resolveCode" />
              </div>
            </div>
          </section>

          <section v-for="section in docs.afterProtocols" :id="`api-docs-${section.id}`" :key="section.id" class="card scroll-mt-20 p-5">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ section.title }}</h2>
            <DocBlocks class="mt-3" :blocks="section.blocks" :resolve-code="resolveCode" />
          </section>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import DocBlocks from '@/components/docs/DocBlocks.vue'
import { keysAPI } from '@/api/keys'
import type { ApiKey } from '@/types'
import type { VideoDocsContent } from '@/docs/video/types'
import zhDocs from '@/docs/video/zh'
import enDocs from '@/docs/video/en'

const { t, locale } = useI18n()
const docs = computed<VideoDocsContent>(() => (locale.value === 'zh' ? zhDocs : enDocs))

const SELECTED_KEY_STORAGE = 'apiDocs.selectedKeyId'

function readStoredKeyId(): number {
  try {
    const raw = sessionStorage.getItem(SELECTED_KEY_STORAGE)
    const parsed = raw === null ? 0 : Number.parseInt(raw, 10)
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
  } catch {
    return 0
  }
}

const apiKeys = ref<ApiKey[]>([])
const selectedKeyId = ref(readStoredKeyId())
const activeProtocol = ref('seedance')

watch(selectedKeyId, id => {
  try {
    if (id > 0) {
      sessionStorage.setItem(SELECTED_KEY_STORAGE, String(id))
    } else {
      sessionStorage.removeItem(SELECTED_KEY_STORAGE)
    }
  } catch {
    // sessionStorage unavailable (private mode); selection simply won't persist
  }
})

const baseUrl = window.location.origin.replace(/\/$/, '')
const exampleKey = computed(() => apiKeys.value.find(key => key.id === selectedKeyId.value)?.key || 'sk-YOUR_KEY')

const tocEntries = computed(() => [
  ...docs.value.beforeProtocols,
  { id: 'protocols', title: docs.value.protocolsTitle },
  ...docs.value.afterProtocols
])

function maskKey(key: string): string {
  if (!key) return '****'
  if (key.length <= 10) return `${key.slice(0, 3)}…`
  return `${key.slice(0, 6)}…${key.slice(-4)}`
}

function resolveCode(code: string): string {
  return code.split('{{BASE_URL}}').join(baseUrl).split('{{API_KEY}}').join(exampleKey.value)
}

onMounted(async () => {
  try {
    const data = await keysAPI.list(1, 100)
    apiKeys.value = data.items
  } catch {
    apiKeys.value = []
  }
})
</script>
