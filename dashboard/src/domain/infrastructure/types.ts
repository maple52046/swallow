/**
 * swallow-owned Infrastructure grouping: Zone and Pool.
 *
 * A Zone groups Servers for availability/fault/organization; a Pool (resource pool)
 * partitions them for allocation. Both are Site-scoped desired state (docs/decisions/071): the
 * dashboard writes the spec, and the backend's catalog controller makes every grouping-capable
 * provisioner of the Site (MAAS) match it, reporting progress in the status. See the Zone and Pool
 * glossary terms. This mirrors the provider-owned contract shape returned by
 * `/api/v1/infrastructure`; it carries no HTTP, React, or provider vocabulary.
 */

import type { Condition } from '@/domain/convergence/types'

/**
 * Which grouping a value is. The two resources are structurally identical, so a single kind
 * discriminator lets one set of screens and one adapter serve both without duplication; their
 * distinct domain meaning is carried by this value and the endpoint it maps to.
 */
export type GroupKind = 'zone' | 'pool'

/** The desired name (unique within the Site) and description of a Zone or Pool. */
export interface GroupSpec {
  readonly name: string
  readonly description: string
}

/**
 * A Zone's or Pool's realization on one provisioner Integration of its Site. `providerName` is the
 * group's name there, empty until it exists; `observedAt` is null before the first check.
 */
export interface GroupRealization {
  readonly integrationId: string
  readonly providerName: string
  readonly observedGeneration: number
  readonly observedAt: string | null
  readonly conditions: Condition[]
}

/**
 * The backend's report on a Zone or Pool. `conditions` summarize every provisioner (realized
 * only when all of them hold the current spec); `provisioners` has one entry per provisioner.
 */
export interface GroupStatus {
  readonly observedGeneration: number
  readonly conditions: Condition[]
  readonly provisioners: GroupRealization[]
  readonly createdAt: string
  readonly updatedAt: string
}

/**
 * One swallow-owned Zone or Pool as a migrated resource: identity, `generation` (counts spec
 * changes), the spec the dashboard edits, and the read-only status.
 */
export interface GroupingResource {
  readonly id: string
  readonly siteId: string
  readonly generation: number
  readonly spec: GroupSpec
  readonly status: GroupStatus
}

/**
 * A Server's placement spec after a placement request: its new spec generation and the Zone and
 * Pool ids, null for the provisioner's default group. The machine is moved afterwards; the
 * Server's conditions report when it has.
 */
export interface ServerPlacement {
  readonly serverId: string
  readonly generation: number
  readonly zoneId: string | null
  readonly poolId: string | null
}

/** Human-facing label for a grouping kind, used in headings, buttons, and messages. */
export function groupKindLabel(kind: GroupKind): 'Zone' | 'Pool' {
  return kind === 'zone' ? 'Zone' : 'Pool'
}
