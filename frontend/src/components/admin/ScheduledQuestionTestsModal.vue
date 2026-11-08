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
              <td class="px-3 py-2 text-sm font-medium"><div>{{ plan.name }}</div><div v-if="planBadges(plan).length" class="mt-1 flex flex-wrap gap-1" data-test="plan-badges"><span v-for="badge in planBadges(plan)" :key="badge" class="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] font-normal text-gray-600 dark:bg-dark-600 dark:text-gray-300">{{ t(`admin.scheduledQuestionTests.${badge}`) }}</span></div></td><td class="px-3 py-2 text-sm">{{ plan.account_ids.length }}</td><td class="px-3 py-2 text-sm">{{ plan.question_ids.length }}</td>
              <td class="px-3 py-2 font-mono text-xs">{{ plan.cron_expression }}</td><td class="px-3 py-2 text-sm">{{ plan.min_pass_ratio }}%</td>
              <td class="px-3 py-2 text-sm"><span :class="plan.enabled ? 'text-green-600' : 'text-gray-400'">{{ plan.enabled ? t('admin.scheduledQuestionTests.yes') : t('admin.scheduledQuestionTests.no') }}</span></td>
              <td class="px-3 py-2 text-xs"><template v-if="plan.last_status"><template v-if="(plan.skipped_accounts ?? 0) > 0">{{ t('admin.scheduledQuestionTests.recentRunSkipped', { passed: plan.passed_accounts ?? 0, total: plan.total_accounts ?? 0, alerted: plan.alerted_accounts ?? 0, skipped: plan.skipped_accounts }) }}</template><template v-else>{{ t('admin.scheduledQuestionTests.recentRun', { passed: plan.passed_accounts ?? 0, total: plan.total_accounts ?? 0, alerted: plan.alerted_accounts ?? 0 }) }}</template></template><template v-else>—</template></td>
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
          <div class="text-sm"><label>{{ t('admin.scheduledQuestionTests.form.model') }}</label><div class="mt-1 flex gap-3"><label><input v-model="modelMode" type="radio" value="default" /> {{ t('admin.scheduledQuestionTests.form.defaultModel') }}</label><label><input v-model="modelMode" type="radio" value="custom" :disabled="crossPlatformBlocking" /> {{ t('admin.scheduledQuestionTests.form.customModel') }}</label></div><Select v-if="modelMode === 'custom'" v-model="form.model_id" data-test="plan-model" class="mt-1" :options="modelOptionsForForm" :disabled="crossPlatformBlocking || modelLoading || modelOptionsForForm.length === 0" :loading="modelLoading" /></div>
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.cron') }}<input v-model="form.cron_expression" data-test="cron" class="input mt-1" placeholder="*/30 * * * *" /><span class="mt-1 block text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.cronHint') }}</span></label>
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.threshold') }}<input v-model.number="form.min_pass_ratio" data-test="threshold" type="number" min="0" max="100" class="input mt-1" /></label>
          <label class="text-sm">{{ t('admin.scheduledQuestionTests.form.cooldown') }}<input v-model.number="form.cooldown_minutes" data-test="cooldown" type="number" min="0" max="10080" class="input mt-1" /></label>
          <div class="flex flex-wrap items-end gap-4 text-sm"><label><input v-model="form.enabled" type="checkbox" data-test="enabled" /> {{ t('admin.scheduledQuestionTests.form.enabled') }}</label></div>
          <fieldset class="text-sm" data-test="schedule-link-section"><legend>{{ t('admin.scheduledQuestionTests.scheduleLink') }}</legend>
            <div class="mt-1 space-y-1"><label class="block"><input v-model="form.auto_pause_enabled" type="checkbox" data-test="auto-pause" /> {{ t('admin.scheduledQuestionTests.form.autoPause') }}</label><span class="block pl-5 text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.autoPauseHint') }}</span></div>
            <div class="mt-2 space-y-1"><label class="block"><input v-model="form.auto_resume_enabled" type="checkbox" data-test="auto-resume" /> {{ t('admin.scheduledQuestionTests.form.autoResume') }}</label><span class="block pl-5 text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.autoResumeHint') }}</span></div>
            <span class="mt-2 block text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.allFailedHint') }}</span>
            <span class="mt-1 block text-xs text-gray-500" data-test="quota-skip-hint">{{ t('admin.scheduledQuestionTests.form.quotaSkipHint') }}</span>
          </fieldset>
          <fieldset class="text-sm md:col-span-2" data-test="notification-section"><legend>{{ t('admin.scheduledQuestionTests.form.channels') }}</legend>
            <div class="mt-1 flex flex-wrap items-center gap-4"><label><input v-model="form.notify_enabled" type="checkbox" data-test="notify" /> {{ t('admin.scheduledQuestionTests.form.notify') }}</label><label><input v-model="form.report_enabled" type="checkbox" data-test="report" /> {{ t('admin.scheduledQuestionTests.form.report') }}</label><label><input v-model="form.email_enabled" type="checkbox" data-test="channel-email" /> {{ t('admin.scheduledQuestionTests.form.channelEmail') }}</label><span class="text-xs text-gray-500">{{ t('admin.scheduledQuestionTests.form.channelEmailHint') }}</span><label><input v-model="form.feishu_enabled" type="checkbox" data-test="channel-feishu" /> {{ t('admin.scheduledQuestionTests.form.channelFeishu') }}</label></div>
            <div v-if="form.feishu_enabled" class="mt-2 space-y-2 rounded border border-gray-200 p-2 dark:border-dark-600">
              <div><label class="block">{{ t('admin.scheduledQuestionTests.form.feishuWebhook') }}</label><div class="mt-1 flex flex-wrap gap-2"><input v-model="feishuWebhookInput" data-test="feishu-webhook" class="input flex-1" :placeholder="feishuWebhookPlaceholder" /><button type="button" data-test="feishu-webhook-clear" class="btn btn-secondary btn-sm" :disabled="!webhookConfigured" @click="clearFeishuWebhook">{{ t('admin.scheduledQuestionTests.form.clear') }}</button></div></div>
              <div><label class="block">{{ t('admin.scheduledQuestionTests.form.feishuSecret') }}</label><div class="mt-1 flex flex-wrap gap-2"><input v-model="feishuSecretInput" data-test="feishu-secret" type="password" autocomplete="new-password" class="input flex-1" :placeholder="feishuSecretPlaceholder || undefined" /><button type="button" data-test="feishu-secret-clear" class="btn btn-secondary btn-sm" :disabled="!secretConfigured" @click="clearFeishuSecret">{{ t('admin.scheduledQuestionTests.form.clear') }}</button></div></div>
              <div><button type="button" data-test="feishu-test" class="btn btn-secondary btn-sm" :disabled="feishuTesting || !canTestFeishu" @click="testFeishu">{{ feishuTesting ? t('admin.scheduledQuestionTests.form.feishuTesting') : t('admin.scheduledQuestionTests.form.feishuTest') }}</button></div>
            </div>
          </fieldset>
        </div>
        <p v-if="noNotificationChannel" role="alert" data-test="no-channel-hint" class="mt-3 rounded bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.scheduledQuestionTests.form.noChannelHint') }}</p>
        <p v-if="crossPlatformBlocking" role="alert" class="mt-3 text-sm text-red-600">{{ t('admin.scheduledQuestionTests.crossPlatform') }}</p>
        <p v-else-if="crossPlatformHint" class="mt-3 text-sm text-amber-600">{{ t(`admin.scheduledQuestionTests.${crossPlatformHint}`) }}</p>
        <p v-else-if="modelLoadFailed" class="mt-3 text-sm text-amber-600">{{ t('admin.scheduledQuestionTests.modelsLoadFailed') }}</p>
        <p v-else-if="modelOptionsForForm.length === 0 && modelMode === 'custom'" class="mt-3 text-sm text-amber-600">{{ t('admin.scheduledQuestionTests.noCommonModels') }}</p>
        <div class="mt-4 flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="closeForm">{{ t('common.cancel') }}</button><button type="button" data-test="save-plan" class="btn btn-primary" :disabled="saving || crossPlatformBlocking || modelLoading" @click="savePlan">{{ t('common.save') }}</button></div>
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
              <span v-if="run.paused_accounts > 0" class="ml-2 rounded bg-amber-50 px-2 py-0.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300" data-test="run-paused">{{ t('admin.scheduledQuestionTests.pausedCount', { count: run.paused_accounts }) }}</span>
              <span v-if="run.resumed_accounts > 0" class="ml-2 rounded bg-green-50 px-2 py-0.5 text-xs text-green-700 dark:bg-green-900/20 dark:text-green-300" data-test="run-resumed">{{ t('admin.scheduledQuestionTests.resumedCount', { count: run.resumed_accounts }) }}</span>
              <span v-if="run.auto_pause_skipped" class="ml-2 rounded bg-amber-50 px-2 py-0.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300" data-test="run-auto-pause-skipped">{{ t('admin.scheduledQuestionTests.autoPauseSkipped') }}</span>
              <span v-if="run.notifications_suppressed" class="ml-2 rounded bg-amber-50 px-2 py-0.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.scheduledQuestionTests.notificationsSuppressed') }}</span>
            </span>
            <span>{{ t('admin.scheduledQuestionTests.runSummary', { passed: run.passed_accounts, alerted: run.alerted_accounts, skipped: run.skipped_accounts ?? 0, total: run.total_accounts }) }}</span>
          </button>
          <div v-if="expandedRuns.has(run.id)" class="mt-2 space-y-2 pl-3 text-xs">
            <div v-if="!run.summary?.length" class="text-gray-500">{{ t('admin.scheduledQuestionTests.noDetails') }}</div>
            <div v-else class="overflow-auto">
              <table :data-test="`run-table-${run.id}`" class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead class="bg-gray-50 dark:bg-dark-700"><tr>
                  <th v-for="column in runTableColumns" :key="column" class="px-2 py-1 text-left font-medium text-gray-500">{{ t(`admin.scheduledQuestionTests.runTable.${column}`) }}</th>
                </tr></thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="item in sortedRunRows(run)" :key="item.account_id" :data-test="`run-account-${item.account_id}`">
                    <td class="px-2 py-1 whitespace-nowrap">{{ item.account_name }} ({{ item.account_id }})</td>
                    <td class="px-2 py-1"><span :data-test="`run-account-result-${item.account_id}`" class="whitespace-nowrap rounded px-1.5 py-0.5" :class="resultBadgeClass(runResultOf(item))">{{ t(`admin.scheduledQuestionTests.results.${runResultOf(item)}`) }}</span></td>
                    <td class="px-2 py-1 whitespace-nowrap">{{ passedJudgedLabel(item) }}</td>
                    <td class="px-2 py-1 whitespace-nowrap">{{ schedulableLabel(item.schedulable_before) }}</td>
                    <td class="px-2 py-1 whitespace-nowrap">{{ schedulableLabel(item.schedulable_after) }}</td>
                    <td class="px-2 py-1">
                      <span v-if="item.schedule_action" :data-test="`run-account-action-${item.account_id}`" class="whitespace-nowrap rounded px-1.5 py-0.5" :class="scheduleActionClass(item.schedule_action)">{{ scheduleActionLabel(item.schedule_action) }}</span>
                      <span v-else>{{ t('admin.scheduledQuestionTests.none') }}</span>
                    </td>
                    <td class="px-2 py-1">
                      <div class="space-y-0.5">
                        <span v-if="item.incomplete" class="mr-1 rounded bg-amber-50 px-1.5 py-0.5 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.scheduledQuestionTests.incompleteMarker') }}</span>
                        <span v-if="item.auto_paused" :data-test="`run-account-auto-paused-${item.account_id}`" class="mr-1 rounded bg-gray-100 px-1.5 py-0.5 text-gray-600 dark:bg-dark-600 dark:text-gray-300">{{ t('admin.scheduledQuestionTests.autoPausedMarker') }}</span>
                        <span v-if="runResultOf(item) === 'skipped'" :data-test="`run-account-skip-${item.account_id}`" class="block">{{ skipDetailLabel(item) }}</span>
                        <span v-if="item.error" :data-test="`run-account-error-${item.account_id}`" class="block break-words text-red-600 dark:text-red-400" :title="item.error">{{ truncateText(item.error) }}</span>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-if="run.notification_results?.length" data-test="run-notifications">
              <div class="mb-1 text-gray-500">{{ t('admin.scheduledQuestionTests.notifications') }}</div>
              <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead class="bg-gray-50 dark:bg-dark-700"><tr>
                  <th v-for="column in notifyTableColumns" :key="column" class="px-2 py-1 text-left font-medium text-gray-500">{{ t(`admin.scheduledQuestionTests.notifyTable.${column}`) }}</th>
                </tr></thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="(result, index) in run.notification_results" :key="`${result.kind}-${result.channel}-${index}`" data-test="run-notification">
                    <td class="px-2 py-1 whitespace-nowrap">{{ notifyKindLabel(result.kind) }}</td>
                    <td class="px-2 py-1 whitespace-nowrap">{{ notifyChannelLabel(result.channel) }}</td>
                    <td class="px-2 py-1"><span class="whitespace-nowrap rounded px-1.5 py-0.5" :class="result.success ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300' : 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'">{{ result.success ? t('admin.scheduledQuestionTests.notificationSuccess') : t('admin.scheduledQuestionTests.notificationFailed') }}</span></td>
                    <td class="px-2 py-1 break-words">{{ result.detail }}</td>
                  </tr>
                </tbody>
              </table>
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
import type { ScheduledQuestionTestFeishuTestRequest, ScheduledQuestionTestNotificationResult, ScheduledQuestionTestPlan, ScheduledQuestionTestRequest, ScheduledQuestionTestResult, ScheduledQuestionTestRun, ScheduledQuestionTestScheduleAction, ScheduledQuestionTestSummary } from '@/api/admin/scheduledQuestionTests'
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
const form = reactive({ name: '', account_ids: [] as number[], question_ids: [] as number[], model_id: '', cron_expression: '', min_pass_ratio: 80, cooldown_minutes: 360, enabled: true, notify_enabled: true, report_enabled: true, email_enabled: true, feishu_enabled: false, auto_pause_enabled: false, auto_resume_enabled: false })
const feishuWebhookInput = ref('')
const feishuSecretInput = ref('')
const feishuWebhookCleared = ref(false)
const feishuSecretCleared = ref(false)
const feishuTesting = ref(false)
const storedWebhookConfigured = computed(() => !!editing.value?.feishu_webhook_configured && !feishuWebhookCleared.value)
const storedSecretConfigured = computed(() => !!editing.value?.feishu_secret_configured && !feishuSecretCleared.value)
const webhookConfigured = computed(() => feishuWebhookInput.value.trim() !== '' || storedWebhookConfigured.value)
const secretConfigured = computed(() => feishuSecretInput.value.trim() !== '' || storedSecretConfigured.value)
const canTestFeishu = computed(() => webhookConfigured.value)
const feishuWebhookPlaceholder = computed(() => storedWebhookConfigured.value
  ? t('admin.scheduledQuestionTests.form.feishuWebhookKeep', { hint: editing.value?.feishu_webhook_hint || '…' })
  : t('admin.scheduledQuestionTests.form.feishuWebhookPlaceholder'))
