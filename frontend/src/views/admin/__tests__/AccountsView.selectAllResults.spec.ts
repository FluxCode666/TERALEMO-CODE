import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  batchRefresh,
  getBatchTodayStats,
  getUpstreamBillingProbeSettings,
  getAllProxies,
  getAllGroups,
  showError,
  showWarning
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  batchRefresh: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings,
      batchDelete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh,
      bulkUpdate: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    showWarning
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token'
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const makeAccounts = (count: number) => Array.from({ length: count }, (_, index) => ({
  id: index + 1,
  name: `account-${index + 1}`,
  platform: 'grok',
  type: 'oauth',
  status: 'active',
  schedulable: true,
  created_at: '2026-07-23T00:00:00Z',
  updated_at: '2026-07-23T00:00:00Z'
}))

const AccountBulkActionsBarStub = {
  props: ['selectedIds', 'totalResults', 'selectingAll', 'allResultsSelected'],
  emits: ['select-all-results', 'select-page', 'clear', 'refresh-token', 'batch-test'],
  template: `
    <div>
      <span data-test="selected-count">{{ selectedIds.length }}</span>
      <span data-test="total-results">{{ totalResults }}</span>
      <span data-test="all-results-selected">{{ String(allResultsSelected) }}</span>
      <button data-test="select-page" @click="$emit('select-page')">select page</button>
      <button data-test="select-all-results" @click="$emit('select-all-results')">select all</button>
      <button data-test="clear" @click="$emit('clear')">clear</button>
      <button data-test="refresh-token" @click="$emit('refresh-token')">refresh token</button>
      <button data-test="batch-test" @click="$emit('batch-test')">batch test</button>
    </div>
  `
}

const AccountTableFiltersStub = {
  emits: ['change'],
  template: '<button data-test="change-filter" @click="$emit(\'change\')">change filter</button>'
}

const mountView = () => mount(AccountsView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: {
        template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
      },
      DataTable: {
        props: ['data'],
        template: '<div data-test="data-table"><div v-for="row in data" :key="row.id"><slot name="cell-select" :row="row" /></div></div>'
      },
      Pagination: true,
      ConfirmDialog: true,
      AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
      AccountTableFilters: AccountTableFiltersStub,
      AccountBulkActionsBar: AccountBulkActionsBarStub,
      AccountActionMenu: true,
      ImportDataModal: true,
      ReAuthAccountModal: true,
      AccountTestModal: true,
      AccountBatchTestModal: { props: ['show', 'accountIds'], template: '<div data-test="batch-modal" :data-show="String(show)" :data-ids="accountIds.join(\',\')"></div>' },
      AccountStatsModal: true,
      ScheduledTestsPanel: true,
      SyncFromCrsModal: true,
      TempUnschedStatusModal: true,
      ErrorPassthroughRulesModal: true,
      TLSFingerprintProfilesModal: true,
      CreateAccountModal: true,
      EditAccountModal: true,
      BulkEditAccountModal: true,
      PlatformTypeBadge: true,
      AccountCapacityCell: true,
      AccountStatusIndicator: true,
      AccountTodayStatsCell: true,
      AccountGroupsCell: true,
      AccountUsageCell: true,
      Icon: true
    }
  }
})

