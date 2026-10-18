import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({ auth: { user: { enterprise: null as { id: number } | null } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: true, refreshBatchImageAccess: vi.fn() }),
}))

import UserDashboardQuickActions from '../UserDashboardQuickActions.vue'

describe('企业成员快捷入口', () => {
  it('隐藏余额兑换和批量图片，保留 API 密钥与使用记录', () => {
    mocks.auth.user.enterprise = { id: 1 }
    const wrapper = mount(UserDashboardQuickActions, { global: { stubs: { Icon: true } } })
    expect(wrapper.text()).not.toContain('dashboard.redeemCode')
    expect(wrapper.text()).not.toContain('dashboard.batchImageAgent')
    expect(wrapper.text()).toContain('dashboard.createApiKey')
    expect(wrapper.text()).toContain('dashboard.viewUsage')
  })

  it('非企业成员继续显示兑换和批量图片', () => {
    mocks.auth.user.enterprise = null
    const wrapper = mount(UserDashboardQuickActions, { global: { stubs: { Icon: true } } })
    expect(wrapper.text()).toContain('dashboard.redeemCode')
    expect(wrapper.text()).toContain('dashboard.batchImageAgent')
  })
})
