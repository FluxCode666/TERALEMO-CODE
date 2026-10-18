import { effectScope } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useEnterpriseAction } from '../useEnterpriseAction'

const { showError, showSuccess } = vi.hoisted(() => ({ showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/api/enterprise', () => ({ enterpriseErrorMessage: () => 'friendly error' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

describe('enterprise latest read lifecycle', () => {
  beforeEach(() => vi.clearAllMocks())

  it('cancels a superseded read and commits only the latest result even when abort is ignored', async () => {
    const scope = effectScope()
    const action = scope.run(useEnterpriseAction)!
    const old = deferred<number>(), latest = deferred<number>()
    const commit = vi.fn()
    let oldSignal!: AbortSignal
    const first = action.read(signal => { oldSignal = signal; return old.promise }, commit)
    const second = action.read(() => latest.promise, commit)
    expect(oldSignal.aborted).toBe(true)
    latest.resolve(3)
    expect(await second).toBe(true)
    expect(action.busy.value).toBe(false)
    old.resolve(2)
    expect(await first).toBe(false)
    expect(commit).toHaveBeenCalledTimes(1)
    expect(commit).toHaveBeenCalledWith(3)
    scope.stop()
  })

  it('suppresses stale failures and keeps busy until the current read finishes', async () => {
    const scope = effectScope()
    const action = scope.run(useEnterpriseAction)!
    const old = deferred<number>(), latest = deferred<number>()
    const first = action.read(() => old.promise, vi.fn())
    const second = action.read(() => latest.promise, vi.fn())
    old.reject(new Error('stale'))
    await first
    expect(action.busy.value).toBe(true)
    expect(showError).not.toHaveBeenCalled()
    latest.reject(new Error('current'))
    await second
    expect(showError).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith('friendly error')
    expect(action.busy.value).toBe(false)
    scope.stop()
  })

  it('aborts on scope disposal and never commits or starts later reads', async () => {
    const scope = effectScope()
    const action = scope.run(useEnterpriseAction)!
    const pending = deferred<number>(), commit = vi.fn()
    let signal!: AbortSignal
    const read = action.read(value => { signal = value; return pending.promise }, commit)
    scope.stop()
    expect(signal.aborted).toBe(true)
    pending.resolve(1)
    expect(await read).toBe(false)
    const next = vi.fn()
    expect(await action.read(next, commit)).toBe(false)
    expect(commit).not.toHaveBeenCalled()
    expect(next).not.toHaveBeenCalled()
  })

  it('keeps mutations mutually exclusive', async () => {
    const scope = effectScope()
    const action = scope.run(useEnterpriseAction)!
    const pending = deferred<number>(), duplicate = vi.fn()
    const first = action.run(() => pending.promise, 'saved')
    expect(await action.run(duplicate, 'saved')).toBe(false)
    expect(duplicate).not.toHaveBeenCalled()
    pending.resolve(1)
    await first
    expect(showSuccess).toHaveBeenCalledTimes(1)
    expect(showSuccess).toHaveBeenCalledWith('saved')
    scope.stop()
  })
})
