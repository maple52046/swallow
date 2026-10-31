# Infrastructure: Zones and Pools

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Manage swallow-owned **Zone** and **Pool** (resource pool) records and place a **Server** in a
Zone and/or Pool. Zones and Pools are Site-scoped groupings that swallow owns as desired state
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)): an
API write stores the spec and answers at once, and swallow's controllers make every
grouping-capable provisioner of the Site (MAAS) match it, including a provisioner added later.
This surface backs the dashboard *Infrastructure* area and the Server placement action.

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

`admin` only, consistent with the sites, integrations, and provisioning surfaces.

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
([`../openapi/areas/sites.yaml`](../openapi/areas/sites.yaml)). Zones and Pools have the same
shape and behavior; the sections below describe Zones and note that Pools are identical with
`pool`/`poolId` in place of `zone`/`zoneId`.

## Zone / Pool resource

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
  `providerName` is the group's name there (`""` until it exists); each entry has its own
  conditions with the meanings of [servers-list.md](servers-list.md#spec-and-status).
- `status.conditions` summarize the provisioners: `realized` is `true` only when every
  provisioner holds the current spec (and for a Site with no provisioner, which has nothing to
  realize); otherwise it carries the first unrealized provisioner's reason, for example
  `capability_missing` for a provisioner that cannot group (the spec is kept) or `converging`
  while a change is applied. `drifted` and `requires_attention` are `true` when any provisioner's
  are. `status.observedGeneration` is the generation every provisioner holds.
- Drift is a change made in the provisioner: the group was removed, renamed, or given another
  description there. It is reported at once and corrected at swallow's periodic resync (every 10
  minutes by default). A provisioner that refuses a change stops that provisioner with
  `requires_attention` until the spec changes.
- A group that exists only in a provisioner — created in the MAAS UI, or present before swallow
  managed the Site — is adopted into the catalog when swallow first sees it: a new Zone or Pool
  with the provisioner's name and description. The provisioner's default group (MAAS `default`)
  is never part of the catalog; it is where a Server with no placement belongs.
- A Zone or Pool stored before the upgrade to desired state has generation `0` until swallow
  first observes it. Then the description of the provisioner's group with its name becomes the
  spec and the generation becomes 1, with no provisioner write; if no provisioner holds such a
  group, the stored description becomes the spec and the group is created.

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
provider group of the same name is taken as it is).

### Get

`GET /api/v1/infrastructure/zones/{id}` returns the resource or `404`.

### Update

`PATCH /api/v1/infrastructure/zones/{id}`

```json
{ "name": "rack-a1", "description": "renamed" }
```

Both fields optional; only present fields change, and a present `name` follows the create rule.
A change increments the generation; a request that changes nothing returns the stored resource.
The group is renamed on each provisioner afterwards, so its machines stay in it.

### Delete

`DELETE /api/v1/infrastructure/zones/{id}` returns `204`. It is refused with `409 conflict` while
a Server's placement spec names the Zone. Otherwise the group is removed from every provisioner
that holds it, then the record; a provisioner's default group is left in place. This is the one
request that calls the provisioners: one that refuses (for example a group still holding
machines) aborts the delete with `400 validation_error` carrying its explanation, and one that
cannot be reached with `503`; either way the record stays.

## Server placement

`PUT /api/v1/servers/{id}/placement` sets a Server's Zone and/or Pool in its spec. The Server is
addressed by its `serverId`.

```json
{ "zoneId": "b8d1…", "poolId": null }
```

- Each field is optional. A string sets that grouping; an explicit `null` returns the Server to
  its provisioner's default group; an omitted field leaves it unchanged. A request that gives
  neither is `400 validation_error`.
- A referenced Zone/Pool must exist (`404 not_found`) and belong to the same Site as the Server
  (`400 validation_error` otherwise).
- The write is conditional on the Server's spec generation: `409 conflict` when the spec changed
  during the request, or when the Server is not adopted yet (a newly discovered Server is adopted
  within moments; retry).
- The Server controller then moves the machine; the Server's `status.conditions` report when it
  has. A provisioner that cannot group keeps the spec and reports `capability_missing`. A
  placement in a Zone that is not on the provisioner yet waits for the Zone to be created there.
  Moving a machine in the MAAS UI is drift, put back at the next periodic resync.

### Success Response

```json
{
  "serverId": "a1b2…",
  "generation": 5,
  "zoneId": "b8d1…",
  "poolId": null
}
```

The Server's placement spec after the request: its new `generation`, and the Zone and Pool ids,
`null` for the provisioner's default group. The observed zone and pool names are the Server's
`status.providerZone` and `status.providerResourcePool`.

## Error Codes

Beyond the shared codes in [conventions.md](conventions.md):

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | The body is not valid JSON; missing/blank `siteId` or `name`, or a `name` over 100 characters; a placement field that is not a string or `null`, a placement naming neither a Zone nor a Pool, or one in another Site; a provisioner refused a delete. |
| `not_found` | 404 | The Zone, Pool, Site, or Server does not exist. |
| `conflict` | 409 | A Zone/Pool with this `name` already exists in the Site; a Server is still placed in the Zone/Pool being deleted; a placement raced another spec change or targeted a Server not adopted yet. |
| `provider_unavailable` | 503 | A delete could not reach a provisioner, or it has no credential. |

## Compatibility Notes

New surface introduced with decision 029. Field names and enum-like values follow swallow's
glossary (`Zone`, `Pool`, `Site`, `Server`). Adding an optional field is backward compatible;
see [conventions.md](conventions.md) for the breaking-change rules.

On 2026-10-31 Zones, Pools, and Server placement became desired state
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)). Breaking,
with no alias: a Zone/Pool is `{id, siteId, generation, spec, status}` (`name` and `description`
moved under `spec`, `createdAt`/`updatedAt` under `status`, `providerRealized` was replaced by the
conditions); create and update no longer call the provisioner and no longer return `503` or a
provider `400`; a delete of a group still in a Server's placement is `409`. A placement now
writes the Server's spec: `null` clears a field instead of leaving it unchanged, the response is
`{serverId, generation, zoneId, poolId}` instead of the effective names, a provisioner without
grouping no longer fails the request, and `409` is returned for a Server not adopted yet.
Existing Zones and Pools are adopted at their first observation.

## Implementation Notes

- Zones and Pools are persisted by swallow (Mongo), keyed by a swallow-issued id, unique on
  `(siteId, name)`; a spec write increments the generation in the same update.
- The catalog controller (worker process) realizes each Zone and Pool through the provisioning
  `ProviderFactory` and the optional `GroupingController` capability, and adopts
  provisioner-only groups at its resync. The Server controller realizes placement with the same
  capability, using the group's name on the Server's own provisioner, so a Server follows a rename
  once the group is renamed there. No provider vocabulary appears in this contract.
