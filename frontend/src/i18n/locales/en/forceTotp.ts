export default {
  forceTotp: {
    title: 'Require TOTP two-factor authentication for all accounts', hint: 'Includes administrators. Existing sessions of accounts without TOTP are revoked. After signing in again, only TOTP enrollment is available. Passkeys do not replace TOTP; API key model requests are unaffected.',
    keyRequired: 'Configure a fixed TOTP encryption key in the deployment before enabling this requirement.', enable: 'Require 2FA', disable: 'Stop requiring 2FA',
    confirm: '{count} accounts currently have no TOTP. Enabling this requirement signs them out from all sessions and devices. After signing in again, they must complete TOTP enrollment to continue. Enable now?',
    setupTitle: 'Set up two-factor authentication', setupHint: 'Your administrator requires TOTP for all accounts. Complete enrollment to continue using the console.', setup: 'Set up TOTP', logout: 'Sign out', failed: 'Operation failed; please retry',
  },
}
