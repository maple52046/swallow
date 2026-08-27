import type { Overview } from '@/domain/overview/types'

/** Reads the backend-owned operator aggregate without client fan-out. */
export interface OverviewRepository {
  getOverview(siteId?: string): Promise<Overview>
}
