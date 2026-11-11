import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelAllowlistPanel from '../ModelAllowlistPanel.vue'

const api = vi.hoisted(() => ({ getModelAllowlist: vi.fn(), saveModelAllowlist: vi.fn(), testModelAllowlist: vi.fn(), toggleModelAllowlist: vi.fn() }))
vi.mock('@/api/admin/modelAllowlist', () => api)
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: (action: () => unknown) => action() }), isStepUpCancelled: () => false }))

function response() { return { settings: { enabled: false, entries: ['192.0.2.1'], notes: { '192.0.2.1': 'Customer A' }, cloudflare: true, trusted_cdn_proxies: [] }, domains: [], observations: [], impact: { ips: 3, users: 2, keys: 4 } } }
function render() { return mount(ModelAllowlistPanel, { global: { stubs: { TotpStepUpDialog: true, ConfirmDialog: true, Toggle: true } } }) }

beforeEach(() => { vi.clearAllMocks(); api.getModelAllowlist.mockResolvedValue(response()); api.saveModelAllowlist.mockResolvedValue(response()) })

describe('model allowlist workflow', () => {
  it('saves customer notes while enforcement stays disabled', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('input[aria-label="modelAllowlist.note"]').setValue('Updated customer')
    const save = wrapper.findAll('button').find(button => button.text() === 'modelAllowlist.save')!
    await save.trigger('click')
    await flushPromises()
    expect(api.saveModelAllowlist).toHaveBeenCalledWith(expect.objectContaining({ enabled: false, notes: { '192.0.2.1': 'Updated customer' } }))
    expect(api.toggleModelAllowlist).not.toHaveBeenCalled()
  })

  it('requires confirmation before switching enforcement on', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'modelAllowlist.enable')!.trigger('click')
    await flushPromises()
    expect(api.toggleModelAllowlist).not.toHaveBeenCalled()
    expect(wrapper.getComponent({ name: 'ConfirmDialog' }).props('show')).toBe(true)
  })
})
