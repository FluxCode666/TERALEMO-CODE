import type { EnterpriseDepartment, EnterpriseMember } from '@/types/enterprise'
export const departments: EnterpriseDepartment[] = [
  { id: 3, enterprise_id: 1, name: '研发中心', parent_id: null, sort_order: 2, member_count: 1, total_member_count: 3, created_at: '' },
  { id: 4, enterprise_id: 1, name: '后端组', parent_id: 3, sort_order: 1, member_count: 1, total_member_count: 2, created_at: '' },
  { id: 5, enterprise_id: 1, name: '平台组', parent_id: 4, sort_order: 0, member_count: 1, created_at: '' },
  { id: 6, enterprise_id: 1, name: '设计部', parent_id: null, sort_order: 1, member_count: 0, created_at: '' }
]
export const members: EnterpriseMember[] = [
  { id: 7, enterprise_id: 1, user_id: 7, username: 'Alice', email: 'alice@example.com', department_id: 3, department_name: '研发中心', role: 'manager', status: 'active', user_status: 'active', title: '主管', joined_at: '2026-10-01T00:00:00Z', last_login_at: '2026-10-18T00:00:00Z' },
  { id: 8, enterprise_id: 1, user_id: 8, username: 'Bob', email: 'bob@example.com', department_id: 4, department_name: '后端组', department_path_names: ['研发中心', '后端组'], role: 'member', status: 'active', user_status: 'active', title: '工程师', joined_at: '', last_login_at: null },
  { id: 9, enterprise_id: 1, user_id: 9, username: 'Owner', email: 'owner@example.com', department_id: null, department_name: null, role: 'owner', status: 'active', user_status: 'active', title: '', joined_at: '' },
  { id: 10, enterprise_id: 1, user_id: 10, username: 'Carol', email: 'carol@example.com', department_id: 5, department_name: '平台组', role: 'member', status: 'active', user_status: 'active', title: '', joined_at: '' }
]
