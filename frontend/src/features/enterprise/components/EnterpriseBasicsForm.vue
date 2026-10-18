<template>
  <Card><form class="shadcn-scope space-y-4 p-4 sm:p-6" @submit.prevent="save">
    <h2 class="text-lg font-semibold text-foreground">{{ t('enterprise.basicInfo') }}</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.name') }}</span><Input v-model.trim="form.name"  maxlength="100" required data-test="enterprise-basic-name" /></Label>
      <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.status') }}</span><EnterpriseSelect v-model="form.status" :options="statusOptions" :aria-label="t('enterprise.status')" data-test="enterprise-status-select" /></Label>
      <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.contactName') }}</span><Input v-model.trim="form.contact_name"  maxlength="100" /></Label>
      <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.contactEmail') }}</span><Input v-model.trim="form.contact_email"  type="email" maxlength="255" /></Label>
      <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.contactPhone') }}</span><Input v-model.trim="form.contact_phone"  maxlength="50" /></Label>
      <Label><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.lowBalanceThreshold') }}</span><Input v-model="threshold"  type="number" min="0" step="any" :placeholder="t('enterprise.disabled')" data-test="enterprise-basic-threshold" /></Label>
    </div>
    <Label class="block"><span class="mb-2 block text-sm font-medium text-foreground">{{ t('enterprise.notes') }}</span><textarea v-model.trim="form.notes" class="flex w-full rounded-ui-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50" rows="3"></textarea></Label>
    <p v-if="validation || error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation || error }}</p>
    <Button variant="default"  :disabled="busy">{{ t('enterprise.save') }}</Button>
  </form></Card>
</template>
<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
import { enterprisesAPI } from '@/api/admin/enterprises'
import type { Enterprise } from '@/types/enterprise'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ enterprise: Enterprise }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const statusOptions = computed(() => ['active', 'disabled'].map(value => ({ value, label: t(`enterprise.${value}`) })))
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
