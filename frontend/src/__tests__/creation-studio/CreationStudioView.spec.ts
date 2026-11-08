import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import CreationStudioView from '@/views/user/CreationStudioView.vue'
import { creationStudioApi } from '@/features/creation-studio/api'
import { asyncTasksAPI } from '@/api/asyncTasks'
import { keysAPI } from '@/api/keys'
import { optionsOpenAI, optionsVideo, optionsStorageDisabled, estimateSufficient, estimateUnpriced, submitImageFixture, uploadFixture } from '@/features/creation-studio/fixtures'
import en from '@/i18n/locales/en/creationStudio'
import type { CreationModelOption, CreationOptionsResponse } from '@/features/creation-studio/types'
import { clone, NOW, page, snapshot, task, track } from './helpers'
import { errorCases } from './errorCases'
import type { CreationSnapshotImage } from '@/features/creation-studio/types'

const mocks = vi.hoisted(() => ({
  auth: { user: { id: 42, role: 'user', enterprise: null as null | { id: number; role: string } } },
  app: { showToast: vi.fn() },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))

function locale() {
  return createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false,
    messages: { en: { ...en, common: { refresh: 'Refresh', remove: 'Remove' } } },
    messageCompiler: message => new Function(`return ${baseCompile(String(message)).code}`)(),
  })
}

async function render(options: CreationOptionsResponse = clone(optionsOpenAI)) {
  vi.mocked(creationStudioApi.getOptions).mockResolvedValue(options)
  const i18n = locale()
  const wrapper = track(mount(CreationStudioView, { global: { plugins: [i18n], stubs: { Lightbox: true } } }))
  await flushPromises()
  await vi.advanceTimersByTimeAsync(300)
  await flushPromises()
  return { wrapper, t: i18n.global.t }
}

async function fillAndSubmit(wrapper: VueWrapper) {
  await wrapper.get('#creation-prompt').setValue('  QA lighthouse  ')
  await wrapper.get('.cs-submit').trigger('click')
  await flushPromises()
}

async function uploadFile(wrapper: VueWrapper, index = 0) {
  const input = wrapper.findAll('input[type="file"]')[index]
  Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['qa'], 'qa.png', { type: 'image/png' })] })
  await input.trigger('change')
  await flushPromises()
}

function seedReusableTask(model: CreationModelOption, images: CreationSnapshotImage[]) {
  localStorage.setItem('creationStudio:snapshots:v1:42', JSON.stringify({ v: 1, items: [snapshot({
    mode: model.mode, model: model.model, protocol: model.protocol, params: {}, images,
  })] }))
  vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([task({
    id: submitImageFixture.catalog_id, task_id: submitImageFixture.task_id,
    kind: model.mode, model: model.model, protocol: model.protocol,
  })]))
}

async function reuseTask(wrapper: VueWrapper, label: string) {
  const button = wrapper.findAll('.cs-task button').find(button => button.text().includes(label))!
  await button.trigger('click')
  await flushPromises()
}

function snapshotImage(role: CreationSnapshotImage['role']): CreationSnapshotImage {
  return { role, upload_id: uploadFixture.upload_id, url: uploadFixture.url,
    expires_at: NOW + 86400, width: 1024, height: 1024 }
}

