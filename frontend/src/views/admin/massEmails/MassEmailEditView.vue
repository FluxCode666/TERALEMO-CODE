<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <div class="flex items-center justify-between">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">
          {{ isEdit ? t('admin.massEmails.editTitle') : t('admin.massEmails.createTitle') }}
        </h1>
        <button class="btn btn-secondary" @click="router.push('/admin/mass-emails')">{{ t('admin.massEmails.actions.back') }}</button>
      </div>

      <div v-if="loading" class="py-12 text-center text-gray-500">{{ t('common.loading') }}</div>
      <template v-else>
        <div class="card space-y-4 p-5">
          <div>
            <label class="input-label">{{ t('admin.massEmails.form.subject') }}</label>
            <input v-model="subject" type="text" maxlength="200" class="input" :placeholder="t('admin.massEmails.form.subjectPlaceholder')" data-test="subject" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.massEmails.form.body') }}</label>
            <MassEmailEditor v-model="bodyHtml" />
            <p class="mt-1 text-xs text-gray-500">{{ t('admin.massEmails.form.variablesHint', { vars: variablesText }) }}</p>
          </div>
        </div>

        <div class="card space-y-4 p-5">
          <h2 class="text-base font-medium text-gray-900 dark:text-white">{{ t('admin.massEmails.form.recipients') }}</h2>
          <RecipientSelector v-model="recipientForm" :preview="preview" :previewing="previewing" @preview="runPreview" />
        </div>

        <div class="card space-y-3 p-5">
          <h2 class="text-base font-medium text-gray-900 dark:text-white">{{ t('admin.massEmails.form.testSend') }}</h2>
          <div class="flex flex-wrap items-center gap-2">
            <input v-model="testTo" type="email" class="input max-w-sm" :placeholder="t('admin.massEmails.form.testToPlaceholder')" data-test="test-to" />
            <button class="btn btn-secondary" :disabled="testing" data-test="test-btn" @click="runTest">
              {{ t('admin.massEmails.form.testSend') }}
            </button>
          </div>
        </div>

        <div class="card space-y-3 p-5">
          <div class="flex flex-wrap items-end gap-3">
            <div>
              <label class="input-label">{{ t('admin.massEmails.form.scheduleAt') }}</label>
              <input v-model="scheduleAtStr" type="datetime-local" class="input" data-test="schedule-at" />
              <p class="mt-1 text-xs text-gray-500">{{ t('admin.massEmails.form.scheduleHint', { timezone: browserTimezone }) }}</p>
            </div>
            <div class="ml-auto flex flex-wrap gap-2">
              <button class="btn btn-secondary" :disabled="submitting" data-test="save-draft" @click="submit('draft')">
                {{ t('admin.massEmails.form.saveDraft') }}
              </button>
              <button class="btn btn-secondary" :disabled="submitting" data-test="schedule" @click="submit('schedule')">
                {{ t('admin.massEmails.form.schedule') }}
              </button>
              <button class="btn btn-primary" :disabled="submitting" data-test="send-now" @click="submit('send')">
                {{ t('admin.massEmails.form.sendNow') }}
              </button>
            </div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { PreviewRecipientsResult } from '@/api/admin/massEmails'
import { formatDateTimeLocalInput, getBrowserTimeZone, parseDateTimeLocalInput } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import MassEmailEditor from '@/components/admin/massEmails/MassEmailEditor.vue'
import RecipientSelector from '@/components/admin/massEmails/RecipientSelector.vue'
import {
  MASS_EMAIL_VARIABLES,
  buildRecipientFilter,
  createEmptyRecipientForm,
  hasAnyRecipientSource,
  hasEmptyUserFilter,
  isEditableStatus,
  recipientFormFromFilter
} from './recipientForm'
import { massEmailErrorMessage } from './massEmailErrors'

type SubmitMode = 'draft' | 'schedule' | 'send'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()

const editId = computed(() => (route.params.id ? Number(route.params.id) : null))
const isEdit = computed(() => editId.value !== null)

const loading = ref(false)
const submitting = ref(false)
const testing = ref(false)
const previewing = ref(false)

const subject = ref('')
const bodyHtml = ref('')
const recipientForm = ref(createEmptyRecipientForm())
const scheduleAtStr = ref('')
const testTo = ref('')
const preview = ref<PreviewRecipientsResult | null>(null)
const browserTimezone = getBrowserTimeZone()
const recipientFilter = computed(() => buildRecipientFilter(recipientForm.value))
let disposed = false
let previewController: AbortController | null = null
let editController: AbortController | null = null

watch(recipientFilter, () => {
  previewController?.abort()
  preview.value = null
  previewing.value = false
})

const variablesText = MASS_EMAIL_VARIABLES.map((v) => `{{${v}}}`).join(' ')

