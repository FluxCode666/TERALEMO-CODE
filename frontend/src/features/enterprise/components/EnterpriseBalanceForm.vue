<template>
  <form class="shadcn-scope space-y-4" @submit.prevent="save">
    <p class="text-sm text-muted-foreground">{{ t('enterprise.balanceAdjustmentHint') }}</p>
    <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.operation') }}</span><EnterpriseSelect v-model="form.operation" :options="operationOptions" :aria-label="t('enterprise.operation')" data-test="balance-operation" /></Label>
    <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.amount') }}</span><Input v-model="form.amount"  type="number" min="0" step="any" data-test="enterprise-balance-amount" /></Label>
    <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.notes') }}</span><textarea v-model.trim="form.notes" class="flex w-full rounded-ui-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50" rows="3"></textarea></Label>
    <p v-if="validation || error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation || error }}</p>
    <Button variant="default"  :disabled="busy">{{ t('enterprise.save') }}</Button>
  </form>
</template>
<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
import { enterprisesAPI } from '@/api/admin/enterprises'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ enterpriseId: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const operationOptions = computed(() => [{ value: 'add', label: t('enterprise.addBalance') }, { value: 'subtract', label: t('enterprise.subtractBalance') }])
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
