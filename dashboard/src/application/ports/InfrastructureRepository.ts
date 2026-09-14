import type { GroupKind, GroupingResource, ServerPlacement } from '@/domain/infrastructure/types'

/** Values accepted when creating a swallow-owned Zone or Pool. */
export interface CreateGroupInput {
  siteId: string
  name: string
  description: string
}

/** Mutable Zone/Pool fields; Site ownership and identity remain stable after creation. */
export interface UpdateGroupInput {
  name: string
  description: string
}

/**
 * A Server placement request. Each field is optional: a present id assigns that grouping, an
 * explicit `null` is not used by the UI, and an omitted field leaves the grouping unchanged.
 * At least one of the two must be present.
 */
export interface AssignServerPlacementInput {
  zoneId?: string
  poolId?: string
}

/**
 * Admin port for swallow-owned Zone/Pool management and Server placement.
 *
 * Methods are keyed by {@link GroupKind} so one adapter and one set of screens serve both
 * resources; the adapter maps the kind onto the provider's `/zones` or `/pools` endpoint. The
 * repository speaks domain types only — screens never see the transport DTO. Realization in the
 * provisioner is the backend's concern and is reported through `providerRealized`.
 */
export interface InfrastructureRepository {
  /** Lists groupings of one kind, optionally restricted to a single Site. */
  listGroups(kind: GroupKind, siteId?: string): Promise<GroupingResource[]>
  createGroup(kind: GroupKind, input: CreateGroupInput): Promise<GroupingResource>
  updateGroup(kind: GroupKind, id: string, input: UpdateGroupInput): Promise<GroupingResource>
  deleteGroup(kind: GroupKind, id: string): Promise<void>
  /** Assigns a Server to a Zone and/or Pool, returning the effective provider group names. */
  assignServerPlacement(serverId: string, input: AssignServerPlacementInput): Promise<ServerPlacement>
}
