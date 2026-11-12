import { describe, expect, it } from 'vitest'
import { maskApiKey } from '../maskApiKey'

describe('maskApiKey', () => {
  it('preserves server hints and masks legacy full credentials', () => {
    expect(maskApiKey('sk-ab12...9xYz')).toBe('sk-ab12...9xYz')
    expect(maskApiKey('sk-ab12…9xYz')).toBe('sk-ab12…9xYz')
    expect(maskApiKey('sk-long-credential-1234')).toBe('sk-long...1234')
  })

  it.each(['a', 'ab', 'abc', 'abcdefghijk'])('does not reveal a short credential: %s', credential => {
    expect(maskApiKey(credential)).not.toContain(credential)
    expect(maskApiKey(credential)).toContain('***')
  })
})
