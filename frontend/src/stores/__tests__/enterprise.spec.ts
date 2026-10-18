import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { flushPromises } from '@vue/test-utils'
import type { EnterpriseOverview } from '@/types/enterprise'
import { useEnterpriseStore } from '../enterprise'

const mocks = vi.hoisted(() => ({
  overview: vi.fn(),
  auth: { user: null as null | { id: number; role: string; enterprise: { id: number; role: string } | null }, isAdmin: false }
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => reactive(mocks.auth) }))
vi.mock('@/api/enterprise', () => ({ enterpriseAPI: { overview: mocks.overview } }))

const value = {
  enterprise: { id: 1, name: 'Acme', status: 'active' },
  permissions: { edit_quotas: false, edit_member_quota: false, view_audit_logs: false, edit_settings: false },
  enterprise_quota: null, department_quota_visible: false, department_quota: null,
  member_quota_visible: false, member_quota: null
} as EnterpriseOverview

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}

beforeEach(() => {
  setActivePinia(createPinia())
  mocks.auth.user = { id: 42, role: 'user', enterprise: { id: 1, role: 'owner' } }
  mocks.auth.isAdmin = false
  mocks.overview.mockReset().mockResolvedValue(value)
})

afterEach(() => { useEnterpriseStore().$dispose() })

describe('enterprise overview cache', () => {
  it('按需加载，侧边栏、守卫和页面共享进行中的请求与缓存', async () => {
    const request = deferred<EnterpriseOverview>()
    mocks.overview.mockReturnValue(request.promise)
    const store = useEnterpriseStore()
    expect(mocks.overview).not.toHaveBeenCalled()
    const first = store.ensureLoaded()
    const second = store.ensureLoaded()
    expect(mocks.overview).toHaveBeenCalledTimes(1)
    request.resolve(value)
    expect(await first).toBe(value)
    expect(await second).toBe(value)
    await store.ensureLoaded()
    expect(mocks.overview).toHaveBeenCalledTimes(1)
    expect(store.tabs).toEqual(['overview', 'organization', 'groups', 'usage'])
  })

  it('刷新用户后失效重载，同企业保留当前面板数据直到新数据就绪', async () => {
    const store = useEnterpriseStore()
    await store.ensureLoaded()
    const request = deferred<EnterpriseOverview>()
    mocks.overview.mockReturnValue(request.promise)
    reactive(mocks.auth).user = { ...mocks.auth.user! }
    expect(store.stale).toBe(true)
    expect(store.overview).toBe(value)
    request.resolve({ ...value, permissions: { ...value.permissions, edit_settings: true } })
    await store.ensureLoaded()
    expect(store.tabs).toContain('settings')
    expect(mocks.overview).toHaveBeenCalledTimes(2)
  })

  it('退出时清空权限并取消请求，重新登录不复用上一个账号的缓存', async () => {
    const old = deferred<EnterpriseOverview>()
    mocks.overview.mockReturnValueOnce(old.promise)
    const store = useEnterpriseStore()
    const first = store.ensureLoaded()
    const signal = mocks.overview.mock.calls[0][0].signal as AbortSignal
    reactive(mocks.auth).user = null
    expect(signal.aborted).toBe(true)
    expect(store.overview).toBe(null)
    expect(store.tabs).toEqual([])
    old.resolve(value)
    await first
    expect(store.overview).toBe(null)
    reactive(mocks.auth).user = { id: 99, role: 'user', enterprise: { id: 2, role: 'manager' } }
    await store.ensureLoaded()
    expect(mocks.overview).toHaveBeenCalledTimes(2)
  })

  it('企业数据更新后重载，忽略不遵守取消信号的过期响应', async () => {
    const old = deferred<EnterpriseOverview>()
    mocks.overview.mockReturnValueOnce(old.promise)
    const store = useEnterpriseStore()
    const first = store.ensureLoaded()
    const fresh = { ...value, enterprise: { ...value.enterprise, name: 'Latest' } }
    mocks.overview.mockResolvedValueOnce(fresh)
    store.invalidate()
    await store.ensureLoaded()
    old.resolve(value)
    await first
    expect(store.overview).toBe(fresh)
    expect(store.loading).toBe(false)
  })

  it.each(['member', null])('企业角色 %s 不请求 overview；降权立即清除旧数据', async role => {
    const store = useEnterpriseStore()
    await store.ensureLoaded()
    reactive(mocks.auth).user!.enterprise = role ? { id: 1, role } : null
    await store.ensureLoaded()
    expect(store.canAccess).toBe(false)
    expect(store.overview).toBe(null)
    expect(mocks.overview).toHaveBeenCalledTimes(1)
  })

  it('首次加载失败可重试，失败不缓存为成功', async () => {
    mocks.overview.mockRejectedValueOnce(new Error('unavailable'))
    const store = useEnterpriseStore()
    await store.ensureLoaded()
    expect(store.error).toBeInstanceOf(Error)
    expect(store.overview).toBe(null)
    await store.ensureLoaded()
    expect(store.error).toBe(null)
    expect(store.overview).toBe(value)
  })

  it('身份深层变更也使缓存失效', async () => {
    const store = useEnterpriseStore()
    await store.ensureLoaded()
    reactive(mocks.auth).user!.enterprise!.id = 2
    expect(store.overview).toBe(null)
    await flushPromises()
    expect(mocks.overview).toHaveBeenCalledTimes(2)
  })
})
