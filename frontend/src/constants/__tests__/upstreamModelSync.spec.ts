import { describe, expect, it } from 'vitest'
import { supportsUpstreamModelSync } from '../platformCatalog'

describe('supportsUpstreamModelSync', () => {
  it('includes the image platform and keeps existing ones', () => {
    for (const p of ['image', 'openai', 'gemini', 'grok', 'anthropic', 'antigravity']) {
      expect(supportsUpstreamModelSync(p)).toBe(true)
    }
    expect(supportsUpstreamModelSync('video')).toBe(false)
  })
})
