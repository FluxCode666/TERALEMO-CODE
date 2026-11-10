import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.imagePromptDefault': 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.accounts.imageReceived' && params?.count) {
          return `received-${params.count}`
        }
        if (key === 'admin.accounts.imagePreviewAlt' && params?.index) {
          return `test-image-${params.index}`
        }
        return messages[key] || key
      }
    })
  }
})

function createStreamResponse(lines: string[]) {
  const encoder = new TextEncoder()
  const chunks = lines.map((line) => encoder.encode(line))
  let index = 0

  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) {
            return { done: false, value: chunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

function mountModal(account: Record<string, unknown> = {
  id: 42,
  name: 'Gemini Image Test',
  platform: 'gemini',
  type: 'apikey',
  status: 'active'
}) {
  return mount(AccountTestModal, {
    props: {
      show: false,
      account
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: {
          props: ['modelValue', 'options', 'valueKey'],
          emits: ['update:modelValue'],
          template: '<select class="select-stub" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="o in (options || [])" :key="o.id ?? o.value" :value="o.id ?? o.value">{{ o.display_name ?? o.label }}</option></select>'
        },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([
      { id: 'gemini-2.0-flash', display_name: 'Gemini 2.0 Flash' },
      { id: 'gemini-2.5-flash-image', display_name: 'Gemini 2.5 Flash Image' },
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini 3.1 Flash Image' }
    ])
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"gemini-2.5-flash-image"}\n',
        'data: {"type":"image","image_url":"data:image/png;base64,QUJD","mime_type":"image/png"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  const imageAccount = (protocol: string) => ({
    id: 7, name: 'Image Account', platform: 'image', type: 'apikey', status: 'active', credentials: { image_protocol: protocol }
  })

  it('runs image platform accounts as an image test (enabled button, prompt input, generated image preview)', async () => {
    getAvailableModels.mockResolvedValue([{ id: 'gpt-image-1', display_name: 'gpt-image-1' }])
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      'data: {"type":"test_start","model":"gpt-image-1"}\n',
      'data: {"type":"image","image_url":"data:image/png;base64,QUJD","mime_type":"image/png"}\n',
      'data: {"type":"test_complete","success":true}\n'
    ])) as any
    const wrapper = mountModal(imageAccount('openai'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="image-test-unsupported"]').exists()).toBe(false)
    const startBtn = wrapper.findAll('button').find((b) => b.text().includes('admin.accounts.startTest'))!
    expect(startBtn.attributes('disabled')).toBeUndefined()
    const prompt = wrapper.get('textarea.textarea-stub')
    expect((prompt.element as HTMLTextAreaElement).value).toBe('Generate a cute orange cat astronaut sticker on a clean pastel background.')
    await startBtn.trigger('click')
    await flushPromises()
    const body = JSON.parse((global.fetch as any).mock.calls[0][1].body)
    expect(body.model_id).toBe('gpt-image-1')
    expect(body.prompt).toContain('orange cat')
    expect(wrapper.find('img[alt="test-image-1"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it.each(['openai', 'grok'])('shows the reference image upload for %s image accounts', async (protocol) => {
    getAvailableModels.mockResolvedValue([{ id: 'some-image-model', display_name: 'some-image-model' }])
    const wrapper = mountModal(imageAccount(protocol))
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.find('input[type="file"][accept^="image/"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="image-test-imagen-no-reference"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the reference upload for gemini image models but hides it for Imagen with an explanation', async () => {
    getAvailableModels.mockResolvedValue([{ id: 'imagen-4.0-generate-001', display_name: 'imagen-4.0-generate-001' }])
    const imagen = mountModal(imageAccount('gemini'))
    await imagen.setProps({ show: true })
    await flushPromises()
    expect(imagen.find('input[type="file"][accept^="image/"]').exists()).toBe(false)
    expect(imagen.get('[data-testid="image-test-imagen-no-reference"]').text()).toBe('admin.accounts.imageTestImagenNoReference')
    imagen.unmount()

    getAvailableModels.mockResolvedValue([{ id: 'gemini-2.5-flash-image', display_name: 'gemini-2.5-flash-image' }])
    const flash = mountModal(imageAccount('gemini'))
    await flash.setProps({ show: true })
    await flushPromises()
    expect(flash.find('input[type="file"][accept^="image/"]').exists()).toBe(true)
    expect(flash.find('[data-testid="image-test-imagen-no-reference"]').exists()).toBe(false)
    flash.unmount()
  })

  const FILE_SELECTOR = 'input[type="file"][accept^="image/"]'
  const model = (id: string, supports?: boolean) => ({ id, display_name: id, ...(supports === undefined ? {} : { supports_reference_image: supports }) })

  it('hides the upload when an alias maps to an Imagen model (backend flag false)', async () => {
    getAvailableModels.mockResolvedValue([model('public-render-image', false)])
    const wrapper = mountModal(imageAccount('gemini'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.find(FILE_SELECTOR).exists()).toBe(false)
    expect(wrapper.find('[data-testid="image-test-imagen-no-reference"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows the upload for an imagen-* alias that maps to a gemini model (backend flag true)', async () => {
    getAvailableModels.mockResolvedValue([model('imagen-public', true)])
    const wrapper = mountModal(imageAccount('gemini'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.find(FILE_SELECTOR).exists()).toBe(true)
    expect(wrapper.find('[data-testid="image-test-imagen-no-reference"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('falls back to the imagen- prefix when the backend flag is missing', async () => {
    getAvailableModels.mockResolvedValue([model('imagen-4.0-generate-001')])
    const wrapper = mountModal(imageAccount('gemini'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.find(FILE_SELECTOR).exists()).toBe(false)
    wrapper.unmount()
  })

  it('clears an uploaded reference image when switching to a model without reference support', async () => {
    getAvailableModels.mockResolvedValue([model('gemini-2.5-flash-image', true), model('imagen-4.0-generate-001', false)])
    const wrapper = mountModal(imageAccount('gemini'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    const input = wrapper.get(FILE_SELECTOR)
    const file = new File(['abc'], 'ref.png', { type: 'image/png' })
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await new Promise((r) => setTimeout(r, 20))
    await flushPromises()
    expect(wrapper.find('img[src^="data:"]').exists()).toBe(true)
    const modelSelect = wrapper.findAll('select.select-stub')[0]
    await modelSelect.setValue('imagen-4.0-generate-001')
    await flushPromises()
    expect(wrapper.find(FILE_SELECTOR).exists()).toBe(false)
    await modelSelect.setValue('gemini-2.5-flash-image')
    await flushPromises()
    expect(wrapper.find('img[src^="data:"]').exists()).toBe(false)
    const startBtn = wrapper.findAll('button').find((b) => b.text().includes('admin.accounts.startTest'))!
    await startBtn.trigger('click')
    await flushPromises()
    const body = JSON.parse((global.fetch as any).mock.calls[0][1].body)
    expect(body.image_data_url).toBeUndefined()
    wrapper.unmount()
  })

  it('lets an image account start a test with an empty model list and sends no model_id', async () => {
    getAvailableModels.mockResolvedValue([])
    const wrapper = mountModal(imageAccount('gemini'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    const startBtn = wrapper.findAll('button').find((b) => b.text().includes('admin.accounts.startTest'))!
    expect(startBtn.attributes('disabled')).toBeUndefined()
    await startBtn.trigger('click')
    await flushPromises()
    const body = JSON.parse((global.fetch as any).mock.calls[0][1].body)
    expect(body).not.toHaveProperty('model_id')
    wrapper.unmount()
  })

  it('still requires a model for non-image accounts', async () => {
    getAvailableModels.mockResolvedValue([])
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    const startBtn = wrapper.findAll('button').find((b) => b.text().includes('admin.accounts.startTest'))!
    expect(startBtn.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('keeps test questions out of image accounts (no test-type select)', async () => {
    const wrapper = mountModal(imageAccount('openai'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    const normal = mountModal()
    await normal.setProps({ show: true })
    await flushPromises()
    expect(wrapper.findAll('.select-stub').length).toBeLessThan(normal.findAll('.select-stub').length)
    wrapper.unmount()
    normal.unmount()
  })

  it.each([
    ['seedance', 'doubao-seedance-1-0-pro', 'Seedance (Ark)'],
    ['gemini', 'veo-3.0-generate-001', 'Google Gemini (Veo)'],
    ['minimax', 'MiniMax-Hailuo-2.3', 'MiniMax (Hailuo / H3)']
  ])('视频账号使用 %s 视频测试并显示成品', async (protocol, model, label) => {
    getAvailableModels.mockResolvedValue([{ id: model, display_name: model }])
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      `data: ${JSON.stringify({ type: 'test_start', model })}\n`,
      'data: {"type":"status","text":"Video task status: completed"}\n',
      'data: {"type":"video","video_url":"https://cdn.example/test.mp4","mime_type":"video/mp4"}\n',
      'data: {"type":"test_complete","success":true}\n'
    ])) as any
    const wrapper = mountModal({ id: 42, name: 'Video Account', platform: 'video', type: 'apikey', status: 'active', credentials: { video_protocol: protocol } })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.get('[data-testid="video-test-protocol"]').text()).toContain(label)
    expect(wrapper.text()).toContain('admin.accounts.videoTestMode')
    expect(wrapper.text()).not.toContain('admin.accounts.testPrompt')
    const prompt = wrapper.get('textarea.textarea-stub')
    await prompt.setValue('A moving red ball')
    await wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises()
    const [url, request] = (global.fetch as any).mock.calls[0]
    expect(url).toContain('/admin/accounts/42/test')
    expect(JSON.parse(request.body)).toEqual({ model_id: model, prompt: 'A moving red ball', mode: 'video' })
    expect(wrapper.text()).toContain('admin.accounts.sendingVideoRequest')
    expect(wrapper.text()).not.toContain('admin.accounts.sendingTestMessage')
    expect(wrapper.get('video').attributes('src')).toBe('https://cdn.example/test.mp4')
    expect(wrapper.text()).toContain('admin.accounts.testCompleted')
  })

  it('gemini 图片模型测试会携带提示词并渲染图片预览', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const promptInput = wrapper.find('textarea.textarea-stub')
    expect(promptInput.exists()).toBe(true)
    await promptInput.setValue('draw a tiny orange cat astronaut')

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'gemini-3.1-flash-image',
      prompt: 'draw a tiny orange cat astronaut'
    })

    const preview = wrapper.find('img[alt="test-image-1"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('src')).toBe('data:image/png;base64,QUJD')
  })

  it('grok 账号测试默认选择 Grok 模型', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'grok-4.3', display_name: 'Grok 4.3' },
      { id: 'grok-build-0.1', display_name: 'Grok Build 0.1' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"grok-4.3"}\n',
        'data: {"type":"content","text":"ok"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 13,
      name: 'Grok Account',
      platform: 'grok',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'grok-4.3',
      prompt: '',
      mode: 'text'
    })
  })

  it('OpenAI Compact 探测会携带 compact 测试模式', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'gpt-5.4',
      prompt: '',
      mode: 'compact'
    })
  })

  it('组件卸载时中止进行中的测试流', async () => {
    let requestSignal: AbortSignal | undefined
    global.fetch = vi.fn().mockImplementation(async (_url: string, init: RequestInit) => {
      requestSignal = init.signal as AbortSignal
      return new Promise<Response>((_resolve, reject) => {
        requestSignal!.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
      })
    }) as any

    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const startButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.startTest'))
    await startButton!.trigger('click')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(requestSignal?.aborted).toBe(false)

    wrapper.unmount()
    expect(requestSignal?.aborted).toBe(true)
  })

  it('流正常结束但没收到终态事件时显示失败而不是一直处理中', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"gemini-2.5-flash-image"}\n',
        'data: {"type":"content","text":"partial"}\n'
      ])
    ) as any

    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const startButton = wrapper.findAll('button').find((button) => button.text().includes('admin.accounts.startTest'))
    await startButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.testEndedWithoutResult')
    expect(wrapper.text()).not.toContain('admin.accounts.testCompleted')
  })
})
