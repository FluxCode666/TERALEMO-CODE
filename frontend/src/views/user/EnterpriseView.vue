<template>
  <AppLayout><div class="shadcn-scope min-w-0 space-y-6">
    <div v-if="loading && !overview" class="shadcn-scope flex justify-center py-12" :aria-label="t('common.loading')" aria-busy="true">
      <Skeleton v-for="n in 3" :key="n" class="h-32 flex-1" />
    </div>
    <Card v-if="errorMessage" role="alert" class="rounded-ui-lg border bg-card text-card-foreground shadow-sm space-y-4 p-6">
      <p class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
      <Button variant="outline" type="button"  @click="loadOverview">{{ t('enterprise.retry') }}</Button>
    </Card>
    <EnterpriseWorkspace v-if="overview" :overview="overview" @updated="handleUpdated" />
  </div></AppLayout>
</template>

<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'

import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { enterpriseAPI, enterpriseErrorMessage } from '@/api/enterprise'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Skeleton } from '@/components/ui/skeleton'
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
