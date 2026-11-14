// Keep server hints intact and mask credentials from older responses defensively.
export function maskApiKey(key: string): string {
  if (!key) return ''
  if (key.includes('...') || key.includes('…')) return key
  const prefix = key.startsWith('sk-') ? 'sk-' : ''
  const characters = Array.from(key.slice(prefix.length))
  const visible = Math.max(0, Math.min(4, characters.length - 24, Math.floor(characters.length / 4)))
  return `${prefix}...${characters.slice(characters.length - visible).join('')}`
}
