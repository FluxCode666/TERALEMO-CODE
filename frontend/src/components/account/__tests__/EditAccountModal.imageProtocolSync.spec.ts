import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'

const { updateAccountMock, checkMixedChannelRiskMock, syncModelsMock, showWarningMock } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  syncModelsMock: vi.fn(),
  showWarningMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    showWarning: showWarningMock
  })
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { update: updateAccountMock, checkMixedChannelRisk: checkMixedChannelRiskMock },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))
vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn(),
  accountsAPI: { syncUpstreamModels: syncModelsMock }
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import EditAccountModal from '../EditAccountModal.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})
const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: { modelValue: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<div />'
})

// 只渲染 show 的确认框，并暴露标题，便于区分「混合渠道」和「图片协议」两个确认框。
const ConfirmDialogStub = defineComponent({
  name: 'ConfirmDialog',
  props: { show: Boolean, title: String, message: String },
  emits: ['confirm', 'cancel'],
  template: '<div v-if="show" :data-confirm-title="title"><span class="confirm-body">{{ message }}</span><button data-testid="confirm-yes" @click="$emit(\'confirm\')">ok</button><button data-testid="confirm-no" @click="$emit(\'cancel\')">no</button></div>'
})
const SelectStub = defineComponent({
  name: 'SelectStub',
  props: { modelValue: { type: [String, Number, Boolean, null], default: '' }, options: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>'
})

function imageAccount(modelMapping: Record<string, string> = { 'orig-image-model': 'orig-image-model' }) {
  return {
    id: 9, name: 'Image Key', notes: '', platform: 'image', type: 'apikey',
    credentials: { api_key: 'image-key', image_protocol: 'gemini', base_url: 'https://relay.example', model_mapping: modelMapping },
    extra: {}, proxy_id: null, concurrency: 1, priority: 1, rate_multiplier: 1, status: 'active',
    group_ids: [], expires_at: null, auto_pause_on_expired: false
  } as any
}

function mountModal(account = imageAccount()) {
  return mount(EditAccountModal, {
    props: { show: true, account, proxies: [], groups: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub, Select: SelectStub, Icon: true, ProxySelector: true,
        GroupSelector: GroupSelectorStub, ConfirmDialog: ConfirmDialogStub, ModelIcon: true
      }
    }
  })
}

const imageConfirmOpen = (w: ReturnType<typeof mountModal>) =>
  w.find('[data-confirm-title="admin.accounts.imageProtocolSaveConfirmTitle"]').exists()

describe('EditAccountModal image protocol vs upstream-synced models (real ModelWhitelistSelector)', () => {
  beforeEach(() => {
    setPlatformCatalog(BUILTIN_PLATFORM_CATALOG)
    updateAccountMock.mockReset().mockResolvedValue(imageAccount())
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    syncModelsMock.mockReset()
    showWarningMock.mockReset()
  })
  afterEach(() => resetPlatformCatalog())

  it('drops the result of a saved-account sync that is still in flight when the protocol changes', async () => {
    let resolve!: (v: any) => void
    syncModelsMock.mockImplementation(() => new Promise((r) => { resolve = r }))
    const wrapper = mountModal()
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    await selector.get('[data-testid="sync-upstream-models"]').trigger('click')
    await wrapper.get('#edit-image-protocol').setValue('grok')
    resolve({ models: ['gemini-late-image'], metadata: {}, warnings: [] })
    await flushPromises()
    expect(selector.props('modelValue')).not.toContain('gemini-late-image')
    wrapper.unmount()
  })

  it('keeps every model after the protocol is switched and lists them in the save confirmation', async () => {
    syncModelsMock.mockResolvedValue({ models: ['gemini-synced-image', 'gemini-second-image'], metadata: {}, warnings: [] })
    const wrapper = mountModal()
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    await selector.get('[data-testid="sync-upstream-models"]').trigger('click')
    await flushPromises()
    await wrapper.get('#edit-image-protocol').setValue('grok')
    await flushPromises()
    expect(showWarningMock).toHaveBeenCalledWith('admin.accounts.imageProtocolChangedSaveThenResync')
    const expected = ['orig-image-model', 'gemini-synced-image', 'gemini-second-image']
    expect(selector.props('modelValue')).toEqual(expect.arrayContaining(expected))
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(imageConfirmOpen(wrapper)).toBe(true)
    const dialogText = wrapper.get('[data-confirm-title="admin.accounts.imageProtocolSaveConfirmTitle"]').text()
    for (const model of expected) expect(dialogText).toContain(model)
    expect(updateAccountMock).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-yes"]').trigger('click')
    await flushPromises()
    const credentials = updateAccountMock.mock.calls[0][1].credentials
    expect(credentials.image_protocol).toBe('grok')
    expect(credentials.model_mapping).toEqual(Object.fromEntries(expected.map((m) => [m, m])))
    wrapper.unmount()
  })

  it('cancels the save and leaves the form untouched', async () => {
    const wrapper = mountModal()
    await wrapper.get('#edit-image-protocol').setValue('grok')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(imageConfirmOpen(wrapper)).toBe(true)
    await wrapper.get('[data-testid="confirm-no"]').trigger('click')
    await flushPromises()
    expect(updateAccountMock).not.toHaveBeenCalled()
    expect((wrapper.get('#edit-image-protocol').element as HTMLSelectElement).value).toBe('grok')
    expect(wrapper.findComponent(ModelWhitelistSelector).props('modelValue')).toEqual(['orig-image-model'])
    wrapper.unmount()
  })

  it('keeps an alias whose from equals a synced model and other original config after switching', async () => {
    syncModelsMock.mockResolvedValue({ models: ['orig-image-model', 'gemini-new-image'], metadata: {}, warnings: [] })
    const wrapper = mountModal(imageAccount({ 'orig-image-model': 'orig-image-model', 'public-alias': 'orig-image-model' }))
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    await selector.get('[data-testid="sync-upstream-models"]').trigger('click')
    await flushPromises()
    await wrapper.get('#edit-image-protocol').setValue('grok')
    await flushPromises()
    expect(selector.props('modelValue')).toEqual(expect.arrayContaining(['orig-image-model', 'gemini-new-image']))
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(imageConfirmOpen(wrapper)).toBe(true)
    await wrapper.get('[data-testid="confirm-yes"]').trigger('click')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0][1].credentials.model_mapping).toEqual({
      'orig-image-model': 'orig-image-model',
      'gemini-new-image': 'gemini-new-image',
      'public-alias': 'orig-image-model'
    })
    wrapper.unmount()
  })

  it('asks for confirmation before saving a changed protocol that still carries the original models', async () => {
    const wrapper = mountModal()
    await wrapper.get('#edit-image-protocol').setValue('grok')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(imageConfirmOpen(wrapper)).toBe(true)
    expect(updateAccountMock).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-yes"]').trigger('click')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0][1].credentials.image_protocol).toBe('grok')
    wrapper.unmount()
  })

  it('saves without a confirmation when the protocol is unchanged', async () => {
    const wrapper = mountModal()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(imageConfirmOpen(wrapper)).toBe(false)
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
