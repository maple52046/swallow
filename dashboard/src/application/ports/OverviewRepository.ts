import type { Overview } from '@/domain/overview/types'

/** Reads the backend-owned user aggregate without client fan-out. */
export interface OverviewRepository {
  getOverview(siteId?: string): Promise<Overview>
}
