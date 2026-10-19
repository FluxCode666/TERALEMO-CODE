import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseUsageLog } from '@/types/enterprise'
import Panel from '../components/EnterpriseUsagePanel.vue'
import Details from '../components/EnterpriseUsageDetails.vue'
import Pagination from '@/components/common/Pagination.vue'
import { enterpriseUsageExportLimit } from '../usageCsv'
const mocks = vi.hoisted(() => ({ showError: vi.fn(), showWarning: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn().mockResolvedValue({ data: [] }) } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh' } }) }))
const wrappers: VueWrapper[] = []
let createUrl = vi.fn(), revokeUrl = vi.fn(), download = vi.fn()
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear()
  vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date('2026-10-18T04:30:00Z'))
  createUrl = vi.fn(() => 'blob:csv'); revokeUrl = vi.fn()
  vi.stubGlobal('URL', class extends URL { static createObjectURL = createUrl; static revokeObjectURL = revokeUrl })
  download = vi.fn()
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { download(this.download) })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
function row(id = 1): EnterpriseUsageLog { return { id, model: 'gpt', created_at: '2026-10-18T04:00:00Z', actual_cost: 0.123456789012345 } }
async function setup(admin = false) {
  const api = {
    usage: vi.fn().mockResolvedValue({ items: [row()], total: 1001 }),
    departments: vi.fn().mockResolvedValue([]), members: vi.fn().mockResolvedValue({ items: [], total: 0 }),
    usageStats: vi.fn().mockResolvedValue({ total_requests: 1001, total_tokens: 10, total_actual_cost: 1 }),
    usageTrend: vi.fn().mockResolvedValue([]), usageModels: vi.fn().mockResolvedValue([]), usageMembers: vi.fn().mockResolvedValue([]), usageDepartments: vi.fn().mockResolvedValue([])
  }
  const wrapper = mount(Panel, { props: { api: api as unknown as EnterpriseResourceAPI, admin, enterpriseName: 'Acme/研发' }, global: { stubs: { UsageTable: true, Pagination: true, EnterpriseUsageTrend: true, EnterpriseUsageDistribution: true, EnterpriseUsageRanking: true } } })
  wrappers.push(wrapper); await flushPromises()
  return { api, wrapper }
}
async function blobText() {
  const reader = new FileReader()
  const content = new Promise<string>(resolve => { reader.onload = () => resolve(new TextDecoder('utf-8', { ignoreBOM: true }).decode(reader.result as ArrayBuffer)) })
  reader.readAsArrayBuffer(createUrl.mock.calls[0]![0] as Blob)
  return content
}
async function clickExport(wrapper: VueWrapper) { await wrapper.get('[data-test="usage-export"]').trigger('click'); await flushPromises() }
describe('企业 CSV 查询快照与请求生命周期', () => {
  it.each([false, true])('导出全部分页和当前排序，使用已提交滚动窗口与列显隐，admin=%s', async admin => {
    localStorage.setItem(`${admin ? 'admin-enterprise' : 'enterprise'}-usage-hidden-columns`, JSON.stringify(['model', 'tokens']))
    const { wrapper, api } = await setup(admin)
    wrapper.getComponent('[data-test="usage-model"]').vm.$emit('update:modelValue', 'submitted')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const submitted = { ...api.usage.mock.calls.at(-1)![0] }
    vi.setSystemTime(new Date('2026-10-19T04:30:00Z'))
    wrapper.getComponent('[data-test="usage-model"]').vm.$emit('update:modelValue', 'draft')
    wrapper.getComponent(Details).vm.$emit('sort', 'model', 'asc'); await flushPromises()
    wrapper.getComponent(Pagination).vm.$emit('update:page', 7); await flushPromises()
    api.usage.mockClear()
    api.usage.mockResolvedValueOnce({ items: Array.from({ length: 1000 }, (_, index) => row(index)), total: 1001 }).mockResolvedValueOnce({ items: [row(1000)], total: 1001 })
    await clickExport(wrapper)
    expect(api.usage).toHaveBeenCalledTimes(2)
    for (const [index, call] of api.usage.mock.calls.entries()) {
      expect(call[0]).toEqual({ ...submitted, sort_by: 'model', sort_order: 'asc', page: index + 1, page_size: 1000 })
      expect(call[1].signal).toBeInstanceOf(AbortSignal)
    }
    const text = await blobText()
    expect(text.charCodeAt(0)).toBe(0xfeff)
    expect(text.split('\r\n')).toHaveLength(1002)
    expect(text.split('\r\n')[0]).not.toContain('usage.model')
    expect(text.split('\r\n')[0]).not.toContain('admin.usage.inputTokens')
    expect(text).toContain('0.123456789012345')
    expect(download).toHaveBeenCalledWith(expect.stringMatching(/^Acme_研发_.*2026-10-18.*\.csv$/))
    expect(revokeUrl).toHaveBeenCalledWith('blob:csv')
    expect(mocks.showSuccess).toHaveBeenCalled()
  })
  it('无数据、超上限、API 失败或分页中途失败时给出提示且不下载部分结果', async () => {
    const { wrapper, api } = await setup()
    for (const total of [0, enterpriseUsageExportLimit + 1]) {
      api.usage.mockResolvedValueOnce({ items: [], total })
      await clickExport(wrapper)
      expect(mocks.showWarning).toHaveBeenLastCalledWith(`enterprise.usageAnalysis.${total ? 'exportLimit' : 'exportEmpty'}`)
    }
    api.usage.mockRejectedValueOnce(new Error('Unavailable'))
    await clickExport(wrapper)
    expect(mocks.showError).toHaveBeenCalledWith(expect.stringContaining('enterprise.usageAnalysis.exportFailed'))
    api.usage.mockResolvedValueOnce({ items: [row()], total: 2 }).mockRejectedValueOnce(new Error('Page 2 failed'))
    await clickExport(wrapper)
    expect(mocks.showError).toHaveBeenCalledTimes(2)
    expect(createUrl).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="usage-export"]').attributes('disabled')).toBeUndefined()
  })
  it('分页总数发生变化或返回缺失页时停止，提示重新查询', async () => {
    const { wrapper, api } = await setup()
    for (const second of [{ items: [row()], total: 3 }, { items: [], total: 2 }]) {
      api.usage.mockResolvedValueOnce({ items: [row()], total: 2 }).mockResolvedValueOnce(second)
      await clickExport(wrapper)
      expect(mocks.showWarning).toHaveBeenLastCalledWith('enterprise.usageAnalysis.exportIncomplete')
    }
    expect(createUrl).not.toHaveBeenCalled()
  })
  it.each(['unmount', 'search', 'sort', 'refresh', 'api'])('%s 取消导出并忽略迟到响应；进行中防重入', async reason => {
    const { wrapper, api } = await setup()
    let resolve!: (value: unknown) => void
    api.usage.mockClear().mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    await wrapper.get('[data-test="usage-export"]').trigger('click')
    const signal = api.usage.mock.calls[0]![1].signal as AbortSignal
    expect(wrapper.get('[data-test="usage-export"]').attributes('aria-busy')).toBe('true')
    expect(wrapper.get('[data-test="usage-export"]').text()).toContain('common.exporting')
    await wrapper.get('[data-test="usage-export"]').trigger('click')
    expect(api.usage).toHaveBeenCalledTimes(1)
    if (reason === 'unmount') wrapper.unmount()
    if (reason === 'search') await wrapper.get('form').trigger('submit')
    if (reason === 'sort') wrapper.getComponent(Details).vm.$emit('sort', 'model', 'asc')
    if (reason === 'refresh') await wrapper.setProps({ refreshKey: 1 })
    if (reason === 'api') await wrapper.setProps({ api: { ...api } as unknown as EnterpriseResourceAPI })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    resolve({ items: [row()], total: 1 }); await flushPromises()
    expect(createUrl).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
  })
})
