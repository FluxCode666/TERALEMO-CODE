import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupOptionItem from '../GroupOptionItem.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null }),
}))

describe('GroupOptionItem description layout', () => {
  it('applies multiline and overflow-safe text styles', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Example group',
        platform: 'openai',
        description,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })
})

describe('GroupOptionItem rate pill', () => {
  const mountItem = (props: Record<string, unknown>) =>
    mount(GroupOptionItem, {
      props: { name: 'Example group', platform: 'openai', ...props },
      global: { stubs: { GroupBadge: true } },
    })

  it('专属倍率只展示专属倍率,不展示划线的原倍率', () => {
    const wrapper = mountItem({ rateMultiplier: 1.5, userRateMultiplier: 0.8 })

    expect(wrapper.find('.line-through').exists()).toBe(false)
    expect(wrapper.text()).toContain('0.8x admin.groups.rateLabel')
    expect(wrapper.text()).not.toContain('1.5x')
  })

  it('无专属倍率时展示分组倍率', () => {
    const wrapper = mountItem({ rateMultiplier: 1.5, userRateMultiplier: null })

    expect(wrapper.text()).toContain('1.5x admin.groups.rateLabel')
  })
})
