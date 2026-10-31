import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AccountTestQuestionsModal from '../AccountTestQuestionsModal.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  getById: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  delete: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({ adminAPI: { accountTestQuestions: mocks } }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess })
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())

const baseQuestion = {
  id: 7,
  name: 'Identity check',
  content: 'Who are you?',
  answer_patterns: ['claude', 'anthropic'],
  enabled: true,
  sort_order: 3,
  created_at: '2026-10-31T10:00:00Z',
  updated_at: '2026-10-31T10:00:00Z'
}

function question(overrides: Partial<typeof baseQuestion> = {}) {
  return { ...baseQuestion, ...overrides }
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

const globalStubs = {
  BaseDialog: {
    props: ['show', 'title'],
    template: '<div v-if="show" class="base-dialog"><slot /><slot name="footer" /></div>'
  },
  ConfirmDialog: {
    props: ['show', 'title', 'message'],
    emits: ['confirm', 'cancel'],
    template: `<div v-if="show" class="confirm-dialog">
      <button data-test="confirm-delete" @click="$emit('confirm')">confirm</button>
      <button data-test="cancel-delete" @click="$emit('cancel')">cancel</button>
    </div>`
  },
  Icon: true
}

async function openQuestions(items = [question()]) {
  mocks.list.mockResolvedValue(items)
  const wrapper = mount(AccountTestQuestionsModal, {
    props: { show: false },
    global: { stubs: globalStubs }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

async function openCreateForm() {
  const wrapper = await openQuestions([])
  await wrapper.get('[data-test="create-question"]').trigger('click')
  return wrapper
}

beforeEach(() => {
  vi.resetAllMocks()
  mocks.create.mockImplementation(data => Promise.resolve(question(data)))
  mocks.update.mockImplementation((id, data) => Promise.resolve(question({ id, ...data })))
  mocks.delete.mockResolvedValue({ message: 'deleted' })
})

describe('AccountTestQuestionsModal', () => {
  it('loads and renders questions when opened', async () => {
    const wrapper = await openQuestions([
      question({ name: 'First question', content: 'A detailed prompt', sort_order: 12 })
    ])

    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(mocks.list).toHaveBeenCalledWith()
    expect(wrapper.get('tbody').text()).toContain('First question')
    expect(wrapper.get('tbody').text()).toContain('A detailed prompt')
    expect(wrapper.get('tbody').text()).toContain('claude')
    expect(wrapper.get('tbody').text()).toContain('anthropic')
    expect(wrapper.get('tbody').text()).toContain('12')
  })

  it('creates with trimmed content and ordered, deduplicated patterns without splitting commas', async () => {
    const wrapper = await openCreateForm()
    await wrapper.get('[data-test="question-name"]').setValue('  New identity check  ')
    await wrapper.get('[data-test="question-content"]').setValue('  State your model name.  ')
    await wrapper.get('[data-test="question-patterns"]').setValue(
      ' (?i)alpha, beta \n\n(?i)alpha, beta\n ^gamma$, beta \n delta '
    )
    await wrapper.get('[data-test="question-sort-order"]').setValue('9')

    expect((wrapper.get('[data-test="question-enabled"]').element as HTMLInputElement).checked).toBe(true)
    await wrapper.get('[data-test="submit-question"]').trigger('click')
    await flushPromises()

    expect(mocks.create).toHaveBeenCalledWith({
      name: 'New identity check',
      content: 'State your model name.',
      answer_patterns: ['(?i)alpha, beta', '^gamma$, beta', 'delta'],
      enabled: true,
      sort_order: 9
    })
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.accountTestQuestions.createSuccess')
  })

  it('fills the edit form and submits the complete form with PUT', async () => {
    const wrapper = await openQuestions([
      question({ enabled: false, answer_patterns: ['first', 'second'], sort_order: 4 })
    ])
    await wrapper.get('[data-test="edit-question"]').trigger('click')

    expect((wrapper.get('[data-test="question-name"]').element as HTMLInputElement).value).toBe('Identity check')
    expect((wrapper.get('[data-test="question-content"]').element as HTMLTextAreaElement).value).toBe('Who are you?')
    expect((wrapper.get('[data-test="question-patterns"]').element as HTMLTextAreaElement).value).toBe('first\nsecond')
    expect((wrapper.get('[data-test="question-enabled"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-test="question-sort-order"]').element as HTMLInputElement).value).toBe('4')

    await wrapper.get('[data-test="question-name"]').setValue('  Updated name  ')
    await wrapper.get('[data-test="question-content"]').setValue('  Updated prompt  ')
    await wrapper.get('[data-test="question-patterns"]').setValue('updated, pattern\nupdated')
    await wrapper.get('[data-test="question-enabled"]').setValue(true)
    await wrapper.get('[data-test="question-sort-order"]').setValue('-2')
    await wrapper.get('[data-test="submit-question"]').trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(7, {
      name: 'Updated name',
      content: 'Updated prompt',
      answer_patterns: ['updated, pattern', 'updated'],
      enabled: true,
      sort_order: -2
    })
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.accountTestQuestions.updateSuccess')
  })

  it('deletes only after confirmation', async () => {
    const wrapper = await openQuestions()
    await wrapper.get('[data-test="delete-question"]').trigger('click')

    expect(wrapper.find('.confirm-dialog').exists()).toBe(true)
    expect(mocks.delete).not.toHaveBeenCalled()

    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()

    expect(mocks.delete).toHaveBeenCalledWith(7)
    expect(mocks.delete).toHaveBeenCalledTimes(1)
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.accountTestQuestions.deleteSuccess')
  })

  it.each([
    {
      label: 'blank name',
      name: '   ',
      content: 'Valid prompt',
      patterns: 'valid',
      message: 'admin.accountTestQuestions.validation.nameRequired'
    },
    {
      label: 'name over 100 characters',
      name: 'a'.repeat(101),
      content: 'Valid prompt',
      patterns: 'valid',
      message: 'admin.accountTestQuestions.validation.nameLength'
    },
    {
      label: 'blank content',
      name: 'Valid name',
      content: '   ',
      patterns: 'valid',
      message: 'admin.accountTestQuestions.validation.contentRequired'
    },
    {
      label: 'content over 20000 characters',
      name: 'Valid name',
      content: 'a'.repeat(20001),
      patterns: 'valid',
      message: 'admin.accountTestQuestions.validation.contentLength'
    },
    {
      label: 'more than 20 patterns',
      name: 'Valid name',
      content: 'Valid prompt',
      patterns: Array.from({ length: 21 }, (_, index) => `pattern-${index}`).join('\n'),
      message: 'admin.accountTestQuestions.validation.patternCount'
    },
    {
      label: 'pattern over 500 characters',
      name: 'Valid name',
      content: 'Valid prompt',
      patterns: 'a'.repeat(501),
      message: 'admin.accountTestQuestions.validation.patternLength'
    }
  ])('blocks API submission for $label', async ({ name, content, patterns, message }) => {
    const wrapper = await openCreateForm()
    await wrapper.get('[data-test="question-name"]').setValue(name)
    await wrapper.get('[data-test="question-content"]').setValue(content)
    await wrapper.get('[data-test="question-patterns"]').setValue(patterns)
    await wrapper.get('[data-test="submit-question"]').trigger('click')
    await flushPromises()

    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith(message)
  })

  it.each([
    ['create', -2147483649],
    ['create', 2147483648],
    ['edit', -2147483649],
    ['edit', 2147483648]
  ])('blocks %s with out-of-range sort order %s', async (action, sortOrder) => {
    const wrapper = action === 'create' ? await openCreateForm() : await openQuestions()
    if (action === 'edit') await wrapper.get('[data-test="edit-question"]').trigger('click')
    await wrapper.get('[data-test="question-name"]').setValue('Valid name')
    await wrapper.get('[data-test="question-content"]').setValue('Valid prompt')
    await wrapper.get('[data-test="question-sort-order"]').setValue(String(sortOrder))
    await wrapper.get('[data-test="submit-question"]').trigger('click')
    await flushPromises()

    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.accountTestQuestions.validation.sortOrderRange')
  })

  it.each([-2147483648, 2147483647])('accepts sort order boundary %s', async (sortOrder) => {
    const wrapper = await openCreateForm()
    await wrapper.get('[data-test="question-name"]').setValue('Valid name')
    await wrapper.get('[data-test="question-content"]').setValue('Valid prompt')
    await wrapper.get('[data-test="question-sort-order"]').setValue(String(sortOrder))
    await wrapper.get('[data-test="submit-question"]').trigger('click')
    await flushPromises()

    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ sort_order: sortOrder }))
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('shows the backend RE2 validation error without validating through JS RegExp', async () => {
    const wrapper = await openCreateForm()
    await wrapper.get('[data-test="question-name"]').setValue('RE2 question')
    await wrapper.get('[data-test="question-content"]').setValue('Valid prompt')
    await wrapper.get('[data-test="question-patterns"]').setValue('(?i)claude\n(?=invalid)')
    mocks.create.mockRejectedValue({ response: { data: { detail: 'answer pattern 2 "(? =invalid)" is invalid' } } })

    await wrapper.get('[data-test="submit-question"]').trigger('click')
    await flushPromises()

    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      answer_patterns: ['(?i)claude', '(?=invalid)']
    }))
    expect(mocks.showError).toHaveBeenCalledWith('answer pattern 2 "(? =invalid)" is invalid')
  })

  it('updates enabled state through the list toggle', async () => {
    const wrapper = await openQuestions()
    await wrapper.get('[data-test="toggle-question"]').trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(7, { enabled: false })
    expect(wrapper.get('[data-test="toggle-question"]').classes()).not.toContain('bg-primary-600')
  })

  it('prevents duplicate form submissions while saving', async () => {
    const pending = deferred<ReturnType<typeof question>>()
    mocks.create.mockReturnValue(pending.promise)
    const wrapper = await openCreateForm()
    await wrapper.get('[data-test="question-name"]').setValue('Valid name')
    await wrapper.get('[data-test="question-content"]').setValue('Valid prompt')
    const submit = wrapper.get('[data-test="submit-question"]')

    await submit.trigger('click')
    await submit.trigger('click')

    expect(mocks.create).toHaveBeenCalledTimes(1)
    expect(submit.attributes('disabled')).toBeDefined()
    pending.resolve(question())
    await flushPromises()
  })

  it('prevents duplicate deletes while deletion is pending', async () => {
    const pending = deferred<{ message: string }>()
    mocks.delete.mockReturnValue(pending.promise)
    const wrapper = await openQuestions()
    await wrapper.get('[data-test="delete-question"]').trigger('click')
    const confirm = wrapper.get('[data-test="confirm-delete"]')

    await confirm.trigger('click')
    await confirm.trigger('click')

    expect(mocks.delete).toHaveBeenCalledTimes(1)
    pending.resolve({ message: 'deleted' })
    await flushPromises()
  })

  it('ignores stale list responses after closing and reopening', async () => {
    const first = deferred<Array<ReturnType<typeof question>>>()
    const second = deferred<Array<ReturnType<typeof question>>>()
    mocks.list.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = mount(AccountTestQuestionsModal, {
      props: { show: false },
      global: { stubs: globalStubs }
    })

    await wrapper.setProps({ show: true })
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    second.resolve([question({ name: 'Latest question' })])
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('Latest question')

    first.resolve([question({ name: 'Stale question' })])
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('Latest question')
    expect(wrapper.get('tbody').text()).not.toContain('Stale question')
  })
})
