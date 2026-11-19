import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'

import AccountTableFilters from '../AccountTableFilters.vue'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const mountFilters = () => mount(AccountTableFilters, {
  props: {
    searchQuery: '',
    filters: {},
    groups: []
  }
})

const modelSelectOf = (wrapper: VueWrapper) => {
  const selects = wrapper.findAllComponents(Select)
  return selects[selects.length - 1]
}

describe('AccountTableFilters model filter', () => {
  it('reuses the existing model catalog with a searchable, clearable selector', () => {
    const wrapper = mountFilters()
    const modelSelect = modelSelectOf(wrapper)

    const options = modelSelect.props('options') as Array<{ value: string; label: string }>
    expect(options[0]).toEqual({ value: '', label: 'admin.accounts.allModels' })
    expect(modelSelect.props('placeholder')).toBe('admin.accounts.filterByModel')
    expect(modelSelect.props('searchable')).toBe(true)
    expect(modelSelect.props('clearable')).toBe(true)

    const values = options.map(option => option.value)
    expect(values).toContain('claude-opus-5')
    expect(new Set(values).size).toBe(values.length)
    expect(values.filter(value => value === 'claude-fable-5-1')).toHaveLength(1)
  })

  it('emits the selected model id and clears it back to an empty filter', async () => {
    const wrapper = mountFilters()
    const modelSelect = modelSelectOf(wrapper)

    modelSelect.vm.$emit('update:modelValue', 'claude-opus-5')
    expect(wrapper.emitted('update:filters')?.at(-1)?.[0]).toEqual({ model: 'claude-opus-5' })

    modelSelect.vm.$emit('update:modelValue', null)
    expect(wrapper.emitted('update:filters')?.at(-1)?.[0]).toEqual({ model: '' })
  })
})
