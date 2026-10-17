import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { enterpriseErrorMessage } from '@/api/enterprise'

export function useEnterpriseAction() {
  const { t } = useI18n()
  const app = useAppStore()
  const busy = ref(false)
  const error = ref('')

  async function run(action: () => Promise<unknown>, success = ''): Promise<boolean> {
    if (busy.value) return false
    busy.value = true
    error.value = ''
    try {
      await action()
      if (success) app.showSuccess(success)
      return true
    } catch (cause) {
      error.value = enterpriseErrorMessage(cause, t)
      app.showError(error.value)
      return false
    } finally {
      busy.value = false
    }
  }

  return { busy, error, run }
}
