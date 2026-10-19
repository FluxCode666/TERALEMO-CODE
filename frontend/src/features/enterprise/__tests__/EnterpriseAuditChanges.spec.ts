import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import AuditDetail from '../components/EnterpriseAuditDetail.vue'
import { auditValuesEqual } from '../auditReferences'
import type { EnterpriseAuditLog } from '@/types/enterprise'
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
function render(detail: Record<string, unknown>, action = 'member.update') {
  const wrapper = mount(AuditDetail, { props: { log: { id: 1, detail, action, actor_role: 'admin', target_type: 'member', target_id: 1, created_at: '' } as EnterpriseAuditLog, actionLabel: action, targetLabel: 'Alice' }, global: { stubs: { EnterpriseDialog: { template: '<div><slot /></div>' } } } })
  wrappers.push(wrapper); return wrapper
}
describe('审计变化高亮', () => {
  it.each([['before', 'after'], ['old', 'new'], ['from', 'to'], ['old_value', 'new_value']])('展开 %s/%s 快照，仅变化的行高亮并附文字标签', (left, right) => {
    const wrapper = render({ [left]: { name: 'Alice', settings: { enabled: true }, department_id: 1 }, [right]: { name: 'Alice', settings: { enabled: false }, department_id: 2 }, operator: 'admin' })
    expect(wrapper.get('[data-field="name"]').attributes('data-changed')).toBe('false')
    for (const field of ['settings.enabled', 'department_id']) {
      const row = wrapper.get(`[data-field="${field}"]`)
      expect(row.attributes('data-changed')).toBe('true')
      expect(row.classes()).toEqual(expect.arrayContaining(['bg-amber-50', 'dark:bg-amber-950/40', 'border-l-amber-500']))
      expect(row.text()).toContain('enterprise.auditDetails.changed')
    }
    expect(wrapper.get('[data-field="operator"]').attributes('data-changed')).toBeUndefined()
  })
  it('引用数组按集合比较，普通数组保持顺序，新增删除与 null 区分', () => {
    const wrapper = render({ before: { group_ids: [1, 2], member_ids: [3, 4], department_ids: [5, 6], values: [1, 2], removed: null, same: { b: 2, a: 1 } }, after: { group_ids: [2, '1', 1], member_ids: [4, 3], department_ids: [6, 5], values: [2, 1], added: null, same: { a: 1, b: 2 } } })
    for (const field of ['group_ids', 'member_ids', 'department_ids', 'same.a', 'same.b']) expect(wrapper.get(`[data-field="${field}"]`).attributes('data-changed')).toBe('false')
    for (const field of ['removed', 'added', 'values']) expect(wrapper.get(`[data-field="${field}"]`).attributes('data-changed')).toBe('true')
  })
  it.each([{ before: { name: 'Old', group_ids: [1] } }, { after: { name: 'New', group_ids: [1] } }, { before: null, after: { name: 'New', group_ids: [1] } }, { before: { name: 'Old' }, after: null }])('缺失一侧或整对象新增/删除仍高亮 %j', detail => {
    const wrapper = render(detail)
    expect(wrapper.findAll('[data-changed="true"]').length).toBeGreaterThan(0)
    expect(wrapper.findAll('[data-changed="false"]')).toHaveLength(0)
  })
  it('真实 group_pool.update 数组快照与非 comparison 数据', () => {
    expect(render({ before: [1, 2], after: [2, 1] }, 'group_pool.update').get('[data-field="group_ids"]').attributes('data-changed')).toBe('false')
    expect(render({ before: [1], after: [1, 2] }, 'group_pool.update').find('[data-changed="true"]').exists()).toBe(true)
    expect(render({ name: 'Alice', group_ids: [1] }).find('[data-changed]').exists()).toBe(false)
  })
  it('深比较忽略对象键顺序，验证嵌套数组和引用数组', () => {
    expect(auditValuesEqual('', { x: [{ a: 1, b: 2 }], group_ids: [1, 2] }, { group_ids: [2, 1], x: [{ b: 2, a: 1 }] })).toBe(true)
    expect(auditValuesEqual('', { x: [1, 2] }, { x: [2, 1] })).toBe(false)
    expect(auditValuesEqual('group_ids', [1], [2])).toBe(false)
    expect(auditValuesEqual('group_ids', [{ a: 1 }, { a: 2 }], [{ a: 2 }, { a: 1 }])).toBe(false)
    expect(auditValuesEqual('', {}, [])).toBe(false)
    expect(auditValuesEqual('', undefined, null)).toBe(false)
  })
})
