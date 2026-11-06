import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupBadge from '../GroupBadge.vue'

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

const mountBadge = (props: Record<string, unknown>) =>
  mount(GroupBadge, {
    props: { name: 'Example group', platform: 'openai', ...props },
    global: { stubs: { PlatformIcon: true } },
  })

describe('GroupBadge rate label', () => {
  it('专属倍率只展示专属倍率,不展示划线的原倍率', () => {
    const wrapper = mountBadge({ rateMultiplier: 1.5, userRateMultiplier: 0.8 })

    expect(wrapper.find('.line-through').exists()).toBe(false)
    expect(wrapper.text()).toContain('0.8x')
    expect(wrapper.text()).not.toContain('1.5x')
  })

  it('无专属倍率时展示分组倍率', () => {
    const wrapper = mountBadge({ rateMultiplier: 1.5, userRateMultiplier: null })

    expect(wrapper.text()).toContain('1.5x')
  })

  it('专属倍率与分组倍率相同时照常展示', () => {
    const wrapper = mountBadge({ rateMultiplier: 1, userRateMultiplier: 1 })

    expect(wrapper.text()).toContain('1x')
    expect(wrapper.find('.line-through').exists()).toBe(false)
  })

  it('订阅分组无专属倍率时仍展示订阅标签', () => {
    const wrapper = mountBadge({ rateMultiplier: 1.5, subscriptionType: 'subscription' })

    expect(wrapper.text()).toContain('groups.subscription')
    expect(wrapper.text()).not.toContain('1.5x')
  })

  it('订阅分组开启 alwaysShowRate 时展示专属倍率', () => {
    const wrapper = mountBadge({
      rateMultiplier: 1.5,
      userRateMultiplier: 0.8,
      subscriptionType: 'subscription',
      alwaysShowRate: true,
    })

    expect(wrapper.text()).toContain('0.8x')
    expect(wrapper.text()).not.toContain('1.5x')
  })
})
