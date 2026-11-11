interface AccountIDRow {
  id: number
  platform?: string
}

interface AccountListPage {
  items: AccountIDRow[]
  total: number
  pages?: number
}

type AccountPageFetcher = (
  page: number,
  pageSize: number,
  filters: Record<string, unknown>
) => Promise<AccountListPage>

const SELECT_ALL_PAGE_SIZE = 1000

export async function fetchAllAccountIds(
  fetchPage: AccountPageFetcher,
  filters: Record<string, unknown>
): Promise<number[]> {
  return (await fetchAllAccountRows(fetchPage, filters)).map(row => row.id)
}

// 与 fetchAllAccountIds 相同，但同时保留每个账号的平台信息（跨页全选后批量测试要据此排除 image 账号）。
export async function fetchAllAccountRows(
  fetchPage: AccountPageFetcher,
  filters: Record<string, unknown>
): Promise<Array<{ id: number; platform?: string }>> {
  const requestFilters = {
    ...filters,
    lite: '1',
    include_scheduler_score: '0'
  }
  const firstPage = await fetchPage(1, SELECT_ALL_PAGE_SIZE, requestFilters)
  const pageCount = Math.max(
    firstPage.pages ?? 0,
    Math.ceil(firstPage.total / SELECT_ALL_PAGE_SIZE)
  )
  const rows = new Map<number, { id: number; platform?: string }>()
  const collect = (items: AccountIDRow[]) => {
    for (const account of items) rows.set(account.id, { id: account.id, platform: account.platform })
  }
  collect(firstPage.items)

  for (let page = 2; page <= pageCount; page++) {
    const result = await fetchPage(page, SELECT_ALL_PAGE_SIZE, requestFilters)
    collect(result.items)
  }

  if (rows.size !== firstPage.total) {
    throw new Error('账号列表结果不完整')
  }
  return Array.from(rows.values())
}
