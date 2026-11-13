# Infrastructure: Zones and Pools

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

Manage swallow-owned **Zone** and **Pool** records and place a **Server** in a Zone and/or Pool.

- A Zone is a Site-scoped grouping that swallow owns as desired state
  ([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md) (provisioner realized desired state)):
  an API write stores the spec and answers at once, and swallow's controllers make every
  grouping-capable provisioner of the Site (MAAS) match it, including a provisioner added later.
- A Pool is installation-wide and exists only in swallow
  ([decision 082](../../../../../docs/decisions/082-accounts-pool-access-and-directory.md) (accounts, pool access, and directory)):
  it belongs to no Site, may hold Servers of several Sites, and is never written to a provisioner.

This surface backs the dashboard *Infrastructure* Zones, the Pools page, and the Server placement action.

These conventions are inherited from [conventions.md](conventions.md): base path,
bearer authentication, error envelope and code-to-status map, and timestamp format. Only the
endpoint-specific behavior is described below.

## Related Glossary Terms

- `Zone`
- `Pool`
- `Site`
- `Server`
- `Controller`
- `Condition`

## Authentication

Bearer token, per [conventions.md](conventions.md).

## Authorization

Reading Zones and Pools is reference; creating, changing, and deleting them and
`PUT /api/v1/servers/{id}/placement` are admin ([conventions](conventions.md#authorization)). A
Pool's members and Sudo Policy are in [pool-access.md](pool-access.md).

## Endpoints

```text
GET    /api/v1/infrastructure/zones
POST   /api/v1/infrastructure/zones
GET    /api/v1/infrastructure/zones/{id}
PATCH  /api/v1/infrastructure/zones/{id}
DELETE /api/v1/infrastructure/zones/{id}

GET    /api/v1/infrastructure/pools
POST   /api/v1/infrastructure/pools
GET    /api/v1/infrastructure/pools/{id}
PATCH  /api/v1/infrastructure/pools/{id}
DELETE /api/v1/infrastructure/pools/{id}

PUT    /api/v1/servers/{id}/placement
```

The wire shape is the `sites` area of the OpenAPI contract
([`../openapi/areas/sites.yaml`](../openapi/areas/sites.yaml)).

## Zone resource

```json
{
  "id": "b8d1…",
  "siteId": "b0a2…",
  "generation": 2,
  "spec": { "name": "rack-a", "description": "Top-of-rack A" },
  "status": {
    "observedGeneration": 2,
    "conditions": [
      { "type": "realized", "status": "true", "reason": "converged", "message": "", "lastTransitionTime": "2026-10-31T04:00:00Z", "observedGeneration": 2 },
      { "type": "drifted", "status": "false", "reason": "in_sync", "message": "", "lastTransitionTime": "2026-10-31T04:00:00Z", "observedGeneration": 2 },
      { "type": "requires_attention", "status": "false", "reason": "none", "message": "", "lastTransitionTime": "2026-10-31T04:00:00Z", "observedGeneration": 2 }
    ],
    "provisioners": [
      {
        "integrationId": "c7f3…",
        "providerName": "rack-a",
        "observedGeneration": 2,
        "observedAt": "2026-10-31T04:10:00Z",
        "conditions": [ { "type": "realized", "status": "true", "reason": "converged", "message": "", "lastTransitionTime": "2026-10-31T04:00:00Z", "observedGeneration": 2 } ]
      }
    ],
    "createdAt": "2026-09-14T04:00:00Z",
    "updatedAt": "2026-10-31T04:00:00Z"
  }
}
```

- `spec.name` is unique within its `siteId`. `generation` counts spec changes.
- `status.provisioners` has one entry per enabled provisioner Integration of the Site.
  `providerName` is the zone's name there (`""` until it exists); each entry has its own
  conditions with the meanings of [servers-list.md](servers-list.md#spec-and-status).
- `status.conditions` summarize the provisioners: `realized` is `true` only when every
  provisioner holds the current spec (and for a Site with no provisioner, which has nothing to
  realize); otherwise it carries the first unrealized provisioner's reason, for example
  `capability_missing` for a provisioner that cannot group (the spec is kept) or `converging`
  while a change is applied. `drifted` and `requires_attention` are `true` when any provisioner's
  are. `status.observedGeneration` is the generation every provisioner holds.
- Drift is a change made in the provisioner: the zone was removed, renamed, or given another
  description there. It is reported at once and corrected at swallow's periodic resync (every 10
  minutes by default). A provisioner that refuses a change stops that provisioner with
  `requires_attention` until the spec changes.
- A zone that exists only in a provisioner — created in the MAAS UI, or present before swallow
  managed the Site — is adopted into the catalog when swallow first sees it: a new Zone with the
  provisioner's name and description. The provisioner's default zone (MAAS `default`) is never
  part of the catalog; it is where a Server with no Zone placement belongs.
- A Zone stored before the upgrade to desired state has generation `0` until swallow first
  observes it. Then the description of the provisioner's zone with its name becomes the spec and
  the generation becomes 1, with no provisioner write; if no provisioner holds such a zone, the
  stored description becomes the spec and the zone is created.

### List

`GET /api/v1/infrastructure/zones` returns a plain array ordered by `spec.name` (the set is
small and bounded).

| Query | Type | Required | Description |
| --- | --- | ---: | --- |
| `siteId` | string | No | Restrict to one Site. Omitted returns all Zones across Sites. |

### Create

`POST /api/v1/infrastructure/zones`

```json
{ "siteId": "b0a2…", "name": "rack-a", "description": "Top-of-rack A" }
```

`siteId` and `name` are required. `name` is trimmed and must be 1 to 100 characters after
trimming. Returns `201` with the stored resource at generation 1 and empty conditions; no
provisioner is called. The Zone is created on each provisioner shortly after (an existing
provider zone of the same name is taken as it is).

### Get

`GET /api/v1/infrastructure/zones/{id}` returns the resource or `404`.

### Update

`PATCH /api/v1/infrastructure/zones/{id}`

```json
{ "name": "rack-a1", "description": "renamed" }
```

Both fields optional; only present fields change, and a present `name` follows the create rule.
A change increments the generation; a request that changes nothing returns the stored resource.
The zone is renamed on each provisioner afterwards, so its machines stay in it.

### Delete

`DELETE /api/v1/infrastructure/zones/{id}` returns `204`. It is refused with `409 conflict` while
a Server's placement spec names the Zone. Otherwise the zone is removed from every provisioner
that holds it, then the record; a provisioner's default zone is left in place. This is the one
request that calls the provisioners: one that refuses (for example a zone still holding
machines) aborts the delete with `400 validation_error` carrying its explanation, and one that
cannot be reached with `503`; either way the record stays.

## Pool resource

```json
{
  "id": "4c1e…",
  "name": "research",
  "description": "GPU nodes of the research team",
  "createdAt": "2026-11-13T04:00:00Z",
  "updatedAt": "2026-11-13T04:00:00Z"
}
```

- `name` is unique within the installation.
- A Pool needs no realization: it has no `generation` or conditions, and no request on it calls a
  provisioner. A provisioner's own resource pools are not Pools; the one a Server is in is its
  observed `status.providerResourcePool` ([servers-list.md](servers-list.md)).

### List

`GET /api/v1/infrastructure/pools` returns every Pool as a plain array ordered by `name`. It takes no
query parameters.

### Create

`POST /api/v1/infrastructure/pools`

```json
{ "name": "research", "description": "GPU nodes of the research team" }
```

`name` is required, trimmed, and must be 1 to 100 characters after trimming. Returns `201` with the
stored Pool.

### Get

`GET /api/v1/infrastructure/pools/{id}` returns the Pool or `404`.

### Update

`PATCH /api/v1/infrastructure/pools/{id}` takes `name` and `description`, both optional, with the
create rule for a present `name`. A request that changes nothing returns the stored Pool. Renaming a
Pool keeps its members, its Sudo Policy, and its POSIX group names.

### Delete

`DELETE /api/v1/infrastructure/pools/{id}` returns `204` and deletes the Pool's memberships and Sudo
Policy with it ([pool-access.md](pool-access.md)). It is refused with `409 conflict` while a Server's
placement spec names the Pool.

## Server placement

`PUT /api/v1/servers/{id}/placement` sets a Server's Zone and/or Pool in its spec. The Server is
addressed by its `serverId`.

```json
{ "zoneId": "b8d1…", "poolId": null }
```

- Each field is optional. A string sets that grouping; an explicit `null` returns the Server to
  its provisioner's default zone (`zoneId`) or takes it out of every Pool (`poolId`); an omitted
  field leaves it unchanged. A request that gives neither is `400 validation_error`.
- A referenced Zone or Pool must exist (`404 not_found`). A Zone must belong to the same Site as
  the Server (`400 validation_error` otherwise); a Pool may hold Servers of any Site.
- A Server claimed by a Platform may be placed only in the Pool the Platform belongs to, or in no Pool:
  any other Pool is `409 conflict` ([platforms.md](platforms.md#platform-and-pool)). Clearing the Pool
  leaves the Platform in no Pool, which only admins manage; then its Servers may be placed in any Pool,
  which is how an admin moves a Platform to another Pool.
- The write is conditional on the Server's spec generation: `409 conflict` when the spec changed
  during the request, or when the Server is not adopted yet (a newly discovered Server is adopted
  within moments; retry).
- A Pool placement takes effect when it is written: access follows it at once, and nothing changes in
  the provisioner. A newly adopted Server is in no Pool until an admin places it.
- The Server controller moves the machine to its Zone; the Server's `status.conditions` report when it
  has. A provisioner that cannot group keeps the spec and reports `capability_missing`. A placement
  in a Zone that is not on the provisioner yet waits for the Zone to be created there. Moving a
  machine to another zone in the MAAS UI is drift, put back at the next periodic resync; moving it to
  another resource pool is not.

### Success Response

```json
{
  "serverId": "a1b2…",
  "generation": 5,
  "zoneId": "b8d1…",
  "poolId": null
}
```

The Server's placement spec after the request: its new `generation`, the Zone id (`null` for the
provisioner's default zone), and the Pool id (`null` for no Pool). The observed zone and resource
pool names are the Server's `status.providerZone` and `status.providerResourcePool`.

## Error Codes

Beyond the shared codes in [conventions.md](conventions.md):

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | The body is not valid JSON; a missing/blank `name`, a Zone without `siteId`, or a `name` over 100 characters; a placement field that is not a string or `null`, a placement naming neither a Zone nor a Pool, or a Zone in another Site; a provisioner refused a Zone delete. |
| `not_found` | 404 | The Zone, Pool, Site, or Server does not exist. |
| `conflict` | 409 | A Zone with this `name` already exists in the Site, or a Pool with this `name` in the installation; a Server is still placed in the Zone/Pool being deleted; a placement raced another spec change or targeted a Server not adopted yet; a placement would move a Platform-claimed Server into a Pool other than the Platform's. |
| `provider_unavailable` | 503 | A Zone delete could not reach a provisioner, or it has no credential. |

## Compatibility Notes

New surface introduced with decision 071. Field names and enum-like values follow swallow's
glossary (`Zone`, `Pool`, `Site`, `Server`). Adding an optional field is backward compatible;
see [conventions.md](conventions.md) for the breaking-change rules.

On 2026-10-31 Zones, Pools, and Server placement became desired state
(decision 071). Breaking,
with no alias: a Zone/Pool is `{id, siteId, generation, spec, status}` (`name` and `description`
moved under `spec`, `createdAt`/`updatedAt` under `status`, `providerRealized` was replaced by the
conditions); create and update no longer call the provisioner and no longer return `503` or a
provider `400`; a delete of a group still in a Server's placement is `409`. A placement now
writes the Server's spec: `null` clears a field instead of leaving it unchanged, the response is
`{serverId, generation, zoneId, poolId}` instead of the effective names, a provisioner without
grouping no longer fails the request, and `409` is returned for a Server not adopted yet.

Since 2026-11-13 (decision 082) any authenticated User may read Zones and Pools, and a placement
that would move a Platform-claimed Server into a Pool other than the Platform's is `409`. Deleting a Pool also
deletes its memberships and Sudo Policy.

The same day Pools became installation-wide and swallow-only (decision 082). Breaking, with no alias:
a Pool is `{id, name, description, createdAt, updatedAt}` (no `siteId`, `generation`, `spec`, or
`status`); its names are unique in the installation; the list no longer takes `siteId` and create no
longer accepts it; a Pool delete no longer calls the provisioners and no longer returns its `400` or
`503`; a placement may name a Pool of another Site; `poolId: null` means no Pool instead of the
provisioner's default resource pool; and a Pool placement no longer moves the machine. On upgrade each
Site's Pool named `default` is merged into one Pool and every other Pool is deleted, with its memberships,
Sudo Policy, and the placements that named it.

## Implementation Notes

- Zones and Pools are persisted by swallow (Mongo), keyed by a swallow-issued id; Zones are unique on
  `(siteId, name)` and a Zone spec write increments the generation in the same update; Pools are
  unique on `name`.
- The catalog controller (worker process) realizes each Zone through the provisioning
  `ProviderFactory` and the optional `GroupingController` capability, and adopts
  provisioner-only zones at its resync. The Server controller realizes the Zone placement with the
  same capability, using the zone's name on the Server's own provisioner, so a Server follows a
  rename once the zone is renamed there. No provider vocabulary appears in this contract.
