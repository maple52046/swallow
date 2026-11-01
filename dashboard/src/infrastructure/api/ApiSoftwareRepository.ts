import type { SoftwareRepository } from '@/application/ports/SoftwareRepository'
import {
  isSoftwareKind,
  type InstallSoftwareInput,
  type SoftwareAssignment,
  type SoftwareCatalogEntry,
  type SoftwareKind,
  type SoftwareOperationReference,
  type UninstallSoftwareInput,
} from '@/domain/software/types'
import { apiRequest } from './client'

/**
 * The `{ items: [...] }` envelope the software list/catalog endpoints return.
 * Exported only so `contractCompatibility.ts` can prove each list response reads as it.
 */
export interface ItemsEnvelope<T> {
  items: T[]
}

/**
 * A catalog entry as the contract sends it. A kind is the id of a Software module (decision 075),
 * so the contract types every kind as a string. Exported for `contractCompatibility.ts`.
 */
export type WireSoftwareCatalogEntry = Omit<SoftwareCatalogEntry, 'kind' | 'mutuallyExclusiveWith' | 'containerRuntimes'> & {
  kind: string
  mutuallyExclusiveWith: string[]
  containerRuntimes: string[]
}

/** A Software Assignment as the contract sends it, of any module's kind. */
export type WireSoftwareAssignment = Omit<SoftwareAssignment, 'kind'> & { kind: string }

/**
 * Maps the Active Managed Software HTTP contract (`/api/v1/software`, `software.md`) onto the
 * SoftwareRepository port. The contract's JSON matches the domain shapes except that it may carry
 * kinds this dashboard has no screens for; those entries and assignments are left out here.
 * Install/uninstall return only `{ operationId }`; the caller reads progress through the workflows
 * surface. No secret material crosses this boundary.
 */
export class ApiSoftwareRepository implements SoftwareRepository {
  async listCatalog(): Promise<SoftwareCatalogEntry[]> {
    const response = await apiRequest<ItemsEnvelope<WireSoftwareCatalogEntry>>('/api/v1/software/catalog')
    return response.items.flatMap(knownCatalogEntry)
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
    const response = await apiRequest<ItemsEnvelope<WireSoftwareAssignment>>(
      `/api/v1/software/assignments${query ? `?${query}` : ''}`,
    )
    return response.items.flatMap(knownAssignment)
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

/** Keeps a catalog entry of a kind the dashboard presents, naming only such kinds in its rules. */
function knownCatalogEntry(entry: WireSoftwareCatalogEntry): SoftwareCatalogEntry[] {
  const { kind } = entry
  if (!isSoftwareKind(kind)) return []
  return [{
    ...entry,
    kind,
    mutuallyExclusiveWith: entry.mutuallyExclusiveWith.filter(isSoftwareKind),
    containerRuntimes: entry.containerRuntimes.filter(isSoftwareKind),
  }]
}

/** Keeps an assignment of a kind the dashboard presents. */
function knownAssignment(assignment: WireSoftwareAssignment): SoftwareAssignment[] {
  const { kind } = assignment
  return isSoftwareKind(kind) ? [{ ...assignment, kind }] : []
}
