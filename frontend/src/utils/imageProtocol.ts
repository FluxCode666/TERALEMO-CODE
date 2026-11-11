export type ImageProtocol = 'openai' | 'gemini' | 'grok'

export const IMAGE_BASE_URLS: Record<ImageProtocol, string> = {
  openai: 'https://api.openai.com',
  gemini: 'https://generativelanguage.googleapis.com',
  grok: 'https://api.x.ai'
}

export function resolveImageProtocol(value: unknown): ImageProtocol {
  const v = typeof value === 'string' ? value.trim().toLowerCase() : ''
  return v === 'gemini' || v === 'grok' ? v : 'openai'
}

export function imageBaseURL(protocol: string): string {
  return IMAGE_BASE_URLS[resolveImageProtocol(protocol)]
}

const normalize = (url: string): string => url.trim().replace(/\/+$/, '')

// 切换协议时：空地址或仍是某个协议的官方默认地址则换成新协议默认值；自定义中转地址保留。
export function switchImageBaseURL(current: string, protocol: string): string {
  const normalized = normalize(current)
  if (!normalized) return imageBaseURL(protocol)
  return Object.values(IMAGE_BASE_URLS).some((url) => normalize(url) === normalized)
    ? imageBaseURL(protocol)
    : current
}

export function imageApiKeyPlaceholder(protocol: string): string {
  const p = resolveImageProtocol(protocol)
  return p === 'gemini' ? 'AIza...' : p === 'grok' ? 'xai-...' : 'sk-...'
}
