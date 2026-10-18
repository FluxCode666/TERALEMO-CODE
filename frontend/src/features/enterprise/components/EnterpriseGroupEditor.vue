<template>
  <Card><form class="shadcn-scope space-y-4 p-4 sm:p-5" @submit.prevent="save">
    <h3 class="break-words font-medium text-foreground">{{ title }}</h3>
    <RadioGroup v-model="mode" :disabled="!editable || busy" class="grid gap-2 sm:grid-cols-2" :aria-label="t('enterprise.groupAccess.mode')">
      <Label v-for="option in modes" :key="option.value" class="flex cursor-pointer items-center gap-3 rounded-ui-md border p-4" :class="mode === option.value ? 'border-primary bg-primary/5' : ''" :data-test="`group-mode-${option.value}`"><RadioGroupItem :value="option.value" :data-test="`group-radio-${option.value}`" />{{ option.label }}</Label>
    </RadioGroup>
    <template v-if="mode === 'selected'">
      <div v-if="editable" class="flex flex-wrap gap-2"><Button type="button" variant="outline" size="sm" :disabled="busy || !groups.length" data-test="group-select-all" @click="selection = groups.map(group => group.id)">{{ t('enterprise.groupAccess.selectAll') }}</Button><Button type="button" variant="ghost" size="sm" :disabled="busy || !selection.length" data-test="group-clear" @click="selection = []">{{ t('enterprise.groupAccess.clear') }}</Button></div>
      <EnterpriseGroupCards v-model="selection" :groups="groups" selectable :disabled="!editable || busy" />
      <p v-if="!groups.length" class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.noCandidates') }}</p>
      <p v-if="staleIds.length" class="text-sm text-amber-700 dark:text-amber-400">{{ t('enterprise.inactiveWhitelistGroups', { groups: staleIds.map(id => `#${id}`).join(', ') }) }}</p>
    </template>
    <p v-else class="text-sm text-muted-foreground">{{ t('enterprise.groupAccess.inheritHint') }}</p>
    <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
    <Button variant="default" v-if="editable"  :disabled="busy" data-test="group-save">{{ t('enterprise.save') }}</Button>
  </form></Card>
</template>
<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'

import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnterpriseGroup } from '@/types/enterprise'
import EnterpriseGroupCards from './EnterpriseGroupCards.vue'
const props = defineProps<{ title: string; groupIds: number[]; groups: EnterpriseGroup[]; editable?: boolean; busy?: boolean }>()
const emit = defineEmits<{ save: [ids: number[]] }>()
const { t } = useI18n()
const mode = ref(props.groupIds.length ? 'selected' : 'inherit'), selection = ref([...props.groupIds]), validation = ref('')
const modes = computed(() => [{ value: 'inherit', label: t('enterprise.groupAccess.inherit') }, { value: 'selected', label: t('enterprise.groupAccess.selected') }])
const staleIds = computed(() => selection.value.filter(id => !props.groups.some(group => group.id === id)))
watch(() => props.groupIds, ids => { selection.value = [...ids]; mode.value = ids.length ? 'selected' : 'inherit'; validation.value = '' })
function save() {
  if (!props.editable || props.busy) return
  const ids = selection.value.filter(id => props.groups.some(group => group.id === id))
  if (mode.value === 'selected' && !ids.length) { validation.value = t('enterprise.groupAccess.selectRequired'); return }
  emit('save', mode.value === 'inherit' ? [] : ids)
}
</script>