const feishuSecretPlaceholder = computed(() => storedSecretConfigured.value ? t('admin.scheduledQuestionTests.form.feishuSecretPlaceholder') : '')
const noNotificationChannel = computed(() => !form.email_enabled && !form.feishu_enabled && (form.notify_enabled || form.report_enabled))
const parsedAccountIds = computed(() => [...new Set(accountInput.value.split(/[,\s]+/).map(Number).filter(id => Number.isInteger(id) && id > 0))])
/**
 * 编辑时账号集合是否与计划一致（去重、忽略顺序）。
 * 账号没变就不应该因「共同模型」重新校验失败而阻断保存（用户只改 cron 的典型场景）。
 */
const accountsUnchanged = computed(() => {
  const plan = editing.value
  if (!plan) return false
  const original = new Set(plan.account_ids)
  const current = parsedAccountIds.value
  return current.length === original.size && current.every(id => original.has(id))
})
/**
 * 混合平台阻断保存：只看账号是否变过，与模型模式无关。
 * - 新建：一律阻断（后端 Create 要求同平台）；
 * - 编辑且账号已变：阻断（后端 Update 会重新校验，提前拦住以免点完保存才报错）；
 * - 编辑且账号未变：不阻断，只给非阻断提示。
 */
const crossPlatformBlocking = computed(() =>
  crossPlatform.value && (editing.value === null || !accountsUnchanged.value),
)
/** 混合平台但账号未变：按当前模型模式给非阻断提示。 */
const crossPlatformHint = computed(() => {
  if (!crossPlatform.value || crossPlatformBlocking.value) return ''
  return modelMode.value === 'default' ? 'defaultModePlatformHint' : 'unchangedAccountsModelKept'
})
/**
 * 账号未变时把原 `model_id` 补回可选项：共同模型加载失败也能原样保存（自定义模型模式）。
 */
