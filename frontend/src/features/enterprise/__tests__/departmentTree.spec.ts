import { describe, expect, it } from 'vitest'
import { buildDepartmentTree, buildOrganizationTree, departmentPath, departmentSubtreeIds, filterOrganizationTree, flattenOrganizationTree } from '../departmentTree'
import { departments, members } from './organizationFixtures'
describe('department tree model', () => {
  it('sorts siblings, builds multiple levels, and keeps unassigned members last', () => {
    const tree = buildDepartmentTree(departments)
    expect(tree.map(node => node.department.id)).toEqual([6, 3])
    expect(tree[1]?.children[0]?.children[0]?.department.id).toBe(5)
    const organization = buildOrganizationTree(departments, members, 'Acme', '未分配部门', '暂无成员')
    expect(organization[0]?.children.at(-1)?.children[0]?.member?.id).toBe(9)
    expect(organization[0]?.children[0]?.children[0]?.kind).toBe('empty')
    expect(departmentPath(departments, 5)).toBe('研发中心 / 后端组 / 平台组')
  })
  it('keeps search ancestors, auto expands them, and matches member email', () => {
    const tree = buildOrganizationTree(departments, members, 'Acme', '未分配部门', '暂无成员')
    const { nodes, expanded } = filterOrganizationTree(tree, 'carol@example.com')
    expect(flattenOrganizationTree(nodes, expanded).map(row => row.node.key)).toEqual(['all', '3', '4', '5', 'member-10'])
    expect(filterOrganizationTree(tree, '后端组').expanded.has('5')).toBe(true)
    expect(filterOrganizationTree(tree, 'unknown').nodes).toEqual([])
  })
  it('blocks the department and all descendants as move targets, and tolerates legacy and cyclic data', () => {
    expect([...departmentSubtreeIds(departments, 3)]).toEqual([3, 4, 5])
    expect(buildDepartmentTree([{ ...departments[0]!, parent_id: undefined }])).toHaveLength(1)
    expect(buildDepartmentTree([{ ...departments[0]!, parent_id: 4 }, { ...departments[1]!, parent_id: 3 }])).toHaveLength(2)
  })
})
