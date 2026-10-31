<template>
  <BaseDialog :show="show" :title="t('admin.scheduledQuestionTests.title')" width="extra-wide" @close="handleClose">
    <div class="space-y-4">
      <p v-if="errorMessage" role="alert" class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ errorMessage }}</p>
      <div class="flex justify-end">
        <button type="button" data-test="create-plan" class="btn btn-primary btn-sm" @click="openCreate">{{ t('admin.scheduledQuestionTests.create') }}</button>
      </div>
      <div v-if="loading" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <p v-else-if="plans.length === 0" class="rounded-lg border border-dashed border-gray-300 px-4 py-8 text-center text-sm text-gray-500">{{ t('admin.scheduledQuestionTests.empty') }}</p>
      <div v-else class="overflow-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-700"><tr>
            <th v-for="key in ['name','accounts','questions','cron','threshold','enabled','lastRun','nextRun','actions']" :key="key" class="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500">{{ t(`admin.scheduledQuestionTests.columns.${key}`) }}</th>
          </tr></thead>
          <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
            <tr v-for="plan in plans" :key="plan.id">
              <td class="px-3 py-2 text-sm font-medium">{{ plan.name }}</td><td class="px-3 py-2 text-sm">{{ plan.account_ids.length }}</td><td class="px-3 py-2 text-sm">{{ plan.question_ids.length }}</td>
              <td class="px-3 py-2 font-mono text-xs">{{ plan.cron_expression }}</td><td class="px-3 py-2 text-sm">{{ plan.min_pass_ratio }}%</td>
              <td class="px-3 py-2 text-sm"><span :class="plan.enabled ? 'text-green-600' : 'text-gray-400'">{{ plan.enabled ? t('admin.scheduledQuestionTests.yes') : t('admin.scheduledQuestionTests.no') }}</span></td>
              <td class="px-3 py-2 text-xs">{{ plan.last_status ? t('admin.scheduledQuestionTests.recentRun', { passed: plan.passed_accounts ?? 0, total: plan.total_accounts ?? 0, alerted: plan.alerted_accounts ?? 0 }) : '—' }}</td>
              <td class="px-3 py-2 text-xs">{{ plan.next_run_at || '—' }}</td>
              <td class="px-3 py-2"><div class="flex gap-1"><button type="button" data-test="edit-plan" class="btn btn-secondary btn-sm" @click="openEdit(plan)">{{ t('common.edit') }}</button><button type="button" data-test="run-plan" class="btn btn-secondary btn-sm" :disabled="plan.running || runningPlanId !== null" @click="runPlan(plan)">{{ plan.running || runningPlanId === plan.id ? t('admin.scheduledQuestionTests.running') : t('admin.scheduledQuestionTests.runNow') }}</button><button type="button" data-test="history-plan" class="btn btn-secondary btn-sm" @click="openHistory(plan)">{{ t('admin.scheduledQuestionTests.history') }}</button><button type="button" data-test="delete-plan" class="btn btn-danger btn-sm" @click="deletePlan(plan)">{{ t('common.delete') }}</button></div></td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="showForm" class="rounded-lg border border-primary-200 bg-primary-50/30 p-4 dark:border-primary-800 dark:bg-primary-900/10">
        <h3 class="mb-3 font-medium">{{ editing ? t('admin.scheduledQuestionTests.edit') : t('admin.scheduledQuestionTests.create') }}</h3>
        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.name') }}<input v-model="form.name" data-test="plan-name" class="input mt-1" /></label>
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.accountIds') }}<input v-model="accountInput" data-test="account-ids" class="input mt-1" placeholder="1, 2, 3" /><span class="mt-1 block text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.accountIdsHint', { count: parsedAccountIds.length }) }}</span></label>
          <fieldset class="text-sm"><legend>{{ t('admin.scheduledQuestionTests.form.questions') }}</legend><div class="mt-1 max-h-32 overflow-auto rounded border p-2"><label v-for="question in questions" :key="question.id" class="block"><input v-model="form.question_ids" type="checkbox" :value="question.id" data-test="question-option" /> {{ question.name }}</label><span v-if="questions.length === 0" class="text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.noQuestions') }}</span></div></fieldset>
          <div class="text-sm"><label>{{ t('admin.scheduledQuestionTests.form.model') }}</label><div class="mt-1 flex gap-3"><label><input v-model="modelMode" type="radio" value="default" /> {{ t('admin.scheduledQuestionTests.form.defaultModel') }}</label><label><input v-model="modelMode" type="radio" value="custom" :disabled="crossPlatform" /> {{ t('admin.scheduledQuestionTests.form.customModel') }}</label></div><Select v-if="modelMode === 'custom'" v-model="form.model_id" data-test="plan-model" class="mt-1" :options="modelOptions" :disabled="crossPlatform || modelLoading || modelOptions.length === 0" :loading="modelLoading" /></div>
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.cron') }}<input v-model="form.cron_expression" data-test="cron" class="input mt-1" placeholder="*/30 * * * *" /><span class="mt-1 block text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.cronHint') }}</span></label>
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.threshold') }}<input v-model.number="form.min_pass_ratio" data-test="threshold" type="number" min="0" max="100" class="input mt-1" /></label>
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.cooldown') }}<input v-model.number="form.cooldown_minutes" data-test="cooldown" type="number" min="0" max="10080" class="input mt-1" /></label>
          <div class="flex items-end gap-4 text-sm"><label><input v-model="form.enabled" type="checkbox" data-test="enabled" /> {{ t('admin.scheduledQuestionTests.form.enabled') }}</label><label><input v-model="form.notify_enabled" type="checkbox" data-test="notify" /> {{ t('admin.scheduledQuestionTests.form.notify') }}</label></div>
        </div>
        <p v-if="crossPlatform" role="alert" class="mt-3 text-sm text-red-600">{{ t('admin.scheduledQuestionTests.crossPlatform') }}</p>
        <p v-else-if="modelLoadFailed" class="mt-3 text-sm text-amber-600">{{ t('admin.scheduledQuestionTests.modelsLoadFailed') }}</p>
        <p v-else-if="modelOptions.length === 0 && modelMode === 'custom'" class="mt-3 text-sm text-amber-600">{{ t('admin.scheduledQuestionTests.noCommonModels') }}</p>
        <div class="mt-4 flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="closeForm">{{ t('common.cancel') }}</button><button type="button" data-test="save-plan" class="btn btn-primary" :disabled="saving || crossPlatform || modelLoading" @click="savePlan">{{ t('common.save') }}</button></div>
      </div>

      <div v-if="historyPlan" class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
        <div class="mb-3 flex justify-between">
          <h3 class="font-medium">{{ t('admin.scheduledQuestionTests.history') }}: {{ historyPlan.name }}</h3>
          <button type="button" @click="historyPlan = null">{{ t('common.close') }}</button>
        </div>
        <div v-if="runs.length === 0" class="text-sm text-gray-500">{{ t('admin.scheduledQuestionTests.noRuns') }}</div>
        <div v-for="run in runs" :key="run.id" class="border-b py-2 text-sm">
          <button type="button" :data-test="`run-${run.id}`" class="flex w-full justify-between text-left" @click="toggleRun(run.id)">
            <span>
              {{ run.started_at }} · {{ run.status }}
              <span v-if="run.notifications_suppressed" class="ml-2 rounded bg-amber-50 px-2 py-0.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.scheduledQuestionTests.notificationsSuppressed') }}</span>
            </span>
            <span>{{ t('admin.scheduledQuestionTests.recentRun', { passed: run.passed_accounts, total: run.total_accounts, alerted: run.alerted_accounts }) }}</span>
          </button>
          <div v-if="expandedRuns.has(run.id)" class="mt-2 space-y-1 pl-3 text-xs">
            <div v-if="!run.summary?.length" class="text-gray-500">{{ t('admin.scheduledQuestionTests.noDetails') }}</div>
            <div v-for="item in run.summary ?? []" :key="item.account_id" :data-test="`run-account-${item.account_id}`">
              {{ item.account_name }}: {{ item.passed }}/{{ item.total }} ({{ item.ratio ?? '—' }}%) · {{ item.alerting ? t('admin.scheduledQuestionTests.alerting') : t('admin.scheduledQuestionTests.ok') }}
              · {{ t('admin.scheduledQuestionTests.stateAfter', { state: item.state_after === 'alert' ? t('admin.scheduledQuestionTests.stateAlert') : t('admin.scheduledQuestionTests.stateOk') }) }}
              <span v-if="item.error"> · {{ item.error }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { adminAPI } from '@/api/admin'
import type { ScheduledQuestionTestPlan, ScheduledQuestionTestRun } from '@/api/admin/scheduledQuestionTests'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { isSamePlatformModelIntersectionError } from '@/utils/modelIntersectionError'

const props = defineProps<{ show: boolean; selectedAccountIds?: number[] }>()
const emit = defineEmits<{ (event: 'close'): void }>()
const { t } = useI18n()
const appStore = useAppStore()
const plans = ref<ScheduledQuestionTestPlan[]>([])
const questions = ref<Array<{ id: number; name: string; enabled: boolean }>>([])
const loading = ref(false)
const saving = ref(false)
const runningPlanId = ref<number | null>(null)
const errorMessage = ref('')
const showForm = ref(false); const editing = ref<ScheduledQuestionTestPlan | null>(null); const accountInput = ref('')
const modelMode = ref<'default' | 'custom'>('default'); const modelOptions = ref<Array<{ value: string; label: string }>>([]); const modelLoading = ref(false); const modelLoadFailed = ref(false); const crossPlatform = ref(false)
const historyPlan = ref<ScheduledQuestionTestPlan | null>(null); const runs = ref<ScheduledQuestionTestRun[]>([]); const expandedRuns = ref(new Set<number>())
const form = reactive({ name: '', account_ids: [] as number[], question_ids: [] as number[], model_id: '', cron_expression: '', min_pass_ratio: 80, cooldown_minutes: 360, enabled: true, notify_enabled: true })
const parsedAccountIds = computed(() => [...new Set(accountInput.value.split(/[,\s]+/).map(Number).filter(id => Number.isInteger(id) && id > 0))])
let modalActive = false
let modalVersion = 0
let planLoadVersion = 0
let modelLoadVersion = 0
let historyLoadVersion = 0
let pollTimer: ReturnType<typeof setTimeout> | null = null

const stopPolling = () => {
  if (pollTimer !== null) clearTimeout(pollTimer)
  pollTimer = null
}

const schedulePolling = () => {
  stopPolling()
  if (!modalActive || !props.show || !plans.value.some(plan => plan.running)) return
  pollTimer = setTimeout(() => {
    pollTimer = null
    void loadPlans()
  }, 3000)
}

const loadHistory = async (id: number) => {
  const version = ++historyLoadVersion
  try {
    const result = await adminAPI.scheduledQuestionTests.listRuns(id, 50)
    if (version !== historyLoadVersion || !modalActive || historyPlan.value?.id !== id) return
    runs.value = result
  } catch (error) {
    if (version !== historyLoadVersion || !modalActive || historyPlan.value?.id !== id) return
    appStore.showError(extractApiErrorMessage(error, t('admin.scheduledQuestionTests.historyFailed')))
  }
}

const loadPlans = async () => {
  const version = ++planLoadVersion
  stopPolling()
  try {
    const result = await adminAPI.scheduledQuestionTests.list()
    if (version !== planLoadVersion || !modalActive || !props.show) return
    const historyId = historyPlan.value?.id
    const historyWasRunning = plans.value.some(plan => plan.id === historyId && plan.running)
    plans.value = result
    if (historyId && historyWasRunning && result.some(plan => plan.id === historyId && !plan.running)) {
      void loadHistory(historyId)
    }
  } catch {
    if (version === planLoadVersion && modalActive) errorMessage.value = t('admin.scheduledQuestionTests.loadFailed')
  } finally {
    if (version === planLoadVersion) schedulePolling()
  }
}

const resetForm = () => { Object.assign(form, { name: '', account_ids: [], question_ids: [], model_id: '', cron_expression: '', min_pass_ratio: 80, cooldown_minutes: 360, enabled: true, notify_enabled: true }); accountInput.value = (props.selectedAccountIds ?? []).join(', '); modelMode.value = 'default'; modelOptions.value = []; modelLoadFailed.value = false; crossPlatform.value = false }
const load = async () => {
  const version = modalVersion
  loading.value = true
  try {
    const [, result] = await Promise.all([loadPlans(), adminAPI.accountTestQuestions.list(true)])
    if (version !== modalVersion || !modalActive) return
    questions.value = result.filter(q => q.enabled).map(q => ({ id: q.id, name: q.name, enabled: q.enabled }))
  } catch {
    if (version === modalVersion && modalActive) errorMessage.value = t('admin.scheduledQuestionTests.loadFailed')
  } finally {
    if (version === modalVersion && modalActive) loading.value = false
  }
}

const loadModels = async () => {
  const version = ++modelLoadVersion
  crossPlatform.value = false
  modelLoadFailed.value = false
  modelOptions.value = []
  modelLoading.value = false
  const ids = parsedAccountIds.value
  if (!modalActive || !showForm.value || !ids.length) return
  modelLoading.value = true
  try {
    const result = await adminAPI.accounts.getModelIntersection(ids)
    if (version !== modelLoadVersion || !modalActive || !showForm.value) return
    modelOptions.value = result.models.map(model => ({ value: model.id, label: model.display_name || model.id }))
  } catch (error) {
    if (version !== modelLoadVersion || !modalActive || !showForm.value) return
    crossPlatform.value = isSamePlatformModelIntersectionError(error)
    modelLoadFailed.value = !crossPlatform.value
  } finally {
    if (version === modelLoadVersion) modelLoading.value = false
  }
}

const openCreate = () => { editing.value = null; resetForm(); showForm.value = true }
const openEdit = (plan: ScheduledQuestionTestPlan) => { editing.value = plan; Object.assign(form, { name: plan.name, account_ids: [...plan.account_ids], question_ids: [...plan.question_ids], model_id: plan.model_id, cron_expression: plan.cron_expression, min_pass_ratio: plan.min_pass_ratio, cooldown_minutes: plan.cooldown_minutes, enabled: plan.enabled, notify_enabled: plan.notify_enabled }); accountInput.value = plan.account_ids.join(', '); modelMode.value = plan.model_id ? 'custom' : 'default'; showForm.value = true }
const closeForm = () => { modelLoadVersion += 1; modelLoading.value = false; showForm.value = false; editing.value = null }
const savePlan = async () => { const ids = parsedAccountIds.value; const cron = form.cron_expression.trim(); if (!form.name.trim() || ids.length === 0 || form.question_ids.length === 0 || cron.split(/\s+/).length !== 5 || form.min_pass_ratio < 0 || form.min_pass_ratio > 100 || form.cooldown_minutes < 0 || form.cooldown_minutes > 10080 || (modelMode.value === 'custom' && !form.model_id)) { errorMessage.value = t('admin.scheduledQuestionTests.validation'); return } saving.value = true; try { const payload = { name: form.name.trim(), account_ids: ids, question_ids: [...form.question_ids], model_id: modelMode.value === 'custom' ? form.model_id : '', cron_expression: cron, min_pass_ratio: form.min_pass_ratio, cooldown_minutes: form.cooldown_minutes, enabled: form.enabled, notify_enabled: form.notify_enabled }; if (editing.value) await adminAPI.scheduledQuestionTests.update(editing.value.id, payload); else await adminAPI.scheduledQuestionTests.create(payload); closeForm(); await load() } catch (error) { errorMessage.value = extractApiErrorMessage(error, t('admin.scheduledQuestionTests.saveFailed')) } finally { saving.value = false } }
const deletePlan = async (plan: ScheduledQuestionTestPlan) => { if (!window.confirm(t('admin.scheduledQuestionTests.deleteConfirm'))) return; try { await adminAPI.scheduledQuestionTests.delete(plan.id); await load() } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.scheduledQuestionTests.deleteFailed'))) } }
const runPlan = async (plan: ScheduledQuestionTestPlan) => {
  if (plan.running || runningPlanId.value !== null) return
  const version = modalVersion
  runningPlanId.value = plan.id
  let refresh = false
  try {
    await adminAPI.scheduledQuestionTests.runNow(plan.id)
    if (version !== modalVersion || !modalActive) return
    appStore.showSuccess(t('admin.scheduledQuestionTests.started'))
    refresh = true
  } catch (error) {
    if (version !== modalVersion || !modalActive) return
    if (typeof error === 'object' && error !== null && 'status' in error && error.status === 409) {
      appStore.showError(t('admin.scheduledQuestionTests.alreadyRunning'))
      refresh = true
    } else {
      appStore.showError(extractApiErrorMessage(error, t('admin.scheduledQuestionTests.runFailed')))
    }
  } finally {
    if (version === modalVersion) runningPlanId.value = null
  }
  if (refresh) {
    const currentPlan = plans.value.find(item => item.id === plan.id)
    if (currentPlan) currentPlan.running = true
    await loadPlans()
  }
}
const openHistory = async (plan: ScheduledQuestionTestPlan) => { historyPlan.value = plan; runs.value = []; expandedRuns.value = new Set(); await loadHistory(plan.id) }
const toggleRun = (id: number) => { const next = new Set(expandedRuns.value); next.has(id) ? next.delete(id) : next.add(id); expandedRuns.value = next }

const stopRequests = () => {
  modalActive = false
  modalVersion += 1
  planLoadVersion += 1
  modelLoadVersion += 1
  historyLoadVersion += 1
  stopPolling()
}
const closeModal = () => {
  stopRequests()
  runningPlanId.value = null
  modelLoading.value = false
  showForm.value = false
  historyPlan.value = null
}
const handleClose = () => { closeModal(); emit('close') }

watch([showForm, () => parsedAccountIds.value.join(',')], () => { void loadModels() })
watch(() => props.show, show => {
  if (show) {
    modalActive = true
    errorMessage.value = ''
    void load()
  } else {
    closeModal()
  }
}, { immediate: true })
onBeforeUnmount(stopRequests)
</script>
