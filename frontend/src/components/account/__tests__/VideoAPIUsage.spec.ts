import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import VideoAPIUsage from '../../keys/VideoAPIUsage.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function mountUsage(baseUrl: string) {
  return mount(VideoAPIUsage, {
    props: { baseUrl, apiKey: 'test-key' },
    global: {
      stubs: {
        RouterLink: { template: '<a><slot /></a>' }
      }
    }
  })
}

describe('VideoAPIUsage', () => {
  it('baseUrl 为空时回落到 window.location.origin', () => {
    const wrapper = mountUsage('')
    expect(wrapper.text()).toContain(`POST ${window.location.origin}/api/v3/contents/generations/tasks`)
    expect(wrapper.text()).not.toContain('POST /api/v3')
  })

  it('剥离传入地址的尾斜杠与 /v1 后缀', () => {
    const wrapper = mountUsage('https://api.example.com/v1/')
    expect(wrapper.text()).toContain('POST https://api.example.com/api/v3/contents/generations/tasks')
    expect(wrapper.text()).toContain('POST https://api.example.com/v1beta/models/YOUR_VIDEO_MODEL:predictLongRunning')
    expect(wrapper.text()).not.toContain('https://api.example.com/v1/api/v3')
  })
})
