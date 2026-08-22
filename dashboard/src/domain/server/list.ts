/**
 * Pure list operations over `Server`: grouping, filtering, sorting, and the value
 * extraction they need.
 *
 * These live in the domain because they are framework-free rules on domain types — no
 * React, no Radix, no browser — and are the same operations MAAS performs server-side.
 * The gdcm API paginates but does not group or multi-filter, so the servers list applies
 * these client-side over a fetched working set. Everything here is deterministic and
 * unit-testable without rendering.
 */
import type { Server } from './types'
import { serverDisplayName, serverPrimaryAddress } from './types'

/** How the list is grouped. `none` renders a single ungrouped table. */
export type ServerGroupBy = 'none' | 'provisioning' | 'zone' | 'pool' | 'architecture' | 'power'

/** Sortable columns. Maps to the numeric/string fields the table exposes. */
export type ServerSortKey =
  | 'name'
  | 'provisioning'
  | 'power'
  | 'cores'
  | 'memory'
  | 'storage'
  | 'zone'
  | 'pool'

export type SortDirection = 'asc' | 'desc'

/**
 * The multi-dimension filter the panel builds. Each array is an OR within a dimension and
 * dimensions AND together, matching MAAS's filter semantics. `hasGpu` is a tri-state:
 * `null` means "any".
 */
export interface ServerFilters {
  provisioningStates: readonly string[]
  zones: readonly string[]
  pools: readonly string[]
  tags: readonly string[]
  hasGpu: boolean | null
}

/** The no-op filter: matches every server. */
export const EMPTY_SERVER_FILTERS: ServerFilters = {
  provisioningStates: [],
  zones: [],
  pools: [],
  tags: [],
  hasGpu: null,
}

/** True when no dimension constrains the result. */
export function isEmptyFilters(filters: ServerFilters): boolean {
  return (
    filters.provisioningStates.length === 0 &&
    filters.zones.length === 0 &&
    filters.pools.length === 0 &&
    filters.tags.length === 0 &&
    filters.hasGpu === null
  )
}

/** A dimension whose distinct values feed a filter list. */
export type ServerDimension = 'provisioningState' | 'zone' | 'pool' | 'tag' | 'architecture'

/** The placeholder label for a server that has no value on a grouped/filtered dimension. */
export const UNSET_GROUP = 'unknown'

/** The first MAC address, or null when the provisioner reported none. */
export function serverMacAddress(server: Server): string | null {
  return server.hardware.macAddresses[0] ?? null
}

/**
 * The group a server falls into for a given grouping, as a stable key and a display
 * label (identical here, but kept distinct so a future mapping can differ).
 */
export function groupValueOf(server: Server, groupBy: ServerGroupBy): string {
  switch (groupBy) {
    case 'provisioning':
      return server.provisioning?.state ?? UNSET_GROUP
    case 'power':
      return server.provisioning?.powerState ?? UNSET_GROUP
    case 'zone':
      return server.providerZone || UNSET_GROUP
    case 'pool':
      return server.providerResourcePool || UNSET_GROUP
    case 'architecture':
      return server.architecture || UNSET_GROUP
    case 'none':
    default:
      return ''
  }
}

/** Whether a server satisfies every active filter dimension. */
export function matchesServerFilters(server: Server, filters: ServerFilters): boolean {
  if (
    filters.provisioningStates.length > 0 &&
    !filters.provisioningStates.includes(server.provisioning?.state ?? UNSET_GROUP)
  ) {
    return false
  }
  if (filters.zones.length > 0 && !filters.zones.includes(server.providerZone || UNSET_GROUP)) {
    return false
  }
  if (filters.pools.length > 0 && !filters.pools.includes(server.providerResourcePool || UNSET_GROUP)) {
    return false
  }
  if (filters.tags.length > 0 && !filters.tags.some((tag) => server.tags.includes(tag))) {
    return false
  }
  if (filters.hasGpu !== null && server.gpus.length > 0 !== filters.hasGpu) {
    return false
  }
  return true
}

/** Total GPU count across a server's GPU entries, used for sorting and display. */
export function serverGpuCount(server: Server): number {
  return server.gpus.reduce((sum, gpu) => sum + gpu.count, 0)
}

/** The comparable value for a sort key; strings sort case-insensitively. */
function sortValue(server: Server, key: ServerSortKey): string | number {
  switch (key) {
    case 'name':
      return serverDisplayName(server).toLowerCase()
    case 'provisioning':
      return server.provisioning?.state ?? ''
    case 'power':
      return server.provisioning?.powerState ?? ''
    case 'cores':
      return server.cpuCores
    case 'memory':
      return server.memoryMiB
    case 'storage':
      return server.storageGB
    case 'zone':
      return (server.providerZone || '').toLowerCase()
    case 'pool':
      return (server.providerResourcePool || '').toLowerCase()
    default:
      return ''
  }
}

/**
 * Compares two servers by one column and direction. Ties fall back to display name so the
 * order is stable across renders (important for pagination and grouping).
 */
export function compareServers(
  a: Server,
  b: Server,
  key: ServerSortKey,
  direction: SortDirection,
): number {
  const left = sortValue(a, key)
  const right = sortValue(b, key)

  let result = 0
  if (typeof left === 'number' && typeof right === 'number') {
    result = left - right
  } else {
    result = String(left).localeCompare(String(right))
  }
  if (result === 0 && key !== 'name') {
    result = serverDisplayName(a).localeCompare(serverDisplayName(b))
  }
  return direction === 'asc' ? result : -result
}

/** The distinct values a server contributes to one dimension (tags are multi-valued). */
function dimensionValues(server: Server, dimension: ServerDimension): string[] {
  switch (dimension) {
    case 'provisioningState':
      return [server.provisioning?.state ?? UNSET_GROUP]
    case 'zone':
      return [server.providerZone || UNSET_GROUP]
    case 'pool':
      return [server.providerResourcePool || UNSET_GROUP]
    case 'architecture':
      return [server.architecture || UNSET_GROUP]
    case 'tag':
      return server.tags.length > 0 ? server.tags : []
    default:
      return []
  }
}

/**
 * Counts servers by distinct value of a dimension, for building filter option lists with
 * occurrence badges. The primary address is never a dimension; use `serverPrimaryAddress`
 * for display instead.
 */
export function countByDimension(
  servers: readonly Server[],
  dimension: ServerDimension,
): Map<string, number> {
  const counts = new Map<string, number>()
  for (const server of servers) {
    for (const value of dimensionValues(server, dimension)) {
      counts.set(value, (counts.get(value) ?? 0) + 1)
    }
  }
  return counts
}

// Re-exported so list presentation imports address/name helpers from one domain module.
export { serverDisplayName, serverPrimaryAddress }
