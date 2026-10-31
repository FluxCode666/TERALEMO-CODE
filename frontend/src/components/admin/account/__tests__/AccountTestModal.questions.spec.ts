import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, listQuestions, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  listQuestions: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { getAvailableModels },
    accountTestQuestions: { list: listQuestions }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (!params) return key
        const interpolated = Object.entries(params).reduce(
          (message, [name, value]) => message.replace(`{${name}}`, String(value)),
          key
        )
        return interpolated === key ? `${key}:${Object.values(params).join(',')}` : interpolated
      }
    })
  }
})

const SelectStub = {
  props: ['modelValue', 'options', 'valueKey', 'labelKey', 'disabled'],
  emits: ['update:modelValue'],
  template: `
    <select
      class="select-stub"
      :value="modelValue"
      :disabled="disabled"
      @change="$emit('update:modelValue', options[$event.target.selectedIndex]?.[valueKey || 'value'])"
    >
      <option
        v-for="option in options"
        :key="String(option[valueKey || 'value'])"
        :value="option[valueKey || 'value']"
      >
        {{ option[labelKey || 'label'] }}
      </option>
    </select>
  `
}

const question = {
  id: 1,
  name: '身份自检',
  content: '你是谁？请说出你的模型名称。',
  answer_patterns: ['claude', 'anthropic'],
  enabled: true,
  sort_order: 0,
  created_at: '2026-10-31T10:00:00Z',
  updated_at: '2026-10-31T10:00:00Z'
}

const modelsByPlatform: Record<string, Array<{ id: string; display_name: string }>> = {
  anthropic: [{ id: 'claude-sonnet-4', display_name: 'Claude Sonnet 4' }],
  openai: [{ id: 'gpt-5.4', display_name: 'GPT-5.4' }],
  grok: [{ id: 'grok-4.3', display_name: 'Grok 4.3' }],
  gemini: [{ id: 'gemini-2.0-flash', display_name: 'Gemini 2.0 Flash' }]
}

function createStreamResponse(events: Array<Record<string, unknown>>, dataPrefix = 'data: ') {
  const encoder = new TextEncoder()
  const chunks = events.map((event) => encoder.encode(`${dataPrefix}${JSON.stringify(event)}\n`))
  let index = 0
  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) return { done: false, value: chunks[index++] }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