const modelOptionsForForm = computed(() => {
  const options = modelOptions.value
  const current = form.model_id
  if (!current || !accountsUnchanged.value) return options
  if (options.some(option => option.value === current)) return options
  return [{ value: current, label: current }, ...options]
})
let modalActive = false
let modalVersion = 0
let feishuTestVersion = 0
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

const invalidateFeishuTest = () => { feishuTestVersion += 1; feishuTesting.value = false }
const resetFeishuInputs = () => { invalidateFeishuTest(); feishuWebhookInput.value = ''; feishuSecretInput.value = ''; feishuWebhookCleared.value = false; feishuSecretCleared.value = false }
const resetForm = () => { Object.assign(form, { name: '', account_ids: [], question_ids: [], model_id: '', cron_expression: '', min_pass_ratio: 80, cooldown_minutes: 360, enabled: true, notify_enabled: true, report_enabled: true, email_enabled: true, feishu_enabled: false, auto_pause_enabled: false, auto_resume_enabled: false }); resetFeishuInputs(); accountInput.value = (props.selectedAccountIds ?? []).join(', '); modelMode.value = 'default'; modelOptions.value = []; modelLoadFailed.value = false; crossPlatform.value = false }
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
const openEdit = (plan: ScheduledQuestionTestPlan) => { editing.value = plan; Object.assign(form, { name: plan.name, account_ids: [...plan.account_ids], question_ids: [...plan.question_ids], model_id: plan.model_id, cron_expression: plan.cron_expression, min_pass_ratio: plan.min_pass_ratio, cooldown_minutes: plan.cooldown_minutes, enabled: plan.enabled, notify_enabled: plan.notify_enabled, report_enabled: plan.report_enabled ?? false, email_enabled: plan.email_enabled ?? true, feishu_enabled: plan.feishu_enabled ?? false, auto_pause_enabled: plan.auto_pause_enabled ?? false, auto_resume_enabled: plan.auto_resume_enabled ?? false }); resetFeishuInputs(); accountInput.value = plan.account_ids.join(', '); modelMode.value = plan.model_id ? 'custom' : 'default'; showForm.value = true }
const closeForm = () => { modelLoadVersion += 1; modelLoading.value = false; resetFeishuInputs(); showForm.value = false; editing.value = null }
const clearFeishuWebhook = () => { feishuWebhookInput.value = ''; feishuWebhookCleared.value = true }
const clearFeishuSecret = () => { feishuSecretInput.value = ''; feishuSecretCleared.value = true }
const buildRequest = (): ScheduledQuestionTestRequest => {
  const request: ScheduledQuestionTestRequest = {
    name: form.name.trim(), account_ids: parsedAccountIds.value, question_ids: [...form.question_ids], model_id: modelMode.value === 'custom' ? form.model_id : '',
    cron_expression: form.cron_expression.trim(), min_pass_ratio: form.min_pass_ratio, cooldown_minutes: form.cooldown_minutes, enabled: form.enabled,
    notify_enabled: form.notify_enabled, report_enabled: form.report_enabled, email_enabled: form.email_enabled, feishu_enabled: form.feishu_enabled,
    auto_pause_enabled: form.auto_pause_enabled, auto_resume_enabled: form.auto_resume_enabled
  }
  const webhook = feishuWebhookInput.value.trim()
  if (webhook) request.feishu_webhook_url = webhook
  else if (feishuWebhookCleared.value) request.feishu_webhook_url = ''
  const secret = feishuSecretInput.value.trim()
  if (secret) request.feishu_secret = secret
  else if (feishuSecretCleared.value) request.feishu_secret = ''
  return request
}
const testFeishu = async () => {
  if (feishuTesting.value || !canTestFeishu.value) return
  const version = feishuTestVersion
  const request: ScheduledQuestionTestFeishuTestRequest = {}
  if (editing.value) request.plan_id = editing.value.id
  const webhook = feishuWebhookInput.value.trim()
  if (webhook) request.webhook_url = webhook
  const secret = feishuSecretInput.value.trim()
  if (secret) request.secret = secret
  else if (feishuSecretCleared.value) request.secret = ''
  feishuTesting.value = true
  try {
    await adminAPI.scheduledQuestionTests.testFeishu(request)
    if (version !== feishuTestVersion || !modalActive) return
    appStore.showSuccess(t('admin.scheduledQuestionTests.feishuTestSuccess'))
  } catch (error) {
    if (version !== feishuTestVersion || !modalActive) return
    appStore.showError(extractApiErrorMessage(error, t('admin.scheduledQuestionTests.feishuTestFailed')))
  } finally {
    if (version === feishuTestVersion) feishuTesting.value = false
  }
}
const SCHEDULE_ACTION_KEYS: Record<string, string> = { paused: 'paused', resumed: 'resumed', pause_failed: 'pauseFailed', resume_failed: 'resumeFailed', resume_blocked: 'resumeBlocked' }
const runTableColumns = ['account', 'result', 'passedJudged', 'schedulableBefore', 'schedulableAfter', 'action', 'detail']
const notifyTableColumns = ['kind', 'channel', 'result', 'detail']
const BADGE_GREEN = 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300'
const BADGE_RED = 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'
const BADGE_AMBER = 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'
const BADGE_GRAY = 'bg-gray-100 text-gray-600 dark:bg-dark-600 dark:text-gray-300'
const RESULT_BADGE_CLASSES: Record<ScheduledQuestionTestResult, string> = {
  passed: BADGE_GREEN, failed: BADGE_RED, error: BADGE_RED, unjudged: BADGE_AMBER, incomplete: BADGE_AMBER, skipped: BADGE_GRAY
}
const SCHEDULE_ACTION_BADGE_CLASSES: Record<string, string> = {
  paused: BADGE_RED, resumed: BADGE_GREEN, pause_failed: BADGE_AMBER, resume_failed: BADGE_AMBER, resume_blocked: BADGE_GRAY
}
// Runs recorded before v0.3.8 have no `result`; derive it from the legacy fields (contract §4.2 order).
const runResultOf = (item: ScheduledQuestionTestSummary): ScheduledQuestionTestResult => {
  if (item.result) return item.result
  if (item.incomplete) return 'incomplete'
  if (item.judged === 0 && item.error) return 'error'
  if (item.ratio === null || item.ratio === undefined) return 'unjudged'
  return item.alerting ? 'failed' : 'passed'
}
const resultBadgeClass = (result: ScheduledQuestionTestResult) => RESULT_BADGE_CLASSES[result] ?? BADGE_GRAY
const RUN_ROW_GROUPS: Record<ScheduledQuestionTestResult, number> = { passed: 3, failed: 1, error: 1, skipped: 2, unjudged: 3, incomplete: 3 }
const runRowGroup = (item: ScheduledQuestionTestSummary) => (item.schedule_action ? 0 : RUN_ROW_GROUPS[runResultOf(item)])
// Order: accounts with a scheduling action → failed/error → skipped → the rest; ties by account ID (contract §5.1).
const sortedRunRows = (run: ScheduledQuestionTestRun) => [...(run.summary ?? [])].sort((left, right) => runRowGroup(left) - runRowGroup(right) || left.account_id - right.account_id)
const passedJudgedLabel = (item: ScheduledQuestionTestSummary) => {
  if (runResultOf(item) === 'skipped' || !item.judged) return t('admin.scheduledQuestionTests.none')
  return item.ratio === null || item.ratio === undefined
    ? `${item.passed}/${item.judged}`
    : `${item.passed}/${item.judged} (${item.ratio}%)`
}
const schedulableLabel = (value?: boolean | null) => value === true
  ? t('admin.scheduledQuestionTests.schedulableOn')
  : value === false ? t('admin.scheduledQuestionTests.schedulableOff') : t('admin.scheduledQuestionTests.none')
