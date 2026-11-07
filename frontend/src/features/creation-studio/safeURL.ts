/**
 * 结果链接的协议白名单校验（契约 §7.4、规范 §8）。
 *
 * 只允许 `https:`、`http:` 与站内相对路径；`javascript:`、`data:`、`blob:` 等一律返回 undefined，
 * 让模板渲染成「不可打开」而不是把用户点进伪协议。
 */
export function safeURL(value?: string | null): string | undefined {
  if (!value) return undefined
  const trimmed = value.trim()
  if (!trimmed) return undefined
  // 站内相对路径：以单个 `/` 开头（`//host` 是协议相对 URL，走下面的 URL 解析）。
  if (trimmed.startsWith('/') && !trimmed.startsWith('//')) return trimmed
  try {
    const url = new URL(trimmed)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : undefined
  } catch {
    return undefined
  }
}

/**
 * 任务的全部结果链接（契约 §7.2）：`result_urls` 优先，缺失或为空时回落 `result_url`，
 * 再统一过滤掉不安全协议。异步任务页与创作页共用这一套口径。
 */
export function safeResultURLs(resultURLs?: string[] | null, resultURL?: string | null): string[] {
  const list = resultURLs && resultURLs.length ? resultURLs : resultURL ? [resultURL] : []
  return list.map(url => safeURL(url)).filter((url): url is string => Boolean(url))
}
