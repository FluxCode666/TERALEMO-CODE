import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { EnterpriseOverview } from '@/types/enterprise'
import EnterpriseView from '../EnterpriseView.vue'

const { overview, refreshUser } = vi.hoisted(() => ({ overview: vi.fn(), refreshUser: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: {} }))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' }
}))

vi.mock('@/api/enterprise', async () => {
  const actual = await vi.importActual<typeof import('@/api/enterprise')>('@/api/enterprise')
  return { ...actual, enterpriseAPI: { overview } }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ refreshUser })
}))

vi.mock('@/features/enterprise/EnterpriseWorkspace.vue', () => ({
  default: {
    name: 'EnterpriseWorkspace',
    props: ['overview'],
    emits: ['updated'],
    template: '<div data-test="workspace"><span>{{ overview.enterprise.name }}</span><button @click="$emit(\'updated\')">updated</button></div>'
  }
}))

const memberOverview: EnterpriseOverview = {
  enterprise: { id: 1, name: 'Acme', status: 'active' },
  me: { member_id: 7, user_id: 42, role: 'member', status: 'active', department_id: 3, department_name: '研发部', title: '' },
  balance_visible: false,
  balance: null,
  enterprise_quota: null,
  department_quota_visible: false,
  department_quota: null,
  member_quota_visible: false,
  member_quota: null,
  settings: null,
  permissions: {
    manage_departments: false, manage_members: false, create_members: false,
    edit_member_quota: false, edit_member_groups: false, edit_quotas: false,
    edit_settings: false, view_members: false, view_audit_logs: false,
    view_department_usage: false
  },
  stats: { today_cost: 0, month_cost: 0 }
}

function mountView() {
  return mount(EnterpriseView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        LoadingSpinner: true
      }
    }
  })
}

describe('EnterpriseView overview lifecycle', () => {
  beforeEach(() => {
    overview.mockReset()
    overview.mockResolvedValue(memberOverview)
    refreshUser.mockReset()
    refreshUser.mockResolvedValue(undefined)
  })

  it('loads the dedicated overview endpoint and passes server permissions and nulls to the workspace', async () => {
    const wrapper = mountView()
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(true)
    await flushPromises()

    expect(overview).toHaveBeenCalledTimes(1)
    expect(refreshUser).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="workspace"]').text()).toContain('Acme')
    expect(wrapper.findComponent({ name: 'EnterpriseWorkspace' }).props('overview')).toEqual(memberOverview)
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the translated enterprise error and retries without rendering stale privileged content', async () => {
    overview.mockRejectedValueOnce({ code: 'ENTERPRISE_NOT_MEMBER' })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toContain('enterprise.errors.ENTERPRISE_NOT_MEMBER')
    expect(wrapper.find('[data-test="workspace"]').exists()).toBe(false)
    await wrapper.find('[role="alert"] button').trigger('click')
    await flushPromises()

    expect(overview).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="workspace"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('refreshes overview after a workspace mutation changes server permissions', async () => {
    overview.mockResolvedValueOnce(memberOverview).mockResolvedValueOnce({
      ...memberOverview,
      enterprise: { ...memberOverview.enterprise, name: 'Updated enterprise' }
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-test="workspace"] button').trigger('click')
    await flushPromises()

    expect(overview).toHaveBeenCalledTimes(2)
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Updated enterprise')
    wrapper.unmount()
  })
})
