import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RecipientSelector from '../RecipientSelector.vue'
import { createEmptyRecipientForm } from '@/views/admin/massEmails/recipientForm'

const mocks = vi.hoisted(() => ({ getAllIncludingInactive: vi.fn(), list: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: mocks, enterprises: mocks } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('recipient selector options', () => {
  let wrapper: VueWrapper
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getAllIncludingInactive.mockResolvedValue([{ id: 3, name: 'Disabled group' }])
    mocks.list.mockResolvedValue({ items: [], total: 0 })
  })
  afterEach(() => wrapper?.unmount())
  function render() {
    wrapper = mount(RecipientSelector, { props: {
      modelValue: { ...createEmptyRecipientForm(), useEnterprise: true, useUserFilter: true }, preview: null
    } })
  }

  it('loads all enterprise pages and includes inactive user groups', async () => {
    mocks.list.mockResolvedValueOnce({ items: [{ id: 1, name: 'First enterprise' }], total: 2 })
      .mockResolvedValueOnce({ items: [{ id: 2, name: 'Later enterprise' }], total: 2 })
    render()
    await flushPromises()
    expect(mocks.list.mock.calls.map((call) => call[0])).toEqual([{ page: 1, page_size: 100 }, { page: 2, page_size: 100 }])
    expect(wrapper.text()).toContain('Later enterprise')
    expect(wrapper.text()).toContain('Disabled group')
  })

  it('shows an option-loading error and supports retry', async () => {
    mocks.list.mockRejectedValueOnce(new Error('network unavailable'))
    render()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.massEmails.form.optionsLoadFailed')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
