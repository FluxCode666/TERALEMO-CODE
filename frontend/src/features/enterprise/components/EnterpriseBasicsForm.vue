<template>
  <form class="card space-y-4 p-4 sm:p-6" @submit.prevent="save">
    <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('enterprise.basicInfo') }}</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <label><span class="input-label">{{ t('enterprise.name') }}</span><input v-model.trim="form.name" class="input" maxlength="100" required data-test="enterprise-basic-name" /></label>
      <label><span class="input-label">{{ t('enterprise.status') }}</span><select v-model="form.status" class="input"><option value="active">{{ t('enterprise.active') }}</option><option value="disabled">{{ t('enterprise.disabled') }}</option></select></label>
      <label><span class="input-label">{{ t('enterprise.contactName') }}</span><input v-model.trim="form.contact_name" class="input" maxlength="100" /></label>
      <label><span class="input-label">{{ t('enterprise.contactEmail') }}</span><input v-model.trim="form.contact_email" class="input" type="email" maxlength="255" /></label>
      <label><span class="input-label">{{ t('enterprise.contactPhone') }}</span><input v-model.trim="form.contact_phone" class="input" maxlength="50" /></label>
      <label><span class="input-label">{{ t('enterprise.lowBalanceThreshold') }}</span><input v-model="threshold" class="input" type="number" min="0" step="any" :placeholder="t('enterprise.disabled')" data-test="enterprise-basic-threshold" /></label>
    </div>
    <label class="block"><span class="input-label">{{ t('enterprise.notes') }}</span><textarea v-model.trim="form.notes" class="input" rows="3"></textarea></label>
    <p v-if="validation || error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation || error }}</p>
    <button class="btn btn-primary" :disabled="busy">{{ t('enterprise.save') }}</button>
  </form>
</template>
<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { enterprisesAPI } from '@/api/admin/enterprises'
import type { Enterprise } from '@/types/enterprise'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ enterprise: Enterprise }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run } = useEnterpriseAction()
const form = reactive({ name: props.enterprise.name, status: props.enterprise.status, contact_name: props.enterprise.contact_name, contact_email: props.enterprise.contact_email, contact_phone: props.enterprise.contact_phone, notes: props.enterprise.notes })
const threshold = ref<string | number>(props.enterprise.low_balance_threshold === null ? '' : String(props.enterprise.low_balance_threshold))
const validation = ref('')
async function save() {
  validation.value = ''
  if (!form.name) { validation.value = t('enterprise.validationRequired'); return }
  if (String(threshold.value).trim() !== '' && (!Number.isFinite(Number(threshold.value)) || Number(threshold.value) < 0)) { validation.value = t('enterprise.validationQuota'); return }
  if (await run(() => enterprisesAPI.update(props.enterprise.id, { ...form, low_balance_threshold: String(threshold.value).trim() === '' ? null : Number(threshold.value) }), t('enterprise.saved'))) emit('updated')
}
</script>
