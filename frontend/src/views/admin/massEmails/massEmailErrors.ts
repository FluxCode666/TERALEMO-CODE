import { extractI18nErrorMessage } from '@/utils/apiError'

type TranslateFn = (key: string, params?: Record<string, unknown>) => string

/** Map backend `reason` codes to i18n copy, falling back to the backend message. */
export function massEmailErrorMessage(err: unknown, t: TranslateFn, fallbackKey = 'admin.massEmails.messages.actionFailed'): string {
  return extractI18nErrorMessage(err, t, 'admin.massEmails.errors', t(fallbackKey))
}
