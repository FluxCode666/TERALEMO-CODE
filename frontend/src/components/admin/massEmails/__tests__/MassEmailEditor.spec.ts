import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MassEmailEditor from '../MassEmailEditor.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('mass email editor HTML', () => {
  const wrappers: ReturnType<typeof mount>[] = []
  afterEach(() => { wrappers.forEach((wrapper) => wrapper.unmount()); wrappers.length = 0 })

  it('inserts variables and emits semantic HTML without editor classes', async () => {
    const wrapper = mount(MassEmailEditor, { props: { modelValue: '<p><strong>Hello</strong></p><ul><li><p>Item</p></li></ul>' } })
    wrappers.push(wrapper)
    await wrapper.get('[data-test="insert-var-name"]').trigger('click')
    await flushPromises()
    const values = wrapper.emitted('update:modelValue')!
    const html = values[values.length - 1]![0] as string
    expect(html).toContain('{{name}}')
    expect(html).toContain('<strong>')
    expect(html).toContain('<ul>')
    expect(html).not.toMatch(/ProseMirror|class=|contenteditable/)
  })

  it('drops scripts, inline events and unsafe link protocols from pasted HTML', async () => {
    const wrapper = mount(MassEmailEditor, { props: { modelValue: '<p onclick="alert(1)">Hello</p><script>alert(1)</script><a href="javascript:alert(1)">Bad</a>' } })
    wrappers.push(wrapper)
    await wrapper.get('[data-test="insert-var-email"]').trigger('click')
    await flushPromises()
    const values = wrapper.emitted('update:modelValue')!
    const html = values[values.length - 1]![0] as string
    expect(html).not.toMatch(/<script|onclick|javascript:/)
  })
})
