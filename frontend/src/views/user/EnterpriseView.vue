<template>
  <AppLayout compact-padding><div class="shadcn-scope min-w-0 space-y-6">
    <div v-if="loading && !overview" class="shadcn-scope flex justify-center py-12" :aria-label="t('common.loading')" aria-busy="true">
      <Skeleton v-for="n in 3" :key="n" class="h-32 flex-1" />
    </div>
    <Card v-if="errorMessage" role="alert" class="rounded-ui-lg border bg-card text-card-foreground shadow-sm space-y-4 p-6">
      <p class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
      <Button variant="outline" type="button"  @click="loadOverview">{{ t('enterprise.retry') }}</Button>
    </Card>
    <EnterpriseWorkspace v-if="overview" :overview="overview" :section="section" @navigate="navigate" @updated="handleUpdated" />
  </div></AppLayout>
</template>

<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'

import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useEnterpriseStore } from '@/stores/enterprise'
import { enterpriseErrorMessage } from '@/api/enterprise'
import { enterpriseSectionPath, type EnterpriseSection } from '@/features/enterprise/navigation'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Skeleton } from '@/components/ui/skeleton'
import EnterpriseWorkspace from '@/features/enterprise/EnterpriseWorkspace.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const enterpriseStore = useEnterpriseStore()
const { overview, loading, error } = storeToRefs(enterpriseStore)
const route = useRoute()
const router = useRouter()
const section = computed(() => route.meta.enterpriseSection ?? 'overview')
const errorMessage = computed(() => error.value ? enterpriseErrorMessage(error.value, t) : '')
let disposed = false
let identityRefreshSequence = 0

function navigate(value: EnterpriseSection) {
  void router.push(enterpriseSectionPath(value))
}

function loadOverview() {
  return enterpriseStore.ensureLoaded()
}

async function handleUpdated() {
  const sequence = ++identityRefreshSequence
  enterpriseStore.invalidate(false)
  try {
    await authStore.refreshUser({ fresh: true })
  } catch (error) {
    if (!disposed) console.warn('Failed to refresh enterprise identity:', error)
  }
  if (!disposed && sequence === identityRefreshSequence) await loadOverview()
}

// Permissions can change after a mutation or the periodic user refresh, without
// another navigation. Revalidate the current section against the fresh overview.
watch([overview, section], () => {
  if (overview.value && !enterpriseStore.stale && !enterpriseStore.tabs.includes(section.value)) {
    void router.replace(enterpriseSectionPath(enterpriseStore.tabs[0] ?? 'overview'))
  }
})
watch(() => enterpriseStore.canAccess, allowed => {
  if (!allowed) void router.replace('/dashboard')
})
onBeforeUnmount(() => { disposed = true })
onMounted(loadOverview)
</script>
