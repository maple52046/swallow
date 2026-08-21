import type { SiteRepository } from '@/application/ports/SiteRepository'
import type { Integration, IntegrationKind, OSImage, Site } from '@/domain/site/types'
import { apiRequest } from './client'

export class ApiSiteRepository implements SiteRepository {
  async listSites(): Promise<Site[]> {
    return apiRequest<Site[]>('/api/v1/sites')
  }

  async listIntegrations(filters?: { siteId?: string; kind?: IntegrationKind }): Promise<Integration[]> {
    const query = new URLSearchParams()
    if (filters?.siteId) query.set('siteId', filters.siteId)
    if (filters?.kind) query.set('kind', filters.kind)

    const suffix = query.toString() ? `?${query.toString()}` : ''
    return apiRequest<Integration[]>(`/api/v1/integrations${suffix}`)
  }

  async listOSImages(integrationId: string): Promise<OSImage[]> {
    return apiRequest<OSImage[]>(
      `/api/v1/provisioning/images?integrationId=${encodeURIComponent(integrationId)}`,
    )
  }
}
