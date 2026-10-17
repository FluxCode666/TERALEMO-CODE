<template>
  <section class="card space-y-4 p-4 sm:p-6" :aria-busy="busy">
    <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('enterprise.settings') }}</h2>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <form class="space-y-4" @submit.prevent="save">
      <label v-for="key in keys" :key="key" class="flex items-center justify-between gap-4 border-b border-gray-100 py-3 text-sm text-gray-700 last:border-0 dark:border-dark-700 dark:text-gray-200"><span>{{ t(`enterprise.settingLabels.${key}`) }}</span><input v-model="form[key]" type="checkbox" :disabled="readonly || busy" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" /></label>
      <button v-if="!readonly" class="btn btn-primary" :disabled="busy">{{ t('enterprise.save') }}</button>
    </form>
  </section>
</template>
<script setup lang="ts">
import { onMounted, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import { enterprisesAPI } from '@/api/admin/enterprises'
import { defaultEnterpriseSettings, type EnterpriseSettings } from '@/types/enterprise'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; admin?: boolean; enterpriseId: number; settings?: EnterpriseSettings | null; readonly?: boolean }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run } = useEnterpriseAction()
const form = reactive<EnterpriseSettings>({ ...defaultEnterpriseSettings, ...props.settings })
const keys = Object.keys(defaultEnterpriseSettings) as (keyof EnterpriseSettings)[]
async function load() { if (!props.admin) await run(async () => { Object.assign(form, defaultEnterpriseSettings, await props.api.settings()) }) }
async function save() {
  if (props.readonly) return
  if (await run(() => props.admin ? enterprisesAPI.update(props.enterpriseId, { settings: { ...form } }) : props.api.updateSettings({ ...form }), t('enterprise.saved'))) emit('updated')
}
onMounted(load)
</script>
