import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EnterpriseQuotaDisplay from '../components/EnterpriseQuotaDisplay.vue'
import EnterpriseQuotaPanel from '../components/EnterpriseQuotaPanel.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseOverview, EnterpriseQuota } from '@/types/enterprise'

const { showError } = vi.hoisted(() => ({ showError: vi.fn() }))
vi.mock('@/api/enterprise', () => ({ enterpriseErrorMessage: () => 'enterprise.errors.generic' }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

function quotaWithoutUsage(): EnterpriseQuota {
  return {
    scope_type: 'member', scope_id: 7,
    daily_limit_usd: 5, weekly_limit_usd: null, monthly_limit_usd: 20, total_limit_usd: null,
    daily_usage_usd: null, weekly_usage_usd: null, monthly_usage_usd: null, total_usage_usd: null,
    daily_resets_at: null, weekly_resets_at: null, monthly_resets_at: null
  }
}

const stubs = {
  EnterpriseTable: {
    props: ['data', 'columns'],
    template: '<div><div v-for="row in data" :key="`${row.scope_type}-${row.scope_id}`"><div v-for="column in columns" :key="column.key"><slot :name="`cell-${column.key}`" :row="row" /></div></div></div>'
  },
  EnterpriseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
  EnterpriseConfirm: true
}

describe('enterprise quota limits and editing permissions', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders only limits in all four windows when usage is unavailable', () => {
    const wrapper = mount(EnterpriseQuotaDisplay, { props: { title: 'Quota', quota: quotaWithoutUsage() } })
    expect(wrapper.findAll('dd').map(cell => cell.text())).toEqual([
      '$5', 'enterprise.unlimited', '$20', 'enterprise.unlimited'
    ])
    expect(wrapper.text()).not.toContain('$0.0000')
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('never renders recorded spending even when usage values are provided', async () => {
    const quota = { ...quotaWithoutUsage(), daily_usage_usd: 1.2345, weekly_usage_usd: 9.8765 }
    const wrapper = mount(EnterpriseQuotaDisplay, { props: { title: 'Quota', quota } })
    expect(wrapper.findAll('dd').map(cell => cell.text())).toEqual(['$5', 'enterprise.unlimited', '$20', 'enterprise.unlimited'])
    expect(wrapper.text()).not.toContain('1.2345')
    expect(wrapper.text()).not.toContain('9.8765')
    await wrapper.setProps({ quota: { ...quota, daily_usage_usd: 0 } })
    expect(wrapper.findAll('dd')[0]?.text()).toBe('$5')
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('lets an authorized department manager edit a member quota while its usage is hidden', async () => {
    const row = quotaWithoutUsage()
    const updateQuota = vi.fn().mockResolvedValue(row)
    const members = vi.fn().mockResolvedValue({ items: [{ id: 7, department_id: 3, role: 'member', username: 'Alice' }], total: 1 })
    const api = {
      quotas: vi.fn().mockResolvedValue([row]), departments: vi.fn().mockResolvedValue([]), members, updateQuota
    } as unknown as EnterpriseResourceAPI
    const overview = {
      me: { role: 'manager', department_id: 3 },
      permissions: { edit_member_quota: true, edit_quotas: false, view_members: false }
    } as EnterpriseOverview
    const wrapper = mount(EnterpriseQuotaPanel, { props: { api, overview }, global: { stubs } })
    await flushPromises()
    expect(members).toHaveBeenCalled()
    expect(wrapper.findAll('[data-test^="quota-cell-"]').map(cell => cell.text())).toEqual(Array(4).fill('enterprise.quotaTable.hidden'))
    expect(wrapper.find('[data-test="quota-menu-member-7"]').exists()).toBe(false)
    await wrapper.get('[data-test="quota-edit-member-7"]').trigger('click')
    await wrapper.get('[data-test="quota-daily"]').setValue('8')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateQuota).toHaveBeenCalledWith('member', 7, { daily_limit_usd: 8, weekly_limit_usd: null, monthly_limit_usd: 20, total_limit_usd: null })
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(wrapper.findAll('[data-test^="quota-cell-"]').map(cell => cell.text())).toEqual(Array(4).fill('enterprise.quotaTable.hidden'))
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps quotas read-only when the manager has no editing permission', async () => {
    const api = { quotas: vi.fn().mockResolvedValue([quotaWithoutUsage()]), departments: vi.fn().mockResolvedValue([]) } as unknown as EnterpriseResourceAPI
    const overview = { me: { role: 'manager', department_id: 3 }, permissions: { edit_member_quota: false, edit_quotas: false, view_members: false } } as EnterpriseOverview
    const wrapper = mount(EnterpriseQuotaPanel, { props: { api, overview }, global: { stubs } })
    await flushPromises()
    expect(wrapper.find('[data-test="quota-edit-member-7"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('$0.0000')
    wrapper.unmount()
  })

  it('reloads quota rows when the workspace refreshes the current tab', async () => {
    const quotas = vi.fn().mockResolvedValueOnce([quotaWithoutUsage()]).mockResolvedValueOnce([{ ...quotaWithoutUsage(), daily_limit_usd: 9 }])
    const api = { quotas, departments: vi.fn().mockResolvedValue([]) } as unknown as EnterpriseResourceAPI
    const wrapper = mount(EnterpriseQuotaPanel, { props: { api, refreshKey: 0 }, global: { stubs } })
    await flushPromises()
    expect(wrapper.get('[data-test="quota-section-member"]').getComponent({ name: 'EnterpriseQuotaTable' }).props('rows')[0].quota.daily_limit_usd).toBe(5)
    await wrapper.setProps({ refreshKey: 1 })
    await flushPromises()
    expect(quotas).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="quota-section-member"]').getComponent({ name: 'EnterpriseQuotaTable' }).props('rows')[0].quota.daily_limit_usd).toBe(9)
    wrapper.unmount()
  })
})
