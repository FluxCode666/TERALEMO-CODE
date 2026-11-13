import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en/admin/massEmails'
import zh from '@/i18n/locales/zh/admin/massEmails'
import { massEmailErrorMessage } from '../massEmailErrors'

// API contract #5, including server validation and lifecycle errors.
const reasons = [
  'INVALID_RECIPIENT_FILTER', 'INVALID_SUBJECT', 'INVALID_BODY', 'INVALID_EMAIL',
  'INVALID_SCHEDULED_AT', 'NO_RECIPIENTS', 'CAMPAIGN_NOT_EDITABLE', 'RECIPIENT_NOT_RETRYABLE',
  'MASS_EMAIL_NOT_FOUND', 'RECIPIENT_NOT_FOUND', 'CAMPAIGN_SENDING', 'CAMPAIGN_NOT_CANCELLABLE',
  'CAMPAIGN_CANCELLED', 'EMAIL_NOT_CONFIGURED', 'EMAIL_SEND_FAILED'
] as const

describe('mass email API error translations', () => {
  it.each([['en', en], ['zh', zh]] as const)('maps every contract reason in %s', (_name, locale) => {
    const t = (key: string): string => {
      const reason = key.replace('admin.massEmails.errors.', '')
      return locale.massEmails.errors[reason as keyof typeof locale.massEmails.errors] ?? key
    }
    for (const reason of reasons) {
      const message = massEmailErrorMessage({ code: 400, reason, message: 'Server message' }, t)
      expect(message).toBe(locale.massEmails.errors[reason])
      expect(message).not.toBe('Server message')
    }
  })

  it('preserves a server error message for an unknown reason', () => {
    expect(massEmailErrorMessage({ reason: 'UNKNOWN', message: 'Server detail' }, (key) => key)).toBe('Server detail')
  })

  it('uses the localized fallback when no error message is available', () => {
    const key = 'admin.massEmails.messages.actionFailed'
    expect(massEmailErrorMessage({}, (value) => value === key ? 'Operation failed' : value)).toBe('Operation failed')
  })
})