async function openModal(platform = 'gemini') {
  getAvailableModels.mockResolvedValue(modelsByPlatform[platform] ?? modelsByPlatform.gemini)
  listQuestions.mockResolvedValue([question])
  const wrapper = mount(AccountTestModal, {
    props: {
      show: false,
      account: { id: 42, name: `${platform} account`, platform, type: 'apikey', status: 'active' }
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: SelectStub,
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

async function chooseQuestionMode(wrapper: VueWrapper, platform: string) {
  const selector = platform === 'openai'
    ? '[data-testid="openai-test-mode"]'
    : platform === 'grok'
      ? '[data-testid="grok-test-mode"]'
      : '[data-testid="test-type"]'
  await wrapper.get(selector).setValue('question')
  await flushPromises()
}

async function chooseQuestion(wrapper: VueWrapper, platform: string, id = 1) {
  await chooseQuestionMode(wrapper, platform)
  await wrapper.get('[data-testid="test-question"]').setValue(String(id))
  await flushPromises()
}

async function startTest(wrapper: VueWrapper) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes('admin.accounts.startTest'))
  expect(button).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}

function requestBody() {
  const [, request] = (global.fetch as any).mock.calls[0]
  return JSON.parse(request.body)
}

describe('AccountTestModal 测试题目', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(globalThis, 'localStorage', {
      value: { getItem: vi.fn(() => 'test-token') },
      configurable: true
    })
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([{ type: 'test_complete', success: true }])) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('OpenAI 在现有模式中加入测试题目，Grok 将其放在 text 后面', async () => {
    const openai = await openModal('openai')
    expect(openai.get('[data-testid="openai-test-mode"]').findAll('option').map((option) => option.element.value)).toEqual([
      'default', 'compact', 'question'
    ])
    openai.unmount()

    const grok = await openModal('grok')
    expect(grok.get('[data-testid="grok-test-mode"]').findAll('option').map((option) => option.element.value)).toEqual([
      'text', 'question', 'image', 'video', 'search', 'tts', 'stt', 'realtime'
    ])
  })

  it.each(['anthropic', 'gemini'])('%s 使用独立测试类型下拉', async (platform) => {
    const wrapper = await openModal(platform)
    expect(wrapper.get('[data-testid="test-type"]').findAll('option').map((option) => option.element.value)).toEqual([
      'connectivity', 'question'
    ])
  })

  it.each(['video', 'typesafe'])('%s 不显示测试题目选项', async (platform) => {
    const wrapper = await openModal(platform)
    expect(wrapper.find('[data-testid="test-type"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="openai-test-mode"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="grok-test-mode"]').exists()).toBe(false)
  })

  it.each([
    ['anthropic', { model_id: 'claude-sonnet-4', prompt: '', test_question_id: 1 }],
    ['openai', { model_id: 'gpt-5.4', prompt: '', mode: 'default', test_question_id: 1 }],
    ['grok', { model_id: 'grok-4.3', prompt: '', mode: 'text', test_question_id: 1 }],
    ['gemini', { model_id: 'gemini-2.0-flash', prompt: '', test_question_id: 1 }]
  ])('%s 题目模式发送契约请求体', async (platform, expectedBody) => {
    const wrapper = await openModal(platform)
    await chooseQuestion(wrapper, platform)
    expect(wrapper.find('textarea.textarea-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain(question.content)
    expect(wrapper.text()).toContain('claude')
    await startTest(wrapper)
    expect(listQuestions).toHaveBeenCalledWith(true)
    expect(requestBody()).toEqual(expectedBody)
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.starting')
  })

  it('未选择题目前禁止开始，空列表显示配置引导', async () => {
    const wrapper = await openModal('gemini')
    listQuestions.mockResolvedValue([])
    await chooseQuestionMode(wrapper, 'gemini')
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.empty')
    const startButton = wrapper.findAll('button').find((candidate) => candidate.text().includes('admin.accounts.startTest'))
    expect(startButton?.attributes('disabled')).toBeDefined()
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('题目模式过滤图片模型并显示未配置期望答案提示', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini Image' },
      { id: 'gemini-2.0-flash', display_name: 'Gemini Text' }
    ])
    listQuestions.mockResolvedValue([{ ...question, answer_patterns: [] }])
    const wrapper = mount(AccountTestModal, {
      props: { show: false, account: { id: 42, name: 'Gemini', platform: 'gemini', type: 'apikey', status: 'active' } } as any,
      global: { stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: SelectStub,
        TextArea: true,
        Icon: true
      } }
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await chooseQuestionMode(wrapper, 'gemini')
    expect(wrapper.get('[data-testid="test-model"]').findAll('option').map((option) => option.element.value)).toEqual([
      'gemini-2.0-flash'
    ])
    await wrapper.get('[data-testid="test-question"]').setValue('1')
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.noPatterns')
  })

  it('模型列表在题目模式后返回时仍重选文本模型', async () => {
    let resolveModels!: (models: Array<{ id: string; display_name: string }>) => void
    getAvailableModels.mockReturnValue(new Promise((resolve) => { resolveModels = resolve }))
    listQuestions.mockResolvedValue([question])
    const wrapper = mount(AccountTestModal, {
      props: { show: false, account: { id: 42, name: 'Gemini', platform: 'gemini', type: 'apikey', status: 'active' } } as any,
      global: { stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: SelectStub,
        TextArea: true,
        Icon: true
      } }
    })
    await wrapper.setProps({ show: true })
    await chooseQuestionMode(wrapper, 'gemini')
    resolveModels([
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini Image' },
      { id: 'gemini-2.0-flash', display_name: 'Gemini Text' }
    ])
    await flushPromises()
    expect((wrapper.vm as any).selectedModelId).toBe('gemini-2.0-flash')
  })

  it('渲染匹配正则的 question_result，并以 test_complete 成功收尾', async () => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      { type: 'content', text: 'I am Claude' },
      { type: 'question_result', success: true, data: {
        question_id: 1, question_name: question.name, judged: true,
        answer_patterns: ['(?i)claude', '(?i)anthropic'], matched_patterns: ['(?i)claude']
      } },
      { type: 'test_complete', success: true }
    ])) as any
    const wrapper = await openModal('gemini')
    await chooseQuestion(wrapper, 'gemini')
    await startTest(wrapper)
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.answerPatterns')
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.matchedPatterns')
    expect(wrapper.text()).toContain('admin.accounts.testCompleted')
    const matchedLine = wrapper.findAll('div').find((line) => line.text() === 'admin.accounts.testQuestion.matchedPatterns:(?i)claude')
    expect(matchedLine?.classes()).toContain('text-green-400')
  })

  it('渲染未匹配正则的 question_result，最终失败使用前端题目未通过文案', async () => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      { type: 'question_result', success: false, data: {
        question_id: 1, question_name: question.name, judged: true,
        answer_patterns: ['(?i)claude'], matched_patterns: []
      } },
      { type: 'test_complete', success: false, error: 'Answer did not match any expected pattern' }
    ])) as any
    const wrapper = await openModal('gemini')
    await chooseQuestion(wrapper, 'gemini')
    await startTest(wrapper)
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.noMatchedPatterns')
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.failed')
    expect(wrapper.text()).not.toContain('Answer did not match any expected pattern')
    const unmatchedLine = wrapper.findAll('div').find((line) => line.text() === 'admin.accounts.testQuestion.noMatchedPatterns')
    expect(unmatchedLine?.classes()).toContain('text-red-400')
  })

  it.each([
    ['data:', 'Test question is disabled'],
    ['data:', 'Test question not found'],
    ['data: ', 'Test question is disabled'],
    ['data: ', 'Test question not found']
  ])('解析 %s 错误事件并显示 %s', async (dataPrefix, error) => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      { type: 'error', error }
    ], dataPrefix)) as any
    const wrapper = await openModal('gemini')
    await chooseQuestion(wrapper, 'gemini')
    await startTest(wrapper)

    expect(wrapper.text()).toContain(error)
    expect(wrapper.text()).not.toContain('admin.accounts.testEndedWithoutResult')
    expect((wrapper.vm as any).status).toBe('error')
  })

  it('渲染无需判定的 question_result，不显示未匹配或失败', async () => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      { type: 'question_result', success: true, data: {
        question_id: 1, question_name: question.name, judged: false,
        answer_patterns: [], matched_patterns: []
      } },
      { type: 'test_complete', success: true }
    ])) as any
    const wrapper = await openModal('gemini')
    await chooseQuestion(wrapper, 'gemini')
    await startTest(wrapper)
    expect(wrapper.text()).toContain('admin.accounts.testQuestion.noPatternsResult')
    expect(wrapper.text()).not.toContain('admin.accounts.testQuestion.noMatchedPatterns')
    expect(wrapper.text()).not.toContain('admin.accounts.testQuestion.failed')
  })

  it.each([
    ['anthropic', { model_id: 'claude-sonnet-4', prompt: '' }],
    ['openai', { model_id: 'gpt-5.4', prompt: '', mode: 'default' }],
    ['grok', { model_id: 'grok-4.3', prompt: '', mode: 'text' }],
    ['gemini', { model_id: 'gemini-2.0-flash', prompt: '' }]
  ])('%s 默认连通性请求体保持原样', async (platform, expectedBody) => {
    const wrapper = await openModal(platform)
    await startTest(wrapper)
    expect(requestBody()).toEqual(expectedBody)
    expect(requestBody()).not.toHaveProperty('test_question_id')
  })

  it('每次重新打开弹窗都恢复默认测试类型', async () => {
    const wrapper = await openModal('openai')
    await chooseQuestionMode(wrapper, 'openai')
    expect(wrapper.get('[data-testid="openai-test-mode"]').element.value).toBe('question')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.get('[data-testid="openai-test-mode"]').element.value).toBe('default')
    expect(wrapper.find('[data-testid="test-question"]').exists()).toBe(false)
  })
})
