<template>
  <div class="space-y-4">
    <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.massEmails.form.recipientsHint') }}</p>
    <p v-if="optionsLoading" class="text-xs text-gray-500">{{ t('common.loading') }}</p>
    <div v-if="optionsError" class="flex items-center gap-2 text-sm text-red-600" role="alert">
      {{ t('admin.massEmails.form.optionsLoadFailed') }}
      <button type="button" class="btn btn-secondary btn-sm" :disabled="optionsLoading" @click="loadOptions">{{ t('admin.massEmails.actions.retry') }}</button>
    </div>

    <!-- A: all users -->
    <label class="flex items-center gap-2 text-sm text-gray-800 dark:text-gray-200">
      <input v-model="model.includeAllUsers" type="checkbox" class="h-4 w-4 rounded" data-test="src-all-users" />
      {{ t('admin.massEmails.form.sourceAllUsers') }}
    </label>

    <!-- B: user filter -->
    <div class="space-y-2">
      <label class="flex items-center gap-2 text-sm text-gray-800 dark:text-gray-200" :class="model.includeAllUsers ? 'opacity-50' : ''">
        <input v-model="model.useUserFilter" :disabled="model.includeAllUsers" type="checkbox" class="h-4 w-4 rounded" data-test="src-user-filter" />
        {{ t('admin.massEmails.form.sourceUserFilter') }}
      </label>
      <div v-if="model.useUserFilter && !model.includeAllUsers" class="grid gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600 sm:grid-cols-2">
        <div>
          <div class="mb-1 text-xs font-medium text-gray-500">{{ t('admin.massEmails.form.userStatus') }}</div>
          <label v-for="s in statusOptions" :key="s.value" class="mr-3 inline-flex items-center gap-1 text-sm">
            <input v-model="model.userStatuses" type="checkbox" :value="s.value" class="h-4 w-4 rounded" /> {{ s.label }}
          </label>
        </div>
        <div>
          <div class="mb-1 text-xs font-medium text-gray-500">{{ t('admin.massEmails.form.userRole') }}</div>
          <label v-for="r in roleOptions" :key="r.value" class="mr-3 inline-flex items-center gap-1 text-sm">
            <input v-model="model.userRoles" type="checkbox" :value="r.value" class="h-4 w-4 rounded" /> {{ r.label }}
          </label>
        </div>
        <div class="sm:col-span-2">
          <div class="mb-1 text-xs font-medium text-gray-500">{{ t('admin.massEmails.form.userGroups') }}</div>
          <div class="max-h-32 overflow-y-auto rounded border border-gray-200 p-2 dark:border-dark-600">
            <label v-for="g in groups" :key="g.id" class="mr-3 inline-flex items-center gap-1 text-sm">
              <input v-model="model.groupIds" type="checkbox" :value="g.id" class="h-4 w-4 rounded" /> {{ g.name }}
            </label>
          </div>
        </div>
        <div class="sm:col-span-2">
          <input v-model="model.userSearch" type="text" class="input" :placeholder="t('admin.massEmails.form.userSearch')" />
        </div>
      </div>
    </div>

    <!-- C: enterprises -->
    <div class="space-y-2">
      <label class="flex items-center gap-2 text-sm text-gray-800 dark:text-gray-200">
        <input v-model="model.useEnterprise" type="checkbox" class="h-4 w-4 rounded" data-test="src-enterprise" />
        {{ t('admin.massEmails.form.sourceEnterprise') }}
      </label>
      <div v-if="model.useEnterprise" class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <div>
          <div class="mb-1 text-xs font-medium text-gray-500">{{ t('admin.massEmails.form.enterprises') }}</div>
          <input v-model="enterpriseKeyword" type="text" class="input mb-2" :placeholder="t('admin.massEmails.form.enterprisesPlaceholder')" />
          <div class="max-h-40 overflow-y-auto rounded border border-gray-200 p-2 dark:border-dark-600">
            <label v-for="e in filteredEnterprises" :key="e.id" class="mr-3 inline-flex items-center gap-1 text-sm">
              <input v-model="model.enterpriseIds" type="checkbox" :value="e.id" class="h-4 w-4 rounded" /> {{ e.name }}
            </label>
          </div>
        </div>
        <div>
          <div class="mb-1 text-xs font-medium text-gray-500">{{ t('admin.massEmails.form.enterpriseScope') }}</div>
          <label v-for="s in scopeOptions" :key="s.value" class="mr-4 inline-flex items-center gap-1 text-sm">
            <input v-model="model.enterpriseScope" type="radio" :value="s.value" /> {{ s.label }}
          </label>
        </div>
      </div>
    </div>

    <!-- D: manual -->
    <div class="space-y-2">
      <label class="flex items-center gap-2 text-sm text-gray-800 dark:text-gray-200">
        <input v-model="model.useManual" type="checkbox" class="h-4 w-4 rounded" data-test="src-manual" />
        {{ t('admin.massEmails.form.sourceManual') }}
      </label>
      <div v-if="model.useManual">
        <textarea v-model="model.manualText" rows="4" class="input" :placeholder="t('admin.massEmails.form.manualPlaceholder')"></textarea>
        <p v-if="manualInvalid.length" class="mt-1 text-xs text-amber-600">
          {{ t('admin.massEmails.form.manualInvalid', { count: manualInvalid.length, emails: manualInvalid.slice(0, 5).join(', ') }) }}
        </p>
      </div>
    </div>

    <!-- Preview -->
    <div class="flex items-center gap-3">
      <button type="button" class="btn btn-secondary" :disabled="previewing" data-test="preview-btn" @click="$emit('preview')">
        {{ t('admin.massEmails.form.preview') }}
      </button>
    </div>
    <div v-if="preview" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-700" data-test="preview-result">
      <div class="font-medium text-gray-900 dark:text-white">
        {{ t('admin.massEmails.form.previewTotal', { count: preview.total }) }}
      </div>
      <div class="mt-1 text-xs text-gray-500">
        {{ t('admin.massEmails.form.previewBySource', {
          users: preview.by_source.users,
          contacts: preview.by_source.enterprise_contacts,
          members: preview.by_source.enterprise_members,
          manual: preview.by_source.manual
        }) }}
      </div>
      <ul v-if="preview.sample.length" class="mt-2 space-y-0.5">
        <li v-for="s in preview.sample" :key="s.email" class="text-xs text-gray-600 dark:text-gray-300">
          {{ s.email }}<span v-if="s.name"> ({{ s.name }})</span>
          · {{ t(`admin.massEmails.form.sourceLabel.${s.source}`) }}
        </li>
      </ul>
      <p v-if="preview.invalid_emails.length" class="mt-2 text-xs text-amber-600">
        {{ t('admin.massEmails.form.previewInvalid', { emails: preview.invalid_emails.slice(0, 5).join(', ') }) }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { PreviewRecipientsResult } from '@/api/admin/massEmails'
import type { AdminGroup } from '@/types'
import type { Enterprise } from '@/types/enterprise'
import { parseManualEmails, type RecipientFormState } from '@/views/admin/massEmails/recipientForm'

const model = defineModel<RecipientFormState>({ required: true })
defineProps<{ preview: PreviewRecipientsResult | null; previewing?: boolean }>()
defineEmits<{ (e: 'preview'): void }>()

const { t } = useI18n()

const groups = ref<AdminGroup[]>([])
const enterprises = ref<Enterprise[]>([])
const enterpriseKeyword = ref('')
const optionsLoading = ref(false)
const optionsError = ref(false)
let optionsController: AbortController | null = null

const statusOptions = computed(() => [
  { value: 'active', label: t('admin.massEmails.form.statusActive') },
  { value: 'disabled', label: t('admin.massEmails.form.statusDisabled') }
])
const roleOptions = computed(() => [
  { value: 'user', label: t('admin.massEmails.form.roleUser') },
  { value: 'admin', label: t('admin.massEmails.form.roleAdmin') }
])
const scopeOptions = computed(() => [
  { value: 'contacts', label: t('admin.massEmails.form.scopeContacts') },
  { value: 'members', label: t('admin.massEmails.form.scopeMembers') },
  { value: 'both', label: t('admin.massEmails.form.scopeBoth') }
])

const filteredEnterprises = computed(() => {
  const kw = enterpriseKeyword.value.trim().toLowerCase()
  if (!kw) return enterprises.value
  return enterprises.value.filter((e) => e.name.toLowerCase().includes(kw))
})

const manualInvalid = computed(() => parseManualEmails(model.value.manualText).invalid)

async function loadOptions() {
  optionsController?.abort()
  const controller = new AbortController()
  optionsController = controller
  optionsLoading.value = true
  optionsError.value = false
  const results = await Promise.allSettled([
    adminAPI.groups.getAllIncludingInactive(),
    (async () => {
      const all: Enterprise[] = []
      let page = 1
      while (!controller.signal.aborted) {
        const result = await adminAPI.enterprises.list({ page, page_size: 100 }, { signal: controller.signal })
        all.push(...result.items)
        if (!result.items.length || all.length >= result.total) break
        page++
      }
      return all
    })()
  ])
  if (controller.signal.aborted) return
  const [groupResult, enterpriseResult] = results
  if (groupResult.status === 'fulfilled') groups.value = groupResult.value
  if (enterpriseResult.status === 'fulfilled') enterprises.value = enterpriseResult.value
  optionsError.value = results.some((result) => result.status === 'rejected')
  optionsLoading.value = false
}

onMounted(loadOptions)
onBeforeUnmount(() => {
  optionsController?.abort()
})
</script>
