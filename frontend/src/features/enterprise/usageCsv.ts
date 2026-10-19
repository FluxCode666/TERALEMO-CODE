import type { Column } from '@/components/common/types'
import type { EnterpriseDepartment, EnterpriseQuery, EnterpriseUsageLog } from '@/types/enterprise'
import { formatDateTime, formatReasoningEffort } from '@/utils/format'
import { getBillingModeLabel, getDisplayBillingMode } from '@/utils/billingMode'
import { formatUsageOutputRate } from '@/utils/latencyHealth'
import { resolveUsageRequestType } from '@/utils/usageRequestType'
import { normalizeEnterpriseUsage } from './useEnterpriseUsage'

// enterprisePagination -> response.ParsePagination permits at most 1000 rows.
export const enterpriseUsageExportPageSize = 1000
export const enterpriseUsageExportLimit = 50_000

export function escapeCsvCell(value: unknown): string {
  let text = value == null ? '' : String(value)
  if (typeof value === 'string' && /^[=+\-@\t\r]/.test(text)) text = `'${text}`
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text
}

type Translate = (key: string) => string
// Expand the table's composite cells into precise numeric CSV columns.
const metrics = {
  tokens: [
    ['input_tokens', 'admin.usage.inputTokens'], ['output_tokens', 'admin.usage.outputTokens'],
    ['cache_read_tokens', 'admin.usage.cacheReadTokens'], ['cache_creation_tokens', 'admin.usage.cacheCreationTokens'],
    ['cache_creation_5m_tokens', 'enterprise.usageAnalysis.csv.cache5m'], ['cache_creation_1h_tokens', 'enterprise.usageAnalysis.csv.cache1h'],
    ['image_input_tokens', 'enterprise.usageAnalysis.csv.imageInputTokens'], ['image_output_tokens', 'enterprise.usageAnalysis.csv.imageOutputTokens'],
    ['image_count', 'usage.imageCount']
  ],
  cost: [
    ['input_cost', 'admin.usage.inputCost'], ['output_cost', 'admin.usage.outputCost'],
    ['cache_read_cost', 'admin.usage.cacheReadCost'], ['cache_creation_cost', 'admin.usage.cacheCreationCost'],
    ['image_input_cost', 'enterprise.usageAnalysis.csv.imageInputCost'], ['image_output_cost', 'enterprise.usageAnalysis.csv.imageOutputCost'],
    ['total_cost', 'usage.standardCost'], ['rate_multiplier', 'enterprise.usageAnalysis.csv.rateMultiplier'], ['actual_cost', 'usage.actualCost']
  ],
  latency: [['first_token_ms', 'enterprise.usageAnalysis.csv.firstTokenMs'], ['duration_ms', 'enterprise.usageAnalysis.csv.durationMs'], ['output_tps', 'usage.outputTps']]
} as const

export function buildEnterpriseUsageCsv(rows: EnterpriseUsageLog[], columns: Pick<Column, 'key' | 'label' | 'formatter'>[], departments: EnterpriseDepartment[], t: Translate): string {
  const expanded = columns.flatMap(column => column.key in metrics
    ? metrics[column.key as keyof typeof metrics].map(([key, label]) => ({ key, label: t(label) }))
    : [column])
  const lines = [expanded.map(column => escapeCsvCell(column.label)).join(',')]
  for (const source of rows) {
    const row = normalizeEnterpriseUsage(source, departments, t('enterprise.noDepartment'))
    const values: Record<string, unknown> = {
      ...row, created_at: formatDateTime(row.created_at), api_key: row.api_key?.name || '—', group: row.group?.name || '—',
      reasoning_effort: formatReasoningEffort(row.reasoning_effort), endpoint: row.inbound_endpoint || '—',
      stream: t(`usage.${({ ws_v2: 'ws' } as Record<string, string>)[resolveUsageRequestType(row)] ?? resolveUsageRequestType(row)}`),
      billing_mode: getBillingModeLabel(getDisplayBillingMode(row), t),
      output_tps: formatUsageOutputRate(row) === '—' ? null : row.output_tokens * 1000 / row.duration_ms!
    }
    lines.push(expanded.map(column => escapeCsvCell('formatter' in column && column.formatter ? column.formatter(values[column.key], row) : values[column.key])).join(','))
  }
  return '\uFEFF' + lines.join('\r\n')
}

export function enterpriseUsageCsvFilename(name: string, query: EnterpriseQuery): string {
  // eslint-disable-next-line no-control-regex -- Strip filename control characters as well as reserved punctuation.
  const safe = (text: string) => text.replace(/[<>:"/\\|?*\u0000-\u001f\u007f]/g, '_').replace(/[. ]+$/g, '').slice(0, 50) || 'enterprise'
  const time = (timestamp?: string, date?: string) => {
    if (!timestamp) return date || 'all'
    const value = new Date(timestamp), pad = (part: number) => String(part).padStart(2, '0')
    return `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())}_${pad(value.getHours())}-${pad(value.getMinutes())}-${pad(value.getSeconds())}`
  }
  return `${safe(name)}_${safe(time(query.start_time, query.start_date))}_${safe(time(query.end_time, query.end_date))}.csv`
}