async function runPreview() {
  if (hasEmptyUserFilter(recipientForm.value)) {
    appStore.showError(t('admin.massEmails.form.emptyUserFilter'))
    return
  }
  const filter = recipientFilter.value
  if (!hasAnyRecipientSource(filter)) {
    appStore.showError(t('admin.massEmails.form.noSource'))
    return
  }
  previewController?.abort()
  const controller = new AbortController()
  previewController = controller
  previewing.value = true
  try {
    const result = await adminAPI.massEmails.previewRecipients(filter, 5, { signal: controller.signal })
    if (!disposed && !controller.signal.aborted) preview.value = result
  } catch (err) {
    if (!disposed && !controller.signal.aborted) appStore.showError(massEmailErrorMessage(err, t))
  } finally {
    if (previewController === controller) previewing.value = false
  }
}

async function runTest() {
  if (!subject.value.trim()) return appStore.showError(t('admin.massEmails.form.subjectRequired'))
  if (!bodyHtml.value.trim()) return appStore.showError(t('admin.massEmails.form.bodyRequired'))
  testing.value = true
  try {
    const res = await adminAPI.massEmails.sendTest({
      subject: subject.value.trim(),
      body_html: bodyHtml.value,
      to: testTo.value.trim() || null
    })
    appStore.showSuccess(t('admin.massEmails.form.testSent', { to: res.to }))
  } catch (err) {
    appStore.showError(massEmailErrorMessage(err, t))
  } finally {
    testing.value = false
  }
}

function validate(mode: SubmitMode): { ok: boolean; scheduledAt: string | null } {
  if (!subject.value.trim()) {
    appStore.showError(t('admin.massEmails.form.subjectRequired'))
    return { ok: false, scheduledAt: null }
  }
  if (!bodyHtml.value.trim()) {
    appStore.showError(t('admin.massEmails.form.bodyRequired'))
    return { ok: false, scheduledAt: null }
  }
  if (hasEmptyUserFilter(recipientForm.value)) {
    appStore.showError(t('admin.massEmails.form.emptyUserFilter'))
    return { ok: false, scheduledAt: null }
  }
  if (!hasAnyRecipientSource(buildRecipientFilter(recipientForm.value))) {
    appStore.showError(t('admin.massEmails.form.noSource'))
    return { ok: false, scheduledAt: null }
  }
  let scheduledAt: string | null = null
  if (mode === 'schedule') {
    const ts = parseDateTimeLocalInput(scheduleAtStr.value)
    if (ts === null) {
      appStore.showError(t('admin.massEmails.form.scheduleRequired'))
      return { ok: false, scheduledAt: null }
    }
    if (ts * 1000 <= Date.now()) {
      appStore.showError(t('admin.massEmails.form.scheduleFuture'))
      return { ok: false, scheduledAt: null }
    }
    scheduledAt = new Date(ts * 1000).toISOString()
  }
  return { ok: true, scheduledAt }
}

async function submit(mode: SubmitMode) {
  if (submitting.value) return
  const { ok, scheduledAt } = validate(mode)
  if (!ok) return
  submitting.value = true
  try {
    const payload = {
      subject: subject.value.trim(),
      body_html: bodyHtml.value,
      recipient_filter: buildRecipientFilter(recipientForm.value),
      scheduled_at: mode === 'send' ? null : scheduledAt
    }
    let id: number
    if (isEdit.value && editId.value !== null) {
      const updated = await adminAPI.massEmails.update(editId.value, payload)
      id = updated.id
      if (mode === 'send') await adminAPI.massEmails.send(id)
    } else {
      const created = await adminAPI.massEmails.create({ ...payload, send_now: mode === 'send' })
      id = created.id
    }
    appStore.showSuccess(
      mode === 'send'
        ? t('admin.massEmails.form.sentNowOk')
        : mode === 'schedule'
          ? t('admin.massEmails.form.scheduledOk')
          : t('admin.massEmails.form.saved')
    )
    router.push(`/admin/mass-emails/${id}`)
  } catch (err) {
    appStore.showError(massEmailErrorMessage(err, t))
  } finally {
    submitting.value = false
  }
}

watch(editId, async (id) => {
  editController?.abort()
  previewController?.abort()
  subject.value = ''
  bodyHtml.value = ''
  recipientForm.value = createEmptyRecipientForm()
  scheduleAtStr.value = ''
  preview.value = null
  loading.value = false
  if (id === null) return
  const controller = new AbortController()
  editController = controller
  loading.value = true
  try {
    const { campaign } = await adminAPI.massEmails.getById(id, { signal: controller.signal })
    if (disposed || controller.signal.aborted) return
    if (!isEditableStatus(campaign.status)) {
      appStore.showError(t('admin.massEmails.errors.CAMPAIGN_NOT_EDITABLE'))
      router.push(`/admin/mass-emails/${id}`)
      return
    }
    subject.value = campaign.subject
    bodyHtml.value = campaign.body_html
    recipientForm.value = recipientFormFromFilter(campaign.recipient_filter)
    scheduleAtStr.value = campaign.scheduled_at
      ? formatDateTimeLocalInput(Math.floor(new Date(campaign.scheduled_at).getTime() / 1000))
      : ''
  } catch (err) {
    if (disposed || controller.signal.aborted) return
    appStore.showError(massEmailErrorMessage(err, t))
    router.push('/admin/mass-emails')
  } finally {
    if (editController === controller) loading.value = false
  }
}, { immediate: true })

onBeforeUnmount(() => {
  disposed = true
  editController?.abort()
  previewController?.abort()
})
</script>
