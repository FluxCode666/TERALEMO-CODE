import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key,
    }),
  }
})

import UserDashboardMemberQuota from '../UserDashboardMemberQuota.vue'
import type { EnterpriseQuota } from '@/types/enterprise'

function quota(overrides: Partial<EnterpriseQuota> = {}): EnterpriseQuota {
  return {
    scope_type: 'member',
    scope_id: 7,
    daily_limit_usd: 10,
    weekly_limit_usd: null,
    monthly_limit_usd: 100,
    total_limit_usd: null,
    daily_usage_usd: 2.5,
    weekly_usage_usd: 8,
    monthly_usage_usd: 96,
    total_usage_usd: 120,
    daily_resets_at: '2030-01-02T00:00:00Z',
    weekly_resets_at: null,
    monthly_resets_at: '2030-02-01T00:00:00Z',
    ...overrides,
  }
}
function mountRow(q: EnterpriseQuota | null) {
  return mount(UserDashboardMemberQuota, { props: { quota: q }, global: { stubs: { Icon: true } } })
}

describe('UserDashboardMemberQuota 成员限额卡片行', () => {
  it('渲染日/周/月/总四张卡片，含已用/上限与进度条', () => {
    const w = mountRow(quota())
    for (const key of ['daily', 'weekly', 'monthly', 'total']) {
      expect(w.get(`[data-testid="member-quota-${key}"]`).text()).toContain(`enterprise.quotaUsage.${key}`)
    }
    expect(w.get('[data-testid="member-quota-daily"]').text()).toContain('$2.50')
    expect(w.get('[data-testid="member-quota-daily"]').text()).toContain('$10.00')
    expect(w.get('[data-testid="member-quota-daily"]').html()).toContain('bg-green-500')
    // 96/100 = 96% → red bar
    expect(w.get('[data-testid="member-quota-monthly"]').html()).toContain('bg-red-500')
    w.unmount()
  })

  it('不限的窗口显示“不限”但仍显示已用额', () => {
    const w = mountRow(quota())
    const weekly = w.get('[data-testid="member-quota-weekly"]')
    expect(weekly.text()).toContain('enterprise.unlimited')
    expect(weekly.text()).toContain('$8.00')
    w.unmount()
  })

  it('日/周/月卡显示重置时间，总卡不显示', () => {
    const w = mountRow(quota({ weekly_resets_at: '2030-01-06T00:00:00Z' }))
    expect(w.get('[data-testid="member-quota-daily"]').text()).toContain('enterprise.quotaResetsAt')
    expect(w.get('[data-testid="member-quota-weekly"]').text()).toContain('enterprise.quotaResetsAt')
    expect(w.get('[data-testid="member-quota-monthly"]').text()).toContain('enterprise.quotaResetsAt')
    expect(w.get('[data-testid="member-quota-total"]').text()).not.toContain('enterprise.quotaResetsAt')
    w.unmount()
  })

  it('没有限额记录时四档都按不限/零用量渲染', () => {
    const w = mountRow(null)
    expect(w.get('[data-testid="member-quota-daily"]').text()).toContain('$0.00')
    expect(w.get('[data-testid="member-quota-total"]').text()).toContain('enterprise.unlimited')
    w.unmount()
  })

  it('上限为 0 视为禁用：整条进度条红色', () => {
    const w = mountRow(quota({ daily_limit_usd: 0 }))
    expect(w.get('[data-testid="member-quota-daily"]').html()).toContain('bg-red-500')
    expect(w.get('[data-testid="member-quota-daily"]').text()).toContain('enterprise.quotaDisabled')
    w.unmount()
  })
})
