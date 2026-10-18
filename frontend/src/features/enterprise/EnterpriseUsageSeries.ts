import type { EnterpriseDepartment, EnterpriseQuery, EnterpriseUsageTrend } from '@/types/enterprise'
import { usageNumber } from './useEnterpriseUsage'

export type UsageMetric = 'actual_cost' | 'tokens' | 'requests'
export type UsageView = 'share' | 'trend'
export interface UsageSeriesPoint extends EnterpriseUsageTrend { id: string | number; label: string }
export interface UsageDepartment extends EnterpriseDepartment {
  parent_id?: number | null; path_ids?: number[]; path_names?: string[]; depth?: number; total_member_count?: number
}
export interface UsageDepartmentNode { department: UsageDepartment; children: UsageDepartmentNode[] }

/** An unbounded range cannot safely be requested at hourly resolution. Dates are inclusive. */
export function usageAllowsHourly(query: Pick<EnterpriseQuery, 'start_date' | 'end_date'>) {
  if (!query.start_date || !query.end_date) return false
  const days = (Date.parse(query.end_date) - Date.parse(query.start_date)) / 86400000 + 1
  return Number.isFinite(days) && days >= 1 && days <= 31
}

export function usageDepartmentTree(departments: UsageDepartment[], rootId?: number | null): UsageDepartmentNode[] {
  const byId = new Map(departments.map(item => [item.id, item]))
  function withinRoot(item: UsageDepartment) {
    if (rootId === undefined) return true
    if (rootId === null) return false
    const visited = new Set<number>()
    let current: UsageDepartment | undefined = item
    while (current && !visited.has(current.id)) {
      if (current.id === rootId || current.path_ids?.includes(rootId)) return true
      visited.add(current.id)
      current = byId.get(current.parent_id ?? current.path_ids?.at(-2) ?? 0)
    }
    return false
  }
  const allowed = departments.filter(withinRoot)
  const nodes = new Map(allowed.map(item => [item.id, { department: item, children: [] } as UsageDepartmentNode]))
  const roots: UsageDepartmentNode[] = []
  for (const item of allowed) {
    const node = nodes.get(item.id)!
    const parentId = item.parent_id ?? item.path_ids?.at(-2)
    const parent = parentId ? nodes.get(parentId) : undefined
    // Malformed cycles must not hide the entire selector or recurse forever.
    const seen = new Set([item.id])
    let ancestor = parent
    while (ancestor && !seen.has(ancestor.department.id)) {
      seen.add(ancestor.department.id)
      ancestor = nodes.get(ancestor.department.parent_id ?? ancestor.department.path_ids?.at(-2) ?? 0)
    }
    if (parent && !ancestor) parent.children.push(node)
    else roots.push(node)
  }
  function sort(nodes: UsageDepartmentNode[]) {
    nodes.sort((a, b) => (a.department.sort_order ?? 0) - (b.department.sort_order ?? 0) || a.department.id - b.department.id)
    nodes.forEach(node => sort(node.children))
  }
  sort(roots)
  return roots
}

/** Rank across the whole interval by cost, preserving identity even for duplicate names. */
export function usageStackedSeries(points: UsageSeriesPoint[], otherLabel: string, top = 8) {
  const totals = new Map<string | number, { cost: number; label: string }>()
  for (const point of points) {
    if (point.label === '__other__') continue
    const item = totals.get(point.id) ?? { cost: 0, label: point.label }
    item.cost += usageNumber(point.actual_cost)
    totals.set(point.id, item)
  }
  const leaders = [...totals].sort((a, b) => b[1].cost - a[1].cost).slice(0, top)
  const leaderIds = new Set(leaders.map(([id]) => id))
  const dates = [...new Set(points.map(point => point.date))].sort()
  const series = leaders.map(([id, item]) => ({ id, label: item.label, buckets: new Map<string, EnterpriseUsageTrend>() }))
  const other = { id: '__other__', label: otherLabel, buckets: new Map<string, EnterpriseUsageTrend>() }
  const byId = new Map<string | number, typeof other | (typeof series)[number]>(series.map(item => [item.id, item]))
  for (const point of points) {
    const item = point.label !== '__other__' && leaderIds.has(point.id) ? byId.get(point.id)! : other
    const bucket = item.buckets.get(point.date) ?? { date: point.date, requests: 0, total_tokens: 0, actual_cost: 0 }
    bucket.requests += usageNumber(point.requests)
    bucket.total_tokens += usageNumber(point.total_tokens)
    bucket.actual_cost += usageNumber(point.actual_cost)
    item.buckets.set(point.date, bucket)
  }
  if (other.buckets.size) series.push(other)
  return { dates, series: series.map(item => ({ id: item.id, label: item.label, points: dates.map(date => item.buckets.get(date) ?? { date, requests: 0, total_tokens: 0, actual_cost: 0 }) })) }
}

export const usageMetricValue = (point: Pick<EnterpriseUsageTrend, 'requests' | 'total_tokens' | 'actual_cost'>, metric: UsageMetric) => usageNumber(point[metric === 'tokens' ? 'total_tokens' : metric])
