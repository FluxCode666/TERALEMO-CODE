import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EnterpriseDeleteDialog from '../components/EnterpriseDeleteDialog.vue'
import EnterprisesView from '@/views/admin/EnterprisesView.vue'
import EnterpriseDetailView from '@/views/admin/EnterpriseDetailView.vue'

const { removeEnterprise, listEnterprises, push, showSuccess, showError } = vi.hoisted(() => ({
  removeEnterprise: vi.fn(), listEnterprises: vi.fn(), push: vi.fn(), showSuccess: vi.fn(), showError: vi.fn()
}))
vi.mock('@/api/admin/enterprises', () => ({ enterprisesAPI: { remove: removeEnterprise, list: listEnterprises } }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: vi.fn() } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: '7' } }), useRouter: () => ({ push }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  DataTable: { props: ['data'], template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>' },
  ConfirmDialog: { props: ['show', 'message'], emits: ['confirm', 'cancel'], template: '<div v-if="show" data-test="delete-confirm"><p>{{ message }}</p><slot /><button data-test="confirm-delete" @click="$emit(\'confirm\')">Confirm</button><button data-test="cancel-delete" @click="$emit(\'cancel\')">Cancel</button></div>' },
  EnterpriseWorkspace: true, Pagination: true, BaseDialog: true, RouterLink: true
}

describe('Enterprise deletion confirmation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    removeEnterprise.mockReset().mockResolvedValue(undefined)
    listEnterprises.mockResolvedValue({ items: [{ id: 7, name: 'Acme', status: 'active' }], total: 1 })
  })

  it('deletes an empty enterprise without requesting dissolution', async () => {
    const wrapper = mount(EnterpriseDeleteDialog, { props: { enterpriseId: 7 }, global: { stubs } })
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(removeEnterprise).toHaveBeenCalledWith(7, undefined)
    expect(wrapper.emitted('deleted')).toHaveLength(1)
    expect(showSuccess).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it.each([
    { reason: 'ENTERPRISE_NOT_EMPTY', code: 'CONFLICT' },
    { response: { data: { code: 'ENTERPRISE_NOT_EMPTY' } } }
  ])('requires a second confirmation after the not-empty response: %o', async (cause) => {
    removeEnterprise.mockRejectedValueOnce(cause)
    const wrapper = mount(EnterpriseDeleteDialog, { props: { enterpriseId: 7 }, global: { stubs } })
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.enterprises.confirmDissolve')
    expect(removeEnterprise).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('deleted')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(removeEnterprise).toHaveBeenLastCalledWith(7, true)
    expect(wrapper.emitted('deleted')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not request dissolution when the administrator cancels the second confirmation', async () => {
    removeEnterprise.mockRejectedValueOnce({ code: 'ENTERPRISE_NOT_EMPTY' })
    const wrapper = mount(EnterpriseDeleteDialog, { props: { enterpriseId: 7 }, global: { stubs } })
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="cancel-delete"]').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(removeEnterprise).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('preserves ordinary errors without offering dissolution', async () => {
    removeEnterprise.mockRejectedValueOnce({ code: 'ENTERPRISE_PERMISSION_DENIED' })
    const wrapper = mount(EnterpriseDeleteDialog, { props: { enterpriseId: 7 }, global: { stubs } })
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.enterprises.confirmDissolve')
    expect(showError).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('deleted')).toBeUndefined()
    wrapper.unmount()
  })

  it('uses the same two-step flow from the administrator list and refreshes it after success', async () => {
    removeEnterprise.mockRejectedValueOnce({ code: 'ENTERPRISE_NOT_EMPTY' })
    const wrapper = mount(EnterprisesView, { global: { stubs } })
    await flushPromises()
    await wrapper.get('[data-test="enterprise-delete-7"]').trigger('click')
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.enterprises.confirmDissolve')
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(removeEnterprise).toHaveBeenLastCalledWith(7, true)
    expect(wrapper.find('[data-test="delete-confirm"]').exists()).toBe(false)
    expect(listEnterprises).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('uses the same two-step flow from administrator detail and returns to the list', async () => {
    removeEnterprise.mockRejectedValueOnce({ code: 'ENTERPRISE_NOT_EMPTY' })
    const wrapper = mount(EnterpriseDetailView, { global: { stubs } })
    await wrapper.get('[data-test="enterprise-delete-detail"]').trigger('click')
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(push).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()
    expect(removeEnterprise).toHaveBeenLastCalledWith(7, true)
    expect(push).toHaveBeenCalledWith('/admin/enterprises')
    wrapper.unmount()
  })
})
