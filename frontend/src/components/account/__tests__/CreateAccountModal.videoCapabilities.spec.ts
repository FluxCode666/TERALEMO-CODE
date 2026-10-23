import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { createAccountMock, showErrorMock, authIsSimpleMode } = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  showErrorMock: vi.fn(),
  authIsSimpleMode: { value: true }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showWarning: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isSimpleMode() {
      return authIsSimpleMode.value
    }
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      probeUpstreamBilling: vi.fn().mockResolvedValue({}),
      syncUpstreamModels: vi.fn().mockResolvedValue({ models: [], metadata: {} }),
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }),
      importCodexSession: vi.fn(),
      createOpenAICodexPAT: vi.fn()
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showManualOption: Boolean,
    showCodexSessionImportOption: Boolean,
    showAgentIdentityOption: Boolean,
    showCodexPatOption: Boolean,
    initialInputMethod: String
  },
  data: () => ({ inputMethod: 'manual' }),
  emits: ['import-codex-session', 'import-codex-pat'],
  template: '<div />'
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: { modelValue: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<div />'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: { modelValue: { type: Array, default: () => [] }, platform: String, syncCredentials: Object },
  emits: ['update:modelValue', 'upstream-synced'],
  template: '<div />'
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: GroupSelectorStub,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true
      }
    }
  })
}

async function mountVideoForm() {
  const wrapper = mountModal()
  await wrapper.get('[data-testid="platform-button-video"]').trigger('click')
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Video account')
  await wrapper.get('form#create-account-form input[type="password"]').setValue('video-key')
  return wrapper
}

async function submit(wrapper: Awaited<ReturnType<typeof mountVideoForm>>) {
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
}

describe('CreateAccountModal video capabilities', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'video', type: 'apikey' })
    showErrorMock.mockReset()
  })

  it('hides the capability section for non-video platforms', async () => {
    const wrapper = mountModal()
    await wrapper.findAll('button').find(button => button.text().includes('OpenAI'))!.trigger('click')
    expect(wrapper.find('[data-testid="create-video-capability-resolution-480p"]').exists()).toBe(false)
    expect(wrapper.find('#create-video-capability-min-duration').exists()).toBe(false)
    wrapper.unmount()
  })

  it('writes normalized resolutions and integer durations when configured', async () => {
    const wrapper = await mountVideoForm()
    await wrapper.get('[data-testid="create-video-capability-resolution-2k"]').setValue(true)
    await wrapper.get('[data-testid="create-video-capability-resolution-480p"]').setValue(true)
    await wrapper.get('#create-video-capability-min-duration').setValue('4')
    await wrapper.get('#create-video-capability-max-duration').setValue('15')
    await submit(wrapper)
    const credentials = createAccountMock.mock.calls[0]?.[0]?.credentials
    expect(credentials.video_resolutions).toEqual(['480p', '2k'])
    expect(credentials.video_min_duration_seconds).toBe(4)
    expect(credentials.video_max_duration_seconds).toBe(15)
    wrapper.unmount()
  })

  it('omits all three capability keys when nothing is configured', async () => {
    const wrapper = await mountVideoForm()
    await submit(wrapper)
    const credentials = createAccountMock.mock.calls[0]?.[0]?.credentials
    expect(credentials).not.toHaveProperty('video_resolutions')
    expect(credentials).not.toHaveProperty('video_min_duration_seconds')
    expect(credentials).not.toHaveProperty('video_max_duration_seconds')
    expect(Object.values(credentials)).not.toContain(null)
    wrapper.unmount()
  })

  it('writes only video_min_duration_seconds when only the minimum is configured', async () => {
    const wrapper = await mountVideoForm()
    await wrapper.get('#create-video-capability-min-duration').setValue('2')
    await submit(wrapper)
    const credentials = createAccountMock.mock.calls[0]?.[0]?.credentials
    expect(credentials.video_min_duration_seconds).toBe(2)
    expect(credentials).not.toHaveProperty('video_max_duration_seconds')
    expect(credentials).not.toHaveProperty('video_resolutions')
    wrapper.unmount()
  })

  it.each([
    ['15', '4', 'admin.accounts.videoCapability.minGtMax'],
    ['2.5', '', 'admin.accounts.videoCapability.minInvalid'],
    ['0', '', 'admin.accounts.videoCapability.minInvalid'],
    ['', '61', 'admin.accounts.videoCapability.maxInvalid'],
    ['', '0', 'admin.accounts.videoCapability.maxInvalid']
  ])('rejects min=%s max=%s with %s and does not create', async (min, max, errorKey) => {
    const wrapper = await mountVideoForm()
    if (min) await wrapper.get('#create-video-capability-min-duration').setValue(min)
    if (max) await wrapper.get('#create-video-capability-max-duration').setValue(max)
    await submit(wrapper)
    expect(showErrorMock).toHaveBeenCalledWith(errorKey)
    expect(createAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
