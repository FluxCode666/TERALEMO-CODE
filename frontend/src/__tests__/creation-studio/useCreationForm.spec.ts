import { describe, expect, it, vi } from 'vitest'
import { ref, nextTick } from 'vue'
import { isParamValueValid, useCreationForm } from '@/features/creation-studio/useCreationForm'
import { optionsOpenAI, optionsVideo } from '@/features/creation-studio/fixtures'
import type { CreationParamDescriptor } from '@/features/creation-studio/types'
import { clone, setupComposable } from './helpers'

describe('useCreationForm (F7 / contract 5.2)', () => {
  it('initializes descriptor defaults and keeps image roles outside params', () => {
    const form = setupComposable(() => useCreationForm(ref(clone(optionsOpenAI))))
    expect(form.params.value).toEqual({ size: '1024x1024', quality: 'auto', n: 1 })
    expect(form.imageDescriptors.value.map(item => item.key)).toEqual(['source'])
    expect(form.paramsValid.value).toBe(true)
  })

  it.each(['', '   ', 'x'.repeat(8001)])('blocks an empty or oversized prompt #%#', value => {
    const form = setupComposable(() => useCreationForm(ref(clone(optionsOpenAI))))
    form.setPrompt(value)
    expect(form.promptValid.value).toBe(false)
  })

  it('counts astral characters as runes and accepts exactly 8000', () => {
    const form = setupComposable(() => useCreationForm(ref(clone(optionsOpenAI))))
    form.setPrompt(String.fromCodePoint(0x1f30a).repeat(8000))
    expect(form.promptLength.value).toBe(8000)
    expect(form.promptValid.value).toBe(true)
  })

  it('preserves prompt, rebuilds model-specific params and drops obsolete keys', () => {
    const form = setupComposable(() => useCreationForm(ref(clone(optionsOpenAI))))
    form.setPrompt('QA lighthouse')
    form.setParam('size', '1536x1024')
    form.selectModel(optionsOpenAI.models[1].id)
    expect(form.prompt.value).toBe('QA lighthouse')
    expect(form.params.value).toEqual({ aspect_ratio: '1:1', resolution: '1k', n: 1 })
  })

  it('switches form mode and reconciles removed models when options change', async () => {
    const options = ref(clone({ ...optionsOpenAI, models: [...optionsOpenAI.models, ...optionsVideo.models] }))
    const form = setupComposable(() => useCreationForm(options))
    form.setMode('video')
    expect(form.selectedModel.value?.protocol).toBe('seedance')
    options.value = clone(optionsOpenAI)
    await nextTick()
    expect(form.mode.value).toBe('image')
    expect(form.modelId.value).toBe(optionsOpenAI.models[0].id)
  })

  it('restores only valid snapshot parameters, including step and disabled options', () => {
    const options = clone(optionsVideo)
    options.models = [options.models[1]]
    const form = setupComposable(() => useCreationForm(ref(options)))
    form.applyParams({ duration: 5, resolution: '4k', aspect_ratio: '9:16', bogus: 1 })
    expect(form.params.value).toEqual({ duration: 8, resolution: '720p', aspect_ratio: '9:16' })
    const select = { key: 'q', kind: 'select', labelKey: 'q', default: 'a', options: [{ value: 'a', disabled: true }] } as const
    expect(isParamValueValid({ ...select, options: [...select.options] }, 'a')).toBe(false)
  })

  it.each([0, 5, 1.5, NaN, '2junk', '1.5'])('rejects invalid integer n=%s', value => {
    const descriptor = optionsOpenAI.models[0].params.find(item => item.key === 'n')!
    expect(isParamValueValid(descriptor, value)).toBe(false)
  })

  it('warns and completely skips unknown kinds without blocking submission', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const options = clone(optionsOpenAI)
    options.models[0].params.push({ key: 'future', kind: 'future', labelKey: 'future' } as unknown as CreationParamDescriptor)
    const form = setupComposable(() => useCreationForm(ref(options)))
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('unknown param kind'))
    expect(form.params.value).not.toHaveProperty('future')
    expect(form.paramDescriptors.value.map(item => item.key)).not.toContain('future')
    expect(form.paramsValid.value).toBe(true)
    warn.mockRestore()
  })
})
