<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getAll as getAllGroups } from '@/api/admin/groups'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseGroup, EnterpriseOverview } from '@/types/enterprise'
import EnterpriseGroupCards from './EnterpriseGroupCards.vue'
import EnterpriseGroupEditor from './EnterpriseGroupEditor.vue'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; enterpriseId: number; admin?: boolean; readonly?: boolean; overview?: EnterpriseOverview; refreshKey?: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, run, read } = useEnterpriseAction()
const pool = ref<EnterpriseGroup[]>([]), selection = ref<number[]>([]), loaded = ref(false)
const editingPool = ref(false), available = ref<EnterpriseGroup[]>([]), poolSelection = ref<number[]>([])
const rates = ref<Record<number, string | number>>({}), validation = ref('')
const publicGroups = ref<EnterpriseGroup[]>([])
const showConfiguration = computed(() => props.admin || ['owner', 'manager'].includes(props.overview?.me.role ?? ''))
const editable = computed(() => !props.readonly && !!(props.admin || props.overview?.me.role === 'owner'))
const poolEditable = computed(() => props.admin && !props.readonly && 'updateAllowedGroups' in props.api)
// An empty pool leaves the enterprise unrestricted, so administrators pick from the
// same active public groups an owner would see.
const candidates = computed(() => (props.admin && !pool.value.length ? publicGroups.value : pool.value).filter(group => group.status === 'active'))
const selectedPoolGroups = computed(() => available.value.filter(group => poolSelection.value.includes(group.id)))
async function load() {
  await read(async signal => {
    const [groups, lists] = await Promise.all([
      props.admin && 'allowedGroups' in props.api ? props.api.allowedGroups({ signal }) : props.api.groups({ signal }),
      showConfiguration.value ? props.api.groupWhitelists({ signal }) : Promise.resolve([])
    ])
    const unrestricted = props.admin && !groups.length ? (await getAllGroups()).filter(group => !group.is_exclusive) : []
    return [groups, lists, unrestricted] as const
  }, ([groups, lists, unrestricted]) => {
    pool.value = groups
    publicGroups.value = unrestricted.map(group => ({ ...group, default_rate_multiplier: group.rate_multiplier, effective_rate_multiplier: group.rate_multiplier }))
    selection.value = lists.find(row => row.scope_type === 'enterprise' && row.scope_id === props.enterpriseId)?.group_ids ?? []
    loaded.value = true
  })
}
async function save(ids: number[]) {
  if (!editable.value) return
  if (await run(() => props.api.updateGroupWhitelist('enterprise', props.enterpriseId, ids), t('enterprise.saved'))) { emit('updated'); await load() }
}
async function editPool() {
  if (!poolEditable.value) return
  await read(() => getAllGroups(), groups => {
    const existing = new Map(pool.value.map(group => [group.id, group]))
    available.value = groups.map(group => ({ ...group, rate_multiplier: existing.get(group.id)?.rate_multiplier ?? null, default_rate_multiplier: group.rate_multiplier, effective_rate_multiplier: existing.get(group.id)?.effective_rate_multiplier ?? existing.get(group.id)?.rate_multiplier ?? group.rate_multiplier }))
    for (const group of pool.value) if (!available.value.some(item => item.id === group.id)) available.value.push(group)
    poolSelection.value = pool.value.map(group => group.id)
    rates.value = Object.fromEntries(pool.value.map(group => [group.id, group.rate_multiplier ?? '']))
    validation.value = ''
    editingPool.value = true
  })
}
async function savePool() {
  if (!poolEditable.value || !('updateAllowedGroups' in props.api)) return
  const groups = selectedPoolGroups.value.map(group => {
    const value = String(rates.value[group.id] ?? '').trim()
    return { group_id: group.id, rate_multiplier: value === '' ? null : Number(value) }
  })
  if (groups.some(group => group.rate_multiplier !== null && (!Number.isFinite(group.rate_multiplier) || group.rate_multiplier <= 0))) { validation.value = t('enterprise.groupAccess.validationRate'); return }
  validation.value = ''
  const api = props.api
  if (await run(() => api.updateAllowedGroups({ groups }), t('enterprise.saved'))) { editingPool.value = false; emit('updated'); await load() }
}
onMounted(load)
watch(() => props.refreshKey, load)
</script>
<template>
  <section class="shadcn-scope space-y-5" :aria-busy="busy">
    <header><h2 class="text-2xl font-semibold tracking-tight">{{ t('enterprise.groupAccess.title') }}</h2><p class="mt-2 text-sm text-muted-foreground">{{ t('enterprise.groupAccess.description') }}</p></header>
    <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }} <Button variant="ghost" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <Card class="space-y-4 p-4 sm:p-6" data-test="group-pool">
      <div class="flex flex-wrap items-center justify-between gap-3"><h3 class="font-semibold">{{ t('enterprise.groupAccess.availableTitle') }}</h3><Button v-if="poolEditable && !editingPool" type="button" variant="outline" :disabled="busy" data-test="group-pool-edit" @click="editPool">{{ t('enterprise.groupAccess.editAvailable') }}</Button></div>
      <p v-if="!editingPool" class="text-sm text-muted-foreground">{{ t(poolEditable ? 'enterprise.groupAccess.adminAvailableHint' : 'enterprise.groupAccess.availableHint') }}</p>
      <Skeleton v-if="busy && !loaded" class="h-24" />
      <form v-else-if="editingPool && poolEditable" class="space-y-4" data-test="group-pool-form" @submit.prevent="savePool">
        <p class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.adminHint') }}</p>
        <div class="flex flex-wrap gap-2"><Button type="button" variant="outline" size="sm" :disabled="busy || !available.length" data-test="group-pool-select-all" @click="poolSelection = available.map(group => group.id)">{{ t('enterprise.groupAccess.selectAll') }}</Button><Button type="button" variant="ghost" size="sm" :disabled="busy || !poolSelection.length" data-test="group-pool-clear" @click="poolSelection = []">{{ t('enterprise.groupAccess.clear') }}</Button></div>
        <EnterpriseGroupCards v-model="poolSelection" :groups="available" selectable :disabled="busy" />
        <p v-if="!available.length" class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.noAvailable') }}</p>
        <div v-for="group in selectedPoolGroups" :key="group.id" class="grid items-center gap-3 rounded-ui-md border p-4 sm:grid-cols-[minmax(0,1fr)_12rem]">
          <div class="min-w-0"><Label :for="`enterprise-group-rate-${group.id}`" class="break-words">{{ group.name }} · {{ t('enterprise.groupAccess.enterpriseRate') }}</Label><p :id="`enterprise-group-rate-hint-${group.id}`" class="mt-1 text-xs text-muted-foreground">{{ t('enterprise.groupAccess.rateHint') }}</p></div>
          <Input :id="`enterprise-group-rate-${group.id}`" v-model="rates[group.id]" type="number" min="0" step="any" :placeholder="String(group.default_rate_multiplier ?? '')" :disabled="busy" :aria-describedby="`enterprise-group-rate-hint-${group.id}`" :data-test="`group-pool-rate-${group.id}`" />
        </div>
        <p v-if="validation" role="alert" class="text-sm text-destructive">{{ validation }}</p>
        <div class="flex flex-wrap gap-2"><Button :disabled="busy" data-test="group-pool-save">{{ t('enterprise.save') }}</Button><Button type="button" variant="outline" :disabled="busy" data-test="group-pool-cancel" @click="editingPool = false">{{ t('enterprise.cancel') }}</Button></div>
      </form>
      <EnterpriseGroupCards v-else-if="pool.length" :groups="pool" />
      <p v-else-if="loaded" class="rounded-ui-md bg-muted/50 p-4 text-sm text-muted-foreground" data-test="group-pool-empty">{{ t(poolEditable ? 'enterprise.groupAccess.adminEmptyPool' : 'enterprise.groupAccess.personalRules') }}</p>
    </Card>
    <section v-if="loaded && showConfiguration" data-test="group-section-enterprise">
      <EnterpriseGroupEditor :title="t('enterprise.groupAccess.companyConfiguration')" :group-ids="selection" :groups="candidates" :editable="editable" :busy="busy" @save="save" />
    </section>
  </section>
</template>
