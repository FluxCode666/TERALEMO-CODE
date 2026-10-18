<template>
  <EnterpriseConfirm :busy="busy" :show="true" :title="t(dissolve ? 'admin.enterprises.dissolve' : 'admin.enterprises.delete')" :message="t(dissolve ? 'admin.enterprises.confirmDissolve' : 'admin.enterprises.confirmDelete')" danger @cancel="cancel" @confirm="remove">
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
  </EnterpriseConfirm>
</template>

<script setup lang="ts">


import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { enterprisesAPI } from '@/api/admin/enterprises'
import { useAppStore } from '@/stores/app'
import EnterpriseConfirm from '@/features/enterprise/components/EnterpriseConfirm.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'

const props = defineProps<{ enterpriseId: number }>()
const emit = defineEmits<{ cancel: []; deleted: [] }>()
const { t } = useI18n()
const app = useAppStore()
const { busy, error, run } = useEnterpriseAction()
const dissolve = ref(false)

function isNotEmpty(cause: unknown): boolean {
  const value = cause as { reason?: string; code?: string; response?: { data?: { reason?: string; code?: string } } } | null
  return [value?.reason, value?.code, value?.response?.data?.reason, value?.response?.data?.code].includes('ENTERPRISE_NOT_EMPTY')
}

function cancel() { if (!busy.value) emit('cancel') }

async function remove() {
  if (busy.value) return
  let deleted = false
  const success = await run(async () => {
    try {
      await enterprisesAPI.remove(props.enterpriseId, dissolve.value ? true : undefined)
      deleted = true
    } catch (cause) {
      if (!dissolve.value && isNotEmpty(cause)) {
        dissolve.value = true
        return
      }
      throw cause
    }
  })
  if (success && deleted) {
    app.showSuccess(t('enterprise.deleted'))
    emit('deleted')
  }
}
</script>
