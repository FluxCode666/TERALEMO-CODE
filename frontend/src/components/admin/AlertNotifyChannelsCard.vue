<template>
  <div class="card">
    <!-- Header -->
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div class="flex items-center justify-between gap-4">
        <div>
          <h3 class="text-base font-medium text-gray-900 dark:text-white">
            {{ t('admin.settings.alertNotify.title') }}
          </h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.settings.alertNotify.description') }}
          </p>
        </div>
        <button
          type="button"
          class="btn btn-primary btn-sm shrink-0"
          data-test="btn-add-channel"
          @click="openCreate"
        >
          + {{ t('admin.settings.alertNotify.addChannel') }}
        </button>
      </div>
    </div>

    <div class="px-6 py-6">
      <!-- Loading -->
      <div v-if="loading" class="flex items-center justify-center py-8" data-test="channels-loading">
        <div class="h-6 w-6 animate-spin rounded-full border-2 border-primary-500 border-t-transparent" />
      </div>

      <!-- Empty state -->
      <EmptyState
        v-else-if="channels.length === 0"
        :title="t('admin.settings.alertNotify.emptyTitle')"
        :description="t('admin.settings.alertNotify.emptyDescription')"
        :action-text="t('admin.settings.alertNotify.addChannel')"
        @action="openCreate"
      />

      <!-- Channel list -->
      <div v-else class="space-y-3">
        <div
          v-for="channel in channels"
          :key="channel.id"
          class="flex flex-wrap items-center gap-3 rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-700"
          :data-test="`channel-row-${channel.id}`"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="truncate text-sm font-medium text-gray-900 dark:text-white">
                {{ channel.name }}
              </span>
              <span
                class="inline-flex shrink-0 items-center rounded-full bg-primary-50 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
              >
                {{ typeLabel(channel.type) }}
              </span>
            </div>
            <p
              class="mt-1 truncate font-mono text-xs text-gray-500 dark:text-gray-400"
              :title="channel.url_masked"
            >
              {{ channel.url_masked }}
            </p>
            <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
              <span
                v-for="event in channel.events"
                :key="event"
                class="inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700"
              >
                {{ eventLabel(event) }}
              </span>
              <span>{{ t('admin.settings.alertNotify.minSeverityLabel') }}: {{ severityLabel(channel.min_severity) }}</span>
              <span v-if="channel.send_resolved">{{ t('admin.settings.alertNotify.sendResolvedBadge') }}</span>
            </div>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <Toggle
              :model-value="channel.enabled"
              :data-test="`toggle-${channel.id}`"
              @update:model-value="(value: boolean) => onToggleEnabled(channel, value)"
            />
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="testingId === channel.id"
              :data-test="`btn-test-${channel.id}`"
              @click="onTestRow(channel)"
            >
              <span
                v-if="testingId === channel.id"
                class="mr-1 inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent align-middle"
              />
              {{ t('admin.settings.alertNotify.test') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :data-test="`btn-edit-${channel.id}`"
              @click="openEdit(channel)"
            >
              {{ t('admin.settings.alertNotify.edit') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300"
              :data-test="`btn-delete-${channel.id}`"
              @click="requestDelete(channel)"
            >
              {{ t('admin.settings.alertNotify.delete') }}
            </button>
          </div>
        </div>
      </div>

      <!-- Collapsible webhook format help -->
      <div class="mt-6 rounded-lg border border-gray-200 dark:border-dark-700">
        <button
          type="button"
          class="flex w-full items-center justify-between px-4 py-3 text-left text-sm font-medium text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-dark-800"
          data-test="btn-format-help"
          @click="showFormatHelp = !showFormatHelp"
        >
          <span>{{ t('admin.settings.alertNotify.formatHelpTitle') }}</span>
          <Icon
            name="chevronDown"
            size="md"
            :class="['transition-transform duration-200', showFormatHelp && 'rotate-180']"
          />
        </button>
        <div v-if="showFormatHelp" class="border-t border-gray-200 px-4 py-4 text-xs text-gray-600 dark:border-dark-700 dark:text-gray-400">
          <p class="font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.settings.alertNotify.formatBodyTitle') }}
          </p>
          <pre class="mt-2 overflow-x-auto rounded bg-gray-50 p-3 font-mono dark:bg-dark-800">{
  "version": "1",
  "event": "ops.alert",
  "status": "firing",
  "severity": "critical",
  "title": "...",
  "summary": "...",
  "fields": [{"label": "...", "value": "..."}],
  "occurred_at": "2026-11-22T14:00:00Z",
  "site_name": "...",
  "source": {"type": "ops_alert", "id": "123"},
  "test": false
}</pre>
          <p class="mt-3 font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.settings.alertNotify.formatHeadersTitle') }}
          </p>
          <ul class="mt-1 list-inside list-disc space-y-0.5 font-mono">
            <li>X-Webhook-Event</li>
            <li>X-Webhook-Timestamp</li>
            <li>X-Webhook-Signature</li>
          </ul>
          <p class="mt-3 font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.settings.alertNotify.formatSignatureTitle') }}
          </p>
          <code class="mt-1 block overflow-x-auto rounded bg-gray-50 p-2 font-mono dark:bg-dark-800">sha256=hex(HMAC-SHA256(secret, timestamp + "." + body))</code>
        </div>
      </div>
    </div>

    <!-- Editor dialog -->
    <BaseDialog
      :show="editorVisible"
      :title="isEditing ? t('admin.settings.alertNotify.editTitle') : t('admin.settings.alertNotify.createTitle')"
      width="wide"
      @close="closeEditor"
    >
      <div class="space-y-4" data-test="channel-editor">
        <div v-if="formError" class="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-900/30 dark:text-red-300" data-test="form-error">
          {{ formError }}
        </div>

        <!-- Name -->
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300" for="alert-notify-name">
            {{ t('admin.settings.alertNotify.form.name') }} *
          </label>
          <input
            id="alert-notify-name"
            v-model="form.name"
            type="text"
            maxlength="64"
            class="input"
            data-test="input-name"
            :placeholder="t('admin.settings.alertNotify.form.namePlaceholder')"
          />
        </div>

        <!-- Type -->
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300" for="alert-notify-type">
            {{ t('admin.settings.alertNotify.form.type') }}
          </label>
          <Select
            id="alert-notify-type"
            v-model="form.type"
            :options="typeOptions"
            data-test="select-type"
          />
        </div>

        <!-- URL -->
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300" for="alert-notify-url">
            {{ t('admin.settings.alertNotify.form.url') }}
            <span v-if="!isEditing">*</span>
          </label>
          <input
            id="alert-notify-url"
            v-model="form.url"
            type="text"
            class="input font-mono"
            data-test="input-url"
            :placeholder="isEditing && editingChannel ? editingChannel.url_masked : 'https://'"
          />
          <p v-if="isEditing" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.settings.alertNotify.form.urlKeepHint') }}
          </p>
        </div>

        <!-- Secret -->
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300" for="alert-notify-secret">
            {{ t('admin.settings.alertNotify.form.secret') }}
          </label>
          <input
            id="alert-notify-secret"
            v-model="form.secret"
            type="password"
            maxlength="256"
            class="input font-mono"
            data-test="input-secret"
            :placeholder="isEditing && editingChannel?.secret_configured
              ? t('admin.settings.alertNotify.form.secretConfiguredPlaceholder')
              : ''"
          />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ form.type === 'feishu'
              ? t('admin.settings.alertNotify.form.secretHintFeishu')
              : t('admin.settings.alertNotify.form.secretHintWebhook') }}
          </p>
          <label
            v-if="isEditing && editingChannel?.secret_configured"
            class="mt-2 flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"
          >
            <input v-model="form.clear_secret" type="checkbox" class="rounded" data-test="checkbox-clear-secret" />
            {{ t('admin.settings.alertNotify.form.clearSecret') }}
          </label>
        </div>

        <!-- Events -->
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.settings.alertNotify.form.events') }} *
          </label>
          <div class="mt-1 space-y-2">
            <label
              v-for="event in supportedEvents"
              :key="event"
              class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"
            >
              <input
                type="checkbox"
                class="rounded"
                :data-test="`checkbox-event-${event}`"
                :checked="form.events.includes(event)"
                @change="toggleFormEvent(event)"
              />
              {{ eventLabel(event) }}
            </label>
          </div>
        </div>

        <!-- Min severity -->
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300" for="alert-notify-severity">
            {{ t('admin.settings.alertNotify.form.minSeverity') }}
          </label>
          <Select
            id="alert-notify-severity"
            v-model="form.min_severity"
            :options="severityOptions"
            data-test="select-severity"
          />
        </div>

        <!-- Send resolved -->
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.settings.alertNotify.form.sendResolved') }}
            </label>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.alertNotify.form.sendResolvedHint') }}
            </p>
          </div>
          <Toggle v-model="form.send_resolved" data-test="toggle-send-resolved" />
        </div>

        <!-- Enabled -->
        <div class="flex items-center justify-between gap-4">
          <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t('admin.settings.alertNotify.form.enabled') }}
          </label>
          <Toggle v-model="form.enabled" data-test="toggle-enabled" />
        </div>
      </div>

      <template #footer>
        <div class="flex items-center justify-between gap-3">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="testingForm || saving"
            data-test="btn-test-form"
            @click="onTestForm"
          >
            <span
              v-if="testingForm"
              class="mr-1 inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent align-middle"
            />
            {{ t('admin.settings.alertNotify.sendTest') }}
          </button>
          <div class="flex items-center gap-3">
            <button type="button" class="btn btn-secondary" data-test="btn-cancel" @click="closeEditor">
              {{ t('common.cancel') }}
            </button>
            <button
              type="button"
              class="btn btn-primary"
              :disabled="saving"
              data-test="btn-save"
              @click="onSave"
            >
              <span
                v-if="saving"
                class="mr-1 inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent align-middle"
              />
              {{ t('common.save') }}
            </button>
          </div>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete confirm -->
    <ConfirmDialog
      :show="deleteTarget !== null"
      :title="t('admin.settings.alertNotify.deleteTitle')"
      :message="t('admin.settings.alertNotify.deleteConfirm', { name: deleteTarget?.name ?? '' })"
      :confirm-text="t('common.delete')"
      danger
      @confirm="onConfirmDelete"
      @cancel="deleteTarget = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { alertNotifyAPI } from '@/api/admin/alertNotify'