describe('admin AccountsView select all filtered results', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset()
    listWithEtag.mockReset()
    batchRefresh.mockReset()
    getBatchTodayStats.mockReset()
    getUpstreamBillingProbeSettings.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()
    showError.mockReset()
    showWarning.mockReset()

    listWithEtag.mockResolvedValue({
      notModified: true,
      etag: null,
      data: null
    })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getUpstreamBillingProbeSettings.mockResolvedValue({ enabled: true, interval_minutes: 30 })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it.each([
    { name: 'keeps only failed accounts selected', result: { total: 3, success: 2, failed: 1, errors: [{ account_id: 2, error: 'no refresh token available' }] }, expectedIds: [2] },
    { name: 'clears the selection after every account succeeds', result: { total: 3, success: 3, failed: 0 }, expectedIds: [] },
    { name: 'keeps the original selection when failure details are missing', result: { total: 3, success: 2, failed: 1 }, expectedIds: [1, 2, 3] },
  ])('$name after a batch token refresh and table reload', async ({ result, expectedIds }) => {
    listAccounts.mockResolvedValue({ items: makeAccounts(3), total: 3, page: 1, page_size: 20, pages: 1 })
    batchRefresh.mockResolvedValue(result)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="select-page"]').trigger('click')
    await wrapper.get('[data-test="refresh-token"]').trigger('click')
    await flushPromises()

    expect(batchRefresh).toHaveBeenCalledWith([1, 2, 3])
    expect(listAccounts).toHaveBeenCalledTimes(2)
    expect(wrapper.getComponent(AccountBulkActionsBarStub).props('selectedIds')).toEqual(expectedIds)
    expect(wrapper.findAll<HTMLInputElement>('[data-test="data-table"] input').map(input => input.element.checked))
      .toEqual([1, 2, 3].map(id => expectedIds.includes(id)))
    if (result.failed > 0) {
      expect(showError).toHaveBeenCalledWith('admin.accounts.bulkActions.partialSuccess')
      await wrapper.get('[data-test="refresh-token"]').trigger('click')
      await flushPromises()
      expect(batchRefresh).toHaveBeenLastCalledWith(expectedIds)
    }
    wrapper.unmount()
  })

  it('selects all matching IDs in one commit and clears the selection when filters change', async () => {
    const allAccounts = makeAccounts(45)
    listAccounts.mockImplementation(async (_page: number, pageSize: number) => {
      if (pageSize === 1000) {
        return {
          items: allAccounts,
          total: 45,
          page: 1,
          page_size: 1000,
          pages: 1
        }
      }
      return {
        items: allAccounts.slice(0, 20),
        total: 45,
        page: 1,
        page_size: 20,
        pages: 3
      }
    })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="select-all-results"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="selected-count"]').text()).toBe('45')
    expect(wrapper.get('[data-test="total-results"]').text()).toBe('45')
    expect(wrapper.get('[data-test="all-results-selected"]').text()).toBe('true')
    expect(listAccounts).toHaveBeenCalledWith(1, 1000, expect.objectContaining({
      lite: '1',
      include_scheduler_score: '0'
    }))

    await wrapper.get('[data-test="change-filter"]').trigger('click')

    expect(wrapper.get('[data-test="selected-count"]').text()).toBe('0')
    expect(wrapper.get('[data-test="all-results-selected"]').text()).toBe('false')
  })

  it('keeps the original page selection when loading all results fails', async () => {
    const currentPage = makeAccounts(20)
    listAccounts.mockImplementation(async (_page: number, pageSize: number) => {
      if (pageSize === 1000) {
        throw new Error('load all failed')
      }
      return {
        items: currentPage,
        total: 45,
        page: 1,
        page_size: 20,
        pages: 3
      }
    })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="select-page"]').trigger('click')
    expect(wrapper.get('[data-test="selected-count"]').text()).toBe('20')

    await wrapper.get('[data-test="select-all-results"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="selected-count"]').text()).toBe('20')
    expect(wrapper.get('[data-test="all-results-selected"]').text()).toBe('false')
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkActions.selectAllFailed')
  })

  describe('batch test with image accounts', () => {
    const mixed = () => [
      { ...makeAccounts(1)[0], id: 1, platform: 'gemini' },
      { ...makeAccounts(1)[0], id: 2, platform: 'image' },
      { ...makeAccounts(1)[0], id: 3, platform: 'gemini' },
      { ...makeAccounts(1)[0], id: 4, platform: 'image' }
    ]

    it('excludes image accounts from a mixed selection and reports how many were skipped', async () => {
      listAccounts.mockResolvedValue({ items: mixed(), total: 4, page: 1, page_size: 20, pages: 1 })
      const wrapper = mountView()
      await flushPromises()
      await wrapper.get('[data-test="select-page"]').trigger('click')
      await wrapper.get('[data-test="batch-test"]').trigger('click')
      await flushPromises()
      const modal = wrapper.get('[data-test="batch-modal"]')
      expect(modal.attributes('data-show')).toBe('true')
      expect(modal.attributes('data-ids')).toBe('1,3')
      expect(showWarning).toHaveBeenCalledWith('admin.accounts.batchTest.skippedImageAccounts')
      wrapper.unmount()
    })

    it('blocks the batch test when every selected account is an image account', async () => {
      listAccounts.mockResolvedValue({ items: mixed().filter((a) => a.platform === 'image'), total: 2, page: 1, page_size: 20, pages: 1 })
      const wrapper = mountView()
      await flushPromises()
      await wrapper.get('[data-test="select-page"]').trigger('click')
      await wrapper.get('[data-test="batch-test"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-test="batch-modal"]').attributes('data-show')).toBe('false')
      expect(showWarning).toHaveBeenCalledWith('admin.accounts.imageTestUnsupported')
      wrapper.unmount()
    })

    it('excludes image accounts that are only on other pages after selecting all results', async () => {
      const all = Array.from({ length: 45 }, (_, i) => ({
        ...makeAccounts(1)[0], id: i + 1, platform: i + 1 > 30 && i + 1 <= 33 ? 'image' : 'gemini'
      }))
      listAccounts.mockImplementation(async (_page: number, pageSize: number) => pageSize === 1000
        ? { items: all, total: 45, page: 1, page_size: 1000, pages: 1 }
        : { items: all.slice(0, 20), total: 45, page: 1, page_size: 20, pages: 3 })
      const wrapper = mountView()
      await flushPromises()
      // 当前页（1-20）没有 image，31-33 在页外
      await wrapper.get('[data-test="select-all-results"]').trigger('click')
      await flushPromises()
      await wrapper.get('[data-test="batch-test"]').trigger('click')
      await flushPromises()
      const ids = wrapper.get('[data-test="batch-modal"]').attributes('data-ids')!.split(',').map(Number)
      expect(ids).toHaveLength(42)
      expect([31, 32, 33].some((id) => ids.includes(id))).toBe(false)
      expect(showWarning).toHaveBeenCalledWith('admin.accounts.batchTest.skippedImageAccounts')
      wrapper.unmount()
    })

    it('blocks the batch test when all results selected across pages are image accounts', async () => {
      const all = Array.from({ length: 25 }, (_, i) => ({ ...makeAccounts(1)[0], id: i + 1, platform: 'image' }))
      listAccounts.mockImplementation(async (_page: number, pageSize: number) => pageSize === 1000
        ? { items: all, total: 25, page: 1, page_size: 1000, pages: 1 }
        : { items: all.slice(0, 20), total: 25, page: 1, page_size: 20, pages: 2 })
      const wrapper = mountView()
      await flushPromises()
      await wrapper.get('[data-test="select-all-results"]').trigger('click')
      await flushPromises()
      await wrapper.get('[data-test="batch-test"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-test="batch-modal"]').attributes('data-show')).toBe('false')
      expect(showWarning).toHaveBeenCalledWith('admin.accounts.imageTestUnsupported')
      wrapper.unmount()
    })

    it('does not warn when no image account is selected', async () => {
      listAccounts.mockResolvedValue({ items: makeAccounts(2), total: 2, page: 1, page_size: 20, pages: 1 })
      const wrapper = mountView()
      await flushPromises()
      await wrapper.get('[data-test="select-page"]').trigger('click')
      await wrapper.get('[data-test="batch-test"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-test="batch-modal"]').attributes('data-ids')).toBe('1,2')
      expect(showWarning).not.toHaveBeenCalled()
      wrapper.unmount()
    })
  })
})