beforeEach(() => {
  vi.useFakeTimers(); vi.setSystemTime(NOW * 1000); localStorage.clear()
  mocks.auth.user.enterprise = null
  mocks.app.showToast.mockClear()
  vi.spyOn(keysAPI, 'list').mockResolvedValue({ items: [{ id: 7, name: 'QA key', group_id: 3, group: { id: 3, name: 'QA group' }, status: 'active' }], total: 1 } as Awaited<ReturnType<typeof keysAPI.list>>)
  vi.spyOn(creationStudioApi, 'getOptions').mockResolvedValue(clone(optionsOpenAI))
  vi.spyOn(creationStudioApi, 'estimate').mockResolvedValue(clone(estimateSufficient))
  vi.spyOn(creationStudioApi, 'submit').mockImplementation(async body => ({
    ...clone(submitImageFixture), mode: body.mode, model: body.model, protocol: body.protocol,
  }))
  vi.spyOn(creationStudioApi, 'upload').mockResolvedValue(clone(uploadFixture))
  vi.spyOn(asyncTasksAPI, 'list').mockResolvedValue(page([]))
  vi.spyOn(asyncTasksAPI, 'refresh').mockImplementation(async id => task({ id, kind: 'video', status: 'processing' }))
  vi.spyOn(asyncTasksAPI, 'download').mockRejectedValue(new Error('Offline video boundary'))
})
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('CreationStudioView states and validation (Q3)', () => {
  it('renders empty history and a no-key state linking to key creation', async () => {
    vi.mocked(keysAPI.list).mockResolvedValue({ items: [], total: 0 } as Awaited<ReturnType<typeof keysAPI.list>>)
    const { wrapper, t } = await render()
    expect(wrapper.text()).toContain(t('creationStudio.unavailable.noKey.title'))
    expect(wrapper.get('.cs-form-body a').attributes('href')).toBe('/keys')
    expect(wrapper.find('.cs-submit').exists()).toBe(false)
    expect(wrapper.find('.cs-results .cs-empty').exists()).toBe(true)
    expect(creationStudioApi.getOptions).not.toHaveBeenCalled()
  })

  it('renders no-model state without a submit button', async () => {
    const { wrapper, t } = await render({ ...clone(optionsOpenAI), models: [] })
    expect(wrapper.text()).toContain(t('creationStudio.unavailable.noModel.title'))
    expect(wrapper.find('.cs-submit').exists()).toBe(false)
  })

  it('keeps image mode unavailable when storage is closed, while text video remains usable', async () => {
    const { wrapper, t } = await render(clone(optionsStorageDisabled))
    const imageMode = wrapper.findAll('[role="tab"]').find(button => button.text().includes(t('creationStudio.mode.image')))!
    await imageMode.trigger('click')
    expect(wrapper.text()).toContain(t('creationStudio.unavailable.imageStorage.title'))
    expect(wrapper.find('.cs-submit').exists()).toBe(false)
    const videoMode = wrapper.findAll('[role="tab"]').find(button => button.text().includes(t('creationStudio.mode.video')))!
    await videoMode.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.cs-dropzone[data-state="unavailable"]')).toHaveLength(1)
    expect(wrapper.find('input[type="file"]').exists()).toBe(false)
    await fillAndSubmit(wrapper)
    expect(creationStudioApi.submit).toHaveBeenCalledTimes(1)
    expect(vi.mocked(creationStudioApi.submit).mock.calls[0][0]).not.toHaveProperty('images')
  })

  it('shows no-video-account state instead of a usable form', async () => {
    const { wrapper, t } = await render()
    const video = wrapper.findAll('[role="tab"]').find(button => button.text().includes(t('creationStudio.mode.video')))!
    await video.trigger('click')
    expect(wrapper.text()).toContain(t('creationStudio.unavailable.noModel.title'))
    expect(wrapper.find('.cs-submit').exists()).toBe(false)
  })

  it.each(['', '   ', 'x'.repeat(8001)])('disables submit for invalid prompt #%#', async prompt => {
    const { wrapper } = await render()
    await wrapper.get('#creation-prompt').setValue(prompt)
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#creation-prompt').attributes('aria-invalid')).toBe('true')
    await wrapper.get('#creation-prompt').trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(creationStudioApi.submit).not.toHaveBeenCalled()
  })

  it('shows pricing warning callout and unavailable cost, disabling unpriced submissions', async () => {
    vi.mocked(creationStudioApi.estimate).mockResolvedValue(clone(estimateUnpriced))
    const { wrapper, t } = await render()
    await wrapper.get('#creation-prompt').setValue('QA lighthouse')
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeDefined()
    expect(wrapper.get('.cs-cost').attributes('data-state')).toBe('unavailable')
    expect(wrapper.find('.cs-callout--warning .cs-callout-title').exists()).toBe(true)
    expect(wrapper.get('.cs-callout--warning .cs-callout-title').text()).toBe(t('creationStudio.errors.pricingMissing'))
  })

  it('estimate service failure hides only balance and does not show a submit error callout', async () => {
    vi.mocked(creationStudioApi.estimate).mockRejectedValue({ status: 503, reason: 'CREATION_ESTIMATE_UNAVAILABLE' })
    const { wrapper } = await render()
    await wrapper.get('#creation-prompt').setValue('QA lighthouse')
    expect(wrapper.find('.cs-balance').exists()).toBe(false)
    expect(wrapper.find('.cs-callout').exists()).toBe(false)
    expect(wrapper.get('.cs-cost').attributes('data-state')).toBe('unavailable')
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeUndefined()
  })

  it.each([
    [400, 'CREATION_MODEL_NOT_AVAILABLE', 'options'], [404, 'CREATION_KEY_NOT_FOUND', 'keys'],
  ] as const)('estimate %i %s silently refreshes %s', async (status, reason, target) => {
    vi.mocked(creationStudioApi.estimate).mockRejectedValue({ status, reason })
    const { wrapper } = await render()
    expect(wrapper.find('.cs-form-footer .cs-callout').exists()).toBe(false)
    const calls = target === 'options' ? vi.mocked(creationStudioApi.getOptions).mock.calls : vi.mocked(keysAPI.list).mock.calls
    expect(calls.length).toBeGreaterThan(1)
  })

  it('inserts a successful task first and persists the submitted snapshot', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([task({ model: 'older task' })]))
    const { wrapper } = await render()
    await fillAndSubmit(wrapper)
    expect(wrapper.findAll('.cs-task')).toHaveLength(2)
    expect(wrapper.get('.cs-task').attributes('data-status')).toBe('processing')
    expect(wrapper.get('.cs-task-prompt').text()).toBe('QA lighthouse')
    expect(wrapper.get('.cs-task [role="progressbar"]').attributes('data-indeterminate')).toBe('true')
    const stored = JSON.parse(localStorage.getItem('creationStudio:snapshots:v1:42')!)
    expect(stored.items[0]).toMatchObject({ catalog_id: submitImageFixture.catalog_id, prompt: 'QA lighthouse', params: { n: 1 } })
  })

  it('successful upload transitions the rendered thumbnail to ready and permits submit', async () => {
    const { wrapper } = await render()
    await uploadFile(wrapper)
    expect(wrapper.find('.cs-thumb-progress').exists()).toBe(false)
    await fillAndSubmit(wrapper)
    expect(creationStudioApi.submit).toHaveBeenCalledTimes(1)
  })

  it('switching form mode does not change the history filter', async () => {
    const options = { ...clone(optionsOpenAI), video: { available: true }, models: [...clone(optionsOpenAI.models), ...clone(optionsVideo.models)] }
    const { wrapper, t } = await render(options)
    const filter = wrapper.findAll('.cs-results button').find(button => button.text() === t('creationStudio.feed.filterImage'))!
    await filter.trigger('click')
    await flushPromises()
    const video = wrapper.findAll('[role="tab"]').find(button => button.text().includes(t('creationStudio.mode.video')))!
    await video.trigger('click')
    expect(filter.attributes('aria-selected')).toBe('true')
    expect(asyncTasksAPI.list).toHaveBeenLastCalledWith(expect.objectContaining({ kind: 'image' }), expect.any(AbortSignal))
  })

  it.each([1, 2, 3, 4])('renders all %i result_urls with ordered safe images and downloads', async count => {
    const urls = Array.from({ length: count }, (_, index) => `https://img.example.com/qa-${index}.png`)
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([task({ result_urls: urls })]))
    const { wrapper } = await render()
    expect(wrapper.get('.cs-media-grid').attributes('data-count')).toBe(String(count))
    expect(wrapper.findAll('.cs-media-grid img').map(img => img.attributes('src'))).toEqual(urls)
    expect(wrapper.findAll('.cs-media-grid a[download]').map(link => link.attributes('href'))).toEqual(urls)
  })

  it('blocks javascript result URLs in images, download links and the preview', async () => {
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([task({ result_urls: ['javascript:alert(1)', 'https://img.example.com/safe.png'] })]))
    const { wrapper, t } = await render()
    expect(wrapper.findAll('.cs-media-grid img').map(img => img.attributes('src'))).toEqual(['https://img.example.com/safe.png'])
    expect(wrapper.find('[href^="javascript:"], [src^="javascript:"]').exists()).toBe(false)
    await wrapper.get('.cs-media-open').trigger('click')
    const preview = wrapper.findComponent({ name: 'Lightbox' })
    expect(preview.props('items')[preview.props('index')].url).toBe('https://img.example.com/safe.png')
    expect(wrapper.get('.cs-media-open').attributes('aria-label')).toBe(t('creationStudio.action.previewCount', { index: 1, total: 1 }))
  })

  it('restores expired snapshot images as expired thumbnails and blocks resubmission', async () => {
    localStorage.setItem('creationStudio:snapshots:v1:42', JSON.stringify({ v: 1, items: [snapshot({ images: [{
      role: 'source', upload_id: uploadFixture.upload_id, url: uploadFixture.url,
      expires_at: NOW + 899, width: 1024, height: 1024,
    }] })] }))
    vi.mocked(asyncTasksAPI.list).mockResolvedValue(page([task({ id: submitImageFixture.catalog_id, task_id: submitImageFixture.task_id })]))
    const { wrapper, t } = await render()
    const reuse = wrapper.findAll('.cs-task button').find(button => button.text().includes(t('creationStudio.action.reuse')))!
    await reuse.trigger('click')
    expect(wrapper.find('.cs-thumb[data-state="expired"]').exists()).toBe(true)
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeDefined()
  })

  it('resource error on a restored image marks its thumbnail expired', async () => {
    seedReusableTask(optionsOpenAI.models[0], [snapshotImage('source')])
    const { wrapper, t } = await render()
    await reuseTask(wrapper, t('creationStudio.action.reuse'))
    await wrapper.get('.cs-thumb img').trigger('error')
    expect(wrapper.find('.cs-thumb[data-state="expired"]').exists()).toBe(true)
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeDefined()
  })
})

