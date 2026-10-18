<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { enterprisesAPI } from '@/api/admin/enterprises'
import type { Enterprise } from '@/types/enterprise'
import { useAppStore } from '@/stores/app'
import { useEnterpriseAction } from '../useEnterpriseAction'
import EnterpriseConfirm from './EnterpriseConfirm.vue'

const props = defineProps<{ enterprise: Enterprise }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const app = useAppStore()
const { busy, error, run } = useEnterpriseAction()
const input = ref(''), validation = ref('')
const pending = ref<{ concurrency: number | null; count: number }>()
const configured = computed(() => props.enterprise.member_concurrency != null)
watch(() => props.enterprise.member_concurrency, value => { input.value = value == null ? '' : String(value) }, { immediate: true })

function confirmApply() {
  if (busy.value || pending.value) return
  const value = input.value.trim(), concurrency = Number(value)
  if (!/^\d+$/.test(value) || !Number.isInteger(concurrency) || concurrency < 1 || concurrency > 2147483647) {
    validation.value = t('admin.enterprises.memberConcurrency.validation'); return
  }
  validation.value = ''; error.value = ''
  pending.value = { concurrency, count: props.enterprise.member_count }
}
function confirmClear() {
  if (busy.value || pending.value || !configured.value) return
  validation.value = ''; error.value = ''
  pending.value = { concurrency: null, count: props.enterprise.member_count }
}
async function apply() {
  const target = pending.value
  if (!target) return
  let result: Awaited<ReturnType<typeof enterprisesAPI.updateMemberConcurrency>> | undefined
  if (await run(async () => { result = await enterprisesAPI.updateMemberConcurrency(props.enterprise.id, { concurrency: target.concurrency }) })) {
    pending.value = undefined
    input.value = result!.member_concurrency == null ? '' : String(result!.member_concurrency)
    app.showSuccess(t(target.concurrency === null ? 'admin.enterprises.memberConcurrency.cleared' : 'admin.enterprises.memberConcurrency.updated', { count: result!.updated_count }))
    emit('updated')
  }
}
</script>
<template>
  <Card class="shadcn-scope" data-test="member-concurrency-card" :aria-busy="busy">
    <CardHeader>
      <CardTitle class="text-lg">{{ t('admin.enterprises.memberConcurrency.title') }}</CardTitle>
      <CardDescription>{{ t('admin.enterprises.memberConcurrency.description') }}</CardDescription>
    </CardHeader>
    <CardContent class="space-y-4">
      <p class="text-sm" data-test="member-concurrency-current">{{ configured ? t('admin.enterprises.memberConcurrency.current', { value: enterprise.member_concurrency! }) : t('admin.enterprises.memberConcurrency.unset') }}</p>
      <form class="space-y-4" novalidate data-test="member-concurrency-form" @submit.prevent="confirmApply">
        <div class="max-w-sm space-y-2">
          <Label for="enterprise-member-concurrency">{{ t('admin.enterprises.memberConcurrency.input') }}</Label>
          <Input id="enterprise-member-concurrency" v-model="input" type="text" inputmode="numeric" autocomplete="off" :disabled="busy || !!pending" aria-describedby="member-concurrency-hint" :aria-invalid="!!validation" :aria-errormessage="validation ? 'member-concurrency-error' : undefined" data-test="member-concurrency-input" />
          <p id="member-concurrency-hint" class="text-xs text-muted-foreground">{{ t('admin.enterprises.memberConcurrency.validation') }}</p>
        </div>
        <p v-if="validation || error" id="member-concurrency-error" role="alert" class="text-sm text-destructive">{{ validation || error }}</p>
        <div class="flex flex-wrap gap-3">
          <Button type="submit" :disabled="busy || !!pending" data-test="member-concurrency-apply">{{ t('admin.enterprises.memberConcurrency.apply') }}</Button>
          <Button type="button" variant="outline" :disabled="busy || !!pending || !configured" data-test="member-concurrency-clear" @click="confirmClear">{{ t('admin.enterprises.memberConcurrency.clear') }}</Button>
        </div>
      </form>
    </CardContent>
    <EnterpriseConfirm :show="!!pending" :busy="busy" :title="t(pending?.concurrency === null ? 'admin.enterprises.memberConcurrency.clear' : 'admin.enterprises.memberConcurrency.apply')" :message="pending ? t(pending.concurrency === null ? 'admin.enterprises.memberConcurrency.confirmClear' : 'admin.enterprises.memberConcurrency.confirmApply', { count: pending.count, value: pending.concurrency ?? '' }) : ''" @cancel="pending = undefined" @confirm="apply">
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
    </EnterpriseConfirm>
  </Card>
</template>
