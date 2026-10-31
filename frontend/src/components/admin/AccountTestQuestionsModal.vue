<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accountTestQuestions.title')"
    width="extra-wide"
    @close="$emit('close')"
  >
    <div class="space-y-4">
      <div class="flex items-center justify-between gap-4">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.accountTestQuestions.description') }}
        </p>
        <button
          type="button"
          data-test="create-question"
          class="btn btn-primary btn-sm shrink-0"
          @click="openCreateForm"
        >
          <Icon name="plus" size="sm" class="mr-1" />
          {{ t('admin.accountTestQuestions.createQuestion') }}
        </button>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-8">
        <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
      </div>

      <div v-else-if="questions.length === 0" class="py-8 text-center">
        <div class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700">
          <Icon name="questionCircle" size="lg" class="text-gray-400" />
        </div>
        <h4 class="mb-1 text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accountTestQuestions.noQuestions') }}
        </h4>
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.accountTestQuestions.createFirstQuestion') }}
        </p>
      </div>

      <div v-else class="max-h-96 overflow-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="sticky top-0 bg-gray-50 dark:bg-dark-700">
            <tr>
              <th class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                {{ t('admin.accountTestQuestions.columns.name') }}
              </th>
              <th class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                {{ t('admin.accountTestQuestions.columns.content') }}
              </th>
              <th class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                {{ t('admin.accountTestQuestions.columns.patterns') }}
              </th>
              <th class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                {{ t('admin.accountTestQuestions.columns.enabled') }}
              </th>
              <th class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                {{ t('admin.accountTestQuestions.columns.sortOrder') }}
              </th>
              <th class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                {{ t('admin.accountTestQuestions.columns.actions') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-800">
            <tr
              v-for="question in questions"
              :key="question.id"
              class="hover:bg-gray-50 dark:hover:bg-dark-700"
            >
              <td class="px-3 py-2">
                <div class="max-w-40 break-words text-sm font-medium text-gray-900 dark:text-white">
                  {{ question.name }}
                </div>
              </td>
              <td class="px-3 py-2">
                <div class="max-w-xs truncate text-sm text-gray-500 dark:text-gray-400" :title="question.content">
                  {{ summarizeContent(question.content) }}
                </div>
              </td>
              <td class="px-3 py-2">
                <div v-if="question.answer_patterns.length" class="flex max-w-48 flex-wrap gap-1">
                  <span
                    v-for="pattern in question.answer_patterns.slice(0, 3)"
                    :key="pattern"
                    class="badge badge-primary text-xs"
                  >
                    {{ pattern }}
                  </span>
                  <span v-if="question.answer_patterns.length > 3" class="text-xs text-gray-500">
                    +{{ question.answer_patterns.length - 3 }}
                  </span>
                </div>
                <span v-else class="text-xs text-gray-400 dark:text-gray-600">—</span>
              </td>
              <td class="px-3 py-2">
                <button
                  type="button"
                  role="switch"
                  data-test="toggle-question"
                  :aria-checked="question.enabled"
                  :aria-label="t('admin.accountTestQuestions.toggleLabel', { name: question.name })"
                  :disabled="togglingQuestionId !== null"
                  :class="[
                    'relative inline-flex h-4 w-7 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60',
                    question.enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
                  ]"
                  @click="toggleEnabled(question)"
                >
                  <span
                    :class="[
                      'pointer-events-none inline-block h-3 w-3 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                      question.enabled ? 'translate-x-3' : 'translate-x-0'
                    ]"
                  />
                </button>
              </td>
              <td class="whitespace-nowrap px-3 py-2 text-sm text-gray-500 dark:text-gray-400">
                {{ question.sort_order }}
              </td>
              <td class="px-3 py-2">
                <div class="flex items-center gap-1">
                  <button
                    type="button"
                    data-test="edit-question"
                    class="p-1 text-gray-500 hover:text-primary-600 dark:hover:text-primary-400"
                    :title="t('common.edit')"
                    @click="openEditForm(question)"
                  >
                    <Icon name="edit" size="sm" />
                  </button>
                  <button
                    type="button"
                    data-test="delete-question"
                    class="p-1 text-gray-500 hover:text-red-600 dark:hover:text-red-400"
                    :title="t('common.delete')"
                    @click="handleDelete(question)"
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="$emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
    </template>

    <BaseDialog
      :show="showForm"
      :title="editingQuestion
        ? t('admin.accountTestQuestions.editQuestion')
        : t('admin.accountTestQuestions.createQuestion')"
      width="wide"
      :z-index="60"
      @close="closeForm"
    >
      <form class="space-y-4" @submit.prevent="handleSubmit">
        <div>
          <label for="account-test-question-name" class="input-label">
            {{ t('admin.accountTestQuestions.form.name') }}
          </label>
          <input
            id="account-test-question-name"
            v-model="form.name"
            data-test="question-name"
            type="text"
            required
            class="input"
            :placeholder="t('admin.accountTestQuestions.form.namePlaceholder')"
          />
        </div>

        <div>
          <label for="account-test-question-content" class="input-label">
            {{ t('admin.accountTestQuestions.form.content') }}
          </label>
          <textarea
            id="account-test-question-content"
            v-model="form.content"
            data-test="question-content"
            rows="6"
            required
            class="input"
            :placeholder="t('admin.accountTestQuestions.form.contentPlaceholder')"
          />
        </div>

        <div>
          <label for="account-test-question-patterns" class="input-label">
            {{ t('admin.accountTestQuestions.form.patterns') }}
          </label>
          <textarea
            id="account-test-question-patterns"
            v-model="patternsInput"
            data-test="question-patterns"
            rows="4"
            class="input"
            :placeholder="t('admin.accountTestQuestions.form.patternsPlaceholder')"
          />
          <p class="input-hint text-xs">
            {{ t('admin.accountTestQuestions.form.patternsHint') }}
          </p>
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label for="account-test-question-sort-order" class="input-label">
              {{ t('admin.accountTestQuestions.form.sortOrder') }}
            </label>
            <input
              id="account-test-question-sort-order"
              v-model.number="form.sort_order"
              data-test="question-sort-order"
              type="number"
              step="1"
              min="-2147483648"
              max="2147483647"
              required
              class="input"
            />
            <p class="input-hint text-xs">
              {{ t('admin.accountTestQuestions.form.sortOrderHint') }}
            </p>
          </div>
          <label class="flex items-center gap-2 self-end pb-2">
            <input
              v-model="form.enabled"
              data-test="question-enabled"
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accountTestQuestions.form.enabled') }}
            </span>
          </label>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="submitting" @click="closeForm">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            data-test="submit-question"
            class="btn btn-primary"
            :disabled="submitting"
            @click="handleSubmit"
          >
            <Icon v-if="submitting" name="refresh" size="sm" class="mr-1 animate-spin" />
            {{ editingQuestion ? t('common.update') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.accountTestQuestions.deleteQuestion')"
      :message="t('admin.accountTestQuestions.deleteConfirm', { name: deletingQuestion?.name })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    />
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  AccountTestQuestion,
  CreateAccountTestQuestionRequest
} from '@/api/admin/accountTestQuestions'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
}>()
void emit

