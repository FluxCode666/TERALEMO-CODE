<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
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
const showConfiguration = computed(() => props.admin || ['owner', 'manager'].includes(props.overview?.me.role ?? ''))
const editable = computed(() => !props.readonly && !!(props.admin || props.overview?.me.role === 'owner'))
const candidates = computed(() => pool.value.filter(group => group.status === 'active'))
async function load() {
  await read(signal => Promise.all([
    props.admin && 'allowedGroups' in props.api ? props.api.allowedGroups({ signal }) : props.api.groups({ signal }),
    showConfiguration.value ? props.api.groupWhitelists({ signal }) : Promise.resolve([])
  ]), ([groups, lists]) => {
    pool.value = groups
    selection.value = lists.find(row => row.scope_type === 'enterprise' && row.scope_id === props.enterpriseId)?.group_ids ?? []
    loaded.value = true
  })
}
async function save(ids: number[]) {
  if (!editable.value) return
  if (await run(() => props.api.updateGroupWhitelist('enterprise', props.enterpriseId, ids), t('enterprise.saved'))) { emit('updated'); await load() }
}
onMounted(load)
watch(() => props.refreshKey, load)
</script>
<template>
  <section class="shadcn-scope space-y-5" :aria-busy="busy">
    <header><h2 class="text-2xl font-semibold tracking-tight">{{ t('enterprise.groupAccess.title') }}</h2><p class="mt-2 text-sm text-muted-foreground">{{ t('enterprise.groupAccess.description') }}</p></header>
    <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }} <Button variant="ghost" @click="load">{{ t('enterprise.retry') }}</Button></p>
    <Card class="space-y-4 p-4 sm:p-6" data-test="group-pool">
      <h3 class="font-semibold">{{ t('enterprise.groupAccess.availableTitle') }}</h3>
      <p class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.availableHint') }}</p>
      <Skeleton v-if="busy && !loaded" class="h-24" />
      <EnterpriseGroupCards v-else-if="pool.length" :groups="pool" />
      <p v-else-if="loaded" class="rounded-ui-md bg-muted/50 p-4 text-sm text-muted-foreground">{{ t('enterprise.groupAccess.personalRules') }}</p>
    </Card>
    <section v-if="loaded && showConfiguration" data-test="group-section-enterprise">
      <EnterpriseGroupEditor :title="t('enterprise.groupAccess.companyConfiguration')" :group-ids="selection" :groups="candidates" :editable="editable" :busy="busy" @save="save" />
    </section>
  </section>
</template>
