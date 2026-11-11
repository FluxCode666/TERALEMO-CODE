<template>
  <main class="flex min-h-screen items-center justify-center bg-gray-50 p-6 dark:bg-dark-900">
    <section class="card w-full max-w-lg space-y-5 p-6">
      <h1 class="text-xl font-semibold">{{ t('forceTotp.setupTitle') }}</h1>
      <p class="text-sm text-gray-500">{{ t('forceTotp.setupHint') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <div class="flex gap-3">
        <button type="button" class="btn btn-primary" :disabled="busy" @click="showSetup = true">{{ t('forceTotp.setup') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="logout">{{ t('forceTotp.logout') }}</button>
      </div>
      <TotpSetupModal v-if="showSetup" @close="showSetup = false" @success="completeSetup" />
    </section>
  </main>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import TotpSetupModal from '@/components/user/profile/TotpSetupModal.vue'

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const showSetup = ref(false)
const busy = ref(false)
const error = ref('')

async function completeSetup() {
  showSetup.value = false
  busy.value = true
  error.value = ''
  try {
    await auth.refreshUser({ fresh: true })
    if (!auth.user?.requires_totp_setup) await router.replace(auth.isAdmin ? '/admin/dashboard' : '/dashboard')
  } catch (caught) {
    error.value = (caught as { message?: string }).message || t('forceTotp.failed')
  } finally { busy.value = false }
}

async function logout() { await auth.logout(); await router.replace('/login') }

onMounted(completeSetup)
</script>