const { t } = useI18n()
const appStore = useAppStore()

const questions = ref<AccountTestQuestion[]>([])
const loading = ref(false)
const submitting = ref(false)
const deleting = ref(false)
const togglingQuestionId = ref<number | null>(null)
const showForm = ref(false)
const showDeleteDialog = ref(false)
const editingQuestion = ref<AccountTestQuestion | null>(null)
const deletingQuestion = ref<AccountTestQuestion | null>(null)
const patternsInput = ref('')
let loadRequestId = 0

const form = reactive({
  name: '',
  content: '',
  enabled: true,
  sort_order: 0
})

watch(() => props.show, (show) => {
  if (show) {
    void loadQuestions()
    return
  }

  loadRequestId += 1
  loading.value = false
  showForm.value = false
  showDeleteDialog.value = false
  editingQuestion.value = null
  deletingQuestion.value = null
})

const loadQuestions = async () => {
  const requestId = ++loadRequestId
  loading.value = true
  try {
    const result = await adminAPI.accountTestQuestions.list()
    if (requestId !== loadRequestId || !props.show) return
    questions.value = result
  } catch (error) {
    if (requestId !== loadRequestId || !props.show) return
    appStore.showError(t('admin.accountTestQuestions.loadFailed'))
    console.error('Error loading account test questions:', error)
  } finally {
    if (requestId === loadRequestId) {
      loading.value = false
    }
  }
}

const resetForm = () => {
  form.name = ''
  form.content = ''
  form.enabled = true
  form.sort_order = 0
  patternsInput.value = ''
}

const openCreateForm = () => {
  editingQuestion.value = null
  resetForm()
  showForm.value = true
}

const openEditForm = (question: AccountTestQuestion) => {
  editingQuestion.value = question
  form.name = question.name
  form.content = question.content
  form.enabled = question.enabled
  form.sort_order = question.sort_order
  patternsInput.value = question.answer_patterns.join('\n')
  showForm.value = true
}

