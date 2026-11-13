import { onBeforeUnmount } from 'vue'

/** Interval polling that is stopped on unmount. */
export function usePolling(fn: () => void | Promise<void>, intervalMs: number) {
  let timer: number | null = null
  let inFlight = false
  let disposed = false
  function start() {
    if (disposed || timer !== null) return
    timer = window.setInterval(async () => {
      if (disposed || inFlight) return
      inFlight = true
      try {
        await fn()
      } finally {
        inFlight = false
      }
    }, intervalMs)
  }
  function stop() {
    if (timer !== null) {
      window.clearInterval(timer)
      timer = null
    }
  }
  onBeforeUnmount(() => {
    disposed = true
    stop()
  })
  return { start, stop, isRunning: () => timer !== null }
}