import type {
  AlertNotifyChannelInput,
  AlertNotifyChannelType,
  AlertNotifyChannelView,
  AlertNotifyEvent,
  AlertNotifySeverity
} from '@/api/admin/alertNotify'
import { extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const OPS_ALERT_EVENT: AlertNotifyEvent = 'ops.alert'
const QUOTA_ALERT_EVENT: AlertNotifyEvent = 'account.quota_alert'
const DEFAULT_EVENTS: AlertNotifyEvent[] = [OPS_ALERT_EVENT, QUOTA_ALERT_EVENT]

const loading = ref(false)
const saving = ref(false)
const channels = ref<AlertNotifyChannelView[]>([])
const supportedEvents = ref<AlertNotifyEvent[]>([...DEFAULT_EVENTS])

const editorVisible = ref(false)
const editingChannel = ref<AlertNotifyChannelView | null>(null)
const formError = ref('')
const testingForm = ref(false)
const testingId = ref<string | null>(null)
const deleteTarget = ref<AlertNotifyChannelView | null>(null)
const showFormatHelp = ref(false)

interface EditorForm {
  name: string
  type: AlertNotifyChannelType
  url: string
  secret: string
  clear_secret: boolean
  events: AlertNotifyEvent[]
  min_severity: AlertNotifySeverity | ''
  send_resolved: boolean
  enabled: boolean
}

const form = reactive<EditorForm>({
  name: '',
  type: 'feishu',
  url: '',
  secret: '',
  clear_secret: false,
  events: [...DEFAULT_EVENTS],
  min_severity: '',
  send_resolved: false,
  enabled: true
})

const isEditing = computed(() => editingChannel.value !== null)

const typeOptions = computed<SelectOption[]>(() => [
  { value: 'feishu', label: t('admin.settings.alertNotify.types.feishu') },
  { value: 'webhook', label: t('admin.settings.alertNotify.types.webhook') }
])

const severityOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('admin.settings.alertNotify.severity.all') },
  { value: 'info', label: t('admin.settings.alertNotify.severity.info') },
  { value: 'warning', label: t('admin.settings.alertNotify.severity.warning') },
  { value: 'critical', label: t('admin.settings.alertNotify.severity.critical') }
])