describe('CreationStudioView uniform request mapping (contract 5.1, not backend 5.4)', () => {
  const h3: CreationModelOption = { ...clone(optionsVideo.models[0]),
    id: 'video:minimax:MiniMax-H3', protocol: 'minimax', model: 'MiniMax-H3',
    params: clone(optionsVideo.models[0].params).filter(param => param.key !== 'watermark').map(param => {
      if (param.kind === 'integer') return { ...param, min: 4, max: 15, default: 6 }
      if (param.kind === 'select' && param.key === 'resolution') return { ...param, options: [{ value: '768p' }], default: '768p' }
      return param
    }),
  }
  const cases = [
    { model: optionsOpenAI.models[0], expected: { size: '1024x1024', quality: 'auto', n: 1 } },
    { model: optionsOpenAI.models[1], expected: { aspect_ratio: '1:1', resolution: '1k', n: 1 } },
    { model: optionsVideo.models[0], expected: { resolution: '720p', duration: 5, ratio: '16:9', watermark: 'false' } },
    { model: optionsVideo.models[1], expected: { resolution: '720p', duration: 8, aspect_ratio: '16:9' } },
    { model: optionsVideo.models[2], expected: { resolution: '768p', duration: 6 } },
    { model: h3, expected: { resolution: '768p', duration: 6, ratio: '16:9' } },
    { model: optionsVideo.models[3], expected: { duration: 8, resolution: '480p' } },
  ]

  it.each(cases)('maps $model.id descriptor defaults into uniform request fields', async ({ model, expected }) => {
    const { wrapper } = await render({ ...clone(optionsOpenAI), video: { available: true }, models: [clone(model)] })
    await fillAndSubmit(wrapper)
    expect(creationStudioApi.submit).toHaveBeenCalledTimes(1)
    expect(creationStudioApi.submit).toHaveBeenCalledWith({
      api_key_id: 7, mode: model.mode, protocol: model.protocol, model: model.model,
      prompt: 'QA lighthouse', params: expected,
    })
    const estimated = vi.mocked(creationStudioApi.estimate).mock.calls[0][0]
    expect(estimated).toEqual({ api_key_id: 7, mode: model.mode, protocol: model.protocol, model: model.model, params: expected })
    expect(estimated).not.toHaveProperty('prompt')
  })

  it.each(cases)('maps $model.id images to role/upload_id only, never URL or bytes', async ({ model }) => {
    const role = model.mode === 'image' ? 'source' : 'first_frame'
    seedReusableTask(model, [snapshotImage(role)])
    const { wrapper, t } = await render({ ...clone(optionsOpenAI), video: { available: true }, models: [clone(model)] })
    await reuseTask(wrapper, t('creationStudio.action.reuse'))
    await fillAndSubmit(wrapper)
    expect(creationStudioApi.submit).toHaveBeenCalledTimes(1)
    const body = vi.mocked(creationStudioApi.submit).mock.calls[0][0]
    expect(body.images).toEqual([{ role: model.mode === 'image' ? 'source' : 'first_frame', upload_id: uploadFixture.upload_id }])
    expect(JSON.stringify(body)).not.toContain(uploadFixture.url)
    expect(body.params).not.toHaveProperty('source')
    expect(body.params).not.toHaveProperty('first_frame')
  })

  it('maps changed select/integer fields without retaining defaults or stringifying n', async () => {
    const { wrapper } = await render()
    await wrapper.get('input[name="param-size"][value="1536x1024"]').setValue(true)
    await wrapper.get('input[name="param-quality"][value="high"]').setValue(true)
    await wrapper.get('#param-n').setValue(4)
    await fillAndSubmit(wrapper)
    expect(creationStudioApi.submit).toHaveBeenCalledWith(expect.objectContaining({ params: { size: '1536x1024', quality: 'high', n: 4 } }))
  })

  it.each(['seedance', 'gemini', 'minimax'] as const)('preserves %s reference role, upload id and ordering', async protocol => {
    const model = protocol === 'minimax' ? h3 : optionsVideo.models.find(model => model.protocol === protocol)!
    const refs = [snapshotImage('reference')]
    if (protocol !== 'minimax') refs.push({ ...snapshotImage('reference'), upload_id: `cupl_${'b'.repeat(32)}` })
    seedReusableTask(model, refs)
    const { wrapper, t } = await render({ ...clone(optionsOpenAI), video: { available: true }, models: [clone(model)] })
    await reuseTask(wrapper, t('creationStudio.action.reuse'))
    await fillAndSubmit(wrapper)
    expect(creationStudioApi.submit).toHaveBeenCalledWith(expect.objectContaining({ images: refs.map(({ role, upload_id }) => ({ role, upload_id })) }))
  })
})

