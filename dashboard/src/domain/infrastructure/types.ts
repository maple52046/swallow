/**
 * swallow-owned Server groupings: Zone and Pool.
 *
 * A Zone groups Servers of one Site for availability/fault/organization. It is Site-scoped desired
 * state (docs/decisions/071): the dashboard writes the spec, and the backend's catalog controller
 * makes every grouping-capable provisioner of the Site (MAAS) match it, reporting progress in the
 * status. A Pool partitions Servers for allocation and access across the whole installation
 * (docs/decisions/082): it belongs to no Site and exists only in swallow, so it has no generation
 * or status. See the Zone and Pool glossary terms. These mirror the provider-owned contract shapes
 * returned by `/api/v1/infrastructure`; they carry no HTTP, React, or provider vocabulary.
 */

import type { Condition } from '@/domain/convergence/types'

/** The desired name (unique within the Site) and description of a Zone. */
export interface ZoneSpec {
  readonly name: string
  readonly description: string
}

/**
 * A Zone's realization on one provisioner Integration of its Site. `providerName` is the zone's
 * name there, empty until it exists; `observedAt` is null before the first check.
 */
export interface ZoneRealization {
  readonly integrationId: string
  readonly providerName: string
  readonly observedGeneration: number
  readonly observedAt: string | null
  readonly conditions: Condition[]
}

/**
 * The backend's report on a Zone. `conditions` summarize every provisioner (realized only when all
 * of them hold the current spec); `provisioners` has one entry per provisioner.
 */
export interface ZoneStatus {
  readonly observedGeneration: number
  readonly conditions: Condition[]
  readonly provisioners: ZoneRealization[]
  readonly createdAt: string
  readonly updatedAt: string
}

/**
 * One swallow-owned Zone as a migrated resource: identity, its Site, `generation` (counts spec
 * changes), the spec the dashboard edits, and the read-only status.
 */
export interface Zone {
  readonly id: string
  readonly siteId: string
  readonly generation: number
  readonly spec: ZoneSpec
  readonly status: ZoneStatus
}

/**
 * One installation-wide Pool: the unit of access (members and Sudo Policy hang off its id) that may
 * hold Servers of any Site. `name` is unique in the installation. Nothing realizes a Pool, so a
 * write takes effect when it is answered and there is no status to wait for.
 */
export interface Pool {
  readonly id: string
  readonly name: string
  readonly description: string
  readonly createdAt: string
  readonly updatedAt: string
}

/**
 * A Server's placement spec after a placement request: its new spec generation, the Zone id (null
 * for the provisioner's default zone), and the Pool id (null for no Pool). The machine is moved to
 * its Zone afterwards, and the Server's conditions report when it has; a Pool takes effect at once.
 */
export interface ServerPlacement {
  readonly serverId: string
  readonly generation: number
  readonly zoneId: string | null
  readonly poolId: string | null
}
