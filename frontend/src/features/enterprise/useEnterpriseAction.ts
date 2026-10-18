import { computed, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { enterpriseErrorMessage } from '@/api/enterprise'

export function useEnterpriseAction() {
  const { t } = useI18n()
  const app = useAppStore()
  const writing = ref(false)
  const reading = ref(false)
  const busy = computed(() => writing.value || reading.value)
  const error = ref('')
  let disposed = false
  let request: AbortController | undefined

  // Each reader owns its state commit so even transports that ignore abort cannot
  // overwrite a more recent page/filter result.
  async function read<T>(action: (signal: AbortSignal) => Promise<T>, commit: (value: T) => void): Promise<boolean> {
    if (disposed) return false
    request?.abort()
    const current = new AbortController()
    request = current
    reading.value = true
    error.value = ''
    try {
      const result = await action(current.signal)
      if (disposed || current.signal.aborted || request !== current) return false
      commit(result)
      return true
    } catch (cause) {
      if (disposed || current.signal.aborted || request !== current) return false
      error.value = enterpriseErrorMessage(cause, t)
      app.showError(error.value)
      return false
    } finally {
      if (request === current) reading.value = false
    }
  }

  async function run(action: () => Promise<unknown>, success = ''): Promise<boolean> {
    if (disposed || busy.value) return false
    writing.value = true
    error.value = ''
    try {
      await action()
      if (disposed) return false
      if (success) app.showSuccess(success)
      return true
    } catch (cause) {
      if (disposed) return false
      error.value = enterpriseErrorMessage(cause, t)
      app.showError(error.value)
      return false
    } finally {
      writing.value = false
    }
  }

  onScopeDispose(() => {
    disposed = true
    request?.abort()
  })

  return { busy, error, run, read }
}
