import type {
  AssignServerPlacementInput,
  CreatePoolInput,
  CreateZoneInput,
  InfrastructureRepository,
  UpdateGroupInput,
} from '@/application/ports/InfrastructureRepository'
import type { PlatformPoolMove, Pool, ServerPlacement, Zone } from '@/domain/infrastructure/types'
import { apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/**
 * Maps the Active Infrastructure HTTP contract (`/api/v1/infrastructure` and
 * `PUT /api/v1/servers/{id}/placement`) and a Platform's move to another Pool
 * (`PUT /api/v1/platforms/{id}/pool`, platforms.md) onto the grouping port.
 *
 * Zones (Site-scoped, `?siteId=` filter) and Pools (installation-wide, no filter) have their own
 * routes and shapes. The contract's JSON fields already match {@link Zone}, {@link Pool}, and
 * {@link ServerPlacement}, so responses are returned directly (contractCompatibility.ts checks
 * this at compile time); no secret material crosses this boundary.
 */
export class ApiInfrastructureRepository implements InfrastructureRepository {
  async listZones(siteId?: string): Promise<Zone[]> {
    const suffix = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    return apiRequest<Zone[]>(`/api/v1/infrastructure/zones${suffix}`)
  }

  async createZone(input: CreateZoneInput): Promise<Zone> {
    return apiRequest<Zone>('/api/v1/infrastructure/zones', { method: 'POST', body: JSON.stringify(input) })
  }

  async updateZone(id: string, input: UpdateGroupInput): Promise<Zone> {
    return apiRequest<Zone>(`/api/v1/infrastructure/zones/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  }

  async deleteZone(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/infrastructure/zones/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  async listPools(): Promise<Pool[]> {
    return apiRequest<Pool[]>('/api/v1/infrastructure/pools')
  }

  async createPool(input: CreatePoolInput): Promise<Pool> {
    return apiRequest<Pool>('/api/v1/infrastructure/pools', { method: 'POST', body: JSON.stringify(input) })
  }

  async updatePool(id: string, input: UpdateGroupInput): Promise<Pool> {
    return apiRequest<Pool>(`/api/v1/infrastructure/pools/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  }

  async deletePool(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/infrastructure/pools/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  async assignServerPlacement(serverId: string, input: AssignServerPlacementInput): Promise<ServerPlacement> {
    return apiRequest<ServerPlacement>(`/api/v1/servers/${encodeURIComponent(serverId)}/placement`, {
      method: 'PUT',
      body: JSON.stringify(input),
    })
  }

  async movePlatformToPool(platformId: string, poolId: string | null): Promise<PlatformPoolMove> {
    const body: JsonRequestBody<operations['movePlatformToPool']> = { poolId }
    return apiRequest<JsonResponse<operations['movePlatformToPool'], 200>>(`/api/v1/platforms/${encodeURIComponent(platformId)}/pool`, {
      method: 'PUT',
      body: JSON.stringify(body),
    })
  }
}
