import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, type VueWrapper, flushPromises, mount } from '@vue/test-utils'
import EnterpriseLogsPanel from '../components/EnterpriseLogsPanel.vue'
import EnterpriseAuditDetail from '../components/EnterpriseAuditDetail.vue'
import EnterpriseDeleteDialog from '../components/EnterpriseDeleteDialog.vue'
import type { EnterpriseAuditLog } from '@/types/enterprise'
import type { EnterpriseResourceAPI } from '@/api/enterprise'

const { remove, success } = vi.hoisted(() => ({ remove: vi.fn(), success: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/api/admin/enterprises', () => ({ enterprisesAPI: { remove } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: success, showError: vi.fn() }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => ({ 'enterprise.auditUi.actions.member_update': '更新成员', 'enterprise.auditDetails.fields.status': '状态' }[key] || key), te: (key: string) => key === 'enterprise.auditDetails.fields.status' }) }))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })
beforeEach(() => { vi.clearAllMocks(); HTMLElement.prototype.scrollIntoView = vi.fn() })
const log: EnterpriseAuditLog = { id: 2, actor_user_id: 1, actor_email: 'owner@example.com', actor_role: 'owner', action: 'member.update', target_type: 'member', target_id: 7, created_at: '2026-10-18T01:00:00Z', detail: {} }
async function openDetail(detail: unknown) {
  const auditLogs = vi.fn().mockResolvedValue({ items: [{ ...log, detail }], total: 1 })
  const wrapper = mount(EnterpriseLogsPanel, { attachTo: document.body, props: { api: { auditLogs } as unknown as EnterpriseResourceAPI, kind: 'audit' } })
  wrappers.push(wrapper)
  await flushPromises()
  expect(wrapper.find('details').exists()).toBe(false)
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await wrapper.get('[data-test="audit-detail-2"]').trigger('click')
  await flushPromises()
  return new DOMWrapper(document.querySelector<HTMLElement>('[role="dialog"]')!)
}

describe('企业 shadcn 审计详情', () => {
  it('在弹窗展示动作、操作者、角色、目标、时间及嵌套键值对', async () => {
    const dialog = await openDetail({ status: 'active', context: { reason: 'manual', ids: [1, 2] } })
    expect(dialog.text()).toContain('更新成员')
    expect(dialog.text()).toContain('owner@example.com')
    expect(dialog.text()).toContain('enterprise.roles.owner')
    expect(dialog.text()).toContain('#7')
    expect(dialog.text()).toContain('2026')
    expect(dialog.get('[data-test="audit-detail-fields"]').text()).toContain('状态')
    expect(dialog.text()).toContain('context.reasonmanual')
    expect(dialog.text()).toContain('context.ids')
    expect(dialog.text()).toContain('1,')
  })

  it('将嵌套的修改前后值配对，保留 before/after 之外的附加信息', async () => {
    const dialog = await openDetail({ before: { status: 'active', settings: { visible: false } }, after: { status: 'disabled', settings: { visible: true } }, reason: 'policy' })
    const comparisons = dialog.findAll('[data-test="audit-detail-comparison"]')
    expect(comparisons).toHaveLength(2)
    expect(comparisons[0].findAll('[data-test="audit-value"]').map(item => item.text())).toEqual(['active', 'disabled'])
    expect(comparisons[1].findAll('[data-test="audit-value"]').map(item => item.text())).toEqual(['false', 'true'])
    expect(dialog.text()).toContain('settings.visible')
    expect(dialog.text()).toContain('reasonpolicy')
    expect(dialog.text()).toContain('enterprise.auditDetails.before')
    expect(dialog.text()).toContain('enterprise.auditDetails.after')
  })

  it('未知结构回退为格式化 JSON，并可复制完整原始详情', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const detail = ['unrecognized', { version: 2 }]
    const dialog = await openDetail(detail)
    expect(dialog.get('[data-test="audit-detail-json"]').text()).toBe(JSON.stringify(detail, null, 2))
    await dialog.get('[data-test="audit-detail-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(JSON.stringify(detail, null, 2))
    expect(dialog.text()).toContain('enterprise.auditDetails.copied')
  })

  it.each(['group_pool.update', 'group_whitelist.update'])('将 %s 根层级分组数组映射为名称并标明新增与移除，复制仍保留原始 ID', async action => {
    const detail = { before: [2, 3, 4], after: [2, 3, 5] }
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    wrappers.push(mount(EnterpriseAuditDetail, { attachTo: document.body, props: {
      log: { ...log, action, detail }, actionLabel: action, targetLabel: 'Acme',
      catalog: { groups: [{ id: 2, name: 'Claude' }, { id: 3, name: 'Gemini' }, { id: 5, name: 'GPT' }] }
    } }))
    await flushPromises()
    const dialog = new DOMWrapper(document.querySelector<HTMLElement>('[role="dialog"]')!)
    expect(dialog.text()).toContain('Acme')
    expect(dialog.text()).toContain('Claude')
    expect(dialog.text()).toContain('Gemini')
    expect(dialog.get('[data-change="removed"]').text()).toBe('enterprise.auditDetails.removed ·enterprise.auditDetails.references.group #4')
    expect(dialog.get('[data-change="added"]').text()).toBe('enterprise.auditDetails.added ·GPT')
    expect(dialog.findAll('[data-change]')).toHaveLength(2)
    await dialog.get('[data-test="audit-detail-copy"]').trigger('click'); await flushPromises()
    expect(writeText).toHaveBeenCalledWith(JSON.stringify(detail, null, 2))
  })

  it('映射嵌套 allowed_group_ids、成员和部门字段，未知 ID 结构仍保留 JSON', async () => {
    wrappers.push(mount(EnterpriseAuditDetail, { attachTo: document.body, props: {
      log: { ...log, detail: { before: { allowed_group_ids: [2], department_id: 3 }, after: { allowed_group_ids: [5], department_id: 4 }, member_ids: [7], user_id: 70, group_ids: [{ future: 1 }] } }, actionLabel: '', targetLabel: 'Alice',
      catalog: { groups: [{ id: 2, name: 'Claude' }, { id: 5, name: 'GPT' }], departments: [{ id: 3, name: '研发' }, { id: 4, name: '设计' }], members: [{ id: 7, user_id: 70, username: 'Alice', email: 'alice@example.com' }] }
    } }))
    await flushPromises()
    const dialog = new DOMWrapper(document.querySelector<HTMLElement>('[role="dialog"]')!)
    expect(dialog.text()).toContain('Claude'); expect(dialog.text()).toContain('GPT')
    expect(dialog.text()).toContain('研发'); expect(dialog.text()).toContain('设计')
    expect(dialog.findAll('[data-test="audit-reference-values"]').filter(item => item.text() === 'Alice')).toHaveLength(2)
    expect(dialog.get('pre').text()).toBe(JSON.stringify([{ future: 1 }], null, 2))
  })
})

it('真实 AlertDialog 等待请求结束，删除失败后须再次确认解散，取消不会再请求', async () => {
  let reject!: (reason: unknown) => void
  remove.mockReturnValueOnce(new Promise((_resolve, no) => { reject = no }))
  const wrapper = mount(EnterpriseDeleteDialog, { attachTo: document.body, props: { enterpriseId: 7 } })
  wrappers.push(wrapper)
  await flushPromises()
  const dialog = new DOMWrapper(document.querySelector<HTMLElement>('[role="alertdialog"]')!)
  expect(dialog.text()).toContain('admin.enterprises.confirmDelete')
  await dialog.get('[data-test="enterprise-confirm-action"]').trigger('click')
  expect(dialog.get('[data-test="enterprise-confirm-action"]').attributes('disabled')).toBeDefined()
  expect(dialog.get('[data-test="enterprise-confirm-cancel"]').attributes('disabled')).toBeDefined()
  reject({ code: 'ENTERPRISE_NOT_EMPTY' })
  await flushPromises()
  expect(remove).toHaveBeenCalledTimes(1)
  expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain('admin.enterprises.confirmDissolve')
  expect(wrapper.emitted('deleted')).toBeUndefined()
  await dialog.get('[data-test="enterprise-confirm-cancel"]').trigger('click')
  expect(wrapper.emitted('cancel')).toHaveLength(1)
  expect(remove).toHaveBeenCalledTimes(1)
})
