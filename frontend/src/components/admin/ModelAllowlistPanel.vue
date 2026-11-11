<template>
  <section class="card space-y-4 p-5">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h3 class="font-semibold">{{ t('modelAllowlist.title') }}</h3>
        <p class="mt-1 text-sm text-gray-500">{{ t('modelAllowlist.hint') }}</p>
      </div>
      <button type="button" class="btn btn-primary" :disabled="busy || loading" @click="requestToggle">
        {{ settings.enabled ? t('modelAllowlist.disable') : t('modelAllowlist.enable') }}
      </button>
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <div v-for="(row, index) in rows" :key="index" class="flex gap-2">
      <input v-model="row.entry" class="input flex-1" :placeholder="t('modelAllowlist.entry')" :aria-label="t('modelAllowlist.entry')" />
      <input v-model="row.note" class="input flex-1" maxlength="256" :placeholder="t('modelAllowlist.note')" :aria-label="t('modelAllowlist.note')" />
      <button type="button" class="btn btn-secondary" @click="rows.splice(index, 1)">{{ t('common.delete') }}</button>
    </div>
    <button type="button" class="btn btn-secondary" @click="rows.push({ entry: '', note: '' })">{{ t('modelAllowlist.add') }}</button>
    <div class="flex items-center justify-between">
      <label>{{ t('modelAllowlist.cloudflare') }}</label>
      <Toggle v-model="settings.cloudflare" />
    </div>
    <label class="block text-sm">
      {{ t('modelAllowlist.cdn') }}
      <textarea v-model="cdnText" class="input mt-2 min-h-20 w-full" :placeholder="t('modelAllowlist.cdnHint')" />
    </label>
    <p class="text-xs text-gray-500">{{ t('modelAllowlist.proxyHint') }}</p>
    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn btn-primary" :disabled="busy || loading" @click="save">{{ t('modelAllowlist.save') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="preview">{{ t('modelAllowlist.preview') }}</button>
      <input v-model="testIP" class="input max-w-xs" :placeholder="t('modelAllowlist.testIP')" :aria-label="t('modelAllowlist.testIP')" />
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="test">{{ t('modelAllowlist.test') }}</button>
      <span v-if="testResult !== null" role="status" class="self-center text-sm">{{ testResult ? t('modelAllowlist.allowed') : t('modelAllowlist.denied') }}</span>
    </div>
    <p class="text-xs text-gray-500">{{ t('modelAllowlist.testHint') }}</p>
    <div v-for="domain in domains" :key="domain.entry" class="rounded bg-gray-50 p-2 text-sm dark:bg-dark-800">
      {{ domain.entry }} → {{ domain.error || domain.addresses.join(', ') }}
      <span class="text-xs text-gray-500">{{ new Date(domain.expires_at).toLocaleString() }}</span>
    </div>
    <div class="flex items-center justify-between">
      <h4 class="font-medium">{{ t('modelAllowlist.observations') }}</h4>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="reload">{{ t('modelAllowlist.refresh') }}</button>
    </div>
    <p class="text-xs text-gray-500">{{ t('modelAllowlist.observationHint') }}</p>
    <div class="max-h-96 overflow-auto">
      <table class="w-full text-left text-sm">
        <thead><tr><th>IP</th><th>{{ t('modelAllowlist.user') }}</th><th>{{ t('modelAllowlist.key') }}</th><th>{{ t('modelAllowlist.count') }}</th><th>{{ t('modelAllowlist.lastSeen') }}</th><th>{{ t('modelAllowlist.note') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="observation in observations" :key="`${observation.ip}:${observation.user_id}:${observation.key_id}`" class="border-t">
            <td class="py-2">{{ observation.ip }}</td><td>{{ observation.user_email }} (#{{ observation.user_id }})</td><td>{{ observation.key_name }} (#{{ observation.key_id }})</td><td>{{ observation.count }}</td><td>{{ new Date(observation.last_seen * 1000).toLocaleString() }}</td>
            <td><input v-model="observationNotes[observation.ip]" class="input" maxlength="256" :aria-label="t('modelAllowlist.note')" /></td>
            <td><button type="button" class="btn btn-secondary" :disabled="busy" @click="addObserved(observation.ip)">{{ t('modelAllowlist.addObserved') }}</button></td>
          </tr>
        </tbody>
      </table>
      <p v-if="!observations.length" class="py-3 text-sm text-gray-500">{{ t('modelAllowlist.empty') }}</p>
    </div>
    <ConfirmDialog :show="confirmEnable" :title="t('modelAllowlist.enable')" :message="t('modelAllowlist.confirm', impact)" danger @confirm="toggle" @cancel="confirmEnable = false" />
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'
import { getModelAllowlist, saveModelAllowlist, testModelAllowlist, toggleModelAllowlist, type ModelAllowlistResponse, type ModelAllowlistSettings, type ModelDomainPreview, type ModelIPObservation } from '@/api/admin/modelAllowlist'

const { t } = useI18n()
const stepUp = useStepUp()
const settings = reactive<ModelAllowlistSettings>({ enabled: false, entries: [], notes: {}, cloudflare: true, trusted_cdn_proxies: [] })
const rows = ref<{ entry: string; note: string }[]>([])
const cdnText = ref('')
const domains = ref<ModelDomainPreview[]>([])
const observations = ref<ModelIPObservation[]>([])
const observationNotes = reactive<Record<string, string>>({})
const impact = reactive({ ips: 0, users: 0, keys: 0 })
const testIP = ref('')
const testResult = ref<boolean | null>(null)
const confirmEnable = ref(false)
const busy = ref(false)
const loading = ref(true)
const error = ref('')

function apply(data: ModelAllowlistResponse) {
  Object.assign(settings, data.settings)
  rows.value = data.settings.entries.map(entry => ({ entry, note: data.settings.notes[entry] || '' }))
  cdnText.value = data.settings.trusted_cdn_proxies.join('\n')
  domains.value = data.domains
  observations.value = data.observations
  Object.assign(impact, data.impact)
}

function draft(): ModelAllowlistSettings {
  const entries = rows.value.map(row => row.entry.trim().toLowerCase().replace(/\.$/, '')).filter(Boolean)
  return { ...settings, entries, notes: Object.fromEntries(rows.value.map(row => [row.entry.trim().toLowerCase().replace(/\.$/, ''), row.note.trim()])), trusted_cdn_proxies: cdnText.value.split(/[\s,]+/).filter(Boolean) }
}

async function run(action: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try { await action() } catch (caught) {
    if (!isStepUpCancelled(caught)) error.value = (caught as { message?: string }).message || t('modelAllowlist.failed')
  } finally { busy.value = false }
}

async function reload() { await run(async () => apply(await getModelAllowlist())); loading.value = false }
async function save() { await run(async () => apply(await stepUp.run(() => saveModelAllowlist(draft())))) }
async function preview() { await run(async () => { domains.value = (await testModelAllowlist(draft(), '')).domains }) }
async function test() { await run(async () => { const result = await testModelAllowlist({ ...draft(), enabled: true }, testIP.value); domains.value = result.domains; testResult.value = result.allowed }) }
async function requestToggle() {
  if (settings.enabled) { await toggle(); return }
  await run(async () => { const current = await getModelAllowlist(); Object.assign(impact, current.impact); confirmEnable.value = true })
}
async function toggle() {
  confirmEnable.value = false
  await run(async () => apply(await stepUp.run(() => toggleModelAllowlist(!settings.enabled))))
}
async function addObserved(address: string) {
  if (!rows.value.some(row => row.entry === address)) rows.value.push({ entry: address, note: observationNotes[address] || '' })
  await save()
}

onMounted(reload)
</script>
