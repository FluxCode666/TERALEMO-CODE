import type { EnterpriseQuota, QuotaWindow } from '@/types/enterprise'
export const quotaWindows: QuotaWindow[] = ['daily', 'weekly', 'monthly', 'total']
export type QuotaTone = 'normal' | 'warning' | 'exceeded' | 'disabled' | 'unlimited' | 'hidden'
export function quotaPercentage(usage: number, limit: number): number { return Math.max(0, usage / limit * 100) }
export function quotaCell(quota: EnterpriseQuota, window: QuotaWindow) {
  const limit = quota[`${window}_limit_usd`] ?? null, usage = quota[`${window}_usage_usd`]
  const hidden = quota.usage_visible === false || usage == null || !Number.isFinite(usage)
  const percent = !hidden && limit !== null && limit > 0 ? quotaPercentage(usage!, limit) : null
  const tone: QuotaTone = hidden ? 'hidden' : limit === 0 ? 'disabled' : percent !== null && percent >= 100 ? 'exceeded' : percent !== null && percent >= 80 ? 'warning' : 'normal'
  return { limit, usage, hidden, percent, tone }
}
export function quotaStatus(quota: EnterpriseQuota): { tone: QuotaTone; percent?: number } {
  const cells = quotaWindows.map(window => quotaCell(quota, window))
  if (cells.some(cell => cell.limit === 0)) return { tone: 'disabled' }
  if (cells.every(cell => cell.limit === null)) return { tone: 'unlimited' }
  if (cells.some(cell => cell.limit !== null && cell.hidden)) return { tone: 'hidden' }
  const percent = Math.max(0, ...cells.map(cell => cell.percent ?? 0))
  return { tone: percent >= 100 ? 'exceeded' : percent >= 80 ? 'warning' : 'normal', percent }
}
export const quotaMoney = (amount: number) => `$${new Intl.NumberFormat('en-US', { maximumFractionDigits: 4 }).format(amount)}`
export const quotaPercentLabel = (amount: number) => `${Math.round(amount * 10) / 10}%`
export function quotaLimitBody(values: Record<QuotaWindow, string | number>, fields: QuotaWindow[]) {
  if (fields.some(window => String(values[window]).trim() !== '' && (!Number.isFinite(Number(values[window])) || Number(values[window]) < 0))) return null
  return Object.fromEntries(fields.map(window => [`${window}_limit_usd`, String(values[window]).trim() === '' ? null : Number(values[window])]))
}
export interface QuotaTableRow { id: number; label: string; username?: string; self?: boolean; quota?: EnterpriseQuota; level?: number; hasChildren?: boolean; expanded?: boolean }
