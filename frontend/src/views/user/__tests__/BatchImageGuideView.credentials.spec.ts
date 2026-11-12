import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import BatchImageGuideView from '../BatchImageGuideView.vue'

const mocks = vi.hoisted(() => ({
  listKeys: vi.fn(),
  listModels: vi.fn(),
  listJobs: vi.fn(),
  fetchPublicSettings: vi.fn(),
}))

vi.mock('@/api', () => ({ keysAPI: { list: mocks.listKeys } }))
vi.mock('@/api/batchImage', () => ({
  listBatchImageModels: mocks.listModels,
  listBatchImageJobs: mocks.listJobs,
  cancelBatchImageJob: vi.fn(),
  deleteBatchImageJobRecord: vi.fn(),
  downloadBatchImageZip: vi.fn(),
  getBatchImageItemContent: vi.fn(),
  getBatchImageJob: vi.fn(),
  listBatchImageItems: vi.fn(),
  saveBlob: vi.fn(),
  submitBatchImageJob: vi.fn(),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ fetchPublicSettings: mocks.fetchPublicSettings, showSuccess: vi.fn(), showError: vi.fn() }),
}))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, locale: ref('en') }),
}))

function render() {
  return mount(BatchImageGuideView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        DataTable: true,
        Select: true,
        SearchInput: true,
        Icon: true,
        'i18n-t': true,
      },
    },
  })
}

describe('Batch image saved credentials', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    sessionStorage.clear()
    mocks.listKeys.mockResolvedValue({ items: [{ id: 7, name: 'Batch key', key: 'sk-test...abcd', key_hint: 'sk-test...abcd', status: 'active', group: { platform: 'gemini', allow_batch_image_generation: true } }] })
    mocks.listModels.mockResolvedValue({ data: [{ id: 'image-model' }] })
    mocks.listJobs.mockResolvedValue({ data: [], has_more: false })
  })

  it('uses only a manually supplied credential and forgets it when leaving the page', async () => {
    const wrapper = render()
    await flushPromises()
    expect(mocks.listModels).not.toHaveBeenCalled()
    expect(mocks.listJobs).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="batch-saved-key"]').setValue('sk-manually-saved-batch-key')
    await wrapper.findAll('button').find(button => button.text() === 'batchImage.actions.useSavedKey')!.trigger('click')
    await flushPromises()
    expect(mocks.listModels).toHaveBeenCalledWith('sk-manually-saved-batch-key')
    expect(mocks.listJobs).toHaveBeenCalledWith('sk-manually-saved-batch-key', expect.any(Object))
    expect(JSON.stringify(localStorage)).not.toContain('sk-manually-saved-batch-key')
    expect(JSON.stringify(sessionStorage)).not.toContain('sk-manually-saved-batch-key')
    expect((wrapper.get('[data-testid="batch-saved-key"]').element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
    mocks.listModels.mockClear()
    mocks.listJobs.mockClear()
    const reopened = render()
    await flushPromises()
    expect(mocks.listModels).not.toHaveBeenCalled()
    expect(mocks.listJobs).not.toHaveBeenCalled()
    reopened.unmount()
  })
})
