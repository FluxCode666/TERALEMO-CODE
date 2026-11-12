// Keep server hints intact and mask credentials from older responses defensively.
export function maskApiKey(key: string): string {
  if (!key) return ''
  if (key.includes('...') || key.includes('…')) return key
  if (key.length <= 11) return `${key.slice(0, Math.min(3, Math.max(0, key.length - 1)))}***`
  return `${key.slice(0, 7)}...${key.slice(-4)}`
}
