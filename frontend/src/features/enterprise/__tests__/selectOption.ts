import { flushPromises, type VueWrapper } from '@vue/test-utils'

/** Exercise the actual common Select, including its teleported options. */
export async function selectOption(wrapper: VueWrapper, selector: string, label: string) {
  await wrapper.get(`${selector} button`).trigger('click')
  await flushPromises()
  const option = [...document.querySelectorAll<HTMLElement>('.select-dropdown-portal:not(.select-dropdown-leave-active) [role="option"]')].filter(item => item.textContent?.trim() === label).at(-1)
  if (!option) throw new Error(`Select option not found: ${label}`)
  option.click()
  await flushPromises()
}
