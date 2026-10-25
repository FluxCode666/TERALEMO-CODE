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
        <nav :aria-label="t('apiDocs.contents')" data-test="api-docs-toc" class="mb-4 flex gap-2 overflow-x-auto pb-1 lg:sticky lg:top-20 lg:mb-0 lg:max-h-[calc(100vh-6rem)] lg:w-56 lg:shrink-0 lg:flex-col lg:overflow-y-auto">
          <span class="shrink-0 rounded-md bg-primary-50 px-3 py-1.5 text-sm font-medium text-primary-700 dark:bg-primary-950 dark:text-primary-300">{{ docs.category }}</span>
          <template v-for="entry in docs.beforeProtocols" :key="entry.id">
            <a
              :href="`#api-docs-${entry.id}`"
              :data-toc-id="entry.id"
              @click.prevent="jumpTo(entry.id)"
              class="shrink-0 rounded-md px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              :class="tocItemClass(entry.id)"
            >{{ entry.title }}</a>
          </template>

          <div class="shrink-0 lg:w-full">
            <a
              href="#api-docs-protocols"
              data-test="api-docs-toc-protocols"
              data-toc-id="protocols"
              @click.prevent="jumpTo('protocols')"
              class="block rounded-md px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
              :class="tocItemClass('protocols')"
            >{{ docs.protocolsTitle }}</a>
            <div class="pl-2">
              <div v-for="protocol in docs.protocols" :key="protocol.id" class="mt-1">
                <div class="flex items-center gap-1">
                  <a
                    :href="`#api-docs-${protocol.id}`"
                    data-test="api-docs-toc-protocol"
                    :data-protocol-id="protocol.id"
                    :data-toc-id="protocol.id"
                    @click.prevent="jumpToProtocol(protocol.id)"
                    class="min-w-0 flex-1 rounded-md px-3 py-1.5 text-sm font-medium text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                    :class="tocItemClass(protocol.id)"
                  >{{ protocol.name }}</a>
                  <button
                    type="button"
                    data-test="api-docs-toc-toggle"
                    :aria-expanded="isProtocolExpanded(protocol.id)"
                    :aria-label="`${isProtocolExpanded(protocol.id) ? 'Collapse' : 'Expand'} ${protocol.name}`"
                    class="rounded px-1.5 py-0.5 text-xs text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-700"
                    @click="toggleProtocol(protocol.id)"
                  >{{ isProtocolExpanded(protocol.id) ? '−' : '+' }}</button>
                </div>
                <div v-show="isProtocolExpanded(protocol.id)" class="hidden ml-2 border-l border-gray-200 pl-2 dark:border-dark-700 lg:block">
                  <template v-for="section in protocol.sections" :key="section.id">
                    <a
                      :href="`#api-docs-${protocol.id}-${section.id}`"
                      data-test="api-docs-toc-interface"
                      :data-protocol-id="protocol.id"
                      :data-interface-id="section.id"
                      :data-toc-id="protocolSectionToken(protocol.id, section.id)"
                      @click.prevent="jumpToProtocolSection(protocol.id, section.id)"
                      class="block rounded-md px-2 py-1 text-xs text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-700"
                      :class="tocItemClass(protocolSectionToken(protocol.id, section.id))"
                    >{{ section.title }}</a>
                    <div v-if="sectionExamples(section).length" class="ml-3 border-l border-gray-200 pl-2 dark:border-dark-700">
                      <a
                        v-for="example in sectionExamples(section)"
                        :key="example.id"
                        :href="`#api-docs-${protocol.id}-${example.id}`"
                        data-test="api-docs-toc-example"
                        :data-protocol-id="protocol.id"
                        :data-example-id="example.id"
                        :data-toc-id="exampleToken(protocol.id, example.id)"
                        @click.prevent="jumpToExample(protocol.id, example.id)"
                        class="block rounded-md px-2 py-1 text-xs text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-700"
                        :class="tocItemClass(exampleToken(protocol.id, example.id))"
                      >{{ example.title }}</a>
                    </div>
                  </template>
                </div>
              </div>
            </div>
          </div>

          <template v-for="entry in docs.afterProtocols" :key="entry.id">
            <a
              :href="`#api-docs-${entry.id}`"
              :data-toc-id="entry.id"
              @click.prevent="jumpTo(entry.id)"
              class="shrink-0 rounded-md px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              :class="tocItemClass(entry.id)"
            >{{ entry.title }}</a>
          </template>
        </nav>

        <div class="min-w-0 flex-1 space-y-8">
          <section v-for="section in docs.beforeProtocols" :id="`api-docs-${section.id}`" :data-scrollspy-id="section.id" :key="section.id" class="card scroll-mt-20 p-5">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ section.title }}</h2>
            <DocBlocks class="mt-3" :blocks="section.blocks" :resolve-code="resolveCode" />
          </section>

          <section id="api-docs-protocols" data-scrollspy-id="protocols" class="card scroll-mt-20 p-5">
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
            <div v-for="protocol in docs.protocols" v-show="activeProtocol === protocol.id" :id="`api-docs-${protocol.id}`" :key="protocol.id" :data-protocol="protocol.id" :data-scrollspy-id="protocol.id" class="mt-5 space-y-6 scroll-mt-20">
              <select
                v-if="protocolExamples(protocol).length"
                :id="`api-docs-example-${protocol.id}`"
                class="input mb-2 md:hidden"
                :aria-label="`${protocol.name} examples`"
                :value="selectedExample(protocol.id)"
                @change="onExampleSelect(protocol.id, $event)"
              >
                <option value="">{{ docs.protocolsTitle }}</option>
                <option v-for="example in protocolExamples(protocol)" :key="example.id" :value="example.id">{{ example.title }}</option>
              </select>
              <div v-for="section in protocol.sections" :id="`api-docs-${protocol.id}-${section.id}`" :data-scrollspy-id="protocolSectionToken(protocol.id, section.id)" :key="section.id" class="scroll-mt-20">
                <h3 class="font-medium text-gray-900 dark:text-gray-100">{{ section.title }}</h3>
                <DocBlocks class="mt-3" :blocks="section.blocks" :resolve-code="resolveCode" :example-prefix="protocol.id" />
              </div>
            </div>
          </section>

          <section v-for="section in docs.afterProtocols" :id="`api-docs-${section.id}`" :data-scrollspy-id="section.id" :key="section.id" class="card scroll-mt-20 p-5">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ section.title }}</h2>
            <DocBlocks class="mt-3" :blocks="section.blocks" :resolve-code="resolveCode" />
          </section>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import DocBlocks from '@/components/docs/DocBlocks.vue'
