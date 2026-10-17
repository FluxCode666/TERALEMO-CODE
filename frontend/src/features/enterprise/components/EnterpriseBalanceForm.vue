<template>
  <form class="space-y-4" @submit.prevent="save">
    <label class="block"><span class="input-label">{{ t('enterprise.operation') }}</span><select v-model="form.operation" class="input"><option value="add">{{ t('enterprise.addBalance') }}</option><option value="subtract">{{ t('enterprise.subtractBalance') }}</option></select></label>
    <label class="block"><span class="input-label">{{ t('enterprise.amount') }}</span><input v-model="form.amount" class="input" type="number" min="0" step="any" data-test="enterprise-balance-amount" /></label>
    <label class="block"><span class="input-label">{{ t('enterprise.notes') }}</span><textarea v-model.trim="form.notes" class="input" rows="3"></textarea></label>
    <p v-if="validation || error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation || error }}</p>
    <button class="btn btn-primary" :disabled="busy">{{ t('enterprise.save') }}</button>
  </form>
</template>
<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { enterprisesAPI } from '@/api/admin/enterprises'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ enterpriseId: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run } = useEnterpriseAction()
const form = reactive({ operation: 'add' as 'add' | 'subtract', amount: '', notes: '' })
const validation = ref('')
async function save() {
  const amount = Number(form.amount)
  if (!Number.isFinite(amount) || amount <= 0) { validation.value = t('enterprise.validationAmount'); return }
  validation.value = ''
  if (await run(() => enterprisesAPI.adjustBalance(props.enterpriseId, { operation: form.operation, amount, notes: form.notes }), t('enterprise.saved'))) emit('updated')
}
</script>
