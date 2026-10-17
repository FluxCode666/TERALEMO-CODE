<template>
  <AppLayout>
    <div v-if="loading" class="flex justify-center py-12" :aria-label="t('common.loading')" aria-busy="true">
      <LoadingSpinner />
    </div>
    <div v-else-if="errorMessage" role="alert" class="card space-y-4 p-6">
      <p class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
      <button type="button" class="btn btn-secondary" @click="loadOverview">{{ t('enterprise.retry') }}</button>
    </div>
    <EnterpriseWorkspace v-else-if="overview" :overview="overview" @updated="handleUpdated" />
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
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

async function loadOverview() {
  loading.value = true
  errorMessage.value = ''
  try {
    overview.value = await enterpriseAPI.overview()
  } catch (error) {
    errorMessage.value = enterpriseErrorMessage(error, t)
  } finally {
    loading.value = false
  }
}

async function handleUpdated() {
  try {
    await authStore.refreshUser()
  } catch (error) {
    console.warn('Failed to refresh enterprise identity:', error)
  }
  await loadOverview()
}

onMounted(loadOverview)
</script>
