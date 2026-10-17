import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseMember } from '@/types/enterprise'

/** Scope selectors must include members beyond the first table page. */
export async function loadMemberOptions(api: EnterpriseResourceAPI): Promise<EnterpriseMember[]> {
  const items: EnterpriseMember[] = []
  let page = 1
  while (true) {
    const result = await api.members({ page, page_size: 100 })
    items.push(...result.items)
    if (items.length >= result.total || result.items.length === 0) return items
    page++
  }
}
