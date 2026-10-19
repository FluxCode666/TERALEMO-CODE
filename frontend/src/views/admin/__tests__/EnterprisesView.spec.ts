import { clickMenuItem } from '../../../features/enterprise/__tests__/selectOption'
import { selectOption } from '@/features/enterprise/__tests__/selectOption'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { Enterprise, EnterprisePage } from '@/types/enterprise'
import { defaultEnterpriseSettings } from '@/types/enterprise'
import EnterprisesView from '../EnterprisesView.vue'
import { formatDateTime } from '@/utils/format'

const { listEnterprises, removeEnterprise, showError, showSuccess } = vi.hoisted(() => ({
  listEnterprises: vi.fn(), removeEnterprise: vi.fn(), showError: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('@/api/admin/enterprises', () => ({ enterprisesAPI: { list: listEnterprises, remove: removeEnterprise } }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: vi.fn() } } }))
vi.mock('@/api/enterprise', () => ({ enterpriseErrorMessage: () => 'enterprise.errors.generic' }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const stubs = {
  AppLayout: { props: { compactPadding: Boolean }, template: '<main><slot /></main>' },
  TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  EnterpriseTable: {
    name: 'EnterpriseTable', props: ['data', 'loading'],
    template: '<div data-test="rows"><div v-for="row in data" :key="row.id"><span>{{ row.name }}</span><slot name="cell-status" :value="row.status" /><slot name="cell-actions" :row="row" /></div></div>'
  },
  EnterprisePagination: { name: 'EnterprisePagination', props: ['page', 'pageSize', 'total'], emits: ['update:page', 'update:pageSize'], template: '<div />' },
  EnterpriseConfirm: {
    props: ['show', 'message'], emits: ['confirm', 'cancel'],
    template: '<div v-if="show" data-test="delete-confirm"><p>{{ message }}</p><slot /><button data-test="confirm-delete" @click="$emit(\'confirm\')">Confirm</button><button data-test="cancel-delete" @click="$emit(\'cancel\')">Cancel</button></div>'
  },
  EnterpriseDialog: true,
  RouterLink: true
}

function enterprise(id: number): Enterprise {
  return {
    id, name: `Enterprise ${id}`, status: 'active', balance: 0, frozen_balance: 0, total_recharged: 0,
    low_balance_threshold: null, contact_name: '', contact_email: '', contact_phone: '', notes: '',
    settings: { ...defaultEnterpriseSettings }, member_count: 0, department_count: 0, created_at: '', updated_at: ''
  }
}
function page(id: number, total = 100): EnterprisePage<Enterprise> {
  return { items: [enterprise(id)], total, page: 1, page_size: 20, pages: Math.ceil(total / 20) }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const wrappers: VueWrapper[] = []
function mountList() {
  const wrapper = mount(EnterprisesView, { global: { stubs } })
  wrappers.push(wrapper)
  return wrapper
}
async function changePage(wrapper: VueWrapper, value: number) {
  wrapper.findComponent({ name: 'EnterprisePagination' }).vm.$emit('update:page', value)
  await flushPromises()
}
function expectPage(wrapper: VueWrapper, current: number, id: number, total: number) {
  expect(wrapper.get('[data-test="enterprise-status-badge"]').text()).toBe('enterprise.active')
  const pagination = wrapper.findComponent({ name: 'EnterprisePagination' })
  expect(pagination.props('page')).toBe(current)
  expect(pagination.props('total')).toBe(total)
  expect(wrapper.findComponent({ name: 'EnterpriseTable' }).props('data')).toEqual([enterprise(id)])
}

beforeEach(() => {
  vi.clearAllMocks()
  listEnterprises.mockReset().mockResolvedValue(page(1))
  removeEnterprise.mockReset().mockResolvedValue(undefined)
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount() })

describe('administrator enterprise latest reads', () => {
  it('uses full width and the same compact padding as enterprise details', async () => {
    const wrapper = mountList()
    await flushPromises()
    expect(wrapper.getComponent(stubs.AppLayout).props('compactPadding')).toBe(true)
    expect(wrapper.get('[data-layout="wide"]').classes()).toContain('max-w-full')
  })

  it('keeps page 3 rows and total when page 2 responds last', async () => {
    const wrapper = mountList()
    await flushPromises()
    const second = deferred<EnterprisePage<Enterprise>>(), third = deferred<EnterprisePage<Enterprise>>()
    listEnterprises.mockReturnValueOnce(second.promise).mockReturnValueOnce(third.promise)
    await changePage(wrapper, 2)
    const oldSignal = listEnterprises.mock.calls[1][1].signal as AbortSignal
    await changePage(wrapper, 3)
    expect(oldSignal.aborted).toBe(true)
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 3, page_size: 20 }, expect.objectContaining({ signal: expect.any(AbortSignal) }))

    third.resolve(page(3, 83))
    await flushPromises()
    second.resolve(page(2, 42))
    await flushPromises()
    expectPage(wrapper, 3, 3, 83)
    expect(showError).not.toHaveBeenCalled()
  })

  it('can supersede the initial load before it finishes', async () => {
    const initial = deferred<EnterprisePage<Enterprise>>()
    listEnterprises.mockReturnValueOnce(initial.promise)
    const wrapper = mountList()
    await flushPromises()
    const signal = listEnterprises.mock.calls[0][1].signal as AbortSignal
    await wrapper.get('input[aria-label="enterprise.search"]').setValue('Acme')
    await selectOption(wrapper, '[data-test="enterprise-status-filter"]', 'enterprise.disabled')
    expect(wrapper.get('form [data-test="enterprise-search"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 1, page_size: 20, search: 'Acme', status: 'disabled' }, expect.anything())
    initial.resolve(page(99))
    await flushPromises()
    expectPage(wrapper, 1, 1, 100)
  })

  it('submits every new search and status filter while reads are busy and ignores stale failures', async () => {
    const wrapper = mountList()
    await flushPromises()
    await changePage(wrapper, 4)
    const old = deferred<EnterprisePage<Enterprise>>(), latest = deferred<EnterprisePage<Enterprise>>()
    listEnterprises.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const search = wrapper.get('input[aria-label="enterprise.search"]')
    await search.setValue('old')
    await selectOption(wrapper, '[data-test="enterprise-status-filter"]', 'enterprise.active')
    await wrapper.get('form').trigger('submit')
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 1, page_size: 20, search: 'old', status: 'active' }, expect.anything())
    await search.setValue('latest')
    await selectOption(wrapper, '[data-test="enterprise-status-filter"]', 'enterprise.disabled')
    expect(wrapper.get('form [data-test="enterprise-search"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 1, page_size: 20, search: 'latest', status: 'disabled' }, expect.anything())
    latest.resolve(page(9, 25))
    await flushPromises()
    old.reject({ code: 'ENTERPRISE_NOT_FOUND' })
    await flushPromises()
    expectPage(wrapper, 1, 9, 25)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(showError).not.toHaveBeenCalled()
  })

  it('uses submitted filters for paging, page-size changes and retries rather than draft edits', async () => {
    const wrapper = mountList()
    await flushPromises()
    const search = wrapper.get('input[aria-label="enterprise.search"]')
    await search.setValue('applied')
    await selectOption(wrapper, '[data-test="enterprise-status-filter"]', 'enterprise.active')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await search.setValue('draft')
    await selectOption(wrapper, '[data-test="enterprise-status-filter"]', 'enterprise.disabled')
    await changePage(wrapper, 2)
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 2, page_size: 20, search: 'applied', status: 'active' }, expect.anything())

    const pending = deferred<EnterprisePage<Enterprise>>()
    listEnterprises.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(page(50))
    await changePage(wrapper, 3)
    const signal = listEnterprises.mock.calls.at(-1)![1].signal as AbortSignal
    wrapper.findComponent({ name: 'EnterprisePagination' }).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 1, page_size: 50, search: 'applied', status: 'active' }, expect.anything())
    pending.resolve(page(3))
    await flushPromises()
    expectPage(wrapper, 1, 50, 100)
    expect(wrapper.findComponent({ name: 'EnterprisePagination' }).props('pageSize')).toBe(50)

    listEnterprises.mockRejectedValueOnce(new Error('offline'))
    await changePage(wrapper, 2)
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 2, page_size: 50, search: 'applied', status: 'active' }, expect.anything())
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('aborts the current list request on unmount without displaying its eventual error', async () => {
    const pending = deferred<EnterprisePage<Enterprise>>()
    listEnterprises.mockReturnValueOnce(pending.promise)
    const wrapper = mountList()
    await flushPromises()
    const signal = listEnterprises.mock.calls[0][1].signal as AbortSignal
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    pending.reject(new Error('late failure'))
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
  })

  it('preserves dissolution confirmation and reloads the previous page with the applied filters after deleting its last row', async () => {
    const wrapper = mountList()
    await flushPromises()
    const search = wrapper.get('input[aria-label="enterprise.search"]')
    await search.setValue('applied')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    listEnterprises.mockResolvedValueOnce(page(7, 21))
    await changePage(wrapper, 2)
    await search.setValue('draft')
    removeEnterprise.mockRejectedValueOnce({ code: 'ENTERPRISE_NOT_EMPTY' })
    await clickMenuItem(wrapper, '[data-test="enterprise-menu-7"]', '[data-test="enterprise-delete-7"]')
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(removeEnterprise).toHaveBeenCalledTimes(1)
    expect(removeEnterprise).toHaveBeenLastCalledWith(7, undefined)
    expect(wrapper.text()).toContain('admin.enterprises.confirmDissolve')

    listEnterprises.mockResolvedValueOnce(page(8, 20))
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(removeEnterprise).toHaveBeenLastCalledWith(7, true)
    expect(listEnterprises).toHaveBeenLastCalledWith({ page: 1, page_size: 20, search: 'applied', status: undefined }, expect.anything())
    expectPage(wrapper, 1, 8, 20)
    expect(wrapper.find('[data-test="delete-confirm"]').exists()).toBe(false)
    expect(showSuccess).toHaveBeenCalledTimes(1)
  })
})