describe('CreationStudioView error placement (contract 6.1 / design 7.1-7.2)', () => {
  it('normalizes panel HTTP 429 string code RATE_LIMITED without reason to retryable rate-limit copy', async () => {
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status: 429, code: 'RATE_LIMITED', message: 'QA panel rate limit' })
    const { wrapper, t } = await render()
    await fillAndSubmit(wrapper)
    const callout = wrapper.get('.cs-form-footer .cs-callout')
    expect(callout.classes()).toContain('cs-callout--warning')
    expect(callout.get('.cs-callout-title').text()).toBe(t('creationStudio.errors.rateLimited', { retryAfter: 0 }))
    expect(callout.text()).toContain('QA panel rate limit')
    expect(callout.get('button').text()).toBe(t('creationStudio.action.retry'))
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeUndefined()
  })

  it.each(errorCases)('$status $reason renders errors.$key above submit with $tone tone', async ({ status, reason, key, tone }) => {
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status, reason, message: 'QA upstream detail', metadata: {} })
    const { wrapper, t } = await render()
    await fillAndSubmit(wrapper)
    const callout = wrapper.get('.cs-form-footer .cs-callout')
    expect(callout.classes()).toContain(`cs-callout--${tone}`)
    expect(callout.get('.cs-callout-title').text()).toBe(t(`creationStudio.errors.${key}`, {
      max: '10.0', maxSide: '4096', maxPixels: 16777216, types: 'PNG / JPEG / WebP', retryAfter: 0,
    }))
    expect(callout.text()).toContain('QA upstream detail')
    expect(callout.element.compareDocumentPosition(wrapper.get('.cs-submit').element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect((wrapper.get('#creation-prompt').element as HTMLTextAreaElement).value).toBe('  QA lighthouse  ')
    expect(localStorage.getItem('creationStudio:snapshots:v1:42')).toBeNull()
  })

  it.each([402, 403])('%i balance error marks the cost summary insufficient', async status => {
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status, reason: status === 402 ? 'insufficient_balance' : 'INSUFFICIENT_BALANCE' })
    const { wrapper } = await render(); await fillAndSubmit(wrapper)
    expect(wrapper.get('.cs-cost').attributes('data-state')).toBe('insufficient')
    expect(wrapper.get('.cs-callout a[href="/purchase"]').exists()).toBe(true)
  })

  it('enterprise wallet failure never offers personal recharge', async () => {
    mocks.auth.user.enterprise = { id: 1, role: 'member' }
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status: 403, reason: 'ENTERPRISE_BALANCE_INSUFFICIENT' })
    const { wrapper } = await render(); await fillAndSubmit(wrapper)
    expect(wrapper.find('.cs-callout a[href="/purchase"]').exists()).toBe(false)
  })

  it('counts down retry_after to zero and restores the retry action', async () => {
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status: 429, reason: 'rate_limit_exceeded', metadata: { retry_after: '2' } })
    const { wrapper, t } = await render(); await fillAndSubmit(wrapper)
    expect(wrapper.get('.cs-callout').text()).toContain(t('creationStudio.errors.rateLimitedCountdown', { retryAfter: 2 }))
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.find('.cs-callout button').exists()).toBe(true)
    expect(wrapper.get('.cs-callout button').text()).toBe(t('creationStudio.action.retry'))
  })

  it.each(['CREATION_UPLOAD_NOT_FOUND', 'CREATION_UPLOAD_EXPIRED'])('%s marks submitted thumbnails expired', async reason => {
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status: reason.endsWith('NOT_FOUND') ? 404 : 410, reason })
    seedReusableTask(optionsOpenAI.models[0], [snapshotImage('source')])
    const { wrapper, t } = await render()
    await reuseTask(wrapper, t('creationStudio.action.reuse'))
    await fillAndSubmit(wrapper)
    expect(creationStudioApi.submit).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.cs-thumb[data-state="expired"]').exists()).toBe(true)
    expect(wrapper.find('.cs-field-warning').exists()).toBe(true)
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeDefined()
  })

  it.each([
    [413, 'CREATION_UPLOAD_TOO_LARGE', 'uploadTooLarge'], [415, 'CREATION_UPLOAD_UNSUPPORTED_TYPE', 'uploadUnsupportedType'],
    [422, 'CREATION_UPLOAD_INVALID_IMAGE', 'uploadInvalidImage'], [429, 'CREATION_UPLOAD_LIMIT_EXCEEDED', 'uploadLimit'],
  ] as const)('upload %i %s is displayed inside the image field, not submit callout', async (status, reason, key) => {
    vi.mocked(creationStudioApi.upload).mockRejectedValue({ status, reason })
    const { wrapper, t } = await render(); await uploadFile(wrapper)
    const uploadErrors = wrapper.findAll('.cs-field-error').filter(field => field.text().includes('qa.png'))
    expect(uploadErrors).toHaveLength(1)
    expect(uploadErrors[0].text()).toContain(t(`creationStudio.errors.${key}`, { max: '10', maxSide: 4096, maxPixels: 16777216, types: 'PNG / JPEG / WebP' }))
    expect(wrapper.find('.cs-form-footer .cs-callout').exists()).toBe(false)
    expect(wrapper.find('.cs-thumb').exists()).toBe(false)
    const error = uploadErrors[0]
    expect(wrapper.find('.cs-dropzone').exists()).toBe(true)
    const describedBy = wrapper.get('.cs-dropzone').attributes('aria-describedby')?.split(' ') ?? []
    expect(describedBy).toContain(error.attributes('id'))
  })

  it('upload 404 storage-disabled refreshes options and renders the unavailable upload field', async () => {
    vi.mocked(creationStudioApi.upload).mockRejectedValue({ status: 404, reason: 'CREATION_STORAGE_DISABLED' })
    const { wrapper } = await render()
    vi.mocked(creationStudioApi.getOptions).mockResolvedValue({ ...clone(optionsOpenAI), uploads: { available: false, reason: 'storage_disabled' } })
    await uploadFile(wrapper)
    expect(creationStudioApi.getOptions).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.cs-dropzone[data-state="unavailable"]').exists()).toBe(true)
  })

  it('submit storage-disabled keeps text video usable with a warning and unavailable upload state', async () => {
    const options = { ...clone(optionsVideo), models: [clone(optionsVideo.models[0])] }
    seedReusableTask(options.models[0], [snapshotImage('first_frame')])
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status: 404, reason: 'CREATION_STORAGE_DISABLED', message: 'QA storage disabled' })
    const { wrapper, t } = await render(options)
    await reuseTask(wrapper, t('creationStudio.action.reuse'))
    vi.mocked(creationStudioApi.getOptions).mockResolvedValue({ ...options, uploads: { available: false, reason: 'storage_disabled' } })
    await fillAndSubmit(wrapper)
    expect(wrapper.find('.cs-dropzone[data-state="unavailable"]').exists()).toBe(true)
    expect(wrapper.find('.cs-thumb').exists()).toBe(false)
    expect(wrapper.get('.cs-form-footer .cs-callout').classes()).toContain('cs-callout--warning')
    expect(wrapper.get('.cs-submit').attributes('disabled')).toBeUndefined()
  })

  it('submit 404 ASYNC_IMAGE_DISABLED shows only unavailable state, no generic invalid-request callout', async () => {
    vi.mocked(creationStudioApi.submit).mockRejectedValue({ status: 404, reason: 'ASYNC_IMAGE_DISABLED' })
    const { wrapper, t } = await render()
    vi.mocked(creationStudioApi.getOptions).mockResolvedValue({ ...clone(optionsOpenAI), image: { available: false, reason: 'storage_disabled' } })
    await fillAndSubmit(wrapper)
    expect(wrapper.text()).toContain(t('creationStudio.unavailable.imageStorage.title'))
    expect(wrapper.find('.cs-form-footer').exists()).toBe(false)
  })
})
