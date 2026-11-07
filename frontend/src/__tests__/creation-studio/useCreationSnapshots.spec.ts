import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { useCreationSnapshots } from '@/features/creation-studio/useCreationSnapshots'
import { NOW, setupComposable, snapshot } from './helpers'

const key = 'creationStudio:snapshots:v1:42'
const storageMethods = localStorage instanceof Storage ? Storage.prototype : localStorage
beforeEach(() => { localStorage.clear(); vi.useFakeTimers(); vi.setSystemTime(NOW * 1000) })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('useCreationSnapshots (F12 / contract 8)', () => {
  it('stores the versioned envelope per user and resolves catalog before task id', async () => {
    const id = ref(42)
    const state = setupComposable(() => useCreationSnapshots(id))
    state.add(snapshot({ catalog_id: 'one', task_id: 'task-one' }))
    state.add(snapshot({ catalog_id: 'two', task_id: 'task-two', created_at: NOW + 1 }))
    expect(JSON.parse(localStorage.getItem(key)!)).toMatchObject({ v: 1, items: [{ catalog_id: 'two' }, { catalog_id: 'one' }] })
    expect(state.find('one', 'task-two')?.task_id).toBe('task-one')
    expect(state.find('missing', 'task-two')?.catalog_id).toBe('two')
    id.value = 43
    await nextTick()
    expect(state.items.value).toEqual([])
    expect(localStorage.getItem('creationStudio:snapshots:v1:43')).toBeNull()
  })

  it('prunes records older than seven days on reads and writes, and caps at 100', () => {
    localStorage.setItem(key, JSON.stringify({ v: 1, items: [snapshot({ created_at: NOW - 604801 })] }))
    const state = setupComposable(() => useCreationSnapshots(ref(42)))
    expect(state.items.value).toEqual([])
    for (let i = 0; i < 101; i++) state.add(snapshot({ catalog_id: String(i), created_at: NOW + i }))
    expect(state.items.value).toHaveLength(100)
    expect(state.items.value[0].catalog_id).toBe('100')
    expect(state.items.value.at(-1)?.catalog_id).toBe('1')
    state.add(snapshot({ catalog_id: 'old', created_at: NOW - 604801 }))
    expect(state.find('old', '')).toBeUndefined()
  })

  it.each(['{bad json', '{"v":2,"items":[]}', '{"v":1,"items":null}'])('recovers corrupt or incompatible JSON %s', raw => {
    localStorage.setItem(key, raw)
    const state = setupComposable(() => useCreationSnapshots(ref(42)))
    expect(state.items.value).toEqual([])
    expect(JSON.parse(localStorage.getItem(key)!)).toEqual({ v: 1, items: [] })
  })

  it('recovers a storage read exception by writing an empty envelope', () => {
    const get = vi.spyOn(storageMethods, 'getItem').mockImplementationOnce(() => { throw new Error('denied') })
    const set = vi.spyOn(storageMethods, 'setItem')
    const state = setupComposable(() => useCreationSnapshots(ref(42)))
    expect(state.items.value).toEqual([])
    expect(set).toHaveBeenCalledWith(key, JSON.stringify({ v: 1, items: [] }))
    get.mockRestore()
  })

  it('drops the oldest half and retries exactly once on QuotaExceededError', () => {
    const state = setupComposable(() => useCreationSnapshots(ref(42)))
    for (let i = 0; i < 3; i++) state.add(snapshot({ catalog_id: String(i), created_at: NOW + i }))
    const set = vi.spyOn(storageMethods, 'setItem').mockImplementationOnce(() => { throw new DOMException('full', 'QuotaExceededError') })
    state.add(snapshot({ catalog_id: 'newest', created_at: NOW + 10 }))
    expect(set).toHaveBeenCalledTimes(2)
    expect(JSON.parse(set.mock.calls[1][1]).items.map((item: { catalog_id: string }) => item.catalog_id)).toEqual(['newest', '2'])
    expect(state.items.value).toHaveLength(2)
  })

  it('does not throw or interrupt submission if both quota writes fail', () => {
    const state = setupComposable(() => useCreationSnapshots(ref(42)))
    const set = vi.spyOn(storageMethods, 'setItem').mockImplementation(() => { throw new DOMException('full', 'QuotaExceededError') })
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    expect(() => state.add(snapshot())).not.toThrow()
    expect(set).toHaveBeenCalledTimes(2)
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('skipping write'))
  })
})
