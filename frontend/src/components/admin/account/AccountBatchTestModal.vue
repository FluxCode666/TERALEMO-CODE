<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.batchTest.title')"
    width="extra-wide"
    @close="handleClose"
  >
    <div class="space-y-5">
      <div class="rounded-lg bg-primary-50 px-4 py-3 text-sm font-medium text-primary-900 dark:bg-primary-900/20 dark:text-primary-100">
        {{ t('admin.accounts.batchTest.selectedAccounts', { count: accountIds.length }) }}
      </div>

      <p v-if="errorMessage" role="alert" class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        {{ errorMessage }}
      </p>
      <p v-if="crossPlatform" role="alert" class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        {{ t('admin.accounts.batchTest.crossPlatform') }}
      </p>
      <p v-if="modelIntersectionLoading" class="rounded-lg border border-gray-200 px-4 py-3 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
        {{ t('admin.accounts.batchTest.loadingModels') }}
      </p>
      <p v-else-if="modelIntersectionError" class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('admin.accounts.batchTest.modelsLoadFailed') }}
      </p>
      <p v-else-if="modelOptions.length === 0 && accountIds.length > 0" class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('admin.accounts.batchTest.noCommonModels') }}
      </p>

      <section v-if="!crossPlatform" aria-labelledby="batch-test-questions-label">
        <div class="mb-2 flex items-center justify-between gap-3">
          <span id="batch-test-questions-label" class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.batchTest.questionsLabel') }}
          </span>
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
            <input
              data-test="select-all"
              type="checkbox"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="allQuestionsSelected"
              :disabled="running || loadingQuestions || questions.length === 0"
              @change="toggleAllQuestions"
            >
            {{ t('admin.accounts.batchTest.selectAll') }}
          </label>
        </div>

        <p v-if="loadingQuestions" class="rounded-lg border border-gray-200 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
          {{ t('admin.accounts.batchTest.loadingQuestions') }}
        </p>
        <p v-else-if="questions.length === 0" class="rounded-lg border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-500 dark:text-gray-400">
          {{ t('admin.accounts.batchTest.emptyQuestions') }}
        </p>
        <div v-else class="grid max-h-56 gap-2 overflow-y-auto rounded-lg border border-gray-200 p-3 dark:border-dark-600 sm:grid-cols-2">
          <label
            v-for="question in questions"
            :key="question.id"
            :for="`batch-test-question-${question.id}`"
            class="flex cursor-pointer items-start gap-3 rounded-md px-2 py-2 hover:bg-gray-50 dark:hover:bg-dark-700"
          >
            <input
              :id="`batch-test-question-${question.id}`"
              v-model="selectedQuestionIds"
              :data-test="`question-${question.id}`"
              type="checkbox"
              :value="question.id"
              :disabled="running"
              class="mt-1 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            >
            <span class="min-w-0">
              <span class="block break-words text-sm font-medium text-gray-900 dark:text-gray-100">{{ question.name }}</span>
              <span
                :class="[
                  'mt-0.5 block text-xs',
                  question.answer_patterns.length > 0
                    ? 'text-gray-500 dark:text-gray-400'
                    : 'text-amber-600 dark:text-amber-400'
                ]"
              >
                {{ question.answer_patterns.length > 0
                  ? t('admin.accounts.batchTest.patternsConfigured')
                  : t('admin.accounts.batchTest.noPatterns') }}
              </span>
            </span>
          </label>
        </div>
      </section>

      <div class="grid gap-4 md:grid-cols-2">
        <fieldset :disabled="running" class="space-y-2">
          <legend class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.batchTest.modelLabel') }}
          </legend>
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              v-model="modelMode"
              data-test="model-default"
              type="radio"
              value="default"
              class="border-gray-300 text-primary-600 focus:ring-primary-500"
            >
            {{ t('admin.accounts.batchTest.modelDefault') }}
          </label>
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              v-model="modelMode"
              data-test="model-custom"
              type="radio"
              value="custom"
              class="border-gray-300 text-primary-600 focus:ring-primary-500"
            >
            {{ t('admin.accounts.batchTest.modelCustom') }}
          </label>
          <div v-if="modelMode === 'custom'" class="pt-1">
            <label for="batch-test-model-id" class="sr-only">
              {{ t('admin.accounts.batchTest.modelIdLabel') }}
            </label>
            <Select
              v-model="customModelId"
              data-test="model-select"
              :options="modelOptions"
              :disabled="modelIntersectionLoading || modelOptions.length === 0"
              :placeholder="t('admin.accounts.batchTest.modelIdPlaceholder')"
              :searchable="modelOptions.length > 5"
              :loading="modelIntersectionLoading"
            />
            <!-- Keep a native binding for keyboard/autofill integrations; the visible control is Select. -->
            <input v-model="customModelId" data-test="model-id" type="text" class="sr-only" tabindex="-1" aria-hidden="true">
          </div>
        </fieldset>

        <div>
          <label for="batch-test-concurrency" class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.batchTest.concurrencyLabel') }}
          </label>
          <select
            id="batch-test-concurrency"
            v-model.number="concurrency"
            data-test="concurrency"
            :disabled="running"
            class="input"
          >
            <option v-for="value in [1, 2, 3, 4, 5]" :key="value" :value="value">{{ value }}</option>
          </select>
        </div>
      </div>

      <section v-if="total > 0 || running || stoppedByUser" class="space-y-3" aria-live="polite">
        <div class="flex items-center justify-between gap-3">
          <p data-test="progress" class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.batchTest.progress', { completed: completedCount, total }) }}
          </p>
          <button
            v-if="sortedResults.length > 0"
            type="button"
            data-test="copy-results"
            class="btn btn-secondary btn-sm"
            @click="copyResults"
          >
            {{ t('admin.accounts.batchTest.copyResults') }}
          </button>
        </div>
        <div class="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
          <div
            class="h-full rounded-full bg-primary-500 transition-all"
            :style="{ width: `${progressPercent}%` }"
          />
        </div>
        <dl data-test="summary" class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
          <div class="rounded-lg bg-gray-50 p-2 dark:bg-dark-700">
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.summary.total') }}</dt>
            <dd class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ displaySummary.total }}</dd>
          </div>
          <div class="rounded-lg bg-green-50 p-2 dark:bg-green-900/20">
            <dt class="text-xs text-green-600 dark:text-green-400">{{ t('admin.accounts.batchTest.summary.passed') }}</dt>
            <dd class="text-lg font-semibold text-green-700 dark:text-green-300">{{ displaySummary.passed }}</dd>
          </div>
          <div class="rounded-lg bg-red-50 p-2 dark:bg-red-900/20">
            <dt class="text-xs text-red-600 dark:text-red-400">{{ t('admin.accounts.batchTest.summary.failed') }}</dt>
            <dd class="text-lg font-semibold text-red-700 dark:text-red-300">{{ displaySummary.failed }}</dd>
          </div>
          <div class="rounded-lg bg-yellow-50 p-2 dark:bg-yellow-900/20">
            <dt class="text-xs text-yellow-600 dark:text-yellow-400">{{ t('admin.accounts.batchTest.summary.unjudged') }}</dt>
            <dd class="text-lg font-semibold text-yellow-700 dark:text-yellow-300">{{ displaySummary.unjudged }}</dd>
          </div>
          <div class="rounded-lg bg-orange-50 p-2 dark:bg-orange-900/20">
            <dt class="text-xs text-orange-600 dark:text-orange-400">{{ t('admin.accounts.batchTest.summary.error') }}</dt>
            <dd class="text-lg font-semibold text-orange-700 dark:text-orange-300">{{ displaySummary.error }}</dd>
          </div>
          <div class="rounded-lg bg-gray-50 p-2 dark:bg-dark-700">
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.summary.cancelled') }}</dt>
            <dd class="text-sm font-semibold text-gray-900 dark:text-gray-100">
              {{ displaySummary.cancelled
                ? t('admin.accounts.batchTest.summary.cancelledYes')
                : t('admin.accounts.batchTest.summary.cancelledNo') }}
            </dd>
          </div>
        </dl>
      </section>

      <div v-if="sortedResults.length > 0" class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-700">
            <tr>
              <th scope="col" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.columns.account') }}</th>
              <th scope="col" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.columns.platform') }}</th>
              <th scope="col" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.columns.question') }}</th>
              <th scope="col" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.columns.model') }}</th>
              <th scope="col" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.columns.status') }}</th>
              <th scope="col" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.columns.latency') }}</th>
              <th scope="col" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.columns.details') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-800">
            <template v-for="result in sortedResults" :key="result.index">
              <tr :data-test="`result-${result.index}`">
                <td class="px-3 py-2 text-sm font-medium text-gray-900 dark:text-gray-100">{{ result.account_name }}</td>
                <td class="px-3 py-2 text-sm text-gray-600 dark:text-gray-300">{{ result.platform }}</td>
                <td class="px-3 py-2 text-sm text-gray-600 dark:text-gray-300">{{ result.question_name }}</td>
                <td class="max-w-48 break-all px-3 py-2 text-sm text-gray-600 dark:text-gray-300">{{ result.model }}</td>
                <td class="px-3 py-2">
                  <span
                    :data-test="`status-${result.index}`"
                    :data-status="result.status"
                    :class="['inline-flex rounded-full px-2.5 py-1 text-xs font-semibold', statusClasses[result.status]]"
                  >
                    {{ t(`admin.accounts.batchTest.status.${result.status}`) }}
                  </span>
                </td>
                <td class="whitespace-nowrap px-3 py-2 text-sm text-gray-600 dark:text-gray-300">
                  {{ t('admin.accounts.batchTest.latencyValue', { latency: result.latency_ms }) }}
                </td>
                <td class="px-3 py-2">
                  <button
                    type="button"
                    :data-test="`expand-${result.index}`"
                    class="text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400"
                    :aria-expanded="expandedIndexes.has(result.index)"
                    :aria-controls="`batch-test-result-details-${result.index}`"
                    :aria-label="t('admin.accounts.batchTest.expandResult', { name: result.account_name })"
                    @click="toggleDetails(result.index)"
                  >
                    {{ t('admin.accounts.batchTest.details') }}
                  </button>
                </td>
              </tr>
              <tr v-if="expandedIndexes.has(result.index)" :id="`batch-test-result-details-${result.index}`">
                <td colspan="7" class="bg-gray-50 px-4 py-3 dark:bg-dark-700">
                  <div class="space-y-3 text-sm">
                    <div v-if="result.answer">
                      <div class="mb-1 text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.answer') }}</div>
                      <p class="whitespace-pre-wrap break-words text-gray-700 dark:text-gray-200">{{ result.answer }}</p>
                    </div>
                    <div v-if="result.matched_patterns.length > 0">
                      <div class="mb-1 text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.accounts.batchTest.matchedPatterns') }}</div>
                      <div class="flex flex-wrap gap-1">
                        <span v-for="pattern in result.matched_patterns" :key="pattern" class="badge badge-primary text-xs">{{ pattern }}</span>
                      </div>
                    </div>
                    <div v-if="result.error">
                      <div class="mb-1 text-xs font-medium text-red-500">{{ t('admin.accounts.batchTest.columns.error') }}</div>
                      <p class="whitespace-pre-wrap break-words text-red-600 dark:text-red-300">{{ result.error }}</p>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="running" @click="handleClose">
          {{ t('admin.accounts.batchTest.close') }}
        </button>
        <button v-if="running" type="button" data-test="stop" class="btn btn-danger" @click="stopBatchTest">
          {{ t('admin.accounts.batchTest.stop') }}
        </button>
        <button
          type="button"
          data-test="start"
          class="btn btn-primary"
          :disabled="!canStart"
          @click="startBatchTest"
        >
          {{ running ? t('admin.accounts.batchTest.running') : t('admin.accounts.batchTest.start') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import { buildApiUrl } from '@/api/client'
import { ADMIN_UI_REQUEST_HEADER } from '@/api/adminUIRequest'
import { useClipboard } from '@/composables/useClipboard'
import Select from '@/components/common/Select.vue'
import { isSamePlatformModelIntersectionError } from '@/utils/modelIntersectionError'

interface TestQuestion {
  id: number
  name: string
  enabled: boolean
  sort_order: number
  answer_patterns: string[]
}

interface BatchTestRequest {
  account_ids: number[]
  question_ids: number[]
  model_id: string
  concurrency: number
}

interface BatchStartData {
  total: number
  account_count: number
  question_count: number
  concurrency: number
}

type ItemStatus = 'passed' | 'failed' | 'unjudged' | 'error'

interface BatchItemResult {
  index: number
  account_id: number
  account_name: string
  platform: string
  question_id: number
  question_name: string
  model: string
  status: ItemStatus
  judged: boolean
  answer_patterns: string[]
  matched_patterns: string[]
  answer: string
  error: string
  latency_ms: number
}

interface BatchCompleteData {
  total: number
  passed: number
  failed: number
  unjudged: number
  error: number
  cancelled: boolean
}

type BatchTestEvent =
  | { type: 'batch_start'; data: BatchStartData }
  | { type: 'item_result'; data: BatchItemResult }
  | { type: 'batch_complete'; data: BatchCompleteData }

const props = defineProps<{
  show: boolean
  accountIds: number[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const questions = ref<TestQuestion[]>([])
const selectedQuestionIds = ref<number[]>([])
const loadingQuestions = ref(false)
const modelMode = ref<'default' | 'custom'>('default')
const customModelId = ref('')
const modelOptions = ref<Array<{ value: string; label: string }>>([])
const modelIntersectionLoading = ref(false)
const modelIntersectionError = ref(false)
const crossPlatform = ref(false)
const concurrency = ref(3)
const running = ref(false)
const stoppedByUser = ref(false)
const errorMessage = ref('')
const results = ref<BatchItemResult[]>([])
const completedIndexes = ref<Set<number>>(new Set())
const expandedIndexes = ref<Set<number>>(new Set())
const total = ref(0)
const batchSummary = ref<BatchCompleteData | null>(null)

let abortController: AbortController | null = null
let questionLoadVersion = 0
let modelLoadVersion = 0

const statusClasses: Record<ItemStatus, string> = {
  passed: 'bg-green-100 text-green-700 dark:bg-green-500/20 dark:text-green-400',
  failed: 'bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-400',
  unjudged: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-500/20 dark:text-yellow-400',
  error: 'bg-orange-100 text-orange-700 dark:bg-orange-500/20 dark:text-orange-400'
}

const allQuestionsSelected = computed(() =>
  questions.value.length > 0 && questions.value.every((question) => selectedQuestionIds.value.includes(question.id))
)

const orderedSelectedQuestionIds = computed(() =>
  questions.value
    .filter((question) => selectedQuestionIds.value.includes(question.id))
    .map((question) => question.id)
)

const canStart = computed(() => {
  if (running.value || loadingQuestions.value || modelIntersectionLoading.value || crossPlatform.value) return false
  if (props.accountIds.length === 0 || orderedSelectedQuestionIds.value.length === 0) return false
  return modelMode.value === 'default' || customModelId.value.trim().length > 0
})

const sortedResults = computed(() => [...results.value].sort((a, b) => a.index - b.index))
const completedCount = computed(() => completedIndexes.value.size)
const progressPercent = computed(() => {
  if (total.value <= 0) return 0
  return Math.min(100, Math.round((completedCount.value / total.value) * 100))
})
const liveSummary = computed<BatchCompleteData>(() => {
  const counts: BatchCompleteData = {
    total: total.value,
    passed: 0,
    failed: 0,
    unjudged: 0,
    error: 0,
    cancelled: stoppedByUser.value
  }
  for (const result of results.value) counts[result.status] += 1
  return counts
})
const displaySummary = computed(() => ({
  ...(batchSummary.value ?? liveSummary.value),
  cancelled: stoppedByUser.value || (batchSummary.value?.cancelled ?? false)
}))

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null

const normalizeQuestion = (value: unknown): TestQuestion | null => {
  if (!isRecord(value)) return null
  if (
    typeof value.id !== 'number' ||
    typeof value.name !== 'string' ||
    typeof value.enabled !== 'boolean' ||
    typeof value.sort_order !== 'number'
  ) return null
  return {
    id: value.id,
    name: value.name,
    enabled: value.enabled,
    sort_order: value.sort_order,
    answer_patterns: Array.isArray(value.answer_patterns)
      ? value.answer_patterns.filter((pattern): pattern is string => typeof pattern === 'string')
      : []
  }
}

const resetBatchOutput = () => {
  stoppedByUser.value = false
  errorMessage.value = ''
  results.value = []
  completedIndexes.value = new Set()
  expandedIndexes.value = new Set()
  total.value = 0
  batchSummary.value = null
}

const resetState = () => {
  abortStream()
  questions.value = []
  selectedQuestionIds.value = []
  loadingQuestions.value = false
  modelMode.value = 'default'
  customModelId.value = ''
  modelOptions.value = []
  modelIntersectionLoading.value = false
  modelIntersectionError.value = false
  crossPlatform.value = false
  concurrency.value = 3
  resetBatchOutput()
}

const abortStream = () => {
  const controller = abortController
  abortController = null
  running.value = false
  controller?.abort()
}

const stopBatchTest = () => {
  if (!running.value) return
  stoppedByUser.value = true
  abortStream()
}

const loadQuestions = async () => {
  const version = ++questionLoadVersion
  loadingQuestions.value = true
  try {
    const response: unknown = await adminAPI.accountTestQuestions.list(true)
    if (version !== questionLoadVersion || !props.show) return
    if (!Array.isArray(response)) throw new Error('Invalid account test questions response')
    questions.value = response
      .map(normalizeQuestion)
      .filter((question): question is TestQuestion => question !== null && question.enabled)
      .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id)
  } catch (error) {
    if (version !== questionLoadVersion || !props.show) return
    questions.value = []
    errorMessage.value = t('admin.accounts.batchTest.loadQuestionsFailed')
    console.error('Failed to load account test questions:', error)
  } finally {
    if (version === questionLoadVersion) loadingQuestions.value = false
  }
}

const loadModelIntersection = async () => {
  const version = ++modelLoadVersion
  crossPlatform.value = false
  modelIntersectionError.value = false
  modelOptions.value = []
  const ids = [...new Set(props.accountIds)]
  if (ids.length === 0) return version
  modelIntersectionLoading.value = true
  try {
    const result = await adminAPI.accounts.getModelIntersection(ids)
    if (version !== modelLoadVersion || !props.show) return false
    modelOptions.value = (result.models ?? []).map(model => ({
      value: model.id,
      label: model.display_name || model.id
    }))
  } catch (error) {
    if (version !== modelLoadVersion || !props.show) return false
    if (isSamePlatformModelIntersectionError(error)) {
      crossPlatform.value = true
      questions.value = []
      modelOptions.value = []
    } else {
      modelIntersectionError.value = true
    }
  } finally {
    if (version === modelLoadVersion) modelIntersectionLoading.value = false
  }
  return version
}

watch(
  [() => props.show, () => props.accountIds.join(',')],
  ([show]) => {
    questionLoadVersion += 1
    modelLoadVersion += 1
    if (!show) {
      abortStream()
      loadingQuestions.value = false
      modelIntersectionLoading.value = false
      return
    }
    resetState()
    void loadModelIntersection().then((version) => {
      if (version === modelLoadVersion && props.show && !crossPlatform.value) void loadQuestions()
    })
  },
  { immediate: true }
)

const toggleAllQuestions = (event: Event) => {
  const checked = (event.target as HTMLInputElement).checked
  selectedQuestionIds.value = checked ? questions.value.map((question) => question.id) : []
}

const toggleDetails = (index: number) => {
  const next = new Set(expandedIndexes.value)
  if (next.has(index)) next.delete(index)
  else next.add(index)
  expandedIndexes.value = next
}

const handleClose = () => {
  questionLoadVersion += 1
  modelLoadVersion += 1
  abortStream()
  emit('close')
}

const isAbortError = (error: unknown): boolean =>
  (error instanceof DOMException && error.name === 'AbortError') ||
  (isRecord(error) && error.name === 'AbortError')

const readResponseError = async (response: Response): Promise<string> => {
  const fallback = t('admin.accounts.batchTest.httpError', { status: response.status })
  try {
    const text = (await response.text()).trim()
    if (!text) return fallback
    try {
      const body: unknown = JSON.parse(text)
      if (isRecord(body)) {
        for (const key of ['detail', 'error', 'message']) {
          const value = body[key]
          if (typeof value === 'string' && value.trim()) return value
          if (isRecord(value) && typeof value.message === 'string' && value.message.trim()) {
            return value.message
          }
        }
      }
    } catch {
      return text
    }
    return fallback
  } catch {
    return fallback
  }
}

const handleEvent = (event: BatchTestEvent) => {
  switch (event.type) {
    case 'batch_start':
      total.value = event.data.total
      break
    case 'item_result': {
      const result = event.data
      const existingIndex = results.value.findIndex((item) => item.index === result.index)
      if (existingIndex >= 0) results.value.splice(existingIndex, 1, result)
      else results.value.push(result)
      completedIndexes.value = new Set(completedIndexes.value).add(result.index)
      break
    }
    case 'batch_complete':
      batchSummary.value = event.data
      total.value = event.data.total
      break
  }
}

const startBatchTest = async () => {
  if (!canStart.value) return

  abortStream()
  resetBatchOutput()
  const controller = new AbortController()
  abortController = controller
  running.value = true
  let completed = false

  const requestBody: BatchTestRequest = {
    account_ids: [...props.accountIds],
    question_ids: orderedSelectedQuestionIds.value,
    model_id: modelMode.value === 'custom' ? customModelId.value.trim() : '',
    concurrency: concurrency.value
  }

  try {
    const response = await fetch(buildApiUrl('/admin/accounts/batch-test'), {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
        'Content-Type': 'application/json',
        [ADMIN_UI_REQUEST_HEADER]: '1'
      },
      body: JSON.stringify(requestBody),
      signal: controller.signal
    })

    if (!response.ok) throw new Error(await readResponseError(response))
    if (controller.signal.aborted || abortController !== controller) return

    const reader = response.body?.getReader()
    if (!reader) throw new Error(t('admin.accounts.batchTest.noResponseBody'))

    const decoder = new TextDecoder()
    let buffer = ''
    const handleSSELine = (line: string) => {
      if (controller.signal.aborted || abortController !== controller || !props.show) return
      if (!line.startsWith('data:')) return
      const json = line.slice(5).trim()
      if (!json) return
      try {
        const event = JSON.parse(json) as BatchTestEvent
        if (event.type === 'batch_complete') completed = true
        handleEvent(event)
      } catch (error) {
        console.error('Failed to parse batch test SSE event:', error)
      }
    }

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''
      for (const line of lines) handleSSELine(line)
    }
    buffer += decoder.decode()
    if (buffer) handleSSELine(buffer)

    if (
      !completed &&
      !controller.signal.aborted &&
      abortController === controller &&
      props.show
    ) {
      errorMessage.value = t('admin.accounts.batchTest.endedWithoutComplete')
    }
  } catch (error: unknown) {
    if (isAbortError(error) || controller.signal.aborted || abortController !== controller) return
    errorMessage.value = error instanceof Error && error.message
      ? error.message
      : t('admin.accounts.batchTest.unknownError')
  } finally {
    if (abortController === controller) {
      abortController = null
      running.value = false
    }
  }
}

const tsvCell = (value: string | number): string => String(value).replace(/[\t\r\n]+/g, ' ')

const copyResults = () => {
  const header = [
    t('admin.accounts.batchTest.columns.account'),
    t('admin.accounts.batchTest.columns.platform'),
    t('admin.accounts.batchTest.columns.question'),
    t('admin.accounts.batchTest.columns.model'),
    t('admin.accounts.batchTest.columns.status'),
    t('admin.accounts.batchTest.columns.latency'),
    t('admin.accounts.batchTest.columns.error')
  ]
  const rows = sortedResults.value.map((result) => [
    result.account_name,
    result.platform,
    result.question_name,
    result.model,
    t(`admin.accounts.batchTest.status.${result.status}`),
    result.latency_ms,
    result.error
  ].map(tsvCell).join('\t'))
  void copyToClipboard([header.join('\t'), ...rows].join('\n'), t('admin.accounts.batchTest.copied'))
}

onBeforeUnmount(() => {
  questionLoadVersion += 1
  modelLoadVersion += 1
  abortStream()
})
</script>
