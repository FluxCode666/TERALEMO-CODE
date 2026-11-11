<template>
  <section class="card space-y-4 p-5">
    <h3 class="font-semibold">{{ t('forceTotp.title') }}</h3>
    <p class="text-sm text-gray-500">{{ t('forceTotp.hint') }}</p>
    <p v-if="!settings.encryption_key_configured" class="text-sm text-amber-600">{{ t('forceTotp.keyRequired') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <button type="button" class="btn btn-primary" :disabled="busy || loading || (!settings.enabled && !settings.encryption_key_configured)" @click="requestToggle">
      {{ settings.enabled ? t('forceTotp.disable') : t('forceTotp.enable') }}
    </button>
    <ConfirmDialog :show="showConfirm" :title="t('forceTotp.enable')" :message="t('forceTotp.confirm', { count: settings.users_without_totp })" danger @confirm="toggle" @cancel="showConfirm = false" />
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'
import { getForceTotp, updateForceTotp } from '@/api/admin/forceTotp'

const { t } = useI18n()
const stepUp = useStepUp()
const settings = reactive({ enabled: false, users_without_totp: 0, encryption_key_configured: false })
const busy = ref(false)
const loading = ref(true)
const showConfirm = ref(false)
const error = ref('')

async function run(action: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try { await action() } catch (caught) {
    if (!isStepUpCancelled(caught)) error.value = (caught as { message?: string }).message || t('forceTotp.failed')
  } finally { busy.value = false }
}

async function requestToggle() {
  if (settings.enabled) { await toggle(); return }
  await run(async () => { Object.assign(settings, await getForceTotp()); showConfirm.value = true })
}

async function toggle() {
  showConfirm.value = false
  await run(async () => { Object.assign(settings, await stepUp.run(() => updateForceTotp(!settings.enabled))) })
}

onMounted(async () => { await run(async () => { Object.assign(settings, await getForceTotp()) }); loading.value = false })
</script>
