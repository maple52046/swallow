import type { OverviewRepository } from '@/application/ports/OverviewRepository'
import type { Overview } from '@/domain/overview/types'
import { apiRequest } from './client'

/** HTTP adapter for the provider-owned operator overview. */
export class ApiOverviewRepository implements OverviewRepository {
  async getOverview(siteId?: string): Promise<Overview> {
    const suffix = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    return apiRequest<Overview>(`/api/v1/overview${suffix}`)
  }
}
