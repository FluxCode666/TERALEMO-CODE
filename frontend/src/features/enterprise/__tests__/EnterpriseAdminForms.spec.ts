import { clickMenuItem } from './selectOption'
import { selectOption } from './selectOption'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EnterprisesView from '@/views/admin/EnterprisesView.vue'
import EnterpriseQuotaPanel from '../components/EnterpriseQuotaPanel.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseQuota } from '@/types/enterprise'
import { defaultEnterpriseSettings } from '@/types/enterprise'

const { listUsers, createEnterprise, listEnterprises, updateEnterprise, adjustBalance, showError } = vi.hoisted(() => ({ listUsers: vi.fn(), createEnterprise: vi.fn(), listEnterprises: vi.fn(), updateEnterprise: vi.fn(), adjustBalance: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: listUsers } } }))
vi.mock('@/api/admin/enterprises', () => ({ enterprisesAPI: { list: listEnterprises, create: createEnterprise, update: updateEnterprise, adjustBalance } }))
vi.mock('@/api/enterprise', () => ({ enterpriseErrorMessage: () => 'enterprise.errors.generic' }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const dialog = { name: 'EnterpriseDialog', props: ['show'], template: '<div v-if="show"><slot /></div>' }
const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  EnterpriseDialog: dialog,
  EnterpriseConfirm: { name: 'EnterpriseConfirm', props: ['show'], emits: ['confirm'], template: '<div v-if="show"><slot /><button data-test="confirm-reset" @click="$emit(\'confirm\')">Confirm</button></div>' },
  EnterpriseTable: { props: ['data'], template: '<div><div v-for="row in data" :key="row.scope_id"><slot name="cell-actions" :row="row" /></div></div>' },
  EnterprisePagination: true,
  RouterLink: true
}

describe('Enterprise administrator creation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listEnterprises.mockResolvedValue({ items: [], total: 0 })
    createEnterprise.mockResolvedValue({ id: 1 })
  })

  it('creates an enterprise without an initial owner when no owner is selected', async () => {
    const wrapper = mount(EnterprisesView, { global: { stubs } })
    await flushPromises()
    expect(wrapper.get('[data-layout="wide"]').classes()).toContain('max-w-full')
    await wrapper.get('[data-test="enterprise-create-open"]').trigger('click')
    await wrapper.get('[data-test="enterprise-name"]').setValue('Acme')
    await wrapper.get('[data-test="enterprise-create-form"]').trigger('submit')
    await flushPromises()
    expect(createEnterprise).toHaveBeenCalledWith(expect.objectContaining({ name: 'Acme', low_balance_threshold: null }))
    expect(createEnterprise.mock.calls[0][0]).not.toHaveProperty('owner')
    expect(wrapper.find('[data-test="enterprise-create-form"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('supports searching and selecting an existing initial owner', async () => {
    listUsers.mockResolvedValue({ items: [{ id: 42, username: 'Alice', email: 'alice@example.com', enterprise: null }] })
    const wrapper = mount(EnterprisesView, { global: { stubs } })
    await flushPromises()
    await wrapper.get('[data-test="enterprise-create-open"]').trigger('click')
    await wrapper.get('[data-test="enterprise-name"]').setValue('Acme')
    await wrapper.get('button[role="radio"][value="existing"]').trigger('click')
    await wrapper.get('[data-test="enterprise-create-form"]').trigger('submit')
    expect(createEnterprise).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('enterprise.validationUser')
    const searchInput = wrapper.get('[placeholder="enterprise.searchUsers"]')
    await searchInput.setValue('alice')
    await searchInput.trigger('keydown.enter')
    await flushPromises()
    expect(listUsers).toHaveBeenCalledWith(1, 50, { search: 'alice', role: 'user' })
    await selectOption(wrapper, '[data-test="owner-user"]', 'Alice · alice@example.com')
    await wrapper.get('[data-test="enterprise-create-form"]').trigger('submit')
    await flushPromises()
    expect(createEnterprise).toHaveBeenCalledWith(expect.objectContaining({ owner: { user_id: 42 } }))
    wrapper.unmount()
  })

  it('validates and submits a newly created initial owner without user_id', async () => {
    const wrapper = mount(EnterprisesView, { global: { stubs } })
    await flushPromises()
    await wrapper.get('[data-test="enterprise-create-open"]').trigger('click')
    await wrapper.get('[data-test="enterprise-name"]').setValue('Acme')
    await wrapper.get('button[role="radio"][value="new"]').trigger('click')
    await wrapper.get('[data-test="owner-email"]').setValue('alice@example.com')
    await wrapper.get('[data-test="owner-password"]').setValue('short')
    await wrapper.get('[data-test="enterprise-create-form"]').trigger('submit')
    expect(wrapper.text()).toContain('enterprise.validationPassword')
    expect(createEnterprise).not.toHaveBeenCalled()
    await wrapper.get('[data-test="owner-password"]').setValue('password123')
    await wrapper.get('[data-test="enterprise-create-form"]').trigger('submit')
    await flushPromises()
    expect(createEnterprise).toHaveBeenCalledWith(expect.objectContaining({ owner: { email: 'alice@example.com', password: 'password123', username: undefined } }))
    wrapper.unmount()
  })

  it('edits, disables, and adjusts an enterprise balance directly from the list', async () => {
    const enterprise = { id: 1, name: 'Acme', status: 'active', balance: 100, frozen_balance: 9.25, total_recharged: 200, low_balance_threshold: null, contact_name: '', contact_email: '', contact_phone: '', notes: '', settings: { ...defaultEnterpriseSettings }, member_count: 2, department_count: 1, created_at: '', updated_at: '' }
    listEnterprises.mockResolvedValue({ items: [enterprise], total: 1 })
    updateEnterprise.mockResolvedValue(enterprise)
    adjustBalance.mockResolvedValue(enterprise)
    const wrapper = mount(EnterprisesView, { global: { stubs } })
    await flushPromises()
    await clickMenuItem(wrapper, '[data-test="enterprise-menu-1"]', '[data-test="enterprise-edit-1"]')
    await wrapper.get('[data-test="enterprise-basic-name"]').setValue('Acme II')
    await wrapper.get('[data-test="enterprise-basic-threshold"]').setValue('25')
    await wrapper.findComponent({ name: 'EnterpriseBasicsForm' }).get('form').trigger('submit')
    await flushPromises()
    expect(updateEnterprise).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Acme II', low_balance_threshold: 25 }))
    await clickMenuItem(wrapper, '[data-test="enterprise-menu-1"]', '[data-test="enterprise-status-1"]')
    await wrapper.get('[data-test="confirm-reset"]').trigger('click')
    await flushPromises()
    expect(updateEnterprise).toHaveBeenCalledWith(1, { status: 'disabled' })
    await clickMenuItem(wrapper, '[data-test="enterprise-menu-1"]', '[data-test="enterprise-balance-1"]')
    const balanceForm = wrapper.findComponent({ name: 'EnterpriseBalanceForm' })
    expect(balanceForm.text()).toContain('enterprise.balanceAdjustmentHint')
    await balanceForm.get('[data-test="enterprise-balance-amount"]').setValue('0')
    await balanceForm.get('form').trigger('submit')
    expect(adjustBalance).not.toHaveBeenCalled()
    expect(balanceForm.text()).toContain('enterprise.validationAmount')
    await balanceForm.get('[data-test="enterprise-balance-amount"]').setValue('25.5')
    await selectOption(wrapper, '[data-test="balance-operation"]', 'enterprise.subtractBalance')
    await balanceForm.get('form').trigger('submit')
    await flushPromises()
    expect(adjustBalance).toHaveBeenCalledWith(1, { operation: 'subtract', amount: 25.5, notes: '' })
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('Enterprise quota forms', () => {
  const quota: EnterpriseQuota = { scope_type: 'member', scope_id: 7, daily_limit_usd: 5, weekly_limit_usd: null, monthly_limit_usd: 20, total_limit_usd: null, daily_usage_usd: 1, weekly_usage_usd: 2, monthly_usage_usd: 3, total_usage_usd: 4 }
  const updateQuota = vi.fn(), resetQuota = vi.fn()
  function api() { return { quotas: vi.fn().mockResolvedValue([quota]), departments: vi.fn().mockResolvedValue([]), members: vi.fn().mockResolvedValue({ items: [], total: 0 }), updateQuota, resetQuota } as unknown as EnterpriseResourceAPI }
  beforeEach(() => { vi.clearAllMocks(); updateQuota.mockResolvedValue(quota); resetQuota.mockResolvedValue(quota) })

  it('sends all four limits, preserving zero and converting empty limits to null', async () => {
    const wrapper = mount(EnterpriseQuotaPanel, { props: { api: api(), admin: true }, global: { stubs } })
    await flushPromises()
    await wrapper.get('[data-test="quota-edit-member-7"]').trigger('click')
    await wrapper.get('[data-test="quota-daily"]').setValue('0')
    await wrapper.get('[data-test="quota-weekly"]').setValue('10')
    await wrapper.get('[data-test="quota-monthly"]').setValue('')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateQuota).toHaveBeenCalledWith('member', 7, { daily_limit_usd: 0, weekly_limit_usd: 10, monthly_limit_usd: null, total_limit_usd: null })
    expect(wrapper.emitted('updated')).toHaveLength(1)
    wrapper.unmount()
  })

  it('rejects negative limits and resets the selected scope using the member record id', async () => {
    const wrapper = mount(EnterpriseQuotaPanel, { props: { api: api(), admin: true }, global: { stubs } })
    await flushPromises()
    await wrapper.get('[data-test="quota-edit-member-7"]').trigger('click')
    await wrapper.get('[data-test="quota-daily"]').setValue('-1')
    await wrapper.get('form').trigger('submit')
    expect(updateQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('enterprise.validationQuota')
    wrapper.getComponent({ name: 'EnterpriseDialog' }).vm.$emit('close')
    await flushPromises()
    await clickMenuItem(wrapper, '[data-test="quota-menu-member-7"]', '[data-test="quota-reset-member-7"]')
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    const dialog = wrapper.getComponent({ name: 'EnterpriseConfirm' })
    expect(dialog.attributes('title')).toBe('enterprise.resetUsage')
    expect(dialog.attributes('message')).toBe('enterprise.resetConfirm')
    await wrapper.get('[data-test="quota-reset-window-total"]').trigger('click')
    await wrapper.get('[data-test="quota-reset-window-daily"]').trigger('click')
    await wrapper.get('[data-test="confirm-reset"]').trigger('click')
    await flushPromises()
    expect(resetQuota).toHaveBeenCalledWith('member', 7, ['daily'])
    wrapper.unmount()
  })
})
