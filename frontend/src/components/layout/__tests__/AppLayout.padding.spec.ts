import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import AppLayout from '../AppLayout.vue'

vi.mock('@/stores', () => ({ useAppStore: () => ({ sidebarCollapsed: false }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { role: 'user' } }) }))
vi.mock('@/stores/onboarding', () => ({ useOnboardingStore: () => ({ setReplayCallback: vi.fn() }) }))
vi.mock('@/composables/useOnboardingTour', () => ({ useOnboardingTour: () => ({ replayTour: vi.fn() }) }))

const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

function render(compactPadding?: boolean) {
  const wrapper = mount(AppLayout, {
    props: { compactPadding },
    global: { stubs: { AppSidebar: true, AppHeader: true } }
  })
  wrappers.push(wrapper)
  return wrapper
}

function expectPadding(wrapper: VueWrapper, compact: boolean) {
  const classes = wrapper.get('main').classes()
  expect(classes).toEqual(expect.arrayContaining(['py-4', 'md:py-6', 'lg:py-8', 'px-4']))
  expect(classes.includes('md:px-6')).toBe(!compact)
  expect(classes.includes('lg:px-8')).toBe(!compact)
}

describe('AppLayout horizontal page padding', () => {
  it('keeps original spacing for pages without compactPadding', () => {
    expectPadding(render(), false)
  })

  it('uses only the prop to compact horizontal spacing and preserves vertical spacing', async () => {
    const wrapper = render(true)
    expectPadding(wrapper, true)
    expectPadding(render(), false)
    expect(wrapper.get('main').classes().some(value => value.includes('transition'))).toBe(false)
    await wrapper.setProps({ compactPadding: false })
    expectPadding(wrapper, false)
  })
})
