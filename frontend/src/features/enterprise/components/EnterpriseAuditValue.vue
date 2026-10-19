<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import { auditReferenceIds, auditReferenceLabel, auditReferenceType, type AuditReferenceCatalog } from '../auditReferences'
import type { EnterpriseAuditLog } from '@/types/enterprise'

const props = defineProps<{ field: string; value: unknown; other?: unknown; change?: 'added' | 'removed'; catalog: AuditReferenceCatalog; log: EnterpriseAuditLog }>()
const { t } = useI18n()
const type = computed(() => auditReferenceType(props.field))
const ids = computed(() => type.value ? auditReferenceIds(props.value) : null)
const otherIds = computed(() => auditReferenceIds(props.other))
const text = computed(() => props.value === undefined ? '—' : typeof props.value === 'string' ? props.value : JSON.stringify(props.value, null, 2))
function changed(id: number) { return props.change && !(otherIds.value ?? []).includes(id) }
</script>
<template>
  <div v-if="type && ids !== null" class="flex flex-wrap gap-1.5" data-test="audit-reference-values">
    <Badge v-for="id in new Set(ids)" :key="id" variant="outline" class="max-w-full gap-1 whitespace-normal break-all text-left" :class="changed(id) ? change === 'added' ? 'border-primary/40 bg-primary/10 text-primary' : 'border-destructive/40 bg-destructive/5 text-destructive' : ''" :data-change="changed(id) ? change : undefined">
      <span v-if="changed(id)">{{ t(`enterprise.auditDetails.${change}`) }} ·</span>{{ auditReferenceLabel(type, id, catalog, t, log) }}
    </Badge>
    <span v-if="!ids.length" class="text-muted-foreground">—</span>
  </div>
  <pre v-else-if="typeof value === 'object' && value !== null" data-test="audit-value" class="whitespace-pre-wrap break-all rounded bg-muted p-2 text-xs">{{ text }}</pre>
  <span v-else data-test="audit-value" class="whitespace-pre-wrap break-all">{{ text }}</span>
</template>
