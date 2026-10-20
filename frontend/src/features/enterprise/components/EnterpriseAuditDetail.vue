<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, ArrowRight } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import EnterpriseDialog from './EnterpriseDialog.vue'
import EnterpriseAuditValue from './EnterpriseAuditValue.vue'
import { auditObject as object, auditReferenceLabel, auditValuesEqual, type AuditReferenceCatalog } from '../auditReferences'
import type { EnterpriseAuditLog } from '@/types/enterprise'
import { formatDateTime } from '@/utils/format'
const props = defineProps<{ log: EnterpriseAuditLog | null; actionLabel: string; targetLabel: string; catalog?: AuditReferenceCatalog }>()
defineEmits<{ close: [] }>()
const { t, te } = useI18n()
const copied = ref(false), copyError = ref(false)
const json = computed(() => JSON.stringify(props.log?.detail ?? null, null, 2))
type Entry = { key: string; value?: unknown; before?: unknown; after?: unknown; comparison?: boolean; changed?: boolean; groupId?: number }
function groupRates(value: unknown) {
  if (!Array.isArray(value) || !value.every(item => object(item) && Number.isSafeInteger(item.group_id) && Number(item.group_id) > 0 && (item.rate_multiplier === null || typeof item.rate_multiplier === 'number'))) return null
  return new Map(value.map(item => [item.group_id as number, item.rate_multiplier as number | null]))
}
function pair(value: Record<string, unknown>) {
  for (const [before, after] of [['before', 'after'], ['old', 'new'], ['from', 'to'], ['old_value', 'new_value']]) if (before in value || after in value) return { before: value[before], after: value[after], keys: [before, after] }
  return null
}
function flatten(value: Record<string, unknown>, prefix = '', depth = 0): Entry[] {
  const comparison = pair(value)
  if (comparison) {
    const rows: Entry[] = []
    const compare = (before: unknown, after: unknown, path: string, level: number, leftPresent = true, rightPresent = true) => {
      const leftRates = groupRates(before), rightRates = groupRates(after)
      if (path.split('.').at(-1) === 'groups' && (leftRates || rightRates)) {
        for (const id of new Set([...(leftRates?.keys() ?? []), ...(rightRates?.keys() ?? [])])) {
          const left = leftRates?.get(id), right = rightRates?.get(id)
          rows.push({ key: `${path}.${id}.rate_multiplier`, groupId: id, comparison: true, before: left, after: right, changed: leftRates?.has(id) !== rightRates?.has(id) || !Object.is(left, right) })
        }
      } else
      if ((object(before) || object(after)) && level < 5 && new Set([...Object.keys(object(before) ? before : {}), ...Object.keys(object(after) ? after : {})]).size) {
        const left = object(before) ? before : {}, right = object(after) ? after : {}
        for (const key of new Set([...Object.keys(left), ...Object.keys(right)])) compare(left[key], right[key], path ? `${path}.${key}` : key, level + 1, key in left, key in right)
      } else {
        const key = path || (['group_pool.update', 'group_whitelist.update'].includes(props.log?.action ?? '') ? 'group_ids' : t('enterprise.detail'))
        rows.push({ key, comparison: true, before, after, changed: leftPresent !== rightPresent || !auditValuesEqual(key, before, after) })
      }
    }
    compare(comparison.before, comparison.after, prefix, depth, comparison.keys[0]! in value, comparison.keys[1]! in value)
    const extra = Object.fromEntries(Object.entries(value).filter(([key]) => !comparison.keys.includes(key)))
    return [...rows, ...flatten(extra, prefix, depth + 1)]
  }
  return Object.entries(value).flatMap(([key, item]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (object(item) && depth < 4 && Object.keys(item).length) return flatten(item, path, depth + 1)
    return [{ key: path, value: item }]
  })
}
const entries = computed(() => object(props.log?.detail) ? flatten(props.log!.detail) : [])
function fieldLabel(key: string) { const translation = `enterprise.auditDetails.fields.${key.split('.').at(-1)}`; return te?.(translation) ? t(translation) : key }
function groupLabel(id: number) { return `${auditReferenceLabel('group', id, props.catalog ?? {}, t, props.log ?? undefined)} · ${t('enterprise.groupAccess.enterpriseRate')}` }
function groupRateText(value: unknown) { return value === undefined ? '—' : value === null ? t('enterprise.groupAccess.followDefault') : `${value}×` }
async function copy() {
  try { await navigator.clipboard.writeText(json.value); copied.value = true; copyError.value = false }
  catch { copyError.value = true }
}
</script>
<template>
  <EnterpriseDialog :show="!!log" :title="t('enterprise.auditDetails.title')" width="wide" @close="$emit('close'); copied = false; copyError = false">
    <template v-if="log">
      <dl class="grid gap-4 rounded-ui-lg border bg-muted/30 p-4 text-sm sm:grid-cols-2">
        <div><dt class="text-muted-foreground">{{ t('enterprise.action') }}</dt><dd class="mt-1 font-medium">{{ actionLabel }}</dd></div>
        <div><dt class="text-muted-foreground">{{ t('enterprise.operator') }}</dt><dd class="mt-1 break-all">{{ log.actor_email || '—' }}</dd></div>
        <div><dt class="text-muted-foreground">{{ t('enterprise.role') }}</dt><dd class="mt-1"><Badge variant="secondary">{{ t(log.actor_role === 'admin' ? 'enterprise.auditUi.admin' : `enterprise.roles.${log.actor_role}`) }}</Badge></dd></div>
        <div><dt class="text-muted-foreground">{{ t('enterprise.target') }}</dt><dd class="mt-1">{{ targetLabel }}</dd></div>
        <div class="sm:col-span-2"><dt class="text-muted-foreground">{{ t('enterprise.createdAt') }}</dt><dd class="mt-1">{{ formatDateTime(log.created_at) }}</dd></div>
      </dl>
      <dl v-if="entries.length" class="divide-y rounded-ui-lg border" data-test="audit-detail-fields">
        <div v-for="entry in entries" :key="entry.key" class="grid min-w-0 gap-2 border-l-4 p-4 text-sm sm:grid-cols-[10rem_minmax(0,1fr)]" :class="entry.changed ? 'border-l-amber-500 bg-amber-50 dark:border-l-amber-400 dark:bg-amber-950/40' : 'border-l-transparent'" :data-changed="entry.comparison ? !!entry.changed : undefined" :data-field="entry.key">
          <dt class="break-words font-medium text-muted-foreground">{{ entry.groupId ? groupLabel(entry.groupId) : fieldLabel(entry.key) }}<Badge v-if="entry.changed" variant="outline" class="mt-1 block w-fit border-amber-400/60 text-amber-900 dark:text-amber-200">{{ t('enterprise.auditDetails.changed') }}</Badge></dt>
          <dd v-if="entry.comparison" class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-start gap-2" data-test="audit-detail-comparison"><div class="min-w-0"><span class="mb-1 block text-xs text-muted-foreground">{{ t('enterprise.auditDetails.before') }}</span><span v-if="entry.groupId" data-test="audit-group-rate">{{ groupRateText(entry.before) }}</span><EnterpriseAuditValue v-else :field="entry.key" :value="entry.before" :other="entry.after" change="removed" :catalog="catalog ?? {}" :log="log" /></div><ArrowRight class="mt-7 size-4 text-muted-foreground" :aria-label="t('enterprise.auditDetails.changedTo')" /><div class="min-w-0"><span class="mb-1 block text-xs text-muted-foreground">{{ t('enterprise.auditDetails.after') }}</span><span v-if="entry.groupId" data-test="audit-group-rate">{{ groupRateText(entry.after) }}</span><EnterpriseAuditValue v-else :field="entry.key" :value="entry.after" :other="entry.before" change="added" :catalog="catalog ?? {}" :log="log" /></div></dd>
          <dd v-else class="min-w-0"><EnterpriseAuditValue :field="entry.key" :value="entry.value" :catalog="catalog ?? {}" :log="log" /></dd>
        </div>
      </dl>
      <pre v-else data-test="audit-detail-json" class="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-ui-lg bg-muted p-4 text-xs">{{ json }}</pre>
      <div class="flex flex-wrap items-center justify-end gap-2"><p v-if="copyError" role="alert" class="text-sm text-destructive">{{ t('enterprise.auditDetails.copyError') }}</p><Button type="button" variant="outline" data-test="audit-detail-copy" @click="copy"><Copy class="mr-2 size-4" />{{ t(copied ? 'enterprise.auditDetails.copied' : 'enterprise.auditDetails.copyJson') }}</Button></div>
    </template>
  </EnterpriseDialog>
</template>
