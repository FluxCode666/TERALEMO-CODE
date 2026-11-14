import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import BatchImageGuideView from '../BatchImageGuideView.vue'

const mocks = vi.hoisted(() => ({
  listKeys: vi.fn(),
  listModels: vi.fn(),
  listJobs: vi.fn(),
  fetchPublicSettings: vi.fn(),
  getJob: vi.fn(),
  listItems: vi.fn(),
  submit: vi.fn(),
  download: vi.fn(),
  deleteJob: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api', () => ({ keysAPI: { list: mocks.listKeys } }))
vi.mock('@/api/batchImage', () => ({
  listBatchImageModels: mocks.listModels,
  listBatchImageJobs: mocks.listJobs,
  cancelBatchImageJob: vi.fn(),
  deleteBatchImageJobRecord: mocks.deleteJob,
  downloadBatchImageZip: mocks.download,
  getBatchImageItemContent: vi.fn(),
  getBatchImageJob: mocks.getJob,
  listBatchImageItems: mocks.listItems,
  saveBlob: vi.fn(),
  submitBatchImageJob: mocks.submit,
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ fetchPublicSettings: mocks.fetchPublicSettings, showSuccess: vi.fn(), showError: mocks.showError }),
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
        DataTable: {
          props: ['data'],
          template: '<div><div v-for="row in data" :key="row.id" :data-job="row.id"><slot name="cell-id" :row="row" /><slot name="cell-actions" :row="row" /></div></div>',
        },
        Select: true,
        SearchInput: true,
        Icon: true,
        'i18n-t': true,
        Teleport: true,
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
    mocks.listItems.mockResolvedValue({ data: [], has_more: false })
  })

  afterEach(() => { vi.restoreAllMocks() })

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

  it('uses the owning saved credential across two keys, details, retry, download and delete', async () => {
    const credentialA = 'saved-credential-a-0123456789abcdef'
    const credentialB = 'saved-credential-b-0123456789abcdef'
    const jobA = {
      id: 'batch-a', task_name: 'Task A', status: 'completed', model: 'image-model', provider: 'gemini_api',
      item_count: 2, success_count: 1, fail_count: 1, estimated_cost: 1, hold_amount: 0, actual_cost: 1, created_at: 1,
    }
    const jobB = { ...jobA, id: 'batch-b', task_name: 'Task B', created_at: 2 }
    mocks.listKeys.mockResolvedValue({ items: [7, 8].map(id => ({
      id, name: `Key ${id}`, key: 'sk-...abcd', key_hint: 'sk-...abcd', status: 'active',
      group: { platform: 'gemini', allow_batch_image_generation: true },
    })) })
    mocks.listJobs.mockImplementation(async credential => ({ data: [credential === credentialA ? jobA : jobB], has_more: false }))
    mocks.getJob.mockImplementation(async (_credential, id) => id === jobA.id ? jobA : jobB)
    mocks.listItems.mockResolvedValue({ data: [{ custom_id: 'failed-item', status: 'failed', prompt_preview: 'Retry prompt' }], has_more: false })
    mocks.submit.mockResolvedValue({ ...jobA, id: 'batch-a-retry', parent_batch_id: jobA.id, status: 'queued' })
    mocks.download.mockResolvedValue(new Blob(['zip']))
    mocks.deleteJob.mockResolvedValue(undefined)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const intervals = vi.spyOn(globalThis, 'setInterval')
    const clearIntervals = vi.spyOn(globalThis, 'clearInterval')
    const wrapper = render()
    await flushPromises()
    const save = async (id: string, credential: string) => {
      await wrapper.get('select[aria-label="batchImage.columns.apiKey"]').setValue(id)
      await wrapper.get('[data-testid="batch-saved-key"]').setValue(credential)
      await wrapper.findAll('button').find(button => button.text() === 'batchImage.actions.useSavedKey')!.trigger('click')
      await flushPromises()
    }
    await save('7', credentialA)
    await save('8', credentialB)
    expect(mocks.listJobs).toHaveBeenCalledWith(credentialA, expect.any(Object))
    expect(mocks.listJobs).toHaveBeenCalledWith(credentialB, expect.any(Object))

    await wrapper.get('[data-job="batch-a"] button[title="batchImage.actions.viewDetail"]').trigger('click')
    await flushPromises()
    expect(mocks.getJob).toHaveBeenLastCalledWith(credentialA, 'batch-a')
    expect(mocks.listItems).toHaveBeenLastCalledWith(credentialA, 'batch-a')
    await wrapper.get('[data-job="batch-b"] button[title="batchImage.actions.viewDetail"]').trigger('click')
    await flushPromises()
    expect(mocks.getJob).toHaveBeenLastCalledWith(credentialB, 'batch-b')
    expect(mocks.listItems).toHaveBeenLastCalledWith(credentialB, 'batch-b')

    // With B selected, A's row actions must still use A's credential.
    await wrapper.get('[data-job="batch-a"] button[title="batchImage.actions.downloadZip"]').trigger('click')
    await flushPromises()
    expect(mocks.download).toHaveBeenLastCalledWith(credentialA, 'batch-a')
    await wrapper.get('[data-job="batch-a"] button[title="batchImage.actions.moreActions"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'batchImage.actions.retryFailedItems')!.trigger('click')
    await flushPromises()
    expect(mocks.submit).toHaveBeenLastCalledWith(credentialA, expect.objectContaining({ parent_batch_id: 'batch-a' }), expect.any(String))

    await wrapper.get('[data-job="batch-b"] button[title="batchImage.actions.moreActions"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'batchImage.actions.deleteRecords')!.trigger('click')
    await flushPromises()
    expect(mocks.deleteJob).toHaveBeenLastCalledWith(credentialB, 'batch-b')
    const timerIDs = intervals.mock.results.map(result => result.value)
    wrapper.unmount()
    for (const timer of timerIDs) expect(clearIntervals).toHaveBeenCalledWith(timer)
    expect(mocks.showError).not.toHaveBeenCalled()
  })
})