describe('administrator enterprise display', () => {
  it('only warns on rows whose active owner count is explicitly zero', async () => {
    listEnterprises.mockResolvedValue({
      items: [
        { ...enterprise(1), active_owner_count: 0 },
        { ...enterprise(2), active_owner_count: 1 },
        enterprise(3)
      ], total: 3, page: 1, page_size: 20, pages: 1
    })
    const wrapper = mount(EnterprisesView, { global: { stubs: { ...stubs, EnterpriseTable: false } } })
    wrappers.push(wrapper)
    await flushPromises()

    const warnings = wrapper.findAll('[data-test="enterprise-owner-warning"]')
    expect(warnings).toHaveLength(1)
    expect(warnings[0].text()).toContain('admin.enterprises.noActiveOwner')
    expect(warnings[0].text()).toContain('admin.enterprises.noActiveOwnerHint')
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('admin.enterprises.noActiveOwner')
    for (const row of rows.slice(1)) expect(row.find('[data-test="enterprise-owner-warning"]').exists()).toBe(false)
  })

  it('adds a frozen balance column and formats zero and nonzero holds independently from available funds', async () => {
    listEnterprises.mockResolvedValue({ ...page(1), items: [
      { ...enterprise(1), balance: 100, frozen_balance: 9.25 }, enterprise(2)
    ] })
    const wrapper = mount(EnterprisesView, { global: { stubs: { ...stubs, EnterpriseTable: false } } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.get('thead').text()).toContain('enterprise.frozenBalance')
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('$100.0000')
    expect(rows[0].get('[title="enterprise.frozenBalanceHint"]').text()).toBe('$9.2500')
    expect(rows[1].get('[title="enterprise.frozenBalanceHint"]').text()).toBe('$0.0000')
  })

  it('shows unified member concurrency and distinguishes unset or missing fields in the list', async () => {
    listEnterprises.mockResolvedValue({ ...page(1), items: [
      { ...enterprise(1), member_concurrency: 24 }, { ...enterprise(2), member_concurrency: null }, enterprise(3)
    ] })
    const wrapper = mount(EnterprisesView, { global: { stubs: { ...stubs, EnterpriseTable: false } } })
    wrappers.push(wrapper); await flushPromises()
    expect(wrapper.get('thead').text()).toContain('admin.enterprises.memberConcurrency.title')
    expect(wrapper.get('[data-test="enterprise-concurrency-1"]').text()).toBe('24')
    expect(wrapper.get('[data-test="enterprise-concurrency-2"]').text()).toBe('—')
    expect(wrapper.get('[data-test="enterprise-concurrency-3"]').text()).toBe('—')
  })

  it('formats enterprise creation timestamps with the shared date formatter', async () => {
    const timestamp = '2026-10-18T00:15:27.98669+08:00'
    listEnterprises.mockResolvedValue({ ...page(1), items: [{ ...enterprise(1), created_at: timestamp }] })
    const wrapper = mount(EnterprisesView, { global: { stubs: { ...stubs, EnterpriseTable: false } } })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.get('tbody').text()).toContain(formatDateTime(timestamp))
    expect(wrapper.get('tbody').text()).not.toContain(timestamp)
  })
})
