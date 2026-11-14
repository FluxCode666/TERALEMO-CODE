import { describe, expect, it } from 'vitest'
import { totpEnrollmentRedirect } from '../totpEnrollment'

describe('forced TOTP route guard', () => {
  it('redirects every other route until TOTP enrollment completes', () => {
    for (const path of ['/dashboard', '/admin/settings', '/profile', '/home', '/login']) {
      expect(totpEnrollmentRedirect({ requires_totp_setup: true }, path)).toBe('/auth/totp-required')
    }
    expect(totpEnrollmentRedirect({ requires_totp_setup: true }, '/auth/totp-required')).toBeNull()
    expect(totpEnrollmentRedirect({ requires_totp_setup: false }, '/dashboard')).toBeNull()
    expect(totpEnrollmentRedirect(null, '/login')).toBeNull()
  })
})
