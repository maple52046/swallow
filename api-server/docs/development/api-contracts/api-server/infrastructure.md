# Infrastructure: Zones and Pools

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Manage swallow-owned **Zone** and **Pool** (resource pool) records and assign a **Server**
to a Zone and/or Pool. Zones and Pools are Site-scoped swallow-owned groupings; when the
Site's provisioner is grouping-capable (MAAS), swallow realizes the change in the provisioner
as part of the same request. See decision 029. This surface backs the dashboard
*Infrastructure* area.

These conventions are inherited from [conventions.md](conventions.md): base path,
bearer authentication, error envelope and code-to-status map, and timestamp format. Only the
endpoint-specific behavior is described below.

## Related Glossary Terms

- `Zone`
- `Pool`
- `Site`
- `Server`

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

Zones and Pools have the same shape and behavior; the sections below describe Zones and note
that Pools are identical with `pool`/`poolId` in place of `zone`/`zoneId`.

## Zone / Pool resource

```json
{
  "id": "b8d1…",
  "siteId": "b0a2…",
  "name": "rack-a",
  "description": "Top-of-rack A",
  "providerRealized": true,
  "createdAt": "2026-09-14T04:00:00Z",
  "updatedAt": "2026-09-14T04:00:00Z"
}
```

- `name` is unique within its `siteId`.
- `providerRealized` reports whether the most recent write was propagated to a
  grouping-capable provisioner. `false` means the Site has no grouping-capable provisioner
  and the record exists only in swallow; it is not an error.

### List

`GET /api/v1/infrastructure/zones` returns a plain array (the set is small and bounded).

| Query | Type | Required | Description |
| --- | --- | ---: | --- |
| `siteId` | string | No | Restrict to one Site. Omitted returns all Zones across Sites. |

### Create

`POST /api/v1/infrastructure/zones`

```json
{ "siteId": "b0a2…", "name": "rack-a", "description": "Top-of-rack A" }
```

`siteId` and `name` are required. Returns `201` with the created resource. If the Site's
provisioner is grouping-capable, the matching provider group is ensured (an existing
provider group of the same name is treated as satisfied).

### Get

`GET /api/v1/infrastructure/zones/{id}` returns the resource or `404`.

### Update

`PATCH /api/v1/infrastructure/zones/{id}`

```json
{ "name": "rack-a1", "description": "renamed" }
```

Both fields optional; only present fields change. A rename is propagated to the provider when
capable. Returns the updated resource.

### Delete

`DELETE /api/v1/infrastructure/zones/{id}` returns `204`. The matching provider group is
removed when the provisioner is grouping-capable and holds it; a provider that refuses to
delete a non-empty or protected group surfaces that refusal as `409`/`400`.

## Server placement

`PUT /api/v1/servers/{id}/placement` assigns or clears a Server's Zone and Pool. The Server
is addressed by its `serverId`.

```json
{ "zoneId": "b8d1…", "poolId": "c9e2…" }
```

- Each field is optional and nullable. A present non-null id assigns that grouping; an
  explicit `null` clears it; an omitted field leaves it unchanged.
- The referenced Zone/Pool must belong to the same Site as the Server (`422`→`validation_error`
  otherwise).
- The assignment is written through to the Server's provisioner. Because a Server always has a
  provisioner, a provisioner that lacks the grouping capability yields `validation_error`
  rather than being silently skipped.

### Success Response

```json
{
  "serverId": "a1b2…",
  "zone": "rack-a",
  "pool": "research"
}
```

`zone` / `pool` are the effective provider group names after the assignment; either is empty
when cleared or unset. The Server projection's observed zone/pool is refreshed by reconcile.

## Error Codes

Beyond the shared codes in [conventions.md](conventions.md):

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | Missing/invalid `siteId` or `name`; referenced Site/Zone/Pool invalid for the Server; provider refused the request; provisioner is not grouping-capable for a Server placement. |
| `not_found` | 404 | The Zone, Pool, Site, or Server does not exist. |
| `conflict` | 409 | A Zone/Pool with this `name` already exists in the Site, or the provider refused to delete a group still in use. |
| `provider_unavailable` | 503 | The Site's provisioner could not be reached or has no usable credential. |

## Compatibility Notes

New surface introduced with decision 029. Field names and enum-like values follow swallow's
glossary (`Zone`, `Pool`, `Site`, `Server`). Adding an optional field is backward compatible;
see [conventions.md](conventions.md) for the breaking-change rules.

## Implementation Notes

- Zones and Pools are persisted by swallow (Mongo), keyed by a swallow-issued id, unique on
  `(siteId, name)`.
- Provider realization reuses the `provisioning` `ProviderFactory` and the optional
  `GroupingController` capability; MAAS is the first realization. No provider vocabulary
  appears in this contract.
- `providerRealized` reflects the last write only; swallow does not currently reconcile its
  catalog against the provider's actual set.