function typeLabel(type: AlertNotifyChannelType): string {
  return t(`admin.settings.alertNotify.types.${type}`)
}

function eventLabel(event: AlertNotifyEvent): string {
  return event === OPS_ALERT_EVENT
    ? t('admin.settings.alertNotify.events.opsAlert')
    : t('admin.settings.alertNotify.events.accountQuota')
}

function severityLabel(severity: AlertNotifySeverity | ''): string {
  return t(`admin.settings.alertNotify.severity.${severity === '' ? 'all' : severity}`)
}

function applyResponse(response: { channels: AlertNotifyChannelView[]; supported_events?: AlertNotifyEvent[] }) {
  channels.value = response.channels ?? []
  if (Array.isArray(response.supported_events) && response.supported_events.length > 0) {
    supportedEvents.value = [...response.supported_events]
  }
}

/** 未改动的渠道以 url/secret 为空的形式提交，后端保留原值 */
function preserveInput(channel: AlertNotifyChannelView): AlertNotifyChannelInput {
  return {
    id: channel.id,
    name: channel.name,
    type: channel.type,
    enabled: channel.enabled,
    url: '',
    secret: '',
    clear_secret: false,
    events: [...channel.events],
    min_severity: channel.min_severity,
    send_resolved: channel.send_resolved
  }
}

