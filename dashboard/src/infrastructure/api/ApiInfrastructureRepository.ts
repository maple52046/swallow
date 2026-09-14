import type {
  AssignServerPlacementInput,
  CreateGroupInput,
  InfrastructureRepository,
  UpdateGroupInput,
} from '@/application/ports/InfrastructureRepository'
import type { GroupKind, GroupingResource, ServerPlacement } from '@/domain/infrastructure/types'
import { apiRequest } from './client'

/**
 * Maps the Active Infrastructure HTTP contract (`/api/v1/infrastructure` and
 * `PUT /api/v1/servers/{id}/placement`) onto the admin grouping port.
 *
 * Zone and Pool share one shape and one set of routes distinguished only by a path segment, so
 * the kind selects `zones` or `pools`. The contract's JSON fields already match
 * {@link GroupingResource} and {@link ServerPlacement}, so responses are returned directly; no
 * secret material crosses this boundary.
 */
export class ApiInfrastructureRepository implements InfrastructureRepository {
  async listGroups(kind: GroupKind, siteId?: string): Promise<GroupingResource[]> {
    const suffix = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    return apiRequest<GroupingResource[]>(`/api/v1/infrastructure/${collectionOf(kind)}${suffix}`)
  }

  async createGroup(kind: GroupKind, input: CreateGroupInput): Promise<GroupingResource> {
    return apiRequest<GroupingResource>(`/api/v1/infrastructure/${collectionOf(kind)}`, {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }

  async updateGroup(kind: GroupKind, id: string, input: UpdateGroupInput): Promise<GroupingResource> {
    return apiRequest<GroupingResource>(`/api/v1/infrastructure/${collectionOf(kind)}/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  }

  async deleteGroup(kind: GroupKind, id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/infrastructure/${collectionOf(kind)}/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }

  async assignServerPlacement(serverId: string, input: AssignServerPlacementInput): Promise<ServerPlacement> {
    return apiRequest<ServerPlacement>(`/api/v1/servers/${encodeURIComponent(serverId)}/placement`, {
      method: 'PUT',
      body: JSON.stringify(input),
    })
  }
}

/** Maps a grouping kind onto the provider's collection path segment. */
function collectionOf(kind: GroupKind): 'zones' | 'pools' {
  return kind === 'zone' ? 'zones' : 'pools'
}
