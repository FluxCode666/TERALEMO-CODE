/**
 * 端点字符串的展示层归一化与聚合。
 *
 * 仅用于渲染：历史数据里端点可能带任务 ID（如
 * /api/v1/admin/async-tasks/1/vidtask_<64hex>/refresh），聚合视图会把每个任务
 * 显示成一行。这里把明显的 ID 路径段归一为 :id 供展示标签使用；
 * 不得把归一化结果作为筛选/查询参数发给后端。
 */

const VIDTASK_SEGMENT = /^vidtask_[A-Za-z0-9]+$/
const UUID_SEGMENT = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/
const LONG_HEX_SEGMENT = /^[0-9a-fA-F]{16,}$/
const NUMERIC_SEGMENT = /^\d+$/

export const NORMALIZED_ID_SEGMENT = ':id'

/** 单个路径段命中已知 ID 形态时归一为 :id，否则原样保留。 */
export function normalizeEndpointSegmentForDisplay(segment: string): string {
  if (
    VIDTASK_SEGMENT.test(segment) ||
    UUID_SEGMENT.test(segment) ||
    LONG_HEX_SEGMENT.test(segment) ||
    NUMERIC_SEGMENT.test(segment)
  ) {
    return NORMALIZED_ID_SEGMENT
  }
  return segment
}

/** 按 / 切段逐段归一；空段、前后斜杠、查询串等其余字符原样保留。 */
export function normalizeEndpointForDisplay(endpoint: string | null | undefined): string {
  if (!endpoint) return ''
  return endpoint.split('/').map(normalizeEndpointSegmentForDisplay).join('/')
}

export interface EndpointStatLike {
  endpoint: string
  requests: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface DisplayEndpointStat extends EndpointStatLike {
  /** 聚合进该展示标签的原始端点（去重，按首次出现顺序），用于 title 与下钻查询。 */
  rawEndpoints: string[]
}

/**
 * 按归一化标签聚合并求和。入参原样顺序中首次出现的标签顺序即输出顺序，
 * 排序由调用方按需进行。
 */
export function aggregateEndpointStatsForDisplay(stats: readonly EndpointStatLike[]): DisplayEndpointStat[] {
  const byLabel = new Map<string, DisplayEndpointStat>()
  for (const stat of stats) {
    const label = normalizeEndpointForDisplay(stat.endpoint)
    const existing = byLabel.get(label)
    if (existing) {
      existing.requests += stat.requests
      existing.total_tokens += stat.total_tokens
      existing.cost += stat.cost
      existing.actual_cost += stat.actual_cost
      if (!existing.rawEndpoints.includes(stat.endpoint)) existing.rawEndpoints.push(stat.endpoint)
    } else {
      byLabel.set(label, {
        endpoint: label,
        requests: stat.requests,
        total_tokens: stat.total_tokens,
        cost: stat.cost,
        actual_cost: stat.actual_cost,
        rawEndpoints: [stat.endpoint],
      })
    }
  }
  return Array.from(byLabel.values())
}
