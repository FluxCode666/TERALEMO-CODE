<template>
  <form class="card space-y-4 p-4 sm:p-5" @submit.prevent="save">
    <h3 class="break-words font-medium text-gray-900 dark:text-white">{{ title }}</h3>
    <div class="grid gap-2 sm:grid-cols-2" :aria-label="t('enterprise.groupAccess.mode')">
      <button v-for="option in modes" :key="option.value" type="button" :disabled="!editable || busy" :aria-pressed="mode === option.value" :data-test="`group-mode-${option.value}`" class="rounded-lg border px-3 py-3 text-left text-sm disabled:cursor-default" :class="mode === option.value ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'border-gray-200 text-gray-600 dark:border-dark-700 dark:text-gray-300'" @click="mode = option.value; validation = ''">{{ option.label }}</button>
    </div>
    <template v-if="mode === 'selected'">
      <EnterpriseGroupCards v-model="selection" :groups="groups" selectable :disabled="!editable || busy" />
      <p v-if="!groups.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.groupAccess.noCandidates') }}</p>
      <p v-if="staleIds.length" class="text-sm text-amber-700 dark:text-amber-400">{{ t('enterprise.inactiveWhitelistGroups', { groups: staleIds.map(id => `#${id}`).join(', ') }) }}</p>
    </template>
    <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.groupAccess.inheritHint') }}</p>
    <p v-if="validation" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ validation }}</p>
    <button v-if="editable" class="btn btn-primary" :disabled="busy" data-test="group-save">{{ t('enterprise.save') }}</button>
  </form>
</template>
<script setup lang="ts">
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
