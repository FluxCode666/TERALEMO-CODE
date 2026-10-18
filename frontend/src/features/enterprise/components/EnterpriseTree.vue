<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Building2, ChevronDown, ChevronRight, Crown, FolderTree, UserRound } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { filterOrganizationTree, flattenOrganizationTree, type OrganizationNode } from '../departmentTree'
const props = defineProps<{ nodes: OrganizationNode[]; modelValue: string; search?: string; label: string; disabledKeys?: Set<string> }>()
const emit = defineEmits<{ 'update:modelValue': [key: string] }>()
const { t } = useI18n()
const root = ref<HTMLElement>()
const expanded = ref(new Set<string>())
const known = new Set<string>()
watch(() => props.nodes, nodes => {
  const visit = (items: OrganizationNode[]) => items.forEach(node => { if (!known.has(node.key)) { known.add(node.key); if (node.children.length) expanded.value.add(node.key) } visit(node.children) })
  visit(nodes)
}, { immediate: true })
const filtered = computed(() => filterOrganizationTree(props.nodes, props.search ?? ''))
const openKeys = computed(() => props.search?.trim() ? filtered.value.expanded : expanded.value)
const rows = computed(() => flattenOrganizationTree(filtered.value.nodes, openKeys.value))
const focusKey = ref(props.modelValue)
const activeKey = computed(() => rows.value.some(row => row.node.key === focusKey.value && row.node.kind !== 'empty') ? focusKey.value : rows.value.find(row => row.node.kind !== 'empty')?.node.key)
function toggle(key: string) { const next = new Set(expanded.value); if (next.has(key)) next.delete(key); else next.add(key); expanded.value = next }
function select(key: string) { focusKey.value = key; if (!props.disabledKeys?.has(key)) emit('update:modelValue', key) }
async function focus(key?: string) { if (!key) return; focusKey.value = key; await nextTick(); const item = Array.from(root.value?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? []).find(el => el.dataset.key === key); item?.focus() }
function keydown(event: KeyboardEvent, key: string) {
  const items = rows.value.filter(row => row.node.kind !== 'empty'), index = items.findIndex(row => row.node.key === key), item = items[index]
  if (!item) return
  if (['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight', 'Home', 'End', 'Enter', ' '].includes(event.key)) event.preventDefault()
  if (event.key === 'ArrowDown') void focus(items[Math.min(index + 1, items.length - 1)]?.node.key)
  if (event.key === 'ArrowUp') void focus(items[Math.max(index - 1, 0)]?.node.key)
  if (event.key === 'Home') void focus(items[0]?.node.key)
  if (event.key === 'End') void focus(items.at(-1)?.node.key)
  if (event.key === 'ArrowRight' && item.node.children.length) { if (!openKeys.value.has(key)) toggle(key); else void focus(items[index + 1]?.node.key) }
  if (event.key === 'ArrowLeft') { if (item.node.children.length && openKeys.value.has(key)) toggle(key); else void focus(item.parent) }
  if (event.key === 'Enter' || event.key === ' ') select(key)
}
function parts(name: string) {
  const query = props.search?.trim() ?? '', index = query ? name.toLocaleLowerCase().indexOf(query.toLocaleLowerCase()) : -1
  return index < 0 ? [name, '', ''] : [name.slice(0, index), name.slice(index, index + query.length), name.slice(index + query.length)]
}
</script>
<template>
  <div ref="root" role="tree" :aria-label="label" class="shadcn-scope min-w-0 space-y-0.5">
    <template v-for="{ node, level } in rows" :key="node.key">
      <div v-if="node.kind === 'empty'" class="tree-line py-2 text-xs text-muted-foreground" :style="{ paddingLeft: `${(level - 1) * 20 + 28}px`, '--tree-indent': `${(level - 1) * 20}px` }">{{ node.name }}</div>
      <Button v-else variant="ghost" role="treeitem" :data-key="node.key" :data-test="`org-node-${node.key}`" :aria-level="level" :aria-selected="modelValue === node.key" :aria-expanded="node.children.length ? openKeys.has(node.key) : undefined" :aria-disabled="disabledKeys?.has(node.key) || undefined" :tabindex="activeKey === node.key ? 0 : -1" :class="cn('tree-line flex h-auto min-h-10 w-full justify-start gap-2 rounded-md py-2 pr-2 text-left font-normal', modelValue === node.key && 'bg-primary/10 font-medium text-primary', disabledKeys?.has(node.key) && 'opacity-40')" :style="{ paddingLeft: `${(level - 1) * 20 + 8}px`, '--tree-indent': `${(level - 1) * 20}px` }" @focus="focusKey = node.key" @click="select(node.key)" @keydown="keydown($event, node.key)">
        <span v-if="node.children.length" class="flex size-5 shrink-0 items-center justify-center rounded hover:bg-muted" aria-hidden="true" @click.stop="toggle(node.key)"><ChevronDown v-if="openKeys.has(node.key)" class="size-3.5" /><ChevronRight v-else class="size-3.5" /></span><span v-else class="w-5 shrink-0" />
        <Crown v-if="node.member?.role === 'owner'" class="size-4 shrink-0 text-amber-600" /><UserRound v-else-if="node.kind === 'member'" class="size-4 shrink-0 text-muted-foreground" /><Building2 v-else-if="node.kind === 'root'" class="size-4 shrink-0" /><FolderTree v-else class="size-4 shrink-0 text-muted-foreground" />
        <span class="min-w-0 break-words whitespace-normal"><span>{{ parts(node.name)[0] }}</span><mark v-if="parts(node.name)[1]" class="rounded-sm bg-amber-200 text-amber-950">{{ parts(node.name)[1] }}</mark><span>{{ parts(node.name)[2] }}</span><span v-if="search && node.member && parts(node.member.email)[1]" class="mt-0.5 block break-all text-xs text-muted-foreground">{{ parts(node.member.email)[0] }}<mark class="rounded-sm bg-amber-200 text-amber-950">{{ parts(node.member.email)[1] }}</mark>{{ parts(node.member.email)[2] }}</span><span v-if="node.count !== undefined" class="ml-1 text-muted-foreground">({{ node.count }})</span></span>
        <Badge v-if="node.member?.role === 'manager'" variant="secondary" class="ml-auto shrink-0 text-[10px]">{{ t('enterprise.roles.manager') }}</Badge>
      </Button>
    </template>
    <p v-if="!rows.length" class="p-4 text-sm text-muted-foreground">{{ t('enterprise.orgTree.noMatches') }}</p>
  </div>
</template>
<style scoped>
.tree-line { background-image: repeating-linear-gradient(to right, transparent 0, transparent 18px, hsl(var(--border)) 18px, hsl(var(--border)) 19px, transparent 19px, transparent 20px); background-size: var(--tree-indent) 100%; background-repeat: no-repeat; }
</style>
