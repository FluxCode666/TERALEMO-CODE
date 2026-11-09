import { describe, expect, it } from 'vitest'
import { KEY_GROUP_PROVIDERS, KEY_GROUP_PROVIDER_ICONS, getKeyGroupProvider } from '../keyGroupProviders'
import { resolveImageProtocol, imageBaseURL, switchImageBaseURL } from '../imageProtocol'

describe('keyGroupProviders', () => {
  it('orders categories with independent image and video', () => {
    expect(KEY_GROUP_PROVIDERS).toEqual(['anthropic', 'openai', 'domestic', 'image', 'video', 'other'])
    expect(KEY_GROUP_PROVIDER_ICONS.image).toEqual(['image'])
    expect(KEY_GROUP_PROVIDER_ICONS.video).toEqual(['video'])
  })

  it('maps image and video to their own categories and leaves others unchanged', () => {
    expect(getKeyGroupProvider('image')).toBe('image')
    expect(getKeyGroupProvider('video')).toBe('video')
    expect(getKeyGroupProvider('anthropic')).toBe('anthropic')
    expect(getKeyGroupProvider('openai')).toBe('openai')
    for (const p of ['kimi', 'zhipu', 'deepseek', 'minimax']) expect(getKeyGroupProvider(p)).toBe('domestic')
    for (const p of ['gemini', 'grok', 'antigravity', 'composite', 'opencode_go', 'typesafe', 'command_code', 'cline', 'unknown_new']) {
      expect(getKeyGroupProvider(p)).toBe('other')
    }
  })
})

describe('imageProtocol', () => {
  it('resolves protocols and defaults to openai', () => {
    expect(resolveImageProtocol(undefined)).toBe('openai')
    expect(resolveImageProtocol('')).toBe('openai')
    expect(resolveImageProtocol(' Gemini ')).toBe('gemini')
    expect(resolveImageProtocol('grok')).toBe('grok')
    expect(resolveImageProtocol('bogus')).toBe('openai')
  })

  it('switches only default base urls', () => {
    expect(imageBaseURL('grok')).toBe('https://api.x.ai')
    expect(switchImageBaseURL('https://api.openai.com/', 'gemini')).toBe('https://generativelanguage.googleapis.com')
    expect(switchImageBaseURL('https://relay.example', 'gemini')).toBe('https://relay.example')
    expect(switchImageBaseURL('', 'grok')).toBe('https://api.x.ai')
  })
})
