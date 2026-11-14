import { describe, expect, it } from 'vitest'
import { maskApiKey } from '../maskApiKey'

describe('maskApiKey', () => {
  it('preserves server hints and masks legacy full credentials', () => {
    expect(maskApiKey('sk-ab12...9xYz')).toBe('sk-ab12...9xYz')
    expect(maskApiKey('sk-ab12…9xYz')).toBe('sk-ab12…9xYz')
    expect(maskApiKey('sk-long-credential-1234')).toBe('sk-...')
    expect(maskApiKey(`sk-${'a'.repeat(64)}`)).toBe('sk-...aaaa')
  })

  it.each(['a', 'ab', 'abc', 'abcdefghijk'])('does not reveal a short credential: %s', credential => {
    expect(maskApiKey(credential)).not.toContain(credential)
    expect(maskApiKey(credential)).toBe('...')
  })

  it('retains the hidden-character budget for every supported length', () => {
    for (const prefix of ['', 'sk-']) {
      for (const character of ['a', '凭']) {
        for (let length = 0; length <= 256; length++) {
          const hint = maskApiKey(prefix + character.repeat(length))
          const visible = Array.from(hint.slice(prefix.length + 3)).length
          const hidden = length - visible
          expect(visible).toBeLessThanOrEqual(4)
          expect(hidden).toBeGreaterThanOrEqual(Math.min(24, length))
          expect(hidden * 4).toBeGreaterThanOrEqual(length * 3)
        }
      }
    }
    expect(maskApiKey('tenant_0123456789abcdef')).toBe('...')
  })
})
