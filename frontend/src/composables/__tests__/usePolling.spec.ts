import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePolling } from '../usePolling'

describe('usePolling lifecycle', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  function setup(fn: () => Promise<void> | void) {
    let poller!: ReturnType<typeof usePolling>
    const wrapper = mount(defineComponent({
      setup() {
        poller = usePolling(fn, 1000)
        return () => null
      }
    }))
    return { wrapper, poller }
  }

  it('does not overlap slow requests or duplicate intervals', async () => {
    let finish!: () => void
    const fn = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
    const { wrapper, poller } = setup(fn)
    poller.start()
    poller.start()
    await vi.advanceTimersByTimeAsync(4000)
    expect(fn).toHaveBeenCalledTimes(1)
    finish()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(2)
    finish()
    wrapper.unmount()
  })

  it('stops and allows explicit restart while mounted', async () => {
    const fn = vi.fn()
    const { wrapper, poller } = setup(fn)
    poller.start()
    await vi.advanceTimersByTimeAsync(1000)
    poller.stop()
    await vi.advanceTimersByTimeAsync(3000)
    expect(fn).toHaveBeenCalledTimes(1)
    poller.start()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('cannot restart from an asynchronous continuation after unmount', async () => {
    const fn = vi.fn()
    const { wrapper, poller } = setup(fn)
    poller.start()
    wrapper.unmount()
    poller.start()
    await vi.advanceTimersByTimeAsync(3000)
    expect(fn).not.toHaveBeenCalled()
    expect(poller.isRunning()).toBe(false)
  })
})
