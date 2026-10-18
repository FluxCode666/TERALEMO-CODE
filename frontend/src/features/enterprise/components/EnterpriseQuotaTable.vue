<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight, Pencil, MoreHorizontal } from '@lucide/vue'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'
import type { EnterpriseQuota } from '@/types/enterprise'
import { quotaWindows, quotaStatus, quotaPercentLabel, type QuotaTableRow } from '../quotaPresentation'
import EnterpriseQuotaCell from './EnterpriseQuotaCell.vue'
const props = defineProps<{ rows: QuotaTableRow[]; kind: string; canEdit: (quota: EnterpriseQuota) => boolean; canReset?: boolean; selectable?: boolean; selected?: number[]; busy?: boolean }>()
const emit = defineEmits<{ edit: [quota: EnterpriseQuota]; reset: [quota: EnterpriseQuota]; toggle: [id: number]; select: [id: number, checked: boolean]; selectPage: [checked: boolean] }>()
const { t } = useI18n()
const editableRows = computed(() => props.rows.filter(row => row.quota && props.canEdit(row.quota)))
const pageSelection = computed(() => {
  const selected = editableRows.value.filter(row => props.selected?.includes(row.id)).length
  return selected && selected < editableRows.value.length ? 'indeterminate' : !!selected
})
function statusClass(quota: EnterpriseQuota) {
  const tone = quotaStatus(quota).tone
  return cn('whitespace-nowrap font-normal', tone === 'exceeded' && 'border-destructive/30 bg-destructive/10 text-destructive', tone === 'warning' && 'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-300')
}
</script>
<template>
  <Table :role="kind === 'department' ? 'treegrid' : undefined" :aria-label="t(`enterprise.quotaSections.${kind}.title`)" class="text-sm">
    <TableHeader><TableRow class="bg-muted/40 hover:bg-muted/40">
      <TableHead v-if="selectable" class="w-10"><Checkbox :model-value="pageSelection" :disabled="busy || !editableRows.length" :aria-label="t('enterprise.quotaTable.selectPage')" data-test="quota-select-page" @update:model-value="emit('selectPage', $event === true)" /></TableHead>
      <TableHead class="min-w-40">{{ t(`enterprise.${kind}Scope`) }}</TableHead>
      <TableHead v-for="window in quotaWindows" :key="window" class="whitespace-nowrap py-3">{{ t(`enterprise.quotaTable.columns.${window}`) }}</TableHead>
      <TableHead>{{ t('enterprise.status') }}</TableHead><TableHead class="text-right">{{ t('enterprise.actions') }}</TableHead>
    </TableRow></TableHeader>
    <TableBody>
      <TableRow v-for="row in rows" :key="row.id" :data-test="`quota-${kind}-${row.id}`" :aria-level="kind === 'department' ? (row.level ?? 0) + 1 : undefined" :aria-expanded="row.hasChildren ? row.expanded : undefined" :data-state="selected?.includes(row.id) ? 'selected' : undefined">
        <TableCell v-if="selectable"><Checkbox v-if="row.quota && canEdit(row.quota)" :model-value="selected?.includes(row.id) ?? false" :disabled="busy" :aria-label="t('enterprise.quotaTable.selectMember', { name: row.label })" :data-test="`quota-select-${row.id}`" @update:model-value="emit('select', row.id, $event === true)" /></TableCell>
        <TableCell><div class="flex min-w-0 items-center gap-1" :style="{ paddingLeft: `${(row.level ?? 0) * 16}px` }"><Button v-if="row.hasChildren" variant="ghost" size="icon" class="size-6 shrink-0" :disabled="busy" :aria-expanded="row.expanded" :aria-label="t('enterprise.quotaTable.toggleDepartment', { name: row.label })" :data-test="`quota-toggle-${row.id}`" @click="emit('toggle', row.id)"><ChevronRight class="size-4" :class="row.expanded ? 'rotate-90' : ''" /></Button><span v-else-if="kind === 'department'" class="w-6 shrink-0" /><div class="min-w-0"><p class="max-w-64 break-words font-medium">{{ row.label }}<span v-if="row.self" class="text-muted-foreground"> {{ t('enterprise.quotaTable.me') }}</span></p><p v-if="row.username" class="mt-1 text-xs text-muted-foreground">{{ row.username }}</p></div></div></TableCell>
        <TableCell v-for="window in quotaWindows" :key="window"><EnterpriseQuotaCell v-if="row.quota" :quota="row.quota" :window="window" /><span v-else class="text-muted-foreground">—</span></TableCell>
        <TableCell><Badge v-if="row.quota" variant="outline" :class="statusClass(row.quota)" :data-status="quotaStatus(row.quota).tone" data-test="quota-status">{{ t(`enterprise.quotaTable.status.${quotaStatus(row.quota).tone}`) }}<template v-if="quotaStatus(row.quota).percent !== undefined"> {{ quotaPercentLabel(quotaStatus(row.quota).percent!) }}</template></Badge><span v-else>—</span></TableCell>
        <TableCell><div v-if="row.quota" class="flex justify-end gap-1"><Button v-if="canEdit(row.quota)" variant="ghost" size="icon" :disabled="busy" :aria-label="`${row.label} · ${t('enterprise.edit')}`" :data-test="`quota-edit-${kind}-${row.id}`" @click="emit('edit', row.quota)"><Pencil class="size-4" /></Button><DropdownMenu v-if="canReset"><DropdownMenuTrigger as-child><Button variant="ghost" size="icon" :disabled="busy" :aria-label="`${row.label} · ${t('enterprise.actions')}`" :data-test="`quota-menu-${kind}-${row.id}`"><MoreHorizontal class="size-4" /></Button></DropdownMenuTrigger><DropdownMenuContent align="end"><DropdownMenuItem :data-test="`quota-reset-${kind}-${row.id}`" @select="emit('reset', row.quota)">{{ t('enterprise.resetUsage') }}</DropdownMenuItem></DropdownMenuContent></DropdownMenu></div></TableCell>
      </TableRow>
      <TableRow v-if="!rows.length"><TableCell :colspan="selectable ? 8 : 7" class="h-24 text-center text-muted-foreground">{{ t('enterprise.empty') }}</TableCell></TableRow>
    </TableBody>
  </Table>
</template>
