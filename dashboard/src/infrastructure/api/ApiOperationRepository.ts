import type { OperationRepository, Paginated } from '@/application/ports/OperationRepository'
import type {
  ListOperationsFilters,
  Operation,
  OperationEvents,
} from '@/domain/operation/types'
import { ApiRequestError, apiRequest, apiRequestText } from './client'

/**
 * The operation contract maps directly onto the domain type. Logs are plain text, so they
 * go through the text helper; everything else is JSON. getOperation treats 404 as a stale
 * link (null) rather than an error to surface.
 */
export class ApiOperationRepository implements OperationRepository {
  async listOperations(filters?: ListOperationsFilters): Promise<Paginated<Operation>> {
    const query = new URLSearchParams()
    if (filters?.siteId) query.set('siteId', filters.siteId)
    if (filters?.clusterId) query.set('clusterId', filters.clusterId)
    if (filters?.serverId) query.set('serverId', filters.serverId)
    if (filters?.kind) query.set('kind', filters.kind)
    if (filters?.status) query.set('status', filters.status)
    if (filters?.active) query.set('active', 'true')
    if (filters?.page) query.set('page', String(filters.page))
    if (filters?.pageSize) query.set('pageSize', String(filters.pageSize))

    const suffix = query.toString() ? `?${query.toString()}` : ''
    return apiRequest<Paginated<Operation>>(`/api/v1/operations${suffix}`)
  }

  async getOperation(id: string): Promise<Operation | null> {
    try {
      return await apiRequest<Operation>(`/api/v1/operations/${encodeURIComponent(id)}`)
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async getEvents(id: string): Promise<OperationEvents> {
    return apiRequest<OperationEvents>(`/api/v1/operations/${encodeURIComponent(id)}/events`)
  }

  async getLogs(id: string): Promise<string> {
    return apiRequestText(`/api/v1/operations/${encodeURIComponent(id)}/logs`)
  }

  async retryOperation(id: string): Promise<Operation> {
    return apiRequest<Operation>(`/api/v1/operations/${encodeURIComponent(id)}/retry`, {
      method: 'POST',
    })
  }
}
