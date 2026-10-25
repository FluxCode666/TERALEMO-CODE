import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

const { copyToClipboardMock } = vi.hoisted(() => ({
  copyToClipboardMock: vi.fn().mockResolvedValue(true)
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: copyToClipboardMock
  })
}))

import DocBlocks from '../DocBlocks.vue'

const example = {
  type: 'example' as const,
  id: 'create-text',
  title: 'Text to video',
  description: 'Submit a text prompt.',
  request: "curl '{{BASE_URL}}/v1/video_generation' -H 'Authorization: Bearer {{API_KEY}}'",
  responses: [
    { title: 'Accepted', status: 202, body: '{"task_id":"example-task-id"}' },
    {
      title: 'Video download',
      status: 200,
      headers: 'Content-Type: video/mp4',
      description: 'The response is a video binary stream.'
    }
  ]
}

function render() {
  return mount(DocBlocks, {
    props: {
      blocks: [example],
      examplePrefix: 'minimax-v2',
      resolveCode: (code: string) => code.replace('{{BASE_URL}}', 'https://example.test').replace('{{API_KEY}}', 'sk-test')
    },
    global: {
      stubs: {
        RouterLink: { template: '<a><slot /></a>' }
      }
    }
  })
}

afterEach(() => {
  copyToClipboardMock.mockClear()
  copyToClipboardMock.mockResolvedValue(true)
})

describe('DocBlocks examples', () => {
  it('renders the example request and each HTTP response', () => {
    const wrapper = render()

    const root = wrapper.get('[data-doc-example-id="minimax-v2-create-text"]')
    expect(root.attributes('id')).toBe('api-docs-minimax-v2-create-text')
    expect(root.attributes('data-scrollspy-id')).toBe('minimax-v2-create-text')
    expect(root.text()).toContain('Text to video')
    expect(root.text()).toContain('Submit a text prompt.')
    expect(root.text()).toContain('Accepted')
    expect(root.text()).toContain('HTTP 202')
    expect(root.text()).toContain('\n  "task_id": "example-task-id"')
    expect(root.text()).toContain('Video download')
    expect(root.text()).toContain('HTTP 200')
    expect(root.text()).toContain('Content-Type: video/mp4')
    expect(root.findAll('button[data-doc-copy]')).toHaveLength(3)
    wrapper.unmount()
  })

  it('copies resolved request and response content independently', async () => {
    const wrapper = render()
    const buttons = wrapper.findAll('button[data-doc-copy]')

    await buttons[0]!.trigger('click')
    await flushPromises()
    expect(copyToClipboardMock).toHaveBeenNthCalledWith(1, "curl 'https://example.test/v1/video_generation' -H 'Authorization: Bearer sk-test'", 'apiDocs.copied')

    await buttons[1]!.trigger('click')
    await flushPromises()
    expect(copyToClipboardMock).toHaveBeenNthCalledWith(2, '{\n  "task_id": "example-task-id"\n}', 'apiDocs.copied')

    await buttons[2]!.trigger('click')
    await flushPromises()
    expect(copyToClipboardMock).toHaveBeenNthCalledWith(3, 'Content-Type: video/mp4', 'apiDocs.copied')
    wrapper.unmount()
  })
})
