import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountBatchTestModal from '../AccountBatchTestModal.vue'

const mocks = vi.hoisted(() => ({
  listQuestions: vi.fn(),
  getModelIntersection: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accountTestQuestions: { list: mocks.listQuestions },
    accounts: { getModelIntersection: mocks.getModelIntersection }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: mocks.copyToClipboard })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (!params) return key
        return `${key}:${Object.entries(params).map(([name, value]) => `${name}=${value}`).join(',')}`
      }
    })
  }
})

interface QuestionFixture {
  id: number
  name: string
  content: string
  answer_patterns: string[]
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

interface ItemResultFixture {
  index: number
  account_id: number
  account_name: string
  platform: string
  question_id: number
  question_name: string
  model: string
  status: 'passed' | 'failed' | 'unjudged' | 'error'
  judged: boolean
  answer_patterns: string[]
  matched_patterns: string[]
  answer: string
  error: string
  latency_ms: number
}

const globalStubs = {
  BaseDialog: {
    props: ['show', 'title'],
    emits: ['close'],
    template: `<div v-if="show" class="base-dialog">
      <button data-test="dialog-close" @click="$emit('close')">dialog-close</button>
      <slot /><slot name="footer" />
    </div>`
  },
  Icon: true
}

function question(id: number, overrides: Partial<QuestionFixture> = {}): QuestionFixture {
  return {
    id,
    name: `Question ${id}`,
    content: `Prompt ${id}`,
    answer_patterns: [`pattern-${id}`],
    enabled: true,
    sort_order: id,
    created_at: '2026-10-31T10:00:00Z',
    updated_at: '2026-10-31T10:00:00Z',
    ...overrides
  }
}

function itemResult(index: number, status: ItemResultFixture['status']): ItemResultFixture {
  const accountId = index < 2 ? 1 : 2
  const questionId = index % 2 === 0 ? 10 : 11
  return {
    index,
    account_id: accountId,
    account_name: `Account ${accountId}`,
    platform: accountId === 1 ? 'anthropic' : 'openai',
    question_id: questionId,
    question_name: `Question ${questionId}`,
    model: `model-${accountId}`,
    status,
    judged: status === 'passed' || status === 'failed',
    answer_patterns: status === 'unjudged' ? [] : [`pattern-${questionId}`],
    matched_patterns: status === 'passed' ? [`pattern-${questionId}`] : [],
    answer: `Answer ${index}`,
    error: status === 'error' ? `Failure ${index}` : '',
    latency_ms: 1000 + index
  }
}

function streamResponse(chunks: string[]): Response {
  const encoder = new TextEncoder()
  const encodedChunks = chunks.map((chunk) => encoder.encode(chunk))
  let index = 0
  return {
    ok: true,
    status: 200,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < encodedChunks.length) {
            return { done: false, value: encodedChunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    },
    text: vi.fn()
  } as unknown as Response
}

function eventChunk(event: Record<string, unknown>): string {
  return `data: ${JSON.stringify(event)}\n\n`
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

async function openModal(
  accountIds: number[] = [1, 2],
  questions: QuestionFixture[] = [question(10), question(11)]
): Promise<VueWrapper> {
  mocks.listQuestions.mockResolvedValue(questions)
  const wrapper = mount(AccountBatchTestModal, {
    props: { show: false, accountIds },
    global: { stubs: globalStubs }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

async function selectAllQuestions(wrapper: VueWrapper): Promise<void> {
  await wrapper.get('[data-test="select-all"]').setValue(true)
}

async function startBatch(wrapper: VueWrapper): Promise<void> {
  await wrapper.get('[data-test="start"]').trigger('click')
  await flushPromises()
}

function fetchBody(callIndex = 0): Record<string, unknown> {
  const request = fetchMock.mock.calls[callIndex]?.[1]
  expect(request?.body).toBeTypeOf('string')
  return JSON.parse(String(request?.body)) as Record<string, unknown>
}

const fetchMock = vi.fn<typeof fetch>()

enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('fetch', fetchMock)
  localStorage.setItem('auth_token', 'batch-test-token')
  mocks.copyToClipboard.mockResolvedValue(true)
  mocks.getModelIntersection.mockResolvedValue({ platform: 'anthropic', models: [{ id: 'model-a', display_name: 'Model A' }], account_count: 2, failed_account_ids: [] })
  fetchMock.mockResolvedValue(streamResponse([
    eventChunk({ type: 'batch_start', data: { total: 0, account_count: 0, question_count: 0, concurrency: 3 } }),
    eventChunk({
      type: 'batch_complete',
      data: { total: 0, passed: 0, failed: 0, unjudged: 0, error: 0, cancelled: false }
    })
  ]))
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('AccountBatchTestModal', () => {
  it('loads the model intersection and renders model options for custom mode', async () => {
    const wrapper = await openModal([1, 2], [question(10)])
    expect(mocks.getModelIntersection).toHaveBeenCalledWith([1, 2])
    await wrapper.get('[data-test="question-10"]').setValue(true)
    await wrapper.get('[data-test="model-custom"]').setValue(true)
    expect(wrapper.get('[data-test="model-select"]').exists()).toBe(true)
    expect((wrapper.vm as any).modelOptions).toEqual([{ value: 'model-a', label: 'Model A' }])
  })

  it('disables the form and skips question loading for cross-platform accounts', async () => {
    mocks.getModelIntersection.mockRejectedValue({ status: 400, code: 400, message: 'Batch test requires accounts from the same platform' })
    const wrapper = await openModal([1, 2], [question(10)])
    expect(wrapper.text()).toContain('admin.accounts.batchTest.crossPlatform')
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    expect(mocks.listQuestions).not.toHaveBeenCalled()
  })

  it('recovers after cross-platform accounts are replaced with same-platform accounts', async () => {
    mocks.getModelIntersection.mockRejectedValueOnce({ status: 400, message: 'same platform required' })
    const wrapper = await openModal([1, 9], [question(10)])
    expect(wrapper.text()).toContain('admin.accounts.batchTest.crossPlatform')
    await wrapper.setProps({ accountIds: [1, 2] }); await flushPromises()
    expect(wrapper.text()).not.toContain('admin.accounts.batchTest.crossPlatform')
    expect(mocks.getModelIntersection).toHaveBeenLastCalledWith([1, 2])
    await selectAllQuestions(wrapper)
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeUndefined()
  })

  it('ignores an older cross-platform error after the latest intersection succeeds', async () => {
    const stale = deferred<unknown>()
    mocks.getModelIntersection.mockReturnValueOnce(stale.promise)
    const wrapper = await openModal([1, 9], [question(10)])
    await wrapper.setProps({ accountIds: [1, 2] }); await flushPromises()
    stale.reject({ status: 400, message: 'same platform required' }); await flushPromises()
    expect(wrapper.text()).not.toContain('admin.accounts.batchTest.crossPlatform')
    expect(wrapper.text()).not.toContain('admin.accounts.batchTest.modelsLoadFailed')
    expect(mocks.listQuestions).toHaveBeenCalledTimes(1)
    await selectAllQuestions(wrapper)
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeUndefined()
  })

  it('keeps the latest intersection failure when an older success arrives later', async () => {
    const stale = deferred<unknown>()
    mocks.getModelIntersection.mockReturnValueOnce(stale.promise)
    const wrapper = await openModal([1, 2], [question(10)])
    mocks.getModelIntersection.mockRejectedValueOnce({ status: 400, message: 'same platform required' })
    await wrapper.setProps({ accountIds: [1, 9] }); await flushPromises()
    stale.resolve({ models: [{ id: 'stale-model', display_name: 'Stale Model' }] }); await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.batchTest.crossPlatform')
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    expect(mocks.listQuestions).not.toHaveBeenCalled()
  })

  it('discards an intersection error from a previously closed modal', async () => {
    const stale = deferred<unknown>()
    mocks.getModelIntersection.mockReturnValueOnce(stale.promise)
    const wrapper = await openModal([1, 2], [question(10)])
    await wrapper.get('[data-test="dialog-close"]').trigger('click')
    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }); await flushPromises()
    stale.reject({ status: 400, message: 'same platform required' }); await flushPromises()
    expect(wrapper.text()).not.toContain('admin.accounts.batchTest.crossPlatform')
    expect(mocks.listQuestions).toHaveBeenCalledTimes(1)
    await selectAllQuestions(wrapper)
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeUndefined()
  })

  it('allows default models for other apiClient platform errors', async () => {
    mocks.getModelIntersection.mockRejectedValueOnce({ status: 500, code: 500, message: 'platform not supported' })
    const wrapper = await openModal([1, 2], [question(10)])
    expect(wrapper.text()).toContain('admin.accounts.batchTest.modelsLoadFailed')
    expect(wrapper.text()).not.toContain('admin.accounts.batchTest.crossPlatform')
    await selectAllQuestions(wrapper)
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeUndefined()
    await startBatch(wrapper)
    expect(fetchBody()).toMatchObject({ model_id: '' })
  })

  it('allows default mode when the intersection is empty or unavailable', async () => {
    mocks.getModelIntersection.mockResolvedValueOnce({ platform: 'anthropic', models: [], account_count: 2, failed_account_ids: [] })
    const empty = await openModal([1, 2], [question(10)])
    expect(empty.text()).not.toContain('admin.accounts.batchTest.crossPlatform')
    expect(empty.get('[data-test="start"]').attributes('disabled')).toBeDefined()

    mocks.getModelIntersection.mockRejectedValueOnce(new Error('upstream unavailable'))
    const failed = await openModal([1, 2], [question(10)])
    expect(failed.text()).toContain('admin.accounts.batchTest.modelsLoadFailed')
    expect(failed.get('[data-test="model-default"]').exists()).toBe(true)
  })

  it('loads enabled questions in sort order, supports select all, and shows pattern configuration', async () => {
    const wrapper = await openModal([1, 2, 3], [
      question(11, { name: 'Later', sort_order: 20, answer_patterns: [] }),
      question(12, { name: 'Disabled', sort_order: 0, enabled: false }),
      question(10, { name: 'First', sort_order: 10 })
    ])

    expect(mocks.listQuestions).toHaveBeenCalledWith(true)
    expect(wrapper.text()).toContain('admin.accounts.batchTest.selectedAccounts:count=3')
    expect(wrapper.find('[data-test="question-12"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="question-10"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.accounts.batchTest.patternsConfigured')
    expect(wrapper.text()).toContain('admin.accounts.batchTest.noPatterns')

    await selectAllQuestions(wrapper)
    expect((wrapper.get('[data-test="question-10"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="question-11"]').element as HTMLInputElement).checked).toBe(true)

    await startBatch(wrapper)
    expect(fetchBody()).toMatchObject({ question_ids: [10, 11] })
  })

  it('requires at least one account and one question', async () => {
    const noAccounts = await openModal([], [question(10)])
    await selectAllQuestions(noAccounts)
    expect(noAccounts.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    await startBatch(noAccounts)
    expect(fetchMock).not.toHaveBeenCalled()

    const noQuestions = await openModal([1], [question(10)])
    expect(noQuestions.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    await startBatch(noQuestions)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('requires a non-blank model ID when custom model mode is selected', async () => {
    const wrapper = await openModal([1], [question(10)])
    await wrapper.get('[data-test="question-10"]').setValue(true)
    await wrapper.get('[data-test="model-custom"]').setValue(true)
    await wrapper.get('[data-test="model-id"]').setValue('   ')

    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    await startBatch(wrapper)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows configuration guidance when no enabled questions are available', async () => {
    const wrapper = await openModal([1], [])
    expect(wrapper.text()).toContain('admin.accounts.batchTest.emptyQuestions')
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeDefined()
  })

  it('posts the exact default request body, authorization, UI header, and concurrency', async () => {
    const wrapper = await openModal([3, 1, 3], [question(10)])
    await wrapper.get('[data-test="question-10"]').setValue(true)
    await startBatch(wrapper)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, request] = fetchMock.mock.calls[0]
    expect(String(url)).toContain('/admin/accounts/batch-test')
    expect(request?.method).toBe('POST')
    expect(request?.headers).toEqual({
      Authorization: 'Bearer batch-test-token',
      'Content-Type': 'application/json',
      'X-Admin-UI-Request': '1'
    })
    expect(fetchBody()).toEqual({
      account_ids: [3, 1, 3],
      question_ids: [10],
      model_id: '',
      concurrency: 3
    })
  })

  it('trims a custom model ID and sends the selected concurrency', async () => {
    const wrapper = await openModal([1], [question(10)])
    await wrapper.get('[data-test="question-10"]').setValue(true)
    await wrapper.get('[data-test="model-custom"]').setValue(true)
    await wrapper.get('[data-test="model-id"]').setValue('  claude-sonnet-4-5  ')
    await wrapper.get('[data-test="concurrency"]').setValue('5')
    await startBatch(wrapper)

    expect(fetchBody()).toEqual({
      account_ids: [1],
      question_ids: [10],
      model_id: 'claude-sonnet-4-5',
      concurrency: 5
    })
  })

  it('renders out-of-order SSE results by index, all statuses, details, progress, and final summary', async () => {
    const results = [
      itemResult(3, 'error'),
      itemResult(1, 'failed'),
      itemResult(2, 'unjudged'),
      itemResult(0, 'passed')
    ]
    const payload = [
      eventChunk({ type: 'batch_start', data: { total: 4, account_count: 2, question_count: 2, concurrency: 3 } }),
      ...results.map((result) => eventChunk({ type: 'item_result', data: result })),
      eventChunk({
        type: 'batch_complete',
        data: { total: 4, passed: 1, failed: 1, unjudged: 1, error: 1, cancelled: false }
      })
    ].join('')
    fetchMock.mockResolvedValue(streamResponse([
      payload.slice(0, 37),
      payload.slice(37, 211),
      payload.slice(211)
    ]))

    const wrapper = await openModal([1, 2], [question(10), question(11)])
    await selectAllQuestions(wrapper)
    await startBatch(wrapper)

    expect(wrapper.findAll('[data-test^="result-"]').map((row) => row.attributes('data-test'))).toEqual([
      'result-0', 'result-1', 'result-2', 'result-3'
    ])
    expect(wrapper.get('[data-test="status-0"]').classes().join(' ')).toContain('green')
    expect(wrapper.get('[data-test="status-1"]').classes().join(' ')).toContain('red')
    expect(wrapper.get('[data-test="status-2"]').classes().join(' ')).toContain('yellow')
    expect(wrapper.get('[data-test="status-3"]').classes().join(' ')).toContain('orange')
    expect(wrapper.get('[data-test="progress"]').text()).toContain('completed=4,total=4')
    expect(wrapper.get('[data-test="summary"]').text()).toContain('admin.accounts.batchTest.summary.passed')
    expect(wrapper.get('[data-test="summary"]').text()).toContain('admin.accounts.batchTest.summary.cancelled')

    await wrapper.get('[data-test="expand-0"]').trigger('click')
    expect(wrapper.text()).toContain('Answer 0')
    expect(wrapper.text()).toContain('pattern-10')
    await wrapper.get('[data-test="expand-3"]').trigger('click')
    expect(wrapper.text()).toContain('Failure 3')
  })

  it('counts unique item indexes for progress and parses a residual SSE buffer', async () => {
    const result = itemResult(0, 'passed')
    fetchMock.mockResolvedValue(streamResponse([
      `${eventChunk({ type: 'batch_start', data: { total: 2, account_count: 1, question_count: 2, concurrency: 3 } })}${eventChunk({ type: 'item_result', data: result })}`,
      eventChunk({ type: 'item_result', data: result })
    ]))
    const wrapper = await openModal([1], [question(10), question(11)])
    await selectAllQuestions(wrapper)
    await startBatch(wrapper)

    expect(wrapper.findAll('[data-test^="result-"]')).toHaveLength(1)
    expect(wrapper.get('[data-test="progress"]').text()).toContain('completed=1,total=2')
    expect(wrapper.text()).toContain('admin.accounts.batchTest.endedWithoutComplete')
  })

  it.each(['data:', 'data: '])('parses batch events with the %s prefix', async (dataPrefix) => {
    const events = [
      { type: 'batch_start', data: { total: 1, account_count: 1, question_count: 1, concurrency: 3 } },
      { type: 'item_result', data: itemResult(0, 'passed') },
      { type: 'batch_complete', data: { total: 1, passed: 1, failed: 0, unjudged: 0, error: 0, cancelled: false } }
    ]
    fetchMock.mockResolvedValue(streamResponse(events.map((event) => `${dataPrefix}${JSON.stringify(event)}\n\n`)))
    const wrapper = await openModal([1], [question(10)])
    await selectAllQuestions(wrapper)
    await startBatch(wrapper)

    expect(wrapper.get('[data-test="status-0"]').attributes('data-status')).toBe('passed')
    expect(wrapper.get('[data-test="progress"]').text()).toContain('completed=1,total=1')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it.each(['stop', 'close', 'show-false', 'unmount'])('aborts without showing an error on %s', async (action) => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    let requestSignal: AbortSignal | null = null
    fetchMock.mockImplementation((_input, init) => new Promise<Response>((_resolve, reject) => {
      requestSignal = init?.signal as AbortSignal
      requestSignal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
    }))
    const wrapper = await openModal([1], [question(10)])
    await wrapper.get('[data-test="question-10"]').setValue(true)
    await wrapper.get('[data-test="start"]').trigger('click')
    await flushPromises()
    expect(requestSignal?.aborted).toBe(false)

    if (action === 'stop') await wrapper.get('[data-test="stop"]').trigger('click')
    if (action === 'close') await wrapper.get('[data-test="dialog-close"]').trigger('click')
    if (action === 'show-false') await wrapper.setProps({ show: false })
    if (action === 'unmount') wrapper.unmount()
    await flushPromises()

    expect(requestSignal?.aborted).toBe(true)
    expect(wrapper.text()).not.toContain('Aborted')
    if (action === 'stop') {
      expect(wrapper.get('[data-test="summary"]').text()).toContain('admin.accounts.batchTest.summary.cancelledYes')
    } else {
      expect((wrapper.vm as any).batchSummary).toBeNull()
      expect((wrapper.vm as any).stoppedByUser).toBe(false)
    }
    expect(warn).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    if (action === 'close') expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it.each(['restart', 'reopen'])('preserves completed results on stop and resets cancellation on %s', async (action) => {
    const pendingRead = deferred<ReadableStreamReadResult<Uint8Array>>()
    const payload = [
      eventChunk({ type: 'batch_start', data: { total: 4, account_count: 2, question_count: 2, concurrency: 3 } }),
      eventChunk({ type: 'item_result', data: itemResult(0, 'passed') })
    ].join('')
    fetchMock.mockImplementationOnce((_input, init) => {
      init?.signal?.addEventListener('abort', () => pendingRead.reject(new DOMException('Aborted', 'AbortError')))
      return Promise.resolve({
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn()
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode(payload) })
              .mockReturnValue(pendingRead.promise)
          })
        }
      } as unknown as Response)
    }).mockReturnValueOnce(deferred<Response>().promise)
    const wrapper = await openModal()
    await selectAllQuestions(wrapper)
    await startBatch(wrapper)

    expect(wrapper.get('[data-test="summary"]').text()).toContain('admin.accounts.batchTest.summary.cancelledNo')
    await wrapper.get('[data-test="stop"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="summary"]').text()).toContain('admin.accounts.batchTest.summary.cancelledYes')
    expect(wrapper.get('[data-test="status-0"]').attributes('data-status')).toBe('passed')
    expect(wrapper.get('[data-test="progress"]').text()).toContain('completed=1,total=4')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeUndefined()

    if (action === 'reopen') {
      await wrapper.setProps({ show: false })
      await wrapper.setProps({ show: true })
      await flushPromises()
      expect(wrapper.find('[data-test="summary"]').exists()).toBe(false)
      expect((wrapper.vm as any).batchSummary).toBeNull()
      expect((wrapper.vm as any).stoppedByUser).toBe(false)
      await selectAllQuestions(wrapper)
    }
    await startBatch(wrapper)

    expect(wrapper.findAll('[data-test^="result-"]')).toHaveLength(0)
    expect(wrapper.get('[data-test="summary"]').text()).toContain('admin.accounts.batchTest.summary.cancelledNo')
    expect(wrapper.get('[data-test="progress"]').text()).toContain('completed=0,total=0')
  })

  it('prevents duplicate submissions while a batch is running', async () => {
    const pending = deferred<Response>()
    fetchMock.mockReturnValue(pending.promise)
    const wrapper = await openModal([1], [question(10)])
    await wrapper.get('[data-test="question-10"]').setValue(true)

    await wrapper.get('[data-test="start"]').trigger('click')
    await wrapper.get('[data-test="start"]').trigger('click')
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await wrapper.get('[data-test="stop"]').trigger('click')
  })

  it.each([
    [{ detail: 'Question 10 is disabled' }, 'Question 10 is disabled'],
    [{ error: 'Question 11 does not exist' }, 'Question 11 does not exist'],
    [{ message: 'Invalid batch selection' }, 'Invalid batch selection']
  ])('shows backend error text for a 400 response', async (body, message) => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 400,
      text: vi.fn().mockResolvedValue(JSON.stringify(body))
    } as unknown as Response)
    const wrapper = await openModal([1], [question(10)])
    await wrapper.get('[data-test="question-10"]').setValue(true)
    await startBatch(wrapper)

    expect(wrapper.get('[role="alert"]').text()).toContain(message)
  })

  it('copies indexed results as TSV', async () => {
    const first = itemResult(0, 'passed')
    const second = itemResult(1, 'error')
    fetchMock.mockResolvedValue(streamResponse([
      eventChunk({ type: 'batch_start', data: { total: 2, account_count: 1, question_count: 2, concurrency: 3 } }),
      eventChunk({ type: 'item_result', data: second }),
      eventChunk({ type: 'item_result', data: first }),
      eventChunk({
        type: 'batch_complete',
        data: { total: 2, passed: 1, failed: 0, unjudged: 0, error: 1, cancelled: false }
      })
    ]))
    const wrapper = await openModal([1], [question(10), question(11)])
    await selectAllQuestions(wrapper)
    await startBatch(wrapper)
    await wrapper.get('[data-test="copy-results"]').trigger('click')

    expect(mocks.copyToClipboard).toHaveBeenCalledTimes(1)
    const [text, successMessage] = mocks.copyToClipboard.mock.calls[0]
    expect(successMessage).toBe('admin.accounts.batchTest.copied')
    const lines = String(text).split('\n')
    expect(lines[0]).toBe([
      'admin.accounts.batchTest.columns.account',
      'admin.accounts.batchTest.columns.platform',
      'admin.accounts.batchTest.columns.question',
      'admin.accounts.batchTest.columns.model',
      'admin.accounts.batchTest.columns.status',
      'admin.accounts.batchTest.columns.latency',
      'admin.accounts.batchTest.columns.error'
    ].join('\t'))
    expect(lines[1]).toContain('Account 1\tanthropic\tQuestion 10\tmodel-1\tadmin.accounts.batchTest.status.passed\t1000\t')
    expect(lines[2]).toContain('Failure 1')
  })

  it('resets the form, results, progress, and errors every time it opens', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 400,
      text: vi.fn().mockResolvedValue(JSON.stringify({ detail: 'Old error' }))
    } as unknown as Response)
    const wrapper = await openModal([1], [question(10)])
    await wrapper.get('[data-test="question-10"]').setValue(true)
    await wrapper.get('[data-test="model-custom"]').setValue(true)
    await wrapper.get('[data-test="model-id"]').setValue('old-model')
    await wrapper.get('[data-test="concurrency"]').setValue('5')
    await startBatch(wrapper)
    expect(wrapper.text()).toContain('Old error')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(mocks.listQuestions).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-test="model-id"]').exists()).toBe(false)
    expect((wrapper.get('[data-test="model-default"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="concurrency"]').element as HTMLSelectElement).value).toBe('3')
    expect((wrapper.get('[data-test="question-10"]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.find('[data-test="progress"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="summary"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Old error')
  })

  it('ignores stale question responses from an earlier open', async () => {
    const first = deferred<QuestionFixture[]>()
    const second = deferred<QuestionFixture[]>()
    mocks.listQuestions.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = mount(AccountBatchTestModal, {
      props: { show: false, accountIds: [1] },
      global: { stubs: globalStubs }
    })

    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(mocks.listQuestions).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(mocks.listQuestions).toHaveBeenCalledTimes(2)
    second.resolve([question(11, { name: 'Latest question' })])
    await flushPromises()
    expect(wrapper.text()).toContain('Latest question')

    first.resolve([question(10, { name: 'Stale question' })])
    await flushPromises()
    expect(wrapper.text()).toContain('Latest question')
    expect(wrapper.text()).not.toContain('Stale question')
  })
})
