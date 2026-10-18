<template>
  <section class="shadcn-scope space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2"><h2 class="font-semibold text-foreground">{{ t('enterprise.quotas') }}</h2><Button variant="ghost" class="text-sm text-primary" @click="$emit('manage')">{{ t('enterprise.view') }}</Button></div>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <Button variant="ghost" class="underline" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <div class="grid gap-4 xl:grid-cols-3" :aria-busy="busy">
      <div v-for="kind in kinds" :key="kind" class="min-w-0 space-y-3">
        <EnterpriseSelect v-if="kind !== 'enterprise' && options(kind).length" v-model="selected[kind]" :options="options(kind)" :aria-label="t(`enterprise.quotaSections.${kind}.title`)" />
        <EnterpriseQuotaDisplay v-if="current(kind)" :quota="current(kind)!" :title="t(`enterprise.quotaSections.${kind}.title`)" compact :data-test="`${kind}-quota`" />
        <Card v-else class="rounded-ui-lg border bg-card text-card-foreground shadow-sm p-5"><h3 class="font-medium text-foreground">{{ t(`enterprise.quotaSections.${kind}.title`) }}</h3><p class="mt-4 text-sm text-muted-foreground">{{ t(busy ? 'common.loading' : 'enterprise.empty') }}</p></Card>
      </div>
    </div>
  </section>
</template>
<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'

import { onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseDepartment, EnterpriseMember, EnterpriseQuota, EnterpriseScope } from '@/types/enterprise'
import EnterpriseSelect from '@/features/enterprise/components/EnterpriseSelect.vue'
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
