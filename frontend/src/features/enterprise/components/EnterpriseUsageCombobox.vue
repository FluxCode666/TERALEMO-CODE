<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ListboxFilter } from 'reka-ui'
import { Check, ChevronsUpDown, Search, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Command, CommandGroup, CommandItem, CommandList } from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Skeleton } from '@/components/ui/skeleton'
import type { EnterpriseUsageOption } from '../EnterpriseUsageOptionsAPI'

const props = defineProps<{
  modelValue: string; label: string; selectedLabel?: string
  options?: EnterpriseUsageOption[]
  searchOptions?: (search: string, signal: AbortSignal) => Promise<EnterpriseUsageOption[]>
  searchScope?: string
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; select: [value: EnterpriseUsageOption | undefined] }>()
const { t } = useI18n()
const open = ref(false), search = ref(''), loading = ref(false), failed = ref(false)
const items = ref<EnterpriseUsageOption[]>([])
let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined
let version = 0
function cancel() { clearTimeout(timer); controller?.abort(); version++ }
async function load() {
  cancel()
  const current = version
  controller = new AbortController()
  loading.value = true; failed.value = false
  try {
    const result = props.searchOptions
      ? await props.searchOptions(search.value.trim(), controller.signal)
      : (props.options ?? []).filter(item => `${item.label} ${item.detail ?? ''}`.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase()))
    if (version === current) items.value = result
  } catch {
    if (version === current) { failed.value = true; items.value = [] }
  } finally { if (version === current) loading.value = false }
}
watch(search, () => {
  cancel()
  if (!open.value) return
  loading.value = true
  timer = setTimeout(load, 250)
})
watch(open, value => {
  if (value) { search.value = ''; void load() }
  else { cancel(); loading.value = false }
})
watch([() => props.searchScope, () => props.options], () => { if (open.value) void load() })
onScopeDispose(cancel)
function select(item?: EnterpriseUsageOption) {
  emit('update:modelValue', item?.value ?? '')
  emit('select', item)
  open.value = false
}
</script>

<template>
  <div class="flex min-w-0 items-center gap-1">
    <Popover v-model:open="open">
      <PopoverTrigger as-child>
        <Button type="button" variant="outline" role="combobox" :aria-expanded="open" :aria-label="label" class="w-full min-w-0 justify-between gap-2 font-normal">
          <span class="min-w-0 truncate"><span class="text-xs text-muted-foreground">{{ label }} · </span>{{ selectedLabel || modelValue || t('enterprise.all') }}</span>
          <ChevronsUpDown class="size-4 shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" class="w-[var(--reka-popover-trigger-width)] min-w-64 max-w-[calc(100vw-2rem)] p-0">
        <Command>
          <div class="flex items-center gap-2 border-b px-3">
            <Search class="size-4 shrink-0 text-muted-foreground" />
            <!-- Keep Command keyboard navigation while the endpoint owns result filtering. -->
            <ListboxFilter v-model="search" as-child auto-focus>
              <Input v-model="search" :aria-label="t('enterprise.usageAnalysis.searchOptions', { label })" :placeholder="t('enterprise.usageAnalysis.searchOptions', { label })" class="h-10 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0" />
            </ListboxFilter>
          </div>
          <CommandList :aria-busy="loading">
            <div v-if="loading" role="status" class="space-y-2 p-3"><span class="sr-only">{{ t('common.loading') }}</span><Skeleton v-for="n in 3" :key="n" class="h-7 w-full" /></div>
            <div v-else-if="failed" role="alert" class="p-3 text-sm text-muted-foreground">{{ t('enterprise.usageAnalysis.optionsError') }}<Button type="button" variant="link" size="sm" @click="load">{{ t('enterprise.usageAnalysis.retryOptions') }}</Button></div>
            <template v-else>
              <CommandGroup>
                <CommandItem value="__all__" @select="select()">{{ t('enterprise.all') }}<Check v-if="!modelValue" class="ml-auto size-4" /></CommandItem>
                <CommandItem v-for="item in items" :key="item.value" :value="item.value" @select="select(item)">
                  <div class="min-w-0 flex-1"><p class="truncate">{{ item.label }}</p><p v-if="item.detail" class="truncate text-xs text-muted-foreground">{{ item.detail }}</p></div>
                  <Check v-if="modelValue === item.value" class="ml-auto size-4" />
                </CommandItem>
              </CommandGroup>
              <p v-if="!items.length" class="px-3 py-4 text-center text-sm text-muted-foreground">{{ t('enterprise.usageAnalysis.noOptions') }}</p>
            </template>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
    <Button v-if="modelValue" type="button" variant="ghost" size="icon" class="size-8 shrink-0" :aria-label="`${t('enterprise.usageAnalysis.clear')} ${label}`" @click="select()"><X class="size-3.5" /></Button>
  </div>
</template>
