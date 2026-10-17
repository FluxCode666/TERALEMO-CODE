<template>
  <section class="card space-y-4 p-4 sm:p-6" :aria-busy="busy">
    <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('enterprise.settings') }}</h2>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <p v-if="preservedEdits" role="status" class="rounded-lg bg-blue-50 p-3 text-sm text-blue-800 dark:bg-blue-950/40 dark:text-blue-300">{{ t('enterprise.settingsRefreshedWithEdits') }}</p>
    <form class="space-y-4" @submit.prevent="save">
      <label v-for="key in keys" :key="key" class="flex items-center justify-between gap-4 border-b border-gray-100 py-3 text-sm text-gray-700 last:border-0 dark:border-dark-700 dark:text-gray-200"><span>{{ t(`enterprise.settingLabels.${key}`) }}</span><input v-model="form[key]" type="checkbox" :disabled="readonly || busy" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" /></label>
      <button v-if="!readonly" class="btn btn-primary" :disabled="busy">{{ t('enterprise.save') }}</button>
    </form>
  </section>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import { enterprisesAPI } from '@/api/admin/enterprises'
import { defaultEnterpriseSettings, type EnterpriseSettings } from '@/types/enterprise'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; enterpriseId: number; settings?: EnterpriseSettings | null; readonly?: boolean; refreshKey?: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const form = reactive<EnterpriseSettings>({ ...defaultEnterpriseSettings, ...props.settings })
const keys = Object.keys(defaultEnterpriseSettings) as (keyof EnterpriseSettings)[]
let serverSettings: EnterpriseSettings = { ...form }
const preservedEdits = ref(false)
function applySettings(settings: EnterpriseSettings | null | undefined) {
  const latest = { ...defaultEnterpriseSettings, ...settings }
  const dirtyKeys = keys.filter(key => form[key] !== serverSettings[key])
  for (const key of keys) {
    if (!dirtyKeys.includes(key)) form[key] = latest[key]
  }
  serverSettings = latest
  preservedEdits.value = dirtyKeys.some(key => form[key] !== latest[key])
}
async function load() {
  if (!props.admin) await read(signal => props.api.settings({ signal }), applySettings)
}
async function save() {
  if (props.readonly) return
  const settings = { ...form }
  if (await run(() => props.admin ? enterprisesAPI.update(props.enterpriseId, { settings }) : props.api.updateSettings(settings), t('enterprise.saved'))) {
    serverSettings = settings
    preservedEdits.value = false
    emit('updated')
  }
}
watch(() => props.settings, applySettings)
watch(() => props.refreshKey, load)
onMounted(load)
</script>
