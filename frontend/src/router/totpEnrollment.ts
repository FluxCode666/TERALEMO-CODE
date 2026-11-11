import type { User } from '@/types'

export function totpEnrollmentRedirect(user: Pick<User, 'requires_totp_setup'> | null, path: string): string | null {
  return user?.requires_totp_setup && path !== '/auth/totp-required' ? '/auth/totp-required' : null
}
