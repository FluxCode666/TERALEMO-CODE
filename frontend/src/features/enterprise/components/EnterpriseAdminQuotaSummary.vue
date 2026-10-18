<template>
  <section class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2"><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('enterprise.quotas') }}</h2><button class="text-sm text-primary-600 dark:text-primary-400" @click="$emit('manage')">{{ t('enterprise.view') }}</button></div>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="load">{{ t('enterprise.retry') }}</button></p>
    <div class="grid gap-4 xl:grid-cols-3" :aria-busy="busy">
      <div v-for="kind in kinds" :key="kind" class="min-w-0 space-y-3">
        <Select v-if="kind !== 'enterprise' && options(kind).length" v-model="selected[kind]" :options="options(kind)" :aria-label="t(`enterprise.quotaSections.${kind}.title`)" />
        <EnterpriseQuotaDisplay v-if="current(kind)" :quota="current(kind)!" :title="t(`enterprise.quotaSections.${kind}.title`)" compact :data-test="`${kind}-quota`" />
        <div v-else class="card p-5"><h3 class="font-medium text-gray-900 dark:text-white">{{ t(`enterprise.quotaSections.${kind}.title`) }}</h3><p class="mt-4 text-sm text-gray-500 dark:text-gray-400">{{ t(busy ? 'common.loading' : 'enterprise.empty') }}</p></div>
      </div>
    </div>
  </section>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseQuota, EnterpriseScope } from '@/types/enterprise'
import Select from '@/components/common/Select.vue'
import EnterpriseQuotaDisplay from './EnterpriseQuotaDisplay.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
import { loadMemberOptions } from '../memberOptions'
const props = defineProps<{ api: EnterpriseResourceAPI; quota?: EnterpriseQuota | null; refreshKey?: number }>()
defineEmits<{ manage: [] }>()
const { t } = useI18n(), { busy, error, read } = useEnterpriseAction()
const kinds: EnterpriseScope[] = ['enterprise', 'department', 'member']
const quotas = ref<EnterpriseQuota[]>([]), departments = ref<EnterpriseDepartment[]>([]), members = ref<EnterpriseMember[]>([])
const selected = reactive<Record<EnterpriseScope, number | null>>({ enterprise: null, department: null, member: null })
function options(kind: EnterpriseScope) { return quotas.value.filter(item => item.scope_type === kind).map(item => ({ value: item.scope_id, label: (kind === 'department' ? departments.value.find(row => row.id === item.scope_id)?.name : members.value.find(row => row.id === item.scope_id)?.username || members.value.find(row => row.id === item.scope_id)?.email) || `#${item.scope_id}` })) }
function current(kind: EnterpriseScope) { return kind === 'enterprise' ? props.quota || quotas.value.find(item => item.scope_type === kind) : quotas.value.find(item => item.scope_type === kind && item.scope_id === selected[kind]) }
async function load() {
  await read(signal => Promise.all([props.api.quotas(undefined, { signal }), props.api.departments({ signal }), loadMemberOptions(props.api, signal)]), ([rows, departmentRows, memberRows]) => {
    quotas.value = rows; departments.value = departmentRows; members.value = memberRows
    for (const kind of kinds) if (!options(kind).some(item => item.value === selected[kind])) selected[kind] = options(kind)[0]?.value ?? null
  })
}
onMounted(load)
watch(() => props.refreshKey, load)
</script>
