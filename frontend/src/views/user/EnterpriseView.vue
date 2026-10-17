<template>
  <AppLayout>
    <div v-if="loading && !overview" class="flex justify-center py-12" :aria-label="t('common.loading')" aria-busy="true">
      <LoadingSpinner />
    </div>
    <div v-if="errorMessage" role="alert" class="card space-y-4 p-6">
      <p class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
      <button type="button" class="btn btn-secondary" @click="loadOverview">{{ t('enterprise.retry') }}</button>
    </div>
    <EnterpriseWorkspace v-if="overview" :overview="overview" @updated="handleUpdated" />
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { enterpriseAPI, enterpriseErrorMessage } from '@/api/enterprise'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import EnterpriseWorkspace from '@/features/enterprise/EnterpriseWorkspace.vue'
import type { EnterpriseOverview } from '@/types/enterprise'

const { t } = useI18n()
const authStore = useAuthStore()
const overview = ref<EnterpriseOverview | null>(null)
const loading = ref(true)
const errorMessage = ref('')
let overviewController: AbortController | undefined
let disposed = false
let identityRefreshSequence = 0

async function loadOverview() {
  if (disposed) return
  overviewController?.abort()
  const controller = new AbortController()
  overviewController = controller
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await enterpriseAPI.overview({ signal: controller.signal })
    if (disposed || controller.signal.aborted) return
    overview.value = result
  } catch (error) {
    if (disposed || controller.signal.aborted) return
    errorMessage.value = enterpriseErrorMessage(error, t)
  } finally {
    if (!disposed && overviewController === controller) loading.value = false
  }
}

async function handleUpdated() {
  const sequence = ++identityRefreshSequence
  try {
    await authStore.refreshUser()
  } catch (error) {
    if (!disposed) console.warn('Failed to refresh enterprise identity:', error)
  }
  if (!disposed && sequence === identityRefreshSequence) await loadOverview()
}

onBeforeUnmount(() => {
  disposed = true
  overviewController?.abort()
})

onMounted(loadOverview)
</script>
