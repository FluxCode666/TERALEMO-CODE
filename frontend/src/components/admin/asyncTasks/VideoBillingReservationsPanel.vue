<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <label for="vbr-state" class="text-sm text-gray-600 dark:text-gray-300">{{ t('adminVideoReservations.state') }}</label>
      <select id="vbr-state" v-model="state" class="input w-auto" @change="changeState">
        <option v-for="value in states" :key="value" :value="value">{{ t(`adminVideoReservations.states.${value}`) }}</option>
      </select>
      <button type="button" class="btn btn-primary" :disabled="loading" @click="load(true)">{{ t('common.refresh') }}</button>
    </div>

    <div v-if="forbidden" role="alert" class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-700 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300">
      {{ t('adminVideoReservations.errors.forbidden') }}
    </div>
    <div v-else-if="error" role="alert" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
      {{ error }} <button type="button" class="ml-2 underline" @click="load(true)">{{ t('common.tryAgain') }}</button>
    </div>
    <div v-if="notice" role="status" class="text-sm text-green-700 dark:text-green-400">{{ notice }}</div>

    <div class="card overflow-hidden" :aria-busy="loading">
      <div v-if="loading && !items.length" role="status" class="space-y-4 p-6">
        <span class="sr-only">{{ t('common.loading') }}</span>
        <div v-for="index in 4" :key="index" class="h-12 animate-pulse rounded bg-gray-100 dark:bg-dark-700" />
      </div>
      <div v-else-if="!items.length && !error && !forbidden" role="status" class="px-6 py-16 text-center">
        <h2 class="font-medium text-gray-900 dark:text-gray-100">{{ t('adminVideoReservations.empty') }}</h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('adminVideoReservations.emptyHint') }}</p>
      </div>
      <div v-else-if="items.length" class="overflow-x-auto">
        <table class="w-full min-w-[1400px] text-left text-sm">
          <thead class="whitespace-nowrap border-b border-gray-200 bg-gray-50 text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.id') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.userId') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.apiKeyId') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.groupId') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.protocol') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.model') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.billingMode') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.holdAmount') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.actualAmount') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.multiplier') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.attempts') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.reviewNote') }}</th>
              <th class="px-4 py-3">{{ t('adminVideoReservations.columns.timestamps') }}</th>
              <th class="px-4 py-3">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="row in items" :key="row.id">
              <td class="max-w-[200px] px-4 py-4">
                <div class="break-all font-mono text-xs text-gray-800 dark:text-gray-200">{{ row.id }}</div>
                <span v-if="!row.task_id" class="mt-1 inline-block rounded-md bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">{{ t('adminVideoReservations.noTaskId') }}</span>
              </td>
              <td class="px-4 py-4 text-gray-600 dark:text-gray-300">#{{ row.user_id }}</td>
              <td class="px-4 py-4 text-gray-600 dark:text-gray-300">#{{ row.api_key_id }}</td>
              <td class="px-4 py-4 text-gray-600 dark:text-gray-300">#{{ row.group_id }}</td>
              <td class="px-4 py-4 text-gray-600 dark:text-gray-300">
                <div>{{ row.protocol || '—' }}</div>
                <div class="mt-1 text-xs text-gray-500">{{ row.platform || '—' }}</div>
              </td>
              <td class="max-w-[160px] px-4 py-4 text-gray-600 dark:text-gray-300">
                <div class="break-all">{{ row.model || '—' }}</div>
                <div class="mt-1 text-xs text-gray-500">{{ row.resolution || '—' }}</div>
              </td>
              <td class="whitespace-nowrap px-4 py-4 text-gray-600 dark:text-gray-300">{{ billingModeLabel(row.billing_mode) }}</td>
              <td class="whitespace-nowrap px-4 py-4 text-gray-800 dark:text-gray-200">{{ formatAmount(row.hold_amount) }}</td>
              <td class="whitespace-nowrap px-4 py-4 text-gray-800 dark:text-gray-200">{{ formatAmount(row.actual_amount) }}</td>
              <td class="whitespace-nowrap px-4 py-4 text-gray-600 dark:text-gray-300">{{ formatMultiplier(row.effective_multiplier) }}</td>
              <td class="px-4 py-4 text-gray-600 dark:text-gray-300">{{ row.attempts }}</td>
              <td class="max-w-[240px] px-4 py-4">
                <span class="block truncate text-gray-600 dark:text-gray-300" :title="row.review_note || ''">{{ row.review_note || '—' }}</span>
              </td>
              <td class="whitespace-nowrap px-4 py-4 text-gray-500 dark:text-gray-400">
                {{ formatTime(row.created_at) }}
                <div class="mt-1 text-xs">{{ t('adminVideoReservations.updatedAt') }} {{ formatTime(row.updated_at) }}</div>
              </td>
              <td class="whitespace-nowrap px-4 py-4">
                <template v-if="row.state === 'manual'">
                  <button type="button" class="btn btn-ghost btn-sm" data-testid="vbr-settle" @click="openDialog('settle', row)">{{ t('adminVideoReservations.actions.settle') }}</button>
                  <button type="button" class="btn btn-ghost btn-sm" data-testid="vbr-release" @click="openDialog('release', row)">{{ t('adminVideoReservations.actions.release') }}</button>
                </template>
                <span v-else class="text-xs text-gray-400">{{ t(`adminVideoReservations.states.${row.state}`) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination v-if="total > 0" :page="page" :page-size="pageSize" :total="total" :show-page-size-selector="false" @update:page="changePage" />
    </div>

    <BaseDialog :show="dialogAction !== null" :title="dialogTitle" width="normal" @close="closeDialog">
      <div v-if="target" class="space-y-4">
        <dl class="grid gap-2 text-sm sm:grid-cols-2">
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('adminVideoReservations.columns.id') }}</dt><dd class="break-all font-mono text-xs">{{ target.id }}</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('adminVideoReservations.columns.holdAmount') }}</dt><dd>{{ formatAmount(target.hold_amount) }}</dd></div>
        </dl>
        <p v-if="dialogAction === 'release'" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-700 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300">
          {{ t('adminVideoReservations.release.warning') }}
        </p>
        <p v-else class="rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm text-blue-700 dark:border-blue-900 dark:bg-blue-950 dark:text-blue-300">
          {{ t('adminVideoReservations.settle.cap', { amount: formatAmount(target.hold_amount) }) }}
        </p>

        <template v-if="step === 'form'">
          <div v-if="dialogAction === 'settle' && target.billing_mode === 'video'" class="grid gap-4 sm:grid-cols-2">
            <div>
              <label for="vbr-duration" class="input-label">{{ t('adminVideoReservations.settle.durationSeconds') }}</label>
              <input id="vbr-duration" v-model="durationInput" type="number" min="0" step="any" class="input" />
            </div>
            <div>
              <label for="vbr-count" class="input-label">{{ t('adminVideoReservations.settle.videoCount') }}</label>
              <input id="vbr-count" v-model="countInput" type="number" min="1" step="1" class="input" />
            </div>
          </div>
          <div v-if="dialogAction === 'settle' && target.billing_mode === 'token'">
            <label for="vbr-tokens" class="input-label">{{ t('adminVideoReservations.settle.completionTokens') }}</label>
            <input id="vbr-tokens" v-model="tokensInput" type="number" min="0" step="1" class="input" />
          </div>
          <div>
            <label for="vbr-reason" class="input-label">{{ t('adminVideoReservations.form.reason') }}</label>
            <textarea id="vbr-reason" v-model="reason" rows="3" maxlength="1000" class="input" :placeholder="t('adminVideoReservations.form.reasonPlaceholder')" />
          </div>
          <p v-if="formError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ formError }}</p>
        </template>

        <div v-else class="rounded-lg border border-gray-200 p-3 text-sm text-gray-700 dark:border-dark-700 dark:text-gray-300">
          <p>{{ dialogAction === 'release' ? t('adminVideoReservations.release.confirmHint') : t('adminVideoReservations.settle.confirmHint') }}</p>
          <p class="mt-2 break-words"><span class="text-gray-500">{{ t('adminVideoReservations.form.reason') }}:</span> {{ trimmedReason }}</p>
          <p v-if="dialogAction === 'settle' && usageSummary" class="mt-1 break-words"><span class="text-gray-500">{{ t('adminVideoReservations.settle.usage') }}:</span> {{ usageSummary }}</p>
        </div>

        <p v-if="actionError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ actionError }}</p>
      </div>

      <template #footer>
        <div class="flex justify-end gap-2">
          <button v-if="step === 'confirm'" type="button" class="btn btn-secondary" :disabled="submitting" @click="step = 'form'">{{ t('adminVideoReservations.form.back') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="submitting" @click="closeDialog">{{ t('common.cancel') }}</button>
          <button v-if="step === 'form'" type="button" class="btn btn-primary" data-testid="vbr-continue" @click="continueToConfirm">{{ t('adminVideoReservations.form.continue') }}</button>
          <button v-else type="button" class="btn btn-primary" data-testid="vbr-submit" :disabled="submitting" @click="submit">
            {{ submitting ? t('common.submitting') : t('adminVideoReservations.form.confirm') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  adminVideoBillingReservationsAPI,
  VIDEO_BILLING_RESERVATION_STATES,
  type VideoBillingReservation,
  type VideoBillingReservationState,
  type VideoBillingSettlePayload
} from '@/api/admin/videoBillingReservations'

const { t } = useI18n()

const MAX_INT = 2147483647
const MAX_REASON_LENGTH = 1000

const states = VIDEO_BILLING_RESERVATION_STATES
const state = ref<VideoBillingReservationState>('manual')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const items = ref<VideoBillingReservation[]>([])
const loading = ref(false)
const error = ref('')
const forbidden = ref(false)
const notice = ref('')

const dialogAction = ref<'release' | 'settle' | null>(null)
const target = ref<VideoBillingReservation | null>(null)
const step = ref<'form' | 'confirm'>('form')
const reason = ref('')
const durationInput = ref<string | number>('')
const countInput = ref<string | number>('')
const tokensInput = ref<string | number>('')
const formError = ref('')
const actionError = ref('')
const submitting = ref(false)

const controller = new AbortController()
let generation = 0

const dialogTitle = computed(() => dialogAction.value === 'release' ? t('adminVideoReservations.release.title') : t('adminVideoReservations.settle.title'))
const trimmedReason = computed(() => reason.value.trim())
const usageSummary = computed(() => {
  if (!target.value) return ''
  if (target.value.billing_mode === 'video') return `${t('adminVideoReservations.settle.durationSeconds')}: ${durationInput.value}, ${t('adminVideoReservations.settle.videoCount')}: ${countInput.value}`
  if (target.value.billing_mode === 'token') return `${t('adminVideoReservations.settle.completionTokens')}: ${tokensInput.value}`
  return t('adminVideoReservations.settle.noUsage')
})

function formatAmount(value: number | null | undefined) { return `$${(value ?? 0).toFixed(6)}` }
function formatMultiplier(value: number | null | undefined) { return value ? `×${value}` : '—' }
function formatTime(value: string) {
  const time = new Date(value)
  return Number.isNaN(time.getTime()) ? '—' : time.toLocaleString()
}
function billingModeLabel(mode?: string) {
  if (!mode) return '—'
  return ['video', 'token', 'per_request'].includes(mode) ? t(`adminVideoReservations.billingModes.${mode}`) : mode
}

function apiErrorMessage(err: unknown): string {
  const status = (err as { status?: number } | null)?.status
  switch (status) {
    case 400: return t('adminVideoReservations.errors.invalid')
    case 401: return t('adminVideoReservations.errors.unauthorized')
    case 403: return t('adminVideoReservations.errors.forbidden')
    case 404: return t('adminVideoReservations.errors.notFound')
    case 409: return t('adminVideoReservations.errors.conflict')
    case 503: return t('adminVideoReservations.errors.unavailable')
    default: return t('adminVideoReservations.errors.generic')
  }
}

async function load(manual = false) {
  const current = ++generation
  loading.value = true
  error.value = ''
  if (manual) notice.value = ''
  try {
    const result = await adminVideoBillingReservationsAPI.list({ state: state.value, page: page.value, page_size: pageSize }, controller.signal)
    if (current !== generation) return
    items.value = result.items
    total.value = result.total
    forbidden.value = false
  } catch (err) {
    if (current === generation && !controller.signal.aborted) {
      if ((err as { status?: number })?.status === 403) {
        forbidden.value = true
        items.value = []
        total.value = 0
      } else {
        error.value = apiErrorMessage(err)
      }
    }
  } finally { if (current === generation) loading.value = false }
}
function changeState() { page.value = 1; void load(true) }
function changePage(value: number) { page.value = value; void load() }

function openDialog(action: 'release' | 'settle', row: VideoBillingReservation) {
  dialogAction.value = action
  target.value = row
  step.value = 'form'
  reason.value = ''
  durationInput.value = ''
  countInput.value = ''
  tokensInput.value = ''
  formError.value = ''
  actionError.value = ''
}
function closeDialog() {
  if (submitting.value) return
  dialogAction.value = null
  target.value = null
}

function validateForm(): string {
  const trimmed = trimmedReason.value
  if (!trimmed) return t('adminVideoReservations.form.reasonRequired')
  if ([...trimmed].length > MAX_REASON_LENGTH) return t('adminVideoReservations.form.reasonTooLong')
  if (dialogAction.value !== 'settle' || !target.value) return ''
  const mode = target.value.billing_mode
  if (mode === 'video') {
    const rawDuration = String(durationInput.value).trim()
    const duration = Number(rawDuration)
    if (!rawDuration || !Number.isFinite(duration) || duration <= 0 || duration > MAX_INT) return t('adminVideoReservations.form.durationInvalid')
    const rawCount = String(countInput.value).trim()
    const count = Number(rawCount)
    if (!rawCount || !Number.isInteger(count) || count < 1 || count > MAX_INT) return t('adminVideoReservations.form.videoCountInvalid')
  } else if (mode === 'token') {
    const rawTokens = String(tokensInput.value).trim()
    const tokens = Number(rawTokens)
    if (!rawTokens || !Number.isInteger(tokens) || tokens < 0 || tokens > MAX_INT) return t('adminVideoReservations.form.tokensInvalid')
  }
  return ''
}

function continueToConfirm() {
  formError.value = validateForm()
  if (!formError.value) step.value = 'confirm'
}

async function submit() {
  if (!target.value || !dialogAction.value || submitting.value) return
  formError.value = validateForm()
  if (formError.value) { step.value = 'form'; return }
  submitting.value = true
  actionError.value = ''
  const id = target.value.id
  const action = dialogAction.value
  try {
    if (action === 'release') {
      await adminVideoBillingReservationsAPI.release(id, trimmedReason.value, controller.signal)
    } else {
      const payload: VideoBillingSettlePayload = { reason: trimmedReason.value }
      if (target.value.billing_mode === 'video') {
        payload.duration_seconds = Number(durationInput.value)
        payload.video_count = Number(countInput.value)
      } else if (target.value.billing_mode === 'token') {
        payload.completion_tokens = Number(tokensInput.value)
      }
      await adminVideoBillingReservationsAPI.settle(id, payload, controller.signal)
    }
    notice.value = action === 'release' ? t('adminVideoReservations.released') : t('adminVideoReservations.settled')
    dialogAction.value = null
    target.value = null
    await load()
  } catch (err) {
    if (!controller.signal.aborted) actionError.value = apiErrorMessage(err)
  } finally { submitting.value = false }
}

onMounted(() => { void load() })
onUnmounted(() => { generation++; controller.abort() })
</script>
