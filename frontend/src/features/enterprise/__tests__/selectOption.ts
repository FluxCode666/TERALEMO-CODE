import { DOMWrapper, flushPromises, type VueWrapper } from '@vue/test-utils'

/** Exercise actual selects, including their portal content. */
export async function selectOption(wrapper: VueWrapper, selector: string, label: string) {
  const trigger = wrapper.get(`${selector} button`)
  if (trigger.attributes('role') === 'combobox') await trigger.trigger('keydown', { key: 'Enter' })
  else await trigger.trigger('click')
  await flushPromises()
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].filter(item => item.textContent?.trim() === label).at(-1)
  if (!option) throw new Error(`Select option not found: ${label}`)
  await new DOMWrapper(option).trigger('keydown', { key: 'Enter' })
  // Common Select uses click, shadcn Select handles Enter.
  if (document.body.contains(option)) option.click()
  await flushPromises()
}

export async function clickMenuItem(wrapper: VueWrapper, trigger: string, item: string) {
  await wrapper.get(trigger).trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const element = document.querySelector<HTMLElement>(item)
  if (!element) throw new Error(`Menu item not found: ${item}`)
  element.click()
  await flushPromises()
}
