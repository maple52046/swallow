import type { SoftwareRepository } from '@/application/ports/SoftwareRepository'
import type {
  InstallSoftwareInput,
  SoftwareAssignment,
  SoftwareCatalogEntry,
  SoftwareKind,
  SoftwareOperationReference,
  UninstallSoftwareInput,
} from '@/domain/software/types'
import { apiRequest } from './client'

/** The `{ items: [...] }` envelope the software list/catalog endpoints return. */
interface ItemsEnvelope<T> {
  items: T[]
}

/**
 * Maps the Active Managed Software HTTP contract (`/api/v1/software`, `software.md`) onto the
 * SoftwareRepository port. The contract's JSON already matches the domain shapes, so responses are
 * returned directly. Install/uninstall return only `{ operationId }`; the caller reads progress
 * through the workflows surface. No secret material crosses this boundary.
 */
export class ApiSoftwareRepository implements SoftwareRepository {
  async listCatalog(): Promise<SoftwareCatalogEntry[]> {
    const response = await apiRequest<ItemsEnvelope<SoftwareCatalogEntry>>('/api/v1/software/catalog')
    return response.items
  }

  async listAssignments(filters?: {
    serverId?: string
    kind?: SoftwareKind
    includeAbsent?: boolean
  }): Promise<SoftwareAssignment[]> {
    const params = new URLSearchParams()
    if (filters?.serverId) params.set('serverId', filters.serverId)
    if (filters?.kind) params.set('kind', filters.kind)
    if (filters?.includeAbsent) params.set('includeAbsent', 'true')
    const query = params.toString()
    const response = await apiRequest<ItemsEnvelope<SoftwareAssignment>>(
      `/api/v1/software/assignments${query ? `?${query}` : ''}`,
    )
    return response.items
  }

  async installSoftware(input: InstallSoftwareInput): Promise<SoftwareOperationReference> {
    return apiRequest<SoftwareOperationReference>('/api/v1/software/assignments', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }

  async uninstallSoftware(input: UninstallSoftwareInput): Promise<SoftwareOperationReference> {
    return apiRequest<SoftwareOperationReference>('/api/v1/software/uninstall', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }
}
