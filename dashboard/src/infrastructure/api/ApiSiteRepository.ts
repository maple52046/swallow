import type {
  CreateIntegrationInput,
  CreateSiteInput,
  SiteRepository,
  UpdateIntegrationInput,
  UpdateSiteInput,
} from '@/application/ports/SiteRepository'
import type { Integration, IntegrationKind, OSImage, Site } from '@/domain/site/types'
import { apiRequest } from './client'

/**
 * Maps the Active Sites/Integrations HTTP contract onto the admin registry port.
 * Secret values are sent only in write requests and are absent from every response type.
 */
export class ApiSiteRepository implements SiteRepository {
  async listSites(): Promise<Site[]> {
    return apiRequest<Site[]>('/api/v1/sites')
  }

  async createSite(input: CreateSiteInput): Promise<Site> {
    return apiRequest<Site>('/api/v1/sites', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }

  async updateSite(id: string, input: UpdateSiteInput): Promise<Site> {
    return apiRequest<Site>(`/api/v1/sites/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  }

  async deleteSite(id: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`/api/v1/sites/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }

  async listIntegrations(filters?: { siteId?: string; kind?: IntegrationKind }): Promise<Integration[]> {
    const query = new URLSearchParams()
    if (filters?.siteId) query.set('siteId', filters.siteId)
    if (filters?.kind) query.set('kind', filters.kind)

    const suffix = query.toString() ? `?${query.toString()}` : ''
    return apiRequest<Integration[]>(`/api/v1/integrations${suffix}`)
  }

  async createIntegration(input: CreateIntegrationInput): Promise<Integration> {
    return apiRequest<Integration>('/api/v1/integrations', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }

  async updateIntegration(id: string, input: UpdateIntegrationInput): Promise<Integration> {
    return apiRequest<Integration>(`/api/v1/integrations/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  }

  async replaceIntegrationCredential(id: string, credential: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`/api/v1/integrations/${encodeURIComponent(id)}/credential`, {
      method: 'PUT',
      body: JSON.stringify({ credential }),
    })
  }

  async deleteIntegration(id: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`/api/v1/integrations/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }

  async listOSImages(integrationId: string): Promise<OSImage[]> {
    return apiRequest<OSImage[]>(
      `/api/v1/provisioning/images?integrationId=${encodeURIComponent(integrationId)}`,
    )
  }
}
