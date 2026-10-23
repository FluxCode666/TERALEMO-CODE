export const VIDEO_BASE_URLS: Record<string, string> = {
  seedance: 'https://ark.cn-beijing.volces.com/api/v3',
  gemini: 'https://generativelanguage.googleapis.com',
  minimax: 'https://api.minimax.io'
}

export function resolveVideoProtocol(value: unknown): string {
  return typeof value === 'string' && Object.prototype.hasOwnProperty.call(VIDEO_BASE_URLS, value) ? value : 'seedance'
}

export function videoBaseURL(protocol: string): string {
  return VIDEO_BASE_URLS[protocol] ?? VIDEO_BASE_URLS.seedance!
}

// 额外的官方默认地址别名：与 VIDEO_BASE_URLS 一样视为"可替换的默认地址"，
// 切换协议时直接覆盖，而不是当自定义中转地址保留。
const VIDEO_DEFAULT_BASE_URL_ALIASES: readonly string[] = ['https://api.minimaxi.com']

const normalizeVideoBaseURL = (url: string): string => url.trim().replace(/\/+$/, '')

export function switchVideoBaseURL(current: string, protocol: string): string {
  const normalized = normalizeVideoBaseURL(current)
  if (!normalized) return videoBaseURL(protocol)
  const replaceable = [...Object.values(VIDEO_BASE_URLS), ...VIDEO_DEFAULT_BASE_URL_ALIASES]
  return replaceable.some((url) => normalizeVideoBaseURL(url) === normalized)
    ? videoBaseURL(protocol)
    : current
}
