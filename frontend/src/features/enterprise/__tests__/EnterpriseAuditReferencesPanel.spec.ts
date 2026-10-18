import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EnterpriseLogsPanel from '../components/EnterpriseLogsPanel.vue'
import EnterpriseAuditDetail from '../components/EnterpriseAuditDetail.vue'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseAuditLog, EnterpriseOverview } from '@/types/enterprise'

vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })
const base: EnterpriseAuditLog = { id: 1, actor_email: '', actor_role: 'owner', actor_user_id: 1, action: 'member.update', target_type: 'enterprise', target_id: 1, detail: {}, created_at: '' }
function api(log = base) {
  return {
    auditLogs: vi.fn().mockResolvedValue({ items: [log], total: 1 }),
    departments: vi.fn().mockResolvedValue([{ id: 3, name: '研发部' }]),
    members: vi.fn().mockResolvedValue({ items: [{ id: 7, user_id: 70, username: 'Alice', email: 'alice@example.com' }], total: 1 }),
    groups: vi.fn().mockResolvedValue([{ id: 2, name: 'Claude' }])
  }
}
function render(requests: ReturnType<typeof api>, props: { admin?: boolean; overview?: EnterpriseOverview } = { admin: true }) {
  const wrapper = mount(EnterpriseLogsPanel, { attachTo: document.body, props: { api: requests as unknown as EnterpriseResourceAPI, kind: 'audit', enterprise: { id: 1, name: 'Acme' }, ...props } })
  wrappers.push(wrapper)
  return wrapper
}
describe('审计表格与弹窗名称', () => {
  it.each([['enterprise', 1, 'Acme'], ['department', 3, '研发部'], ['member', 7, 'Alice'], ['member', 8, 'enterprise.auditDetails.references.member #8']])('共用 %s 目标名称和回退', async (type, id, expected) => {
    const requests = api({ ...base, target_type: String(type), target_id: Number(id) }), wrapper = render(requests)
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain(expected)
    await wrapper.get('[data-test="audit-detail-1"]').trigger('click'); await flushPromises()
    const dialog = new DOMWrapper(document.querySelector<HTMLElement>('[role="dialog"]')!)
    expect(dialog.text()).toContain(expected)
  })
  it('企业侧也使用目录名称，成员无查看权限时不请求成员接口', async () => {
    const requests = api({ ...base, target_type: 'member', target_id: 9, detail: { username: 'Historical' } })
    const overview = { permissions: { view_members: false } } as EnterpriseOverview
    const wrapper = render(requests, { overview }); await flushPromises()
    expect(requests.members).not.toHaveBeenCalled()
    expect(wrapper.get('tbody').text()).toContain('Historical')
    await wrapper.setProps({ overview: { permissions: { view_members: true } } as EnterpriseOverview })
    await flushPromises()
    expect(requests.members).toHaveBeenCalledWith({ page: 1, page_size: 100 }, { signal: expect.any(AbortSignal) })
  })
  it('名称查询失败不影响日志，成功的目录仍用于展示', async () => {
    const requests = api({ ...base, target_type: 'department', target_id: 3 })
    requests.groups.mockRejectedValue(new Error('Unavailable')); requests.members.mockRejectedValue(new Error('Forbidden'))
    const wrapper = render(requests); await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('研发部')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it('刷新和卸载取消名称请求，迟到的旧目录不会覆盖新目录', async () => {
    let resolve!: (value: unknown) => void
    const requests = api()
    requests.groups.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    const wrapper = render(requests); await flushPromises()
    const firstSignal = requests.groups.mock.calls[0]![0]!.signal
    await wrapper.setProps({ refreshKey: 1 }); await flushPromises()
    expect(firstSignal.aborted).toBe(true)
    resolve([{ id: 9, name: 'Stale' }]); await flushPromises()
    expect(wrapper.getComponent(EnterpriseAuditDetail).props('catalog').groups).toEqual([{ id: 2, name: 'Claude' }])
    const currentSignal = requests.groups.mock.calls.at(-1)![0]!.signal
    wrapper.unmount()
    expect(currentSignal.aborted).toBe(true)
  })
})
