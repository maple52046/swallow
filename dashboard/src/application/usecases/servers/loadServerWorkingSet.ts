/**
 * Loads the full set of servers the list needs to group, multi-filter, and paginate on
 * the client.
 *
 * The swallow API paginates but offers no grouping or multi-dimension filtering, so the
 * servers list fetches a working set and does that work in the browser. Only the coarse
 * filters the API supports (`siteId`, `keyword`, `includeAbsent`) are pushed server-side;
 * the rest is applied by the domain list helpers. Every page is fetched sequentially so
 * grouping and selection always cover the complete scoped fleet.
 *
 * This is application orchestration over the `ServerRepository` port: it holds no React
 * or transport detail and is testable with a fake repository.
 */
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { Server } from '@/domain/server/types'

/** The API's maximum page size; fetching in the largest pages minimises round trips. */
const PAGE_SIZE = 100

/** Coarse filters that the API applies server-side before the client refines the set. */
export interface WorkingSetQuery {
  siteId?: string
  provisioningState?: string
  keyword?: string
  includeAbsent?: boolean
}

/** The loaded working set plus whether the safety cap truncated it. */
export interface ServerWorkingSet {
  servers: Server[]
  total: number
  truncated: boolean
}

export async function loadServerWorkingSet(
  repository: ServerRepository,
  query: WorkingSetQuery = {},
): Promise<ServerWorkingSet> {
  const servers: Server[] = []
  let total = 0

  for (let page = 1; ; page++) {
    const result = await repository.listServers({
      page,
      pageSize: PAGE_SIZE,
      siteId: query.siteId,
      provisioningState: query.provisioningState,
      keyword: query.keyword,
      includeAbsent: query.includeAbsent,
    })
    total = result.total
    servers.push(...result.items)

    // Stop once every server has been collected, or the provider returned a short page.
    if (servers.length >= result.total || result.items.length < PAGE_SIZE) {
      return { servers, total, truncated: false }
    }
  }
}
