import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'

const { createAccountMock, syncPreviewMock, syncUpstreamModelsMock, showWarningMock } = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  syncPreviewMock: vi.fn(),
  syncUpstreamModelsMock: vi.fn(),
  showWarningMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showInfo: vi.fn(), showSuccess: vi.fn(), showWarning: showWarningMock })
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      probeUpstreamBilling: vi.fn().mockResolvedValue({}),
      syncUpstreamModels: syncUpstreamModelsMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))
vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([]),
  accountsAPI: { syncUpstreamModels: syncUpstreamModelsMock, syncUpstreamModelsPreview: syncPreviewMock }
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import CreateAccountModal from '../CreateAccountModal.vue'
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

function mountModal() {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub, ConfirmDialog: true, Select: true, Icon: true, PlatformIcon: true,
        ProxySelector: true, ProxyAdBanner: true, GroupSelector: GroupSelectorStub, QuotaLimitCard: true, ModelIcon: true
      }
    }
  })
}

async function openImageForm(protocol: string) {
  const wrapper = mountModal()
  await wrapper.get('[data-testid="platform-button-image"]').trigger('click')
  await wrapper.get('#create-image-protocol').setValue(protocol)
  await wrapper.get('form#create-account-form input[type="text"]').setValue('race account')
  await wrapper.get('form#create-account-form input[type="password"]').setValue('image-key')
  const selector = wrapper.findComponent(ModelWhitelistSelector)
  await selector.findAll('button').find((b) => b.text() === 'admin.accounts.clearAllModels')!.trigger('click')
  return { wrapper, selector }
}

describe('CreateAccountModal image protocol vs in-flight upstream sync (real ModelWhitelistSelector)', () => {
  beforeEach(() => {
    setPlatformCatalog(BUILTIN_PLATFORM_CATALOG)
    createAccountMock.mockReset().mockResolvedValue({ id: 43, platform: 'image', type: 'apikey' })
    syncPreviewMock.mockReset()
    syncUpstreamModelsMock.mockReset().mockResolvedValue({ models: [], metadata: {} })
    showWarningMock.mockReset()
  })
  afterEach(() => resetPlatformCatalog())

  it('discards a first sync that returns after the protocol was switched, and never submits its models', async () => {
    let resolve!: (v: any) => void
    syncPreviewMock.mockImplementation(() => new Promise((r) => { resolve = r }))
    const { wrapper, selector } = await openImageForm('gemini')
    await selector.get('[data-testid="sync-upstream-models"]').trigger('click')
    expect(syncPreviewMock.mock.calls[0][0].image_protocol).toBe('gemini')
    await wrapper.get('#create-image-protocol').setValue('grok')
    resolve({ models: ['gemini-late-image'], metadata: {}, warnings: [] })
    await flushPromises()
    expect(selector.props('modelValue')).toEqual([])
    expect(selector.emitted('upstream-synced')).toBeUndefined()
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    const credentials = createAccountMock.mock.calls[0][0].credentials
    expect(credentials.image_protocol).toBe('grok')
    expect(credentials.model_mapping).toBeUndefined()
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('still applies a sync result when the protocol did not change', async () => {
    syncPreviewMock.mockResolvedValue({ models: ['gemini-ok-image'], metadata: {}, warnings: [] })
    const { wrapper, selector } = await openImageForm('gemini')
    await selector.get('[data-testid="sync-upstream-models"]').trigger('click')
    await flushPromises()
    expect(selector.props('modelValue')).toEqual(['gemini-ok-image'])
    wrapper.unmount()
  })
})