function formInput(): AlertNotifyChannelInput {
  return {
    id: editingChannel.value?.id ?? '',
    name: form.name.trim(),
    type: form.type,
    enabled: form.enabled,
    url: form.url.trim(),
    secret: form.secret,
    clear_secret: form.clear_secret,
    events: [...form.events],
    min_severity: form.min_severity,
    send_resolved: form.send_resolved
  }
}

function toggleFormEvent(event: AlertNotifyEvent) {
  const index = form.events.indexOf(event)
  if (index >= 0) {
    form.events.splice(index, 1)
  } else {
    form.events.push(event)
  }
}

function resetForm(channel: AlertNotifyChannelView | null) {
  form.name = channel?.name ?? ''
  form.type = channel?.type ?? 'feishu'
  form.url = ''
  form.secret = ''
  form.clear_secret = false
  form.events = channel ? [...channel.events] : [...DEFAULT_EVENTS]
  form.min_severity = channel?.min_severity ?? ''
  form.send_resolved = channel?.send_resolved ?? false
  form.enabled = channel?.enabled ?? true
}

function openCreate() {
  editingChannel.value = null
  resetForm(null)
  formError.value = ''
  editorVisible.value = true
}

function openEdit(channel: AlertNotifyChannelView) {
  editingChannel.value = channel
  resetForm(channel)
  formError.value = ''
  editorVisible.value = true
}

