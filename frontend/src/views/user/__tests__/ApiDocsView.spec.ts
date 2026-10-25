import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { nextTick } from 'vue'

const mocks = vi.hoisted(() => ({
  listKeys: vi.fn(),
  setLocale: (_value: string) => {},
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  const locale = ref('en')
  mocks.setLocale = (value: string) => { locale.value = value }
  return { useI18n: () => ({ t: (key: string) => key, locale }) }
})
vi.mock('@/api/keys', () => ({ keysAPI: { list: mocks.listKeys }, default: { list: mocks.listKeys } }))
vi.mock('@/i18n', () => ({ i18n: { global: { t: (key: string) => key } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ isReady: () => Promise.resolve() }) }))

import ApiDocsView from '../ApiDocsView.vue'

const userKey = { id: 5, name: 'Prod key', key: 'sk-full-secret-key-1234' }

function render() {
  return mount(ApiDocsView, {
    attachTo: document.body,
    global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } }
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  sessionStorage.clear()
  window.history.replaceState(null, '', window.location.pathname)
  mocks.setLocale('en')
  mocks.listKeys.mockResolvedValue({ items: [userKey], total: 1, page: 1, page_size: 100, pages: 1 })
  Object.defineProperty(window, 'isSecureContext', { value: true, writable: true, configurable: true })
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    writable: true,
    configurable: true
  })
})

