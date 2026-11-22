import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import AlertNotifyChannelsCard from '../AlertNotifyChannelsCard.vue'
import type { AlertNotifyChannelView } from '@/api/admin/alertNotify'

const { getChannels, updateChannels, testChannel, showError, showSuccess } = vi.hoisted(() => ({
  getChannels: vi.fn(),
  updateChannels: vi.fn(),
  testChannel: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin/alertNotify', () => ({
  alertNotifyAPI: { getChannels, updateChannels, testChannel }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const CHANNEL_A: AlertNotifyChannelView = {
  id: 'a1b2c3d4e5f60718',
  name: '运维飞书群',
  type: 'feishu',
  enabled: true,
  url_masked: 'https://open.feishu.cn/open-apis/bot/v2/hook/****9f3a',
  secret_configured: true,
  events: ['ops.alert', 'account.quota_alert'],
  min_severity: '',
  send_resolved: false,
  updated_at: '2026-11-22T14:00:00Z'
}

const CHANNEL_B: AlertNotifyChannelView = {
  id: 'b2c3d4e5f6071829',
  name: '备用 Webhook',
  type: 'webhook',
  enabled: false,
  url_masked: 'https://hooks.example.com/****abcd',
  secret_configured: false,
  events: ['account.quota_alert'],
  min_severity: 'warning',
  send_resolved: true,
  updated_at: '2026-11-22T15:00:00Z'
}

function responseOf(channels: AlertNotifyChannelView[]) {
  return { channels, supported_events: ['ops.alert', 'account.quota_alert'] }
}

const CHANNEL_C: AlertNotifyChannelView = {
  id: 'c3d4e5f60718293a',
  name: '第三个渠道',
  type: 'webhook',
  enabled: true,
  url_masked: 'https://third.example.com/****wxyz',
  secret_configured: false,
  events: ['ops.alert'],
  min_severity: 'critical',
  send_resolved: false,
  updated_at: '2026-11-22T17:00:00Z'
}

const mountCard = () => mount(AlertNotifyChannelsCard, {
  global: {
    stubs: {
      // BaseDialog 内部使用 Teleport，stub 掉以便在 wrapper 内直接查询表单元素
      BaseDialog: {
        props: ['show', 'title'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      }
    }
  }
})

const setInputValue = async (wrapper: ReturnType<typeof mountCard>, testId: string, value: string) => {
  const input = wrapper.get(`[data-test="${testId}"]`)
  ;(input.element as HTMLInputElement).value = value
  await input.trigger('input')
}

async function openEditor(wrapper: ReturnType<typeof mountCard>) {
  await wrapper.get('[data-test="btn-add-channel"]').trigger('click')
  await nextTick()
  return wrapper.get('[data-test="channel-editor"]')
}

beforeEach(() => {
  vi.clearAllMocks()
  getChannels.mockResolvedValue(responseOf([CHANNEL_A, CHANNEL_B]))
  testChannel.mockResolvedValue({ success: true })
  updateChannels.mockImplementation(async (channels: Array<{ id: string } & Partial<AlertNotifyChannelView>>) => ({
    // 模拟服务端：已有渠道保留原视图字段并应用 input 变更，新建渠道生成 id 并打码 URL
    channels: channels.map((item) => {
      const existing = [CHANNEL_A, CHANNEL_B].find((c) => c.id === item.id)
      const base: AlertNotifyChannelView = existing
        ? { ...existing, ...item, id: existing.id }
        : {
            id: 'newchannelid0001',
            name: item.name ?? '',
            type: item.type ?? 'webhook',
            enabled: item.enabled ?? true,
            url_masked: 'https://hooks.example.com/****oken',
            secret_configured: Boolean(item.secret),
            events: item.events ?? [],
            min_severity: item.min_severity ?? '',
            send_resolved: item.send_resolved ?? false,
            updated_at: '2026-11-22T16:00:00Z'
          }
      return base
    }),
    supported_events: ['ops.alert', 'account.quota_alert']
  }))
})

describe('AlertNotifyChannelsCard', () => {
  it('renders the channel list after loading (name, url_masked, type)', async () => {
    const wrapper = mountCard()
    await flushPromises()

    expect(getChannels).toHaveBeenCalledTimes(1)
    const rowA = wrapper.get('[data-test="channel-row-a1b2c3d4e5f60718"]')
    expect(rowA.text()).toContain('运维飞书群')
    expect(rowA.text()).toContain('https://open.feishu.cn/open-apis/bot/v2/hook/****9f3a')
    expect(rowA.text()).toContain('admin.settings.alertNotify.types.feishu')
    const rowB = wrapper.get('[data-test="channel-row-b2c3d4e5f6071829"]')
    expect(rowB.text()).toContain('备用 Webhook')
    expect(rowB.text()).toContain('admin.settings.alertNotify.types.webhook')
  })

  it('creates a channel: new item has empty id and submitted url, existing channels keep url/secret empty', async () => {
    const wrapper = mountCard()
    await flushPromises()

    await openEditor(wrapper)
    await setInputValue(wrapper, 'input-name', '新渠道')
    await setInputValue(wrapper, 'input-url', 'https://open.feishu.cn/open-apis/bot/v2/hook/newtoken')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()

    expect(updateChannels).toHaveBeenCalledTimes(1)
    const submitted = updateChannels.mock.calls[0][0]
    expect(submitted).toHaveLength(3)

    const created = submitted.find((item: { id: string }) => item.id === '')
    expect(created).toMatchObject({
      id: '',
      name: '新渠道',
      type: 'feishu',
      url: 'https://open.feishu.cn/open-apis/bot/v2/hook/newtoken',
      enabled: true,
      clear_secret: false
    })
    expect(created.events).toEqual(['ops.alert', 'account.quota_alert'])

    const untouchedA = submitted.find((item: { id: string }) => item.id === CHANNEL_A.id)
    const untouchedB = submitted.find((item: { id: string }) => item.id === CHANNEL_B.id)
    expect(untouchedA).toMatchObject({ url: '', secret: '', clear_secret: false, name: CHANNEL_A.name })
    expect(untouchedB).toMatchObject({ url: '', secret: '', clear_secret: false, enabled: false })
  })

  it('edits without changing URL (submits empty url) and submits clear_secret when checked', async () => {
    const wrapper = mountCard()
    await flushPromises()

    // 编辑已有渠道且不改 URL：该项 url 为 ''
    await wrapper.get('[data-test="btn-edit-a1b2c3d4e5f60718"]').trigger('click')
    await nextTick()
    await setInputValue(wrapper, 'input-name', '改名后的渠道')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()

    let submitted = updateChannels.mock.calls[0][0]
    expect(submitted).toHaveLength(2)
    expect(submitted.find((item: { id: string }) => item.id === CHANNEL_A.id)).toMatchObject({
      id: CHANNEL_A.id,
      name: '改名后的渠道',
      url: '',
      secret: '',
      clear_secret: false
    })

    // 勾选清除密钥后 clear_secret 为 true
    await wrapper.get('[data-test="btn-edit-a1b2c3d4e5f60718"]').trigger('click')
    await nextTick()
    const clearCheckbox = wrapper.get('[data-test="checkbox-clear-secret"]')
    ;(clearCheckbox.element as HTMLInputElement).checked = true
    await clearCheckbox.trigger('change')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()

    submitted = updateChannels.mock.calls[1][0]
    expect(submitted.find((item: { id: string }) => item.id === CHANNEL_A.id)).toMatchObject({
      id: CHANNEL_A.id,
      url: '',
      clear_secret: true
    })
  })

  it('toggling enabled immediately calls updateChannels with the flipped value', async () => {
    const wrapper = mountCard()
    await flushPromises()

    await wrapper.get('[data-test="toggle-b2c3d4e5f6071829"]').trigger('click')
    await flushPromises()

    expect(updateChannels).toHaveBeenCalledTimes(1)
    const submitted = updateChannels.mock.calls[0][0]
    expect(submitted).toHaveLength(2)
    expect(submitted.find((item: { id: string }) => item.id === CHANNEL_B.id)).toMatchObject({
      id: CHANNEL_B.id,
      enabled: true,
      url: '',
      secret: '',
      clear_secret: false
    })
    expect(submitted.find((item: { id: string }) => item.id === CHANNEL_A.id)).toMatchObject({
      id: CHANNEL_A.id,
      enabled: true
    })
  })

  it('requires confirmation before delete, and the deleted channel is absent from the submitted list', async () => {
    const wrapper = mountCard()
    await flushPromises()

    await wrapper.get('[data-test="btn-delete-a1b2c3d4e5f60718"]').trigger('click')
    await nextTick()
    expect(updateChannels).not.toHaveBeenCalled()

    const confirmDialog = wrapper.getComponent({ name: 'ConfirmDialog' })
    expect(confirmDialog.props('show')).toBe(true)
    confirmDialog.vm.$emit('confirm')
    await flushPromises()

    expect(updateChannels).toHaveBeenCalledTimes(1)
    const submitted = updateChannels.mock.calls[0][0]
    expect(submitted).toHaveLength(1)
    expect(submitted[0]).toMatchObject({ id: CHANNEL_B.id, url: '', secret: '' })
    expect(submitted.some((item: { id: string }) => item.id === CHANNEL_A.id)).toBe(false)
  })

  it('row test button calls testChannel with the channel id and preserved fields', async () => {
    const wrapper = mountCard()
    await flushPromises()

    await wrapper.get('[data-test="btn-test-a1b2c3d4e5f60718"]').trigger('click')
    await flushPromises()

    expect(testChannel).toHaveBeenCalledTimes(1)
    expect(testChannel.mock.calls[0][0]).toMatchObject({
      id: CHANNEL_A.id,
      name: CHANNEL_A.name,
      type: 'feishu',
      url: '',
      secret: '',
      clear_secret: false,
      enabled: true,
      min_severity: ''
    })
    expect(testChannel.mock.calls[0][0].events).toEqual(['ops.alert', 'account.quota_alert'])
    expect(showSuccess).toHaveBeenCalled()
  })

  it('blocks invalid submissions: empty name/url/events, and http:// for feishu', async () => {
    const wrapper = mountCard()
    await flushPromises()

    // 新建时 URL 为空 → 不发请求
    let editor = await openEditor(wrapper)
    await setInputValue(wrapper, 'input-name', '缺 URL 的渠道')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()
    expect(updateChannels).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="form-error"]').exists()).toBe(true)

    // 名称为空 → 不发请求
    editor = await openEditor(wrapper)
    await setInputValue(wrapper, 'input-url', 'https://hooks.example.com/abc')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()
    expect(updateChannels).not.toHaveBeenCalled()

    // 未选任何事件 → 不发请求
    editor = await openEditor(wrapper)
    await setInputValue(wrapper, 'input-name', '无事件渠道')
    await setInputValue(wrapper, 'input-url', 'https://hooks.example.com/abc')
    await editor.get('[data-test="checkbox-event-ops.alert"]').trigger('change')
    await editor.get('[data-test="checkbox-event-account.quota_alert"]').trigger('change')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()
    expect(updateChannels).not.toHaveBeenCalled()

    // 飞书类型填 http:// 地址 → 被拦截
    editor = await openEditor(wrapper)
    await setInputValue(wrapper, 'input-name', '飞书渠道')
    await setInputValue(wrapper, 'input-url', 'http://open.feishu.cn/open-apis/bot/v2/hook/token')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()
    expect(updateChannels).not.toHaveBeenCalled()
  })

  it('editing the first channel preserves list order (in-place replace, not moved to end)', async () => {
    getChannels.mockResolvedValue(responseOf([CHANNEL_A, CHANNEL_B, CHANNEL_C]))
    const wrapper = mountCard()
    await flushPromises()

    // 编辑第一个渠道并保存
    await wrapper.get('[data-test="btn-edit-a1b2c3d4e5f60718"]').trigger('click')
    await nextTick()
    await setInputValue(wrapper, 'input-name', '首位改名')
    await wrapper.get('[data-test="btn-save"]').trigger('click')
    await flushPromises()

    expect(updateChannels).toHaveBeenCalledTimes(1)
    const submitted = updateChannels.mock.calls[0][0]
    // id 顺序与原列表一致，被编辑项仍在下标 0
    expect(submitted.map((item: { id: string }) => item.id)).toEqual([
      CHANNEL_A.id,
      CHANNEL_B.id,
      CHANNEL_C.id
    ])
    expect(submitted[0]).toMatchObject({ id: CHANNEL_A.id, name: '首位改名', url: '' })
    // 其余项保持 url/secret 为空（保留原值）
    expect(submitted[1]).toMatchObject({ id: CHANNEL_B.id, url: '', secret: '' })
    expect(submitted[2]).toMatchObject({ id: CHANNEL_C.id, url: '', secret: '' })
  })
})