function closeEditor() {
  editorVisible.value = false
  editingChannel.value = null
  formError.value = ''
}

function validateForm(): string | null {
  if (!form.name.trim()) {
    return t('admin.settings.alertNotify.validation.nameRequired')
  }
  if (!isEditing.value && !form.url.trim()) {
    return t('admin.settings.alertNotify.validation.urlRequired')
  }
  const url = form.url.trim()
  if (url && !/^https?:\/\//i.test(url)) {
    return t('admin.settings.alertNotify.validation.urlScheme')
  }
  if (url && form.type === 'feishu' && !/^https:\/\//i.test(url)) {
    return t('admin.settings.alertNotify.validation.urlFeishuHttps')
  }
  if (form.events.length === 0) {
    return t('admin.settings.alertNotify.validation.eventsRequired')
  }
  return null
}

async function load() {
  loading.value = true
  try {
    const response = await alertNotifyAPI.getChannels()
    applyResponse(response)
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.settings.alertNotify.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function submitChannels(next: AlertNotifyChannelInput[]): Promise<boolean> {
  const response = await alertNotifyAPI.updateChannels(next)
  applyResponse(response)
  return true
}

async function onSave() {
  const error = validateForm()
  if (error) {
    formError.value = error
    return
  }
  const editingId = editingChannel.value?.id ?? null
  let next: AlertNotifyChannelInput[]
  if (editingId) {
    // 编辑已有渠道：原位替换，保留列表顺序（后端按提交顺序存储）
    next = channels.value.map((channel) =>
      channel.id === editingId ? formInput() : preserveInput(channel)
    )
  } else {
    // 新建渠道：追加到末尾
    next = channels.value.map(preserveInput)
    next.push(formInput())
  }

  saving.value = true
  formError.value = ''
  try {
    await submitChannels(next)
    appStore.showSuccess(t('admin.settings.alertNotify.saveSuccess'))
    closeEditor()
  } catch (error) {
    // 保存失败：保持对话框打开、本地列表不变
    formError.value = extractApiErrorMessage(error, t('admin.settings.alertNotify.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function onToggleEnabled(channel: AlertNotifyChannelView, enabled: boolean) {
  const previous = [...channels.value]
  channels.value = channels.value.map((item) =>
    item.id === channel.id ? { ...item, enabled } : item
  )
  const next: AlertNotifyChannelInput[] = channels.value.map((item) => preserveInput(item))
  try {
    await submitChannels(next)
    appStore.showSuccess(t('admin.settings.alertNotify.saveSuccess'))
  } catch (error) {
    // 失败回滚开关
    channels.value = previous
    appStore.showError(extractApiErrorMessage(error, t('admin.settings.alertNotify.saveFailed')))
  }
}

function requestDelete(channel: AlertNotifyChannelView) {
  deleteTarget.value = channel
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  deleteTarget.value = null
  if (!target) return
  const next: AlertNotifyChannelInput[] = channels.value
    .filter((channel) => channel.id !== target.id)
    .map(preserveInput)
  try {
    await submitChannels(next)
    appStore.showSuccess(t('admin.settings.alertNotify.deleteSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.settings.alertNotify.deleteFailed')))
  }
}

async function runTest(input: AlertNotifyChannelInput) {
  await alertNotifyAPI.testChannel(input)
  appStore.showSuccess(t('admin.settings.alertNotify.testSuccess'))
}

async function onTestRow(channel: AlertNotifyChannelView) {
  testingId.value = channel.id
  try {
    await runTest(preserveInput(channel))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.settings.alertNotify.testFailed')))
  } finally {
    testingId.value = null
  }
}

async function onTestForm() {
  const error = validateForm()
  if (error) {
    formError.value = error
    return
  }
  formError.value = ''
  testingForm.value = true
  try {
    await runTest(formInput())
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.settings.alertNotify.testFailed')))
  } finally {
    testingForm.value = false
  }
}

onMounted(load)
</script>