describe('ApiDocsView', () => {
  it('renders the video category, catalog entries and all four protocol tabs', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('Video Generation')
    for (const anchor of ['Overview', 'Billing & Balance', 'Downloading Results', 'Per-Protocol Integration', 'Request Validation Rules', 'Error Codes']) {
      expect(wrapper.text()).toContain(anchor)
    }
    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map(tab => tab.text())).toEqual(['Seedance (Ark)', 'Google Gemini (Veo)', 'MiniMax H3 (v2)', 'MiniMax Hailuo (v1)'])
    expect(wrapper.text()).toContain(window.location.origin)
    wrapper.unmount()
  })

  it('switches protocol tabs and toggles panel visibility', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-protocol="seedance"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-protocol="gemini"]').isVisible()).toBe(false)
    const tabs = wrapper.findAll('[role="tab"]')
    await tabs[2]!.trigger('click')
    expect(tabs[2]!.attributes('aria-selected')).toBe('true')
    expect(tabs[0]!.attributes('aria-selected')).toBe('false')
    const mm = wrapper.get('[data-protocol="minimax-v2"]')
    const sd = wrapper.get('[data-protocol="seedance"]')
    expect(mm.isVisible()).toBe(true)
    expect(sd.isVisible()).toBe(false)
    wrapper.unmount()
  })

  it('fills the selected full key into examples and shows the placeholder otherwise', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('sk-YOUR_KEY')
    expect(wrapper.text()).not.toContain('sk-full-secret-key-1234')
    const select = wrapper.get('#api-docs-key')
    expect(select.text()).toContain('Prod key (sk-ful…1234)')
    await select.setValue('5')
    expect(wrapper.text()).toContain('Authorization: Bearer sk-full-secret-key-1234')
    expect(wrapper.text()).not.toContain('sk-YOUR_KEY')
    await select.setValue('0')
    expect(wrapper.text()).toContain('sk-YOUR_KEY')
    wrapper.unmount()
  })

  it('copies the resolved example through the clipboard', async () => {
    const wrapper = render()
    await flushPromises()
    const copyButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'apiDocs.copy' && button.element.parentElement?.parentElement?.querySelector('pre')?.textContent?.includes('curl'))!
    await copyButton.trigger('click')
    await flushPromises()
    expect(navigator.clipboard.writeText).toHaveBeenCalledTimes(1)
    const copied = vi.mocked(navigator.clipboard.writeText).mock.calls[0]![0] as string
    expect(copied).toContain('Authorization: Bearer sk-YOUR_KEY')
    expect(copied).toContain(window.location.origin)
    expect(copied).not.toContain('{{API_KEY}}')
    expect(copied).not.toContain('{{BASE_URL}}')
    expect(mocks.showSuccess).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('switches the docs body between English and Chinese with the locale', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('Overview')
    expect(wrapper.text()).not.toContain('概览')
    mocks.setLocale('zh')
    await nextTick()
    expect(wrapper.text()).toContain('概览')
    expect(wrapper.text()).toContain('计费与余额')
    expect(wrapper.text()).not.toContain('Billing & Balance')
    mocks.setLocale('en')
    await nextTick()
    expect(wrapper.text()).toContain('Billing & Balance')
    wrapper.unmount()
  })

  it('keeps the key selector usable when the keys request fails', async () => {
    mocks.listKeys.mockRejectedValue(new Error('offline'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('sk-YOUR_KEY')
    expect(wrapper.get('#api-docs-key').findAll('option')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps the selected key across a locale-switch remount', async () => {
    const first = render()
    await flushPromises()
    await first.get('#api-docs-key').setValue('5')
    expect(first.text()).toContain('Authorization: Bearer sk-full-secret-key-1234')
    first.unmount()
    mocks.setLocale('zh')
    const second = render()
    await flushPromises()
    expect(second.text()).toContain('Authorization: Bearer sk-full-secret-key-1234')
    expect(second.text()).not.toContain('sk-YOUR_KEY')
    expect((second.get('#api-docs-key').element as HTMLSelectElement).value).toBe('5')
    second.unmount()
  })

  it('pins the catalog navigation on desktop while the docs scroll', async () => {
    const wrapper = render()
    await flushPromises()
    const toc = wrapper.get('[data-test="api-docs-toc"]')
    expect(toc.classes()).toEqual(expect.arrayContaining(['lg:sticky', 'lg:top-20', 'lg:max-h-[calc(100vh-6rem)]', 'lg:overflow-y-auto']))
    expect(toc.findAll('a').length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('scrolls to the section and updates the hash without a router navigation when a catalog entry is clicked', async () => {
    const scrollIntoView = vi.fn()
    const original = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollIntoView
    const replaceState = vi.spyOn(window.history, 'replaceState')
    const wrapper = render()
    await flushPromises()
    try {
      const link = wrapper.get('[data-test="api-docs-toc"] a[href="#api-docs-errors"]')
      const event = new MouseEvent('click', { bubbles: true, cancelable: true })
      link.element.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(true)
      expect(scrollIntoView).toHaveBeenCalledTimes(1)
      expect(scrollIntoView.mock.contexts[0]).toBe(wrapper.get('#api-docs-errors').element)
      expect(replaceState).toHaveBeenLastCalledWith(window.history.state, '', '#api-docs-errors')
    } finally {
      Element.prototype.scrollIntoView = original
      replaceState.mockRestore()
      wrapper.unmount()
    }
  })

  it('renders a mobile protocol select that mirrors the tab list', async () => {
    const wrapper = render()
    await flushPromises()
    const select = wrapper.get('#api-docs-protocol')
    expect(select.classes()).toContain('md:hidden')
    expect(select.findAll('option').map(option => option.text())).toEqual([
      'Seedance (Ark)',
      'Google Gemini (Veo)',
      'MiniMax H3 (v2)',
      'MiniMax Hailuo (v1)'
    ])
    await select.setValue('gemini')
    expect(wrapper.get('[data-protocol="gemini"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-protocol="seedance"]').isVisible()).toBe(false)
    wrapper.unmount()
  })

  it('renders one protocol directory with interface and example children per protocol', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('[data-test="api-docs-toc-protocol"]')).toHaveLength(4)
    expect(wrapper.findAll('[data-test="api-docs-toc-interface"]').length).toBeGreaterThan(0)
    const tocExamples = wrapper.findAll('[data-test="api-docs-toc-example"]')
    const renderedExamples = wrapper.findAll('[data-doc-example]')
    expect(tocExamples).toHaveLength(renderedExamples.length)
    for (const protocol of ['seedance', 'gemini', 'minimax-v2', 'minimax-v1']) {
      expect(tocExamples.filter(link => link.attributes('data-protocol-id') === protocol).length).toBeGreaterThan(0)
    }
    expect(wrapper.findAll('[data-test="api-docs-toc-toggle"]')).toHaveLength(4)
    wrapper.unmount()
  })

  it('keeps each protocol directory anchor order identical to the page DOM order', async () => {
    const wrapper = render()
    await flushPromises()
    const toc = wrapper.get('[data-test="api-docs-toc"]')
    for (const protocol of ['seedance', 'gemini', 'minimax-v2', 'minimax-v1']) {
      const tocOrder = toc.findAll('[data-protocol-id]')
        .filter(item => ['api-docs-toc-interface', 'api-docs-toc-example'].includes(item.attributes('data-test') || ''))
        .filter(item => item.attributes('data-protocol-id') === protocol)
        .map(item => item.attributes('data-toc-id'))
      const pageOrder = wrapper.get(`[data-protocol="${protocol}"]`).findAll('[data-scrollspy-id]')
        .map(item => item.attributes('data-scrollspy-id'))
        .filter(token => token?.startsWith(`${protocol}-`))
      expect(tocOrder).toEqual(pageOrder)
    }
    wrapper.unmount()
  })

  it('switches protocol, scrolls to the selected example, and updates the example hash', async () => {
    const scrollIntoView = vi.fn()
    const original = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollIntoView
    const replaceState = vi.spyOn(window.history, 'replaceState')
    const wrapper = render()
    await flushPromises()
    try {
      const link = wrapper.get('[data-test="api-docs-toc-example"][data-protocol-id="gemini"][data-example-id="query"]')
      await link.trigger('click')
      await nextTick()
      expect((wrapper.get('#api-docs-protocol').element as HTMLSelectElement).value).toBe('gemini')
      expect(scrollIntoView).toHaveBeenCalled()
      expect(scrollIntoView.mock.contexts.at(-1)).toBe(wrapper.get('#api-docs-gemini-query').element)
      expect(replaceState).toHaveBeenLastCalledWith(window.history.state, '', '#api-docs-gemini-query')
    } finally {
      Element.prototype.scrollIntoView = original
      replaceState.mockRestore()
      wrapper.unmount()
    }
  })

  it('keeps the clicked example highlighted while the smooth scroll passes other sections', async () => {
    type ObserverEntry = { isIntersecting: boolean; target: Element; boundingClientRect: { top: number } }
    let callback: ((entries: ObserverEntry[]) => void) | null = null
    class MockIntersectionObserver {
      observe = vi.fn()
      disconnect = vi.fn()
      constructor(handler: (entries: ObserverEntry[]) => void) { callback = handler }
    }
    vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)
    const original = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = vi.fn()
    const wrapper = render()
    await flushPromises()
    const exampleLink = (protocol: string, example: string) => wrapper.get(`[data-test="api-docs-toc-example"][data-protocol-id="${protocol}"][data-example-id="${example}"]`)
    try {
      await exampleLink('minimax-v2', 'reference-image').trigger('click')
      await nextTick()
      callback?.([{ isIntersecting: true, target: wrapper.get('#api-docs-minimax-v2-query').element, boundingClientRect: { top: 90 } }])
      await nextTick()
      expect(exampleLink('minimax-v2', 'reference-image').classes()).toContain('bg-primary-100')
      expect(exampleLink('minimax-v2', 'query').classes()).not.toContain('bg-primary-100')
      window.dispatchEvent(new Event('scrollend'))
      callback?.([{ isIntersecting: true, target: wrapper.get('#api-docs-minimax-v2-query').element, boundingClientRect: { top: 90 } }])
      await nextTick()
      expect(exampleLink('minimax-v2', 'query').classes()).toContain('bg-primary-100')
      await exampleLink('seedance', 'draft-task').trigger('click')
      await nextTick()
      expect(exampleLink('seedance', 'draft-task').classes()).toContain('bg-primary-100')
      expect(exampleLink('seedance', 'draft').classes()).not.toContain('bg-primary-100')
    } finally {
      Element.prototype.scrollIntoView = original
      vi.unstubAllGlobals()
      wrapper.unmount()
    }
  })

  it('opens an example hash on the matching protocol tab after mount', async () => {
    window.history.replaceState(null, '', '#api-docs-minimax-v1-file')
    const scrollIntoView = vi.fn()
    const original = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollIntoView
    const wrapper = render()
    try {
      await flushPromises()
      await new Promise(resolve => window.setTimeout(resolve, 25))
      await nextTick()
      expect((wrapper.get('#api-docs-protocol').element as HTMLSelectElement).value).toBe('minimax-v1')
      expect(scrollIntoView.mock.contexts.some(context => context === wrapper.get('#api-docs-minimax-v1-file').element)).toBe(true)
    } finally {
      Element.prototype.scrollIntoView = original
      wrapper.unmount()
    }
  })

  it('uses IntersectionObserver scrollspy to highlight and expand the active example', async () => {
    type ObserverEntry = { isIntersecting: boolean; target: Element; boundingClientRect: { top: number } }
    let callback: ((entries: ObserverEntry[]) => void) | null = null
    const observers: Array<{ observe: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }> = []
    class MockIntersectionObserver {
      observe = vi.fn()
      disconnect = vi.fn()

      constructor(handler: (entries: ObserverEntry[]) => void) {
        callback = handler
        observers.push(this)
      }
    }
    vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)
    const replaceState = vi.spyOn(window.history, 'replaceState')
    const wrapper = render()
    await flushPromises()
    try {
      expect(observers).toHaveLength(1)
      const target = wrapper.get('#api-docs-gemini-reference-image').element
      callback?.([{ isIntersecting: true, target, boundingClientRect: { top: 10 } }])
      await nextTick()
      expect((wrapper.get('#api-docs-protocol').element as HTMLSelectElement).value).toBe('gemini')
      const protocolToggle = wrapper.find('[data-test="api-docs-toc-toggle"][aria-label*="Gemini"]')
      expect(protocolToggle.attributes('aria-expanded')).toBe('true')
      expect(wrapper.get('[data-test="api-docs-toc-example"][data-protocol-id="gemini"][data-example-id="reference-image"]').classes()).toContain('bg-primary-100')
      expect(replaceState).not.toHaveBeenCalled()
    } finally {
      vi.unstubAllGlobals()
      replaceState.mockRestore()
      wrapper.unmount()
    }
  })

  it('switches the copy button label to the copied state after a successful copy', async () => {
    const wrapper = render()
    await flushPromises()
    const copyButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'apiDocs.copy' && button.element.parentElement?.parentElement?.querySelector('pre')?.textContent?.includes('curl'))!
    await copyButton.trigger('click')
    await flushPromises()
    expect(copyButton.text()).toBe('apiDocs.copied')
    wrapper.unmount()
  })

  it('shows an error toast and keeps the button label when copying fails', async () => {
    vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'))
    const wrapper = render()
    await flushPromises()
    const copyButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'apiDocs.copy' && button.element.parentElement?.parentElement?.querySelector('pre')?.textContent?.includes('curl'))!
    await copyButton.trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalled()
    expect(copyButton.text()).toBe('apiDocs.copy')
    wrapper.unmount()
  })

  it('renders no v-html anywhere in the view or the blocks component', () => {
    const dir = dirname(fileURLToPath(import.meta.url))
    const viewSource = readFileSync(resolve(dir, '../ApiDocsView.vue'), 'utf8')
    const blocksSource = readFileSync(resolve(dir, '../../../components/docs/DocBlocks.vue'), 'utf8')
    expect(viewSource).not.toContain('v-html')
    expect(blocksSource).not.toContain('v-html')
  })
})
