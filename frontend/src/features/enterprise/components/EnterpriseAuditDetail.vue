<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, ArrowRight } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import EnterpriseDialog from './EnterpriseDialog.vue'
import EnterpriseAuditValue from './EnterpriseAuditValue.vue'
import { auditObject as object, type AuditReferenceCatalog } from '../auditReferences'
import type { EnterpriseAuditLog } from '@/types/enterprise'
import { formatDateTime } from '@/utils/format'
const props = defineProps<{ log: EnterpriseAuditLog | null; actionLabel: string; targetLabel: string; catalog?: AuditReferenceCatalog }>()
defineEmits<{ close: [] }>()
const { t, te } = useI18n()
const copied = ref(false), copyError = ref(false)
const json = computed(() => JSON.stringify(props.log?.detail ?? null, null, 2))
type Entry = { key: string; value?: unknown; before?: unknown; after?: unknown; comparison?: boolean }
function pair(value: Record<string, unknown>) {
  for (const [before, after] of [['before', 'after'], ['old', 'new'], ['from', 'to'], ['old_value', 'new_value']]) if (before in value && after in value) return { before: value[before], after: value[after], keys: [before, after] }
  return null
}
function flatten(value: Record<string, unknown>, prefix = '', depth = 0): Entry[] {
  const comparison = pair(value)
  if (comparison) {
    const rows: Entry[] = []
    const compare = (before: unknown, after: unknown, path: string, level: number) => {
      if ((object(before) || object(after)) && level < 5) {
        const left = object(before) ? before : {}, right = object(after) ? after : {}
        for (const key of new Set([...Object.keys(left), ...Object.keys(right)])) compare(left[key], right[key], path ? `${path}.${key}` : key, level + 1)
      } else rows.push({ key: path || (['group_pool.update', 'group_whitelist.update'].includes(props.log?.action ?? '') ? 'group_ids' : t('enterprise.detail')), comparison: true, before, after })
    }
    compare(comparison.before, comparison.after, prefix, depth)
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
        <div v-for="entry in entries" :key="entry.key" class="grid min-w-0 gap-2 p-4 text-sm sm:grid-cols-[10rem_minmax(0,1fr)]">
          <dt class="break-words font-medium text-muted-foreground">{{ fieldLabel(entry.key) }}</dt>
          <dd v-if="entry.comparison" class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-start gap-2" data-test="audit-detail-comparison"><div class="min-w-0"><span class="mb-1 block text-xs text-muted-foreground">{{ t('enterprise.auditDetails.before') }}</span><EnterpriseAuditValue :field="entry.key" :value="entry.before" :other="entry.after" change="removed" :catalog="catalog ?? {}" :log="log" /></div><ArrowRight class="mt-7 size-4 text-muted-foreground" :aria-label="t('enterprise.auditDetails.changedTo')" /><div class="min-w-0"><span class="mb-1 block text-xs text-muted-foreground">{{ t('enterprise.auditDetails.after') }}</span><EnterpriseAuditValue :field="entry.key" :value="entry.after" :other="entry.before" change="added" :catalog="catalog ?? {}" :log="log" /></div></dd>
          <dd v-else class="min-w-0"><EnterpriseAuditValue :field="entry.key" :value="entry.value" :catalog="catalog ?? {}" :log="log" /></dd>
        </div>
      </dl>
      <pre v-else data-test="audit-detail-json" class="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-ui-lg bg-muted p-4 text-xs">{{ json }}</pre>
      <div class="flex flex-wrap items-center justify-end gap-2"><p v-if="copyError" role="alert" class="text-sm text-destructive">{{ t('enterprise.auditDetails.copyError') }}</p><Button type="button" variant="outline" data-test="audit-detail-copy" @click="copy"><Copy class="mr-2 size-4" />{{ t(copied ? 'enterprise.auditDetails.copied' : 'enterprise.auditDetails.copyJson') }}</Button></div>
    </template>
  </EnterpriseDialog>
</template>
