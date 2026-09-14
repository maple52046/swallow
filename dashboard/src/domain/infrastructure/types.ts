/**
 * swallow-owned Infrastructure grouping: Zone and Pool.
 *
 * A Zone groups Servers for availability/fault/organization; a Pool (resource pool)
 * partitions them for allocation. Both are Site-scoped and swallow-owned, and are realized in
 * the Site's provisioner (MAAS) when it is grouping-capable. See the Zone and Pool glossary
 * terms and docs/decisions/029. This mirrors the provider-owned contract shape returned by
 * `/api/v1/infrastructure`; it carries no HTTP, React, or provider vocabulary.
 */

/**
 * Which grouping a value is. The two resources are structurally identical, so a single kind
 * discriminator lets one set of screens and one adapter serve both without duplication; their
 * distinct domain meaning is carried by this value and the endpoint it maps to.
 */
export type GroupKind = 'zone' | 'pool'

/**
 * One swallow-owned Zone or Pool.
 *
 * `name` is unique within `siteId`. `providerRealized` reflects only the most recent write: it
 * is true when that write was propagated to a grouping-capable provisioner and false when the
 * Site had none, in which case the record exists in swallow alone. It is not a live
 * reconciliation against the provider's actual set.
 */
export interface GroupingResource {
  readonly id: string
  readonly siteId: string
  readonly name: string
  readonly description: string
  readonly providerRealized: boolean
  readonly createdAt: string
  readonly updatedAt: string
}

/**
 * The effective provider group names for a Server after a placement. A field is empty when the
 * Server has no such grouping; a field left unchanged by the request carries its current value.
 */
export interface ServerPlacement {
  readonly serverId: string
  readonly zone: string
  readonly pool: string
}

/** Human-facing label for a grouping kind, used in headings, buttons, and messages. */
export function groupKindLabel(kind: GroupKind): 'Zone' | 'Pool' {
  return kind === 'zone' ? 'Zone' : 'Pool'
}
