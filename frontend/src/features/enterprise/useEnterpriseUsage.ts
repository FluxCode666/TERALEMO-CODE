import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import type { AdminUsageLog } from '@/types'
import type { EnterpriseDepartment, EnterpriseUsageLog } from '@/types/enterprise'
import type { Column } from '@/components/common/types'

export const usageNumber = (value: unknown): number => typeof value === 'number' && Number.isFinite(value) ? value : 0
export const usageMoney = (value: unknown) => `$${usageNumber(value).toFixed(4)}`
export const usageColors = ['#14b8a6', '#3b82f6', '#f59e0b', '#8b5cf6', '#ec4899', '#06b6d4', '#f97316', '#84cc16']
export type UsagePeriod = 'today' | 'last7' | 'last30' | 'month'
export function usageDateRange(period: UsagePeriod, now = new Date()) {
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  if (period === 'month') start.setDate(1)
  if (period === 'last7') start.setDate(start.getDate() - 6)
  if (period === 'last30') start.setDate(start.getDate() - 29)
  const date = (value: Date) => `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`
  return { start_date: date(start), end_date: date(now) }
}

// Adapt the enterprise's nested summaries to the shared personal UsageTable.
// Preserve provided metrics and null latency; default missing scalar fields.
export function normalizeEnterpriseUsage(row: EnterpriseUsageLog, departments: EnterpriseDepartment[], noDepartment: string): AdminUsageLog & { member: string; department_name: string; media_type: string } {
  const numericFields = ['input_tokens', 'output_tokens', 'cache_creation_tokens', 'cache_read_tokens', 'cache_creation_5m_tokens', 'cache_creation_1h_tokens', 'input_cost', 'output_cost', 'cache_creation_cost', 'cache_read_cost', 'total_cost', 'actual_cost', 'image_count', 'image_input_tokens', 'image_input_cost', 'image_output_tokens', 'image_output_cost'] as const
  const numbers = Object.fromEntries(numericFields.map(key => [key, usageNumber(row[key])]))
  return {
    ...row, ...numbers,
    user_id: row.user?.id ?? row.user_id ?? 0,
    api_key_id: row.api_key?.id ?? row.api_key_id ?? 0,
    account_id: row.account_id ?? null, group_id: row.group?.id ?? row.group_id ?? null, subscription_id: row.subscription_id ?? null,
    request_id: row.request_id ?? '', rate_multiplier: row.rate_multiplier ?? 1,
    duration_ms: row.duration_ms ?? null, first_token_ms: row.first_token_ms ?? null,
    stream: row.stream ?? false, native_compaction_v2: row.native_compaction_v2 ?? false,
    long_context_billing_applied: row.long_context_billing_applied ?? false,
    cache_ttl_overridden: row.cache_ttl_overridden ?? false,
    member: [row.user?.username, row.user?.email].filter(Boolean).join(' · ') || (row.user_id ? `#${row.user_id}` : '—'),
    department_name: row.department_name || departments.find(item => item.id === row.department_id)?.name || (row.department_id ? `#${row.department_id}` : noDepartment),
    media_type: row.media_type || '—', user_agent: row.user_agent ?? null,
    image_size: row.image_size ?? null, image_input_size: row.image_input_size ?? null,
    image_output_size: row.image_output_size ?? null, image_size_source: row.image_size_source ?? null,
    image_size_breakdown: row.image_size_breakdown ?? null
  } as AdminUsageLog & { member: string; department_name: string; media_type: string }
}

export function useEnterpriseUsageColumns(admin: Ref<boolean>, columns: Ref<Column[]>) {
  const hidden = ref<string[]>([])
  const storageKey = computed(() => `${admin.value ? 'admin-enterprise' : 'enterprise'}-usage-hidden-columns`)
  const defaults = ['user_agent', 'ip_address', 'request_id', 'service_tier', 'billing_type', 'media_type']
  watch(storageKey, key => {
    try {
      const saved: unknown = JSON.parse(localStorage.getItem(key) ?? 'null')
      hidden.value = Array.isArray(saved) && saved.every(value => typeof value === 'string') ? saved.filter(value => value !== 'created_at') : [...defaults]
    } catch { hidden.value = [...defaults] }
  }, { immediate: true })
  function toggle(key: string) {
    if (key === 'created_at') return
    hidden.value = hidden.value.includes(key) ? hidden.value.filter(item => item !== key) : [...hidden.value, key]
    try { localStorage.setItem(storageKey.value, JSON.stringify(hidden.value)) } catch { /* Keep in-memory preferences when storage is unavailable. */ }
  }
  return { hidden, toggle, visible: computed(() => columns.value.filter(column => column.key === 'created_at' || !hidden.value.includes(column.key))) }
}

export function useEnterpriseUsageTheme() {
  const dark = ref(document.documentElement.classList.contains('dark'))
  const observer = new MutationObserver(() => { dark.value = document.documentElement.classList.contains('dark') })
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  onScopeDispose(() => observer.disconnect())
  return computed(() => ({ text: dark.value ? '#cbd5e1' : '#475569', grid: dark.value ? 'rgba(148,163,184,0.15)' : '#e5e7eb' }))
}