import { keysAPI } from '@/api/keys'
import type { ApiKey } from '@/types'
import type { VideoDocsContent } from '@/docs/video/types'
import zhDocs from '@/docs/video/zh'
import enDocs from '@/docs/video/en'

const { t, locale } = useI18n()
const router = useRouter()
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
const activeTocId = ref('seedance')
const expandedProtocols = ref<Set<string>>(new Set(['seedance']))
const selectedExamples = ref<Record<string, string>>({})
let scrollSpy: IntersectionObserver | null = null
// While a catalog jump scrolls smoothly, sections it passes must not steal
// the highlight; unlock when the scroll ends (timeout for browsers without scrollend).
let spyLockedUntil = 0
const unlockScrollSpy = () => { spyLockedUntil = 0 }

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

type Protocol = VideoDocsContent['protocols'][number]
type ExampleSummary = { id: string, title: string }

function sectionExamples(section: Protocol['sections'][number]): ExampleSummary[] {
  return section.blocks.flatMap(block => {
    if (block.type !== 'example') return []
    return [{ id: block.id, title: block.title }]
  })
}

function protocolExamples(protocol: Protocol): ExampleSummary[] {
  return protocol.sections.flatMap(sectionExamples)
}

function protocolSectionToken(protocolId: string, sectionId: string): string {
  return `${protocolId}-${sectionId}`
}

function exampleToken(protocolId: string, exampleId: string): string {
  return `${protocolId}-${exampleId}`
}

function isProtocolExpanded(protocolId: string): boolean {
  return expandedProtocols.value.has(protocolId)
}

function selectedExample(protocolId: string): string {
  return selectedExamples.value[protocolId] || ''
}

function tocItemClass(token: string): string {
  // Only a protocol entry also lights up for its children; example ids can be
  // prefixes of each other (draft / draft-task), so they match exactly.
  const isProtocol = docs.value.protocols.some(protocol => protocol.id === token)
  const isActive = activeTocId.value === token || (isProtocol && activeTocId.value.startsWith(`${token}-`))
  return isActive
    ? 'bg-primary-100 text-primary-700 dark:bg-primary-950 dark:text-primary-300'
    : ''
}

function toggleProtocol(protocolId: string): void {
  const next = new Set(expandedProtocols.value)
  if (next.has(protocolId)) next.delete(protocolId)
  else next.add(protocolId)
  expandedProtocols.value = next
}

function maskKey(key: string): string {
  if (!key) return '****'
  if (key.length <= 10) return `${key.slice(0, 3)}…`
  return `${key.slice(0, 6)}…${key.slice(-4)}`
}

function resolveCode(code: string): string {
  return code.split('{{BASE_URL}}').join(baseUrl).split('{{API_KEY}}').join(exampleKey.value)
}

// The router scrolls to the top on every navigation, including hash-only
// changes, which races the native anchor jump. Scroll here and update the
// hash without a router navigation.
function jumpTo(id: string, behavior: 'auto' | 'smooth' = 'smooth'): void {
  const target = document.getElementById(`api-docs-${id}`)
  if (!target) return
  spyLockedUntil = performance.now() + 1500
  target.scrollIntoView({ behavior, block: 'start' })
  activeTocId.value = id
  history.replaceState(history.state, '', `#api-docs-${id}`)
}

