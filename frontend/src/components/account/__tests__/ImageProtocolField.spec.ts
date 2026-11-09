import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

import ImageProtocolField from '../ImageProtocolField.vue'

describe('ImageProtocolField', () => {
  it('offers openai/gemini/grok and echoes the selected value', () => {
    const wrapper = mount(ImageProtocolField, { props: { id: 'p', modelValue: 'gemini' } })
    const select = wrapper.get('select').element as HTMLSelectElement
    expect(Array.from(select.options).map((o) => o.value)).toEqual(['openai', 'gemini', 'grok'])
    expect(select.value).toBe('gemini')
    expect(wrapper.text()).toContain('imageAccounts.geminiHint')
  })

  it('emits the new protocol on change', async () => {
    const wrapper = mount(ImageProtocolField, { props: { id: 'p', modelValue: 'openai' } })
    await wrapper.get('select').setValue('grok')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['grok'])
  })
})
