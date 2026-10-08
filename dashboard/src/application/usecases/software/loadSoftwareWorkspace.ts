import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { SoftwareRepository } from '@/application/ports/SoftwareRepository'
import { loadServerWorkingSet } from '@/application/usecases/servers/loadServerWorkingSet'
import type { Server } from '@/domain/server/types'
import type { SoftwareAssignment, SoftwareCatalogEntry, SoftwareKind } from '@/domain/software/types'

/** Operational facts used to add Site-scoped deployment context to the software catalog. */
export interface SoftwareFootprintWorkingSet {
  assignments: SoftwareAssignment[]
  servers: Server[]
}

/** Complete routed workspace for one Managed Software kind. */
export interface SoftwareDetailWorkingSet extends SoftwareFootprintWorkingSet {
  catalog: SoftwareCatalogEntry[]
  entry: SoftwareCatalogEntry | null
}

/**
 * Loads the Site-scoped facts used only for catalog footprints.
 *
 * Catalog identity is deliberately loaded separately by the page: a transient assignment or
 * Server read must not hide the installable software catalog. The API does not Site-filter
 * assignments, so the returned deployed Server set is the authoritative scope boundary.
 */
export async function loadSoftwareFootprint(
  software: SoftwareRepository,
  servers: ServerRepository,
  siteId?: string,
): Promise<SoftwareFootprintWorkingSet> {
  const [assignments, serverWorkingSet] = await Promise.all([
    software.listAssignments(),
    loadServerWorkingSet(servers, { siteId, provisioningState: 'deployed' }),
  ])
  return { assignments, servers: serverWorkingSet.servers }
}

/**
 * Loads one Software detail route from existing catalog, assignment, and Server ports.
 *
 * The operation performs no transport-specific work. It loads assignments once across kinds so
 * target eligibility can detect mutual exclusions; presentation filters the routed kind while Site
 * scope is applied by joining assignments to the complete deployed Server working set.
 */
export async function loadSoftwareDetailWorkspace(
  software: SoftwareRepository,
  servers: ServerRepository,
  kind: SoftwareKind,
  siteId?: string,
): Promise<SoftwareDetailWorkingSet> {
  const [catalog, assignments, serverWorkingSet] = await Promise.all([
    software.listCatalog(),
    software.listAssignments(),
    loadServerWorkingSet(servers, { siteId, provisioningState: 'deployed' }),
  ])
  return {
    catalog,
    entry: catalog.find((candidate) => candidate.kind === kind) ?? null,
    assignments,
    servers: serverWorkingSet.servers,
  }
}
