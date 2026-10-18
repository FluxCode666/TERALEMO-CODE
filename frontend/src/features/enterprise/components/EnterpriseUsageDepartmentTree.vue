<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, ChevronRight, ChevronsUpDown } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { cn } from '@/lib/utils'
import { usageDepartmentTree, type UsageDepartment, type UsageDepartmentNode } from '../EnterpriseUsageSeries'
const props = defineProps<{ modelValue: string; departments: UsageDepartment[]; rootId?: number | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
const open = ref(false)
const expanded = ref(new Set<number>())
const focused = ref<number>()
const tree = ref<HTMLElement>()
const roots = computed(() => usageDepartmentTree(props.departments, props.rootId))
const rows = computed(() => {
  const result: { node: UsageDepartmentNode; level: number; parent?: number }[] = []
  function visit(nodes: UsageDepartmentNode[], level: number, parent?: number) {
    nodes.forEach(node => {
      result.push({ node, level, parent })
      if (expanded.value.has(node.department.id)) visit(node.children, level + 1, node.department.id)
    })
  }
  visit(roots.value, 1)
  return result
})
const selected = computed(() => props.departments.find(item => String(item.id) === props.modelValue))
const label = computed(() => selected.value?.path_names?.join(' / ') || selected.value?.name)
watch(open, async value => {
  if (!value) return
  // Reveal selected descendants and show the hierarchy on first open.
  expanded.value = new Set(props.departments.map(item => item.id))
  focused.value = Number(props.modelValue) || roots.value[0]?.department.id
  await nextTick()
})
function select(value: string) { emit('update:modelValue', value); open.value = false }
function toggle(id: number) { const next = new Set(expanded.value); if (next.has(id)) next.delete(id); else next.add(id); expanded.value = next }
async function focus(id?: number) {
  focused.value = id
  await nextTick()
  tree.value?.querySelector<HTMLElement>(`[data-department-id="${id}"]`)?.focus()
}
function keydown(event: KeyboardEvent, row: (typeof rows.value)[number]) {
  const id = row.node.department.id
  const index = rows.value.findIndex(item => item.node.department.id === id)
  if (['ArrowDown', 'ArrowUp', 'ArrowRight', 'ArrowLeft', 'Home', 'End', 'Enter', ' '].includes(event.key)) event.preventDefault()
  if (event.key === 'ArrowDown') void focus(rows.value[Math.min(index + 1, rows.value.length - 1)]?.node.department.id)
  if (event.key === 'ArrowUp') void focus(rows.value[Math.max(index - 1, 0)]?.node.department.id)
  if (event.key === 'Home') void focus(rows.value[0]?.node.department.id)
  if (event.key === 'End') void focus(rows.value.at(-1)?.node.department.id)
  if (event.key === 'Enter' || event.key === ' ') select(String(id))
  if (event.key === 'ArrowRight' && row.node.children.length) { if (!expanded.value.has(id)) toggle(id); else void focus(row.node.children[0].department.id) }
  if (event.key === 'ArrowLeft') { if (row.node.children.length && expanded.value.has(id)) toggle(id); else if (row.parent) void focus(row.parent) }
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child><Button type="button" variant="outline" role="combobox" aria-haspopup="tree" :aria-expanded="open" :aria-label="t('enterprise.department')" data-test="usage-department" class="w-full min-w-0 justify-between gap-2 font-normal" :title="label">
      <span class="truncate"><span class="text-xs text-muted-foreground">{{ t('enterprise.department') }} · </span>{{ label || t('enterprise.all') }}</span><ChevronsUpDown class="size-4 shrink-0 text-muted-foreground" />
    </Button></PopoverTrigger>
    <PopoverContent align="start" class="w-80 max-w-[calc(100vw-2rem)] p-2">
      <p class="px-2 py-1 text-xs text-muted-foreground">{{ t('enterprise.usageAnalysis.departmentSubtree') }}</p>
      <Button type="button" variant="ghost" class="mb-1 w-full justify-start" data-test="usage-department-all" @click="select('')">{{ t('enterprise.all') }}<Check v-if="!modelValue" class="ml-auto size-4" /></Button>
      <div ref="tree" role="tree" :aria-label="t('enterprise.department')" class="max-h-72 overflow-auto">
        <div v-for="row in rows" :key="row.node.department.id" role="treeitem" :data-department-id="row.node.department.id" :aria-level="row.level" :aria-expanded="row.node.children.length ? expanded.has(row.node.department.id) : undefined" :aria-selected="modelValue === String(row.node.department.id)" :tabindex="focused === row.node.department.id ? 0 : -1" :class="cn('flex cursor-pointer items-center gap-1 rounded-sm py-2 pr-2 text-sm outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring', modelValue === String(row.node.department.id) && 'bg-accent')" :style="{ paddingLeft: `${(row.level - 1) * 16 + 4}px` }" @focus="focused = row.node.department.id" @click="select(String(row.node.department.id))" @keydown="keydown($event, row)">
          <button v-if="row.node.children.length" type="button" tabindex="-1" :aria-label="t('enterprise.usageAnalysis.toggleDepartment')" class="shrink-0 rounded p-0.5" @click.stop="toggle(row.node.department.id)"><ChevronRight :class="cn('size-3.5', expanded.has(row.node.department.id) && 'rotate-90')" /></button><span v-else class="w-[18px] shrink-0" />
          <span class="min-w-0 flex-1 truncate" :title="row.node.department.path_names?.join(' / ')">{{ row.node.department.name }}</span><Check v-if="modelValue === String(row.node.department.id)" class="size-4 shrink-0" />
        </div>
      </div>
      <p v-if="!rows.length" class="p-3 text-sm text-muted-foreground">{{ t('enterprise.empty') }}</p>
    </PopoverContent>
  </Popover>
</template>
