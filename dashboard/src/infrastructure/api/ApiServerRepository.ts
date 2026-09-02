import type { Paginated, ServerRepository } from '@/application/ports/ServerRepository'
import type {
  DeployServerInput,
  ListServersFilters,
  PowerStateResult,
  ProvisionerDetail,
  ReleaseServerInput,
  ProviderEvents,
  ProvisioningActionResult,
  Server,
  ServerAction,
} from '@/domain/server/types'
import { ApiRequestError, apiRequest } from './client'

/**
 * The API response shape maps directly onto the domain type, so there is no adapter
 * layer here. That is deliberate: the contract was designed so that a client does not
 * have to reshape anything, and a translation step would only be somewhere for the two
 * to drift apart.
 */
export class ApiServerRepository implements ServerRepository {
  async listServers(filters?: ListServersFilters): Promise<Paginated<Server>> {
    const query = new URLSearchParams()
    if (filters?.siteId) query.set('siteId', filters.siteId)
    if (filters?.integrationId) query.set('integrationId', filters.integrationId)
    if (filters?.provisioningState) query.set('provisioningState', filters.provisioningState)
    if (filters?.clusterId) query.set('clusterId', filters.clusterId)
    if (filters?.keyword) query.set('keyword', filters.keyword)
    if (filters?.includeAbsent) query.set('includeAbsent', 'true')
    if (filters?.page) query.set('page', String(filters.page))
    if (filters?.pageSize) query.set('pageSize', String(filters.pageSize))

    const suffix = query.toString() ? `?${query.toString()}` : ''
    return apiRequest<Paginated<Server>>(`/api/v1/servers${suffix}`, { cache: 'no-store' })
  }

  async getServer(id: string): Promise<Server | null> {
    try {
      return await apiRequest<Server>(`/api/v1/servers/${encodeURIComponent(id)}`)
    } catch (error) {
      // A missing server is an expected answer for a stale link, not a failure to
      // report to the user as an error.
      if (error instanceof ApiRequestError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async deployServer(id: string, input: DeployServerInput): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/deploy`,
      { method: 'POST', body: JSON.stringify(input) },
    )
  }

  async releaseServer(id: string, input?: ReleaseServerInput): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/release`,
      input
        ? { method: 'POST', body: JSON.stringify(input) }
        : { method: 'POST' },
    )
  }

  async refreshServer(id: string): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      '/api/v1/servers/' + encodeURIComponent(id) + '/refresh',
      { method: 'POST' },
    )
  }

  async deleteServer(id: string): Promise<void> {
    await apiRequest<void>(
      `/api/v1/servers/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
    )
  }

  async getProvisionerDetail(id: string): Promise<ProvisionerDetail> {
    return apiRequest<ProvisionerDetail>(
      `/api/v1/servers/${encodeURIComponent(id)}/provisioner-detail`,
    )
  }

  async getProviderEvents(id: string, limit = 50): Promise<ProviderEvents> {
    const path = `/api/v1/servers/${encodeURIComponent(id)}/events?limit=${encodeURIComponent(String(limit))}`
    return apiRequest<ProviderEvents>(path)
  }

  async runServerAction(id: string, action: ServerAction): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/${action}`,
      { method: 'POST' },
    )
  }

  async queryPowerState(id: string): Promise<PowerStateResult> {
    return apiRequest<PowerStateResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/power-state`,
    )
  }
}