async function jumpToProtocol(protocolId: string, behavior: 'auto' | 'smooth' = 'smooth'): Promise<void> {
  if (!docs.value.protocols.some(protocol => protocol.id === protocolId)) return
  activeProtocol.value = protocolId
  expandedProtocols.value = new Set([protocolId])
  await nextTick()
  jumpTo(protocolId, behavior)
}

async function jumpToProtocolSection(protocolId: string, sectionId: string, behavior: 'auto' | 'smooth' = 'smooth'): Promise<void> {
  if (!docs.value.protocols.some(protocol => protocol.id === protocolId && protocol.sections.some(section => section.id === sectionId))) return
  activeProtocol.value = protocolId
  expandedProtocols.value = new Set([protocolId])
  await nextTick()
  jumpTo(protocolSectionToken(protocolId, sectionId), behavior)
}

async function jumpToExample(protocolId: string, exampleId: string, behavior: 'auto' | 'smooth' = 'smooth'): Promise<void> {
  const protocol = docs.value.protocols.find(item => item.id === protocolId)
  if (!protocol || !protocolExamples(protocol).some(example => example.id === exampleId)) return
  activeProtocol.value = protocolId
  expandedProtocols.value = new Set([protocolId])
  selectedExamples.value = { ...selectedExamples.value, [protocolId]: exampleId }
  await nextTick()
  jumpTo(exampleToken(protocolId, exampleId), behavior)
}

function onExampleSelect(protocolId: string, event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  if (value) void jumpToExample(protocolId, value)
}

async function navigateToToken(token: string, behavior: 'auto' | 'smooth' = 'auto'): Promise<void> {
  const protocol = docs.value.protocols.find(item => token === item.id || token.startsWith(`${item.id}-`))
  if (!protocol) {
    jumpTo(token, behavior)
    return
  }
  if (token === protocol.id) {
    await jumpToProtocol(protocol.id, behavior)
    return
  }
  const section = protocol.sections.find(item => protocolSectionToken(protocol.id, item.id) === token || item.id === token)
  if (section) {
    await jumpToProtocolSection(protocol.id, section.id, behavior)
    return
  }
  const example = protocolExamples(protocol).find(item => exampleToken(protocol.id, item.id) === token || item.id === token)
  if (example) await jumpToExample(protocol.id, example.id, behavior)
  else jumpTo(token, behavior)
}

function setupScrollSpy(): void {
  scrollSpy?.disconnect()
  scrollSpy = null
  if (typeof IntersectionObserver === 'undefined') return
  document.querySelectorAll<HTMLElement>('[data-doc-example-id]').forEach(target => {
    target.dataset.scrollspyId = target.dataset.docExampleId || ''
  })
  const targets = Array.from(document.querySelectorAll<HTMLElement>('[data-scrollspy-id]'))
  scrollSpy = new IntersectionObserver(entries => {
    if (performance.now() < spyLockedUntil) return
    const visible = entries
      .filter(entry => entry.isIntersecting !== false)
      .sort((left, right) => {
        const leftElement = left.target as HTMLElement
        const rightElement = right.target as HTMLElement
        const leftDepth = leftElement.matches('[data-doc-example-id]') ? 2 : leftElement.dataset.scrollspyId?.includes('-') ? 1 : 0
        const rightDepth = rightElement.matches('[data-doc-example-id]') ? 2 : rightElement.dataset.scrollspyId?.includes('-') ? 1 : 0
        return rightDepth - leftDepth || left.boundingClientRect.top - right.boundingClientRect.top
      })
    const current = visible[0]?.target as HTMLElement | undefined
    const token = current?.dataset.scrollspyId
    if (!token) return
    activeTocId.value = token
    const protocol = docs.value.protocols.find(item => token === item.id || token.startsWith(`${item.id}-`))
    if (protocol) {
      activeProtocol.value = protocol.id
      expandedProtocols.value = new Set([protocol.id])
    }
  }, { rootMargin: '-80px 0px -60% 0px', threshold: [0, 0.1] })
  targets.forEach(target => scrollSpy?.observe(target))
}

watch(activeProtocol, protocolId => {
  if (docs.value.protocols.some(protocol => protocol.id === protocolId)) {
    expandedProtocols.value = new Set([protocolId])
  }
})

watch(locale, async () => {
  await nextTick()
  setupScrollSpy()
})

onMounted(async () => {
  const initial = window.location.hash.startsWith('#api-docs-') ? window.location.hash.slice('#api-docs-'.length) : ''
  await nextTick()
  setupScrollSpy()
  window.addEventListener('scrollend', unlockScrollSpy)
  if (initial) {
    // Wait for the router's own scroll-to-top after the initial navigation.
    await router?.isReady()
    await nextTick()
    window.setTimeout(() => {
      const locate = () => { void navigateToToken(initial, 'auto') }
      if (typeof window.requestAnimationFrame === 'function') window.requestAnimationFrame(locate)
      else locate()
    }, 0)
  }
  try {
    const data = await keysAPI.list(1, 100)
    apiKeys.value = data.items
  } catch {
    apiKeys.value = []
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('scrollend', unlockScrollSpy)
  scrollSpy?.disconnect()
  scrollSpy = null
})
</script>