const formatSkipResetAt = (value?: string | null) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}
const skipDetailLabel = (item: ScheduledQuestionTestSummary) => {
  const reason = item.skip_reason ? t(`admin.scheduledQuestionTests.skipReasons.${item.skip_reason}`) : ''
  const resetAt = formatSkipResetAt(item.skip_reset_at)
  // Only an exhausted quota needs an admin to act. An unknown rate-limit / window-cost recovery time just
  // means the next round retries, so we must not tell the admin to change the quota there (contract v1.1 U3).
  const recovery = resetAt
    ? t('admin.scheduledQuestionTests.skipResetAt', { time: resetAt })
    : item.skip_reason === 'quota_exceeded'
      ? t('admin.scheduledQuestionTests.skipNeedsAdmin')
      : t('admin.scheduledQuestionTests.skipResetUnknown')
  return reason ? `${reason} · ${recovery}` : recovery
}
const truncateText = (value: string, limit = 120) => value.length > limit ? `${value.slice(0, limit)}…` : value
const planBadges = (plan: ScheduledQuestionTestPlan) => [
  plan.auto_pause_enabled ? 'autoPauseBadge' : '',
  plan.auto_resume_enabled ? 'autoResumeBadge' : '',
  plan.feishu_enabled ? 'feishuBadge' : '',
  plan.report_enabled ? 'reportBadge' : ''
].filter(Boolean)
const scheduleActionLabel = (action: ScheduledQuestionTestScheduleAction) => action ? t(`admin.scheduledQuestionTests.scheduleActions.${SCHEDULE_ACTION_KEYS[action] ?? action}`) : ''
const scheduleActionClass = (action: ScheduledQuestionTestScheduleAction) => SCHEDULE_ACTION_BADGE_CLASSES[action] ?? BADGE_GRAY
const notifyKindLabel = (kind: ScheduledQuestionTestNotificationResult['kind']) => t(`admin.scheduledQuestionTests.notifyKinds.${kind}`)
const notifyChannelLabel = (channel: ScheduledQuestionTestNotificationResult['channel']) => t(`admin.scheduledQuestionTests.notifyChannels.${channel}`)
const savePlan = async () => {
  const ids = parsedAccountIds.value
  const cron = form.cron_expression.trim()
  if (!form.name.trim() || ids.length === 0 || form.question_ids.length === 0 || cron.split(/\s+/).length !== 5 || form.min_pass_ratio < 0 || form.min_pass_ratio > 100 || form.cooldown_minutes < 0 || form.cooldown_minutes > 10080 || (modelMode.value === 'custom' && !form.model_id)) { errorMessage.value = t('admin.scheduledQuestionTests.validation'); return }
  if (form.feishu_enabled && !webhookConfigured.value) { errorMessage.value = t('admin.scheduledQuestionTests.form.feishuWebhookRequired'); return }
  errorMessage.value = ''
  saving.value = true
  try {
    const request = buildRequest()
    if (editing.value) await adminAPI.scheduledQuestionTests.update(editing.value.id, request)
    else await adminAPI.scheduledQuestionTests.create(request)
    closeForm(); await load()
  } catch (error) { errorMessage.value = extractApiErrorMessage(error, t('admin.scheduledQuestionTests.saveFailed')) } finally { saving.value = false }
}
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
  invalidateFeishuTest()
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