const closeForm = () => {
  if (submitting.value) return
  showForm.value = false
  editingQuestion.value = null
  resetForm()
}

const parsePatterns = () => {
  const patterns = patternsInput.value
    .split(/\r?\n/)
    .map(pattern => pattern.trim())
    .filter(pattern => pattern.length > 0)
  return [...new Set(patterns)]
}

const characterLength = (value: string) => Array.from(value).length

const summarizeContent = (content: string) => {
  const summary = content.replace(/\s+/g, ' ').trim()
  return characterLength(summary) > 80
    ? `${Array.from(summary).slice(0, 80).join('')}…`
    : summary
}

const validateForm = () => {
  const name = form.name.trim()
  const content = form.content.trim()
  const patterns = parsePatterns()
  const sortOrder = Number(form.sort_order)

  if (!name) {
    appStore.showError(t('admin.accountTestQuestions.validation.nameRequired'))
    return null
  }
  if (characterLength(name) > 100) {
    appStore.showError(t('admin.accountTestQuestions.validation.nameLength'))
    return null
  }
  if (!content) {
    appStore.showError(t('admin.accountTestQuestions.validation.contentRequired'))
    return null
  }
  if (characterLength(content) > 20000) {
    appStore.showError(t('admin.accountTestQuestions.validation.contentLength'))
    return null
  }
  if (patterns.length > 20) {
    appStore.showError(t('admin.accountTestQuestions.validation.patternCount'))
    return null
  }
  if (patterns.some(pattern => characterLength(pattern) > 500)) {
    appStore.showError(t('admin.accountTestQuestions.validation.patternLength'))
    return null
  }
  if (!Number.isInteger(sortOrder)) {
    appStore.showError(t('admin.accountTestQuestions.validation.sortOrderInvalid'))
    return null
  }
  if (sortOrder < -2147483648 || sortOrder > 2147483647) {
    appStore.showError(t('admin.accountTestQuestions.validation.sortOrderRange'))
    return null
  }

  return {
    name,
    content,
    answer_patterns: patterns,
    enabled: form.enabled,
    sort_order: sortOrder
  } satisfies CreateAccountTestQuestionRequest
}

const handleSubmit = async () => {
  if (submitting.value) return
  const payload = validateForm()
  if (!payload) return

  submitting.value = true
  try {
    if (editingQuestion.value) {
      await adminAPI.accountTestQuestions.update(editingQuestion.value.id, payload)
      appStore.showSuccess(t('admin.accountTestQuestions.updateSuccess'))
    } else {
      await adminAPI.accountTestQuestions.create(payload)
      appStore.showSuccess(t('admin.accountTestQuestions.createSuccess'))
    }

    showForm.value = false
    editingQuestion.value = null
    resetForm()
    if (props.show) await loadQuestions()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountTestQuestions.saveFailed')))
    console.error('Error saving account test question:', error)
  } finally {
    submitting.value = false
  }
}

const handleDelete = (question: AccountTestQuestion) => {
  if (deleting.value) return
  deletingQuestion.value = question
  showDeleteDialog.value = true
}

const cancelDelete = () => {
  if (deleting.value) return
  showDeleteDialog.value = false
  deletingQuestion.value = null
}

const confirmDelete = async () => {
  if (deleting.value || !deletingQuestion.value) return
  const questionId = deletingQuestion.value.id
  deleting.value = true
  try {
    await adminAPI.accountTestQuestions.delete(questionId)
    appStore.showSuccess(t('admin.accountTestQuestions.deleteSuccess'))
    showDeleteDialog.value = false
    deletingQuestion.value = null
    if (props.show) await loadQuestions()
  } catch (error) {
    appStore.showError(t('admin.accountTestQuestions.deleteFailed'))
    console.error('Error deleting account test question:', error)
  } finally {
    deleting.value = false
  }
}

const toggleEnabled = async (question: AccountTestQuestion) => {
  if (togglingQuestionId.value !== null) return
  togglingQuestionId.value = question.id
  try {
    const updated = await adminAPI.accountTestQuestions.update(question.id, {
      enabled: !question.enabled
    })
    const current = questions.value.find(item => item.id === question.id)
    if (current) current.enabled = updated.enabled
  } catch (error) {
    appStore.showError(t('admin.accountTestQuestions.toggleFailed'))
    console.error('Error toggling account test question:', error)
  } finally {
    togglingQuestionId.value = null
  }
}
</script>
