import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ParamField from '@/features/creation-studio/components/ParamField.vue'
import { optionsOpenAI, optionsVideo } from '@/features/creation-studio/fixtures'
import type { CreationParamDescriptor } from '@/features/creation-studio/types'
import { track } from './helpers'

const descriptors = [
  optionsOpenAI.models[0].params[0], optionsOpenAI.models[0].params[2],
  optionsVideo.models[0].params[2], optionsOpenAI.models[0].params[3],
]
const images = { entries: [], errors: [], available: true, uploading: false, limitReached: false, removedUnavailable: false }
function render(descriptor: CreationParamDescriptor, disabled = false) {
  return track(mount(ParamField, {
    props: { descriptor, modelValue: 'default' in descriptor ? descriptor.default : '', images, disabled },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] },
  }))
}

describe('ParamField (Q2)', () => {
  it('renders select chips with linked radiogroup and emits string values', async () => {
    const wrapper = render(descriptors[0])
    expect(wrapper.get('[role="radiogroup"]').attributes('aria-labelledby')).toBe('param-label-size')
    expect(wrapper.get('#param-label-size').text()).toBe('creationStudio.params.size')
    await wrapper.get('input[value="1536x1024"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([['1536x1024']])
  })

  it('renders integer bounds and emits numeric stepper values', async () => {
    const wrapper = render(descriptors[1])
    expect(wrapper.get('input[type="number"]').attributes()).toMatchObject({ min: '1', max: '4', step: '1', 'aria-label': 'creationStudio.params.n' })
    await wrapper.get('button[aria-label="creationStudio.param.increase"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[2]])
  })

  it('renders ratio options and emits selected ratio', async () => {
    const wrapper = render(descriptors[2])
    expect(wrapper.findAll('.cs-ratio-glyph')).toHaveLength(7)
    await wrapper.get('input[value="9:16"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([['9:16']])
  })

  it('renders image file accept rules and forwards selected files', async () => {
    const wrapper = render(descriptors[3])
    const input = wrapper.get('input[type="file"]')
    expect(input.attributes('accept')).toBe('image/png,image/jpeg,image/webp')
    const file = new File(['qa'], 'qa.png', { type: 'image/png' })
    Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
    await input.trigger('change')
    expect(wrapper.emitted('pick')).toEqual([[[file]]])
  })

  it.each(descriptors.slice(0, 3))('disables $kind controls', descriptor => {
    const wrapper = render(descriptor, true)
    const inputs = wrapper.findAll('input, button')
    expect(inputs.length).toBeGreaterThan(0)
    expect(inputs.every(input => input.attributes('disabled') !== undefined)).toBe(true)
  })

  it('disables image upload for a disabled descriptor', () => {
    const descriptor = { ...descriptors[3], disabled: true, disabledReason: 'storage_disabled' } as CreationParamDescriptor
    const wrapper = render(descriptor)
    expect(wrapper.find('input[type="file"]').exists()).toBe(false)
    expect(wrapper.get('.cs-dropzone').attributes('aria-disabled')).toBe('true')
  })

  it('skips the entire unknown-kind field including its label', () => {
    const wrapper = render({ kind: 'future', key: 'future', labelKey: 'future.label' } as unknown as CreationParamDescriptor)
    expect(wrapper.text()).toBe('')
    expect(wrapper.find('.cs-field').exists()).toBe(false)
  })
})
