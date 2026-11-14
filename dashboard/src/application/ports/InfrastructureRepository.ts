import type { PlatformPoolMove, Pool, ServerPlacement, Zone } from '@/domain/infrastructure/types'

/** Values accepted when creating a swallow-owned Zone; a Zone belongs to one Site. */
export interface CreateZoneInput {
  siteId: string
  name: string
  description: string
}

/** Values accepted when creating a Pool; a Pool belongs to the installation, not to a Site. */
export interface CreatePoolInput {
  name: string
  description: string
}

/** Mutable Zone/Pool fields; identity (and a Zone's Site) remain stable after creation. */
export interface UpdateGroupInput {
  name: string
  description: string
}

/**
 * A Server placement request. Each field is optional: an id places the Server in that Zone or Pool,
 * `null` returns it to the provisioner's default zone (`zoneId`) or takes it out of every Pool
 * (`poolId`), and an omitted field leaves the grouping unchanged. At least one must be present.
 */
export interface AssignServerPlacementInput {
  zoneId?: string | null
  poolId?: string | null
}

/**
 * Admin port for swallow-owned Zone and Pool management and Server placement.
 *
 * The repository speaks domain types only — screens never see the transport DTO. A Zone's
 * realization in the provisioner is the backend's concern and is reported through its conditions;
 * a Pool exists only in swallow. Reading Zones and Pools is open to every signed-in user; the
 * writes need an admin, and a non-admin caller's rejection surfaces as the thrown API error.
 */
export interface InfrastructureRepository {
  /** Lists Zones, optionally restricted to a single Site. */
  listZones(siteId?: string): Promise<Zone[]>
  createZone(input: CreateZoneInput): Promise<Zone>
  updateZone(id: string, input: UpdateGroupInput): Promise<Zone>
  deleteZone(id: string): Promise<void>
  /** Lists every Pool of the installation, ordered by name. */
  listPools(): Promise<Pool[]>
  createPool(input: CreatePoolInput): Promise<Pool>
  updatePool(id: string, input: UpdateGroupInput): Promise<Pool>
  /** Deletes a Pool with its members and Sudo Policy; refused while a Server is placed in it. */
  deletePool(id: string): Promise<void>
  /**
   * Sets a Server's Zone and/or Pool, returning its placement spec after the request. A Server a
   * Platform claims keeps its Pool: a Pool change for it is refused with `409 conflict`.
   */
  assignServerPlacement(serverId: string, input: AssignServerPlacementInput): Promise<ServerPlacement>
  /**
   * Moves every Server of a Platform to the Pool `poolId` (`null` for no Pool) in one write, keeping
   * their Zones. It is the only way a claimed Server changes Pool; a Server whose spec changed during
   * the move, or one not adopted yet, fails it whole with `409 conflict`.
   */
  movePlatformToPool(platformId: string, poolId: string | null): Promise<PlatformPoolMove>
}
