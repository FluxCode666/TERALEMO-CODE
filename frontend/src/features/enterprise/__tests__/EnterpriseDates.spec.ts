import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import { formatDateTime } from '@/utils/format'
import EnterpriseOrganizationPanel from '../components/EnterpriseOrganizationPanel.vue'
import EnterpriseLogsPanel from '../components/EnterpriseLogsPanel.vue'

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: vi.fn() } } }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/i18n', () => ({ getLocale: () => 'zh', i18n: { global: { t: (key: string) => key } } }))

const timestamp = '2026-10-18T00:15:27.98669+08:00'
const wrappers: VueWrapper[] = []
const stubs = { BaseDialog: true, ConfirmDialog: true, Pagination: true }

function page(items: unknown[]) {
  return { items, total: items.length, page: 1, page_size: 20, pages: 1 }
}

function member(joinedAt: string) {
  return {
    id: 7, enterprise_id: 1, user_id: 42, email: 'alice@example.com', username: 'Alice',
    department_id: null, department_name: null, role: 'member', status: 'active', title: '',
    joined_at: joinedAt, user_status: 'active'
  }
}

function track<T extends VueWrapper>(wrapper: T) {
  wrappers.push(wrapper)
  return wrapper
}

function setViewport(desktop: boolean) {
  vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
    matches: desktop, media: query, onchange: null,
    addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn()
  }))
}

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
})

describe.each([true, false])('enterprise dates with desktop viewport %s', desktop => {
  it('formats member joining times instead of rendering raw ISO strings', async () => {
    setViewport(desktop)
    const api = {
      departments: vi.fn().mockResolvedValue([]),
      members: vi.fn().mockResolvedValue(page([member(timestamp)]))
    } as unknown as EnterpriseResourceAPI
    const wrapper = track(mount(EnterpriseOrganizationPanel, {
      props: { api, mode: 'members' }, global: { stubs }
    }))
    await flushPromises()

    expect(wrapper.text()).toContain(formatDateTime(timestamp))
    expect(wrapper.text()).not.toContain(timestamp)
  })

  it.each(['audit', 'balanceLogs'] as const)('formats %s creation times', async kind => {
    setViewport(desktop)
    const row = kind === 'audit'
      ? { id: 1, created_at: timestamp, actor_email: 'owner@example.com', actor_role: 'owner', action: 'member.add', target_type: 'member', target_id: 7, detail: {} }
      : { id: 1, created_at: timestamp, type: 'add', amount: 10, balance_before: 0, balance_after: 10, operator_email: 'admin@example.com', notes: '' }
    const api = {
      auditLogs: vi.fn().mockResolvedValue(page([row])),
      balanceLogs: vi.fn().mockResolvedValue(page([row]))
    } as unknown as EnterpriseResourceAPI
    const wrapper = track(mount(EnterpriseLogsPanel, { props: { api, kind }, global: { stubs } }))
    await flushPromises()

    expect(wrapper.text()).toContain(formatDateTime(timestamp))
    expect(wrapper.text()).not.toContain(timestamp)
  })
})
