import type { EnterpriseDepartment, EnterpriseMember } from '@/types/enterprise'

export interface DepartmentNode { department: EnterpriseDepartment; children: DepartmentNode[] }
export interface OrganizationNode {
  key: string
  name: string
  kind: 'root' | 'department' | 'member' | 'unassigned' | 'empty'
  count?: number
  department?: EnterpriseDepartment
  member?: EnterpriseMember
  children: OrganizationNode[]
}
export function buildDepartmentTree(departments: EnterpriseDepartment[]): DepartmentNode[] {
  const map = new Map(departments.map(department => [department.id, { department, children: [] } as DepartmentNode]))
  const roots: DepartmentNode[] = []
  for (const node of map.values()) {
    const parent = map.get(node.department.parent_id ?? 0)
    // Malformed or older responses must still render, without recursing forever.
    const visited = new Set([node.department.id])
    let cursor = parent
    while (cursor && !visited.has(cursor.department.id)) { visited.add(cursor.department.id); cursor = map.get(cursor.department.parent_id ?? 0) }
    if (parent && !cursor) parent.children.push(node)
    else roots.push(node)
  }
  const sort = (nodes: DepartmentNode[]) => {
    nodes.sort((a, b) => (a.department.sort_order ?? 0) - (b.department.sort_order ?? 0) || a.department.id - b.department.id)
    nodes.forEach(node => sort(node.children))
  }
  sort(roots)
  return roots
}
export function departmentSubtreeIds(departments: EnterpriseDepartment[], id: number | null | undefined): Set<number> {
  const ids = new Set<number>()
  if (!id) return ids
  ids.add(id)
  let changed = true
  while (changed) {
    changed = false
    for (const department of departments) if (department.parent_id && ids.has(department.parent_id) && !ids.has(department.id)) { ids.add(department.id); changed = true }
  }
  return ids
}
export function departmentAncestors(departments: EnterpriseDepartment[], id: number): EnterpriseDepartment[] {
  const result: EnterpriseDepartment[] = [], seen = new Set<number>()
  let current = departments.find(row => row.id === id)
  while (current && !seen.has(current.id)) { seen.add(current.id); result.unshift(current); current = departments.find(row => row.id === current?.parent_id) }
  return result
}
export function departmentPath(departments: EnterpriseDepartment[], id: number): string {
  const department = departments.find(row => row.id === id)
  return department?.path_names?.join(' / ') || departmentAncestors(departments, id).map(row => row.name).join(' / ')
}
export function departmentTreeNodes(departments: EnterpriseDepartment[]): OrganizationNode[] {
  const convert = (node: DepartmentNode): OrganizationNode => ({ key: String(node.department.id), name: node.department.name, kind: 'department', department: node.department, children: node.children.map(convert) })
  return buildDepartmentTree(departments).map(convert)
}
export function buildOrganizationTree(departments: EnterpriseDepartment[], members: EnterpriseMember[], name: string, unassigned: string, empty: string, total?: number): OrganizationNode[] {
  const leaves = (rows: EnterpriseMember[]): OrganizationNode[] => [...rows].sort((a, b) => (a.username || a.email).localeCompare(b.username || b.email) || a.id - b.id).map(member => ({ key: `member-${member.id}`, name: member.username || member.email, kind: 'member', member, children: [] }))
  const convert = (node: DepartmentNode): OrganizationNode => {
    const children = node.children.map(convert)
    const directMembers = leaves(members.filter(member => member.department_id === node.department.id))
    return { key: String(node.department.id), name: node.department.name, kind: 'department', department: node.department, count: node.department.total_member_count ?? ((node.department.member_count ?? directMembers.length) + children.reduce((sum, child) => sum + (child.count ?? 0), 0)), children: [...children, ...directMembers, ...(!directMembers.length ? [{ key: `empty-${node.department.id}`, name: empty, kind: 'empty' as const, children: [] }] : [])] }
  }
  const nodes = buildDepartmentTree(departments).map(convert)
  const direct = leaves(members.filter(member => !member.department_id))
  return [{ key: 'all', kind: 'root', name, count: total ?? members.length, children: [...nodes, { key: '0', kind: 'unassigned', name: unassigned, count: direct.length, children: direct.length ? direct : [{ key: 'empty-0', kind: 'empty', name: empty, children: [] }] }] }]
}
export function filterOrganizationTree(nodes: OrganizationNode[], query: string): { nodes: OrganizationNode[]; expanded: Set<string> } {
  const needle = query.trim().toLocaleLowerCase(), expanded = new Set<string>()
  if (!needle) return { nodes, expanded }
  const visit = (items: OrganizationNode[], inherited = false): OrganizationNode[] => items.flatMap(node => {
    const matches = node.kind !== 'empty' && `${node.name} ${node.member?.email ?? ''} ${node.member?.title ?? ''}`.toLocaleLowerCase().includes(needle)
    const children = visit(node.children, inherited || matches)
    if (!inherited && !matches && !children.length) return []
    if (children.length) expanded.add(node.key)
    return [{ ...node, children }]
  })
  return { nodes: visit(nodes), expanded }
}
export function flattenOrganizationTree(nodes: OrganizationNode[], expanded: Set<string>, level = 1, parent?: string): { node: OrganizationNode; level: number; parent?: string }[] {
  return nodes.flatMap(node => [{ node, level, parent }, ...(expanded.has(node.key) ? flattenOrganizationTree(node.children, expanded, level + 1, node.key) : [])])
}
