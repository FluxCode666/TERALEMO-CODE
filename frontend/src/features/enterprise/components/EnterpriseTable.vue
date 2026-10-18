<script setup lang="ts" generic="T extends { id: number }">
import { nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import EnterpriseEmpty from './EnterpriseEmpty.vue'
import { useMotion } from '@/composables/useMotion'
const props = defineProps<{ columns: { key: string; label: string }[]; data: T[]; loading?: boolean }>()
const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const motion = useMotion(root)
async function animate() { await nextTick(); motion.stop(); motion.stagger('[data-enterprise-row]', { stagger: 0.025, distance: 4 }) }
watch(() => props.data, animate)
onMounted(animate)
function value(row: T, key: string) { return (row as Record<string, unknown>)[key] }
function pinned(key: string) { return key === 'actions' || key === 'detail' }
function cellClass(key: string) {
  if (pinned(key)) return 'sticky right-0 z-10 w-px border-l bg-card whitespace-nowrap'
  if (['name', 'username', 'actor_email', 'operator_email', 'department_name', 'target'].includes(key)) return 'min-w-24 max-w-64 whitespace-normal break-words'
  return ['notes', 'title'].includes(key) ? 'min-w-24 whitespace-normal break-words' : 'whitespace-nowrap'
}
</script>
<template>
  <div ref="root" class="shadcn-scope min-w-0 overflow-hidden rounded-ui-lg border bg-card text-card-foreground" :aria-busy="loading">
    <Table>
      <TableHeader><TableRow><TableHead v-for="column in columns" :key="column.key" class="whitespace-nowrap" :class="pinned(column.key) ? 'sticky right-0 z-20 w-px border-l bg-card' : ''">{{ column.label }}</TableHead></TableRow></TableHeader>
      <TableBody>
        <template v-if="loading && !data.length"><TableRow v-for="n in 5" :key="n"><TableCell v-for="column in columns" :key="column.key"><Skeleton class="h-5 w-full min-w-16" /></TableCell></TableRow></template>
        <TableRow v-for="row in data" v-else :key="row.id" data-enterprise-row><TableCell v-for="column in columns" :key="column.key" class="max-w-sm" :class="cellClass(column.key)"><slot :name="`cell-${column.key}`" :row="row" :value="value(row, column.key)">{{ value(row, column.key) ?? '—' }}</slot></TableCell></TableRow>
        <TableRow v-if="!loading && !data.length"><TableCell :colspan="columns.length"><slot name="empty"><EnterpriseEmpty :title="t('enterprise.empty')" /></slot></TableCell></TableRow>
      </TableBody>
    </Table>
  </div>
</template>
