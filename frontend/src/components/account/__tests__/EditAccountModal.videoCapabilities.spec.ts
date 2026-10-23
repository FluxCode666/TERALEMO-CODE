import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { updateAccountMock, checkMixedChannelRiskMock, showErrorMock, authIsSimpleMode } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  showErrorMock: vi.fn(),
  authIsSimpleMode: { value: true }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showInfo: vi.fn()
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
      update: updateAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock
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
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

import EditAccountModal from '../EditAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: { modelValue: { type: [String, Number, Boolean, null], default: '' }, options: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: `
    <select v-bind="$attrs" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
      <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
    </select>
  `
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: { modelValue: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<div data-testid="group-selector" />'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: { modelMappings: { type: Array, default: () => [] }, modelValue: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<div />'
})

function buildVideoAccount(credentials: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: 'Video account',
    notes: '',
    platform: 'video',
    type: 'apikey',
    credentials: {
      api_key: 'video-key',
      base_url: 'https://metaso.cn/api/minimax/v2',
      video_protocol: 'minimax',
      ...credentials
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function mountModal(account = buildVideoAccount()) {
  return mount(EditAccountModal, {
    props: { show: true, account, proxies: [], groups: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        ProxySelector: true,
        GroupSelector: GroupSelectorStub,
        ModelWhitelistSelector: ModelWhitelistSelectorStub
      }
    }
  })
}

async function submit(wrapper: ReturnType<typeof mountModal>) {
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
}

describe('EditAccountModal video capabilities', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    updateAccountMock.mockReset().mockImplementation((id: number, payload: any) => Promise.resolve({ id, ...payload }))
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    showErrorMock.mockReset()
  })

  it('hides the capability section for non-video platforms', () => {
    const wrapper = mount(  EditAccountModal, {
      props: {
        show: true,
        account: { ...buildVideoAccount(), platform: 'grok', credentials: { api_key: 'grok-key', base_url: 'https://api.x.ai/v1' } },
        proxies: [],
        groups: []
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          Icon: true,
          ProxySelector: true,
          GroupSelector: GroupSelectorStub,
          ModelWhitelistSelector: ModelWhitelistSelectorStub
        }
      }
    })
    expect(wrapper.find('[data-testid="edit-video-capability-resolution-480p"]').exists()).toBe(false)
    expect(wrapper.find('#edit-video-capability-min-duration').exists()).toBe(false)
    wrapper.unmount()
  })

  it('backfills stored capabilities with normalized resolutions', () => {
    const wrapper = mountModal(buildVideoAccount({
      video_resolutions: ['2K', '480p'],
      video_min_duration_seconds: 4,
      video_max_duration_seconds: 15
    }))
    expect((wrapper.get('[data-testid="edit-video-capability-resolution-480p"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="edit-video-capability-resolution-2k"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="edit-video-capability-resolution-720p"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('#edit-video-capability-min-duration').element as HTMLInputElement).value).toBe('4')
    expect((wrapper.get('#edit-video-capability-max-duration').element as HTMLInputElement).value).toBe('15')
    wrapper.unmount()
  })

  it('does not write capability keys for a legacy account that never configured them', async () => {
    const wrapper = mountModal(buildVideoAccount())
    await submit(wrapper)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).not.toHaveProperty('video_resolutions')
    expect(credentials).not.toHaveProperty('video_min_duration_seconds')
    expect(credentials).not.toHaveProperty('video_max_duration_seconds')
    wrapper.unmount()
  })

  it('writes normalized keys when the user configures a legacy account', async () => {
    const wrapper = mountModal(buildVideoAccount())
    await wrapper.get('[data-testid="edit-video-capability-resolution-1080p"]').setValue(true)
    await wrapper.get('#edit-video-capability-min-duration').setValue('6')
    await wrapper.get('#edit-video-capability-max-duration').setValue('30')
    await submit(wrapper)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials.video_resolutions).toEqual(['1080p'])
    expect(credentials.video_min_duration_seconds).toBe(6)
    expect(credentials.video_max_duration_seconds).toBe(30)
    wrapper.unmount()
  })

  it('omits the keys entirely after the user clears a configured account', async () => {
    const wrapper = mountModal(buildVideoAccount({
      video_resolutions: ['768p', '2k'],
      video_min_duration_seconds: 4,
      video_max_duration_seconds: 15
    }))
    await wrapper.get('[data-testid="edit-video-capability-resolution-768p"]').setValue(false)
    await wrapper.get('[data-testid="edit-video-capability-resolution-2k"]').setValue(false)
    await wrapper.get('#edit-video-capability-min-duration').setValue('')
    await wrapper.get('#edit-video-capability-max-duration').setValue('')
    await submit(wrapper)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).toEqual({
      api_key: 'video-key',
      base_url: 'https://metaso.cn/api/minimax/v2',
      video_protocol: 'minimax'
    })
    wrapper.unmount()
  })

  it.each([
    ['15', '4', 'admin.accounts.videoCapability.minGtMax'],
    ['2.5', '', 'admin.accounts.videoCapability.minInvalid'],
    ['', '61', 'admin.accounts.videoCapability.maxInvalid']
  ])('rejects min=%s max=%s with %s and does not update', async (min, max, errorKey) => {
    const wrapper = mountModal(buildVideoAccount())
    await wrapper.get('#edit-video-capability-min-duration').setValue(min)
    await wrapper.get('#edit-video-capability-max-duration').setValue(max)
    await submit(wrapper)
    expect(showErrorMock).toHaveBeenCalledWith(errorKey)
    expect(updateAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
