<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import type { EnterpriseDepartment } from '@/types/enterprise'
import { departmentTreeNodes } from '../departmentTree'
import EnterpriseTree from './EnterpriseTree.vue'
const props = defineProps<{ modelValue: number | null; departments: EnterpriseDepartment[]; label?: string; rootLabel?: string; allowRoot?: boolean; disabledKeys?: Set<string>; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [id: number] }>()
const { t } = useI18n()
const search = ref('')
const nodes = computed(() => {
  const children = departmentTreeNodes(props.departments)
  return props.allowRoot === false ? children : [{ key: '0', name: props.rootLabel || t('enterprise.orgTree.unassigned'), kind: 'root' as const, children }]
})
</script>
<template>
  <div class="shadcn-scope space-y-2 rounded-md border p-2" :class="disabled ? 'pointer-events-none opacity-50' : ''">
    <Input v-model="search" type="search" :aria-label="t('enterprise.orgTree.searchDepartments')" :placeholder="t('enterprise.orgTree.searchDepartments')" :disabled="disabled" />
    <ScrollArea class="h-48"><div class="w-full overflow-x-auto"><div class="min-w-max"><EnterpriseTree :nodes="nodes" :model-value="String(modelValue ?? 0)" :search="search" :label="label || t('enterprise.departments')" :disabled-keys="disabledKeys" @update:model-value="value => { if (!disabled) emit('update:modelValue', Number(value)) }" /></div></div></ScrollArea>
  </div>
</template>
