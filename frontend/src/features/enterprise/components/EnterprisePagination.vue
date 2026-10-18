<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pagination, PaginationContent, PaginationPrevious, PaginationNext, PaginationItem, PaginationEllipsis } from '@/components/ui/pagination'
import EnterpriseSelect from './EnterpriseSelect.vue'
const props = defineProps<{ page: number; pageSize: number; total: number }>()
defineEmits<{ 'update:page': [page: number]; 'update:pageSize': [size: number] }>()
const { t } = useI18n()
const sizes = [10, 20, 50, 100].map(value => ({ value, label: String(value) }))
const pages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))
</script>
<template>
  <div class="shadcn-scope flex flex-wrap items-center justify-between gap-3 text-sm text-muted-foreground">
    <div class="flex items-center gap-2"><span>{{ t('enterprise.paginationUi.total', { count: total }) }}</span><EnterpriseSelect :model-value="pageSize" :options="sizes" :aria-label="t('enterprise.paginationUi.pageSize')" class="w-20" @update:model-value="$emit('update:pageSize', Number($event))" /></div>
    <Pagination v-slot="{ page: current }" :page="page" :items-per-page="pageSize" :total="total" :sibling-count="0" show-edges class="mx-0 w-auto" @update:page="$emit('update:page', $event)">
      <PaginationContent v-slot="{ items }" class="gap-1"><PaginationPrevious /><template v-for="(item, index) in items" :key="index"><PaginationItem v-if="item.type === 'page'" :value="item.value" :is-active="item.value === current" class="hidden sm:inline-flex">{{ item.value }}</PaginationItem><PaginationEllipsis v-else class="hidden sm:flex" /></template><span class="px-2 tabular-nums sm:hidden">{{ page }} / {{ pages }}</span><PaginationNext /></PaginationContent>
    </Pagination>
  </div>
</template>
