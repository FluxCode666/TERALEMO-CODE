import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import VideoCapabilityField from '../VideoCapabilityField.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function mountField(props: { resolutions?: string[]; minDuration?: string | number; maxDuration?: string | number } = {}) {
  return mount(VideoCapabilityField, {
    props: {
      resolutions: [],
      minDuration: '',
      maxDuration: '',
      idPrefix: 'test-capability',
      ...props
    }
  })
}

describe('VideoCapabilityField', () => {
  it('renders all five resolution options and the duration inputs with hints', () => {
    const wrapper = mountField()
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(5)
    for (const option of ['480p', '720p', '768p', '1080p', '2k']) {
      expect(wrapper.get(`[data-testid="test-capability-resolution-${option}"]`).exists()).toBe(true)
    }
    expect(wrapper.text()).toContain('2K')
    expect(wrapper.text()).toContain('videoAccounts.capability.title')
    expect(wrapper.text()).toContain('videoAccounts.capability.hint')
    expect(wrapper.get('#test-capability-min-duration').exists()).toBe(true)
    expect(wrapper.get('#test-capability-max-duration').exists()).toBe(true)
    wrapper.unmount()
  })

  it('emits normalized selections in canonical order when toggling', async () => {
    const wrapper = mountField({ resolutions: ['1080p'] })
    await wrapper.get('[data-testid="test-capability-resolution-480p"]').setValue(true)
    expect(wrapper.emitted('update:resolutions')?.[0]).toEqual([['480p', '1080p']])
    await wrapper.get('[data-testid="test-capability-resolution-1080p"]').setValue(false)
    expect(wrapper.emitted('update:resolutions')?.[1]).toEqual([[]])
    wrapper.unmount()
  })

  it('emits raw text for duration inputs so empty means official rules', async () => {
    const wrapper = mountField()
    await wrapper.get('#test-capability-min-duration').setValue('4')
    await wrapper.get('#test-capability-max-duration').setValue('15')
    expect(wrapper.emitted('update:minDuration')?.[0]).toEqual(['4'])
    expect(wrapper.emitted('update:maxDuration')?.[0]).toEqual(['15'])
    wrapper.unmount()
  })

  it('reflects backfilled values', () => {
    const wrapper = mountField({ resolutions: ['768p', '2k'], minDuration: '4', maxDuration: '15' })
    expect((wrapper.get('[data-testid="test-capability-resolution-768p"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="test-capability-resolution-2k"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="test-capability-resolution-480p"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('#test-capability-min-duration').element as HTMLInputElement).value).toBe('4')
    expect((wrapper.get('#test-capability-max-duration').element as HTMLInputElement).value).toBe('15')
    wrapper.unmount()
  })

  it('keeps working after rapid toggles', async () => {
    const wrapper = mountField({ resolutions: [] })
    const checkbox = wrapper.get('[data-testid="test-capability-resolution-720p"]')
    await checkbox.setValue(true)
    await flushPromises()
    expect(wrapper.emitted('update:resolutions')?.at(-1)).toEqual([['720p']])
    wrapper.unmount()
  })
})
