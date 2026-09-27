# Operator Overview

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Return one site-scoped, point-in-time read model for the operator landing screen. The
response derives facts from their owning contexts and stores no overview state.

## Related Glossary Terms

- `Server`
- `Site`
- `Integration`
- `Platform`
- [Operation](../../../../../docs/development/glossaries/terms/operation.md)

## Endpoint

```text
GET /api/v1/overview
```

An admin JWT is required according to [conventions](conventions.md).

## Query

| Param | Type | Required | Meaning |
| --- | --- | ---: | --- |
| `siteId` | string | No | Scope every section to one Site. Omit for the swallow-wide view. |

An unknown `siteId` returns `not_found` rather than an empty overview.

## Response

`200 OK`:

```json
{
  "generatedAt": "2026-08-27T08:00:00Z",
  "scope": { "siteId": null },
  "inventory": {
    "sites": 2,
    "servers": 48,
    "absent": 1,
    "deployed": 40,
    "platformed": 32,
    "clustered": 32,
    "gpuDevices": 64,
    "health": { "up": 45, "down": 2, "unknown": 1 }
  },
  "integrations": { "total": 4, "failing": 1, "items": [] },
  "platforms": { "total": 3, "unreachable": 1, "unmatchedMembers": 2 },
  "clusters": { "total": 3, "unreachable": 1, "unmatchedMembers": 2 },
  "operations": { "active": 1, "failedLast24Hours": 2, "recent": [] },
  "monitoring": {
    "available": true,
    "error": null,
    "firing": { "critical": 1, "warning": 2, "items": [] }
  }
}
```

Integration items contain `id`, `siteId`, `name`, `kind`, `providerKind`,
`enabled`, `lastSucceededAt`, and `lastError`. Recent operations reuse the public
Operation summary fields and contain at most eight entries ordered by `requestedAt`
descending. Firing alert items reuse the Monitoring Alert shape and contain at most ten
entries ordered by severity (`critical`, `warning`, then other) and `startsAt`
descending.

`failedLast24Hours` counts failed operations whose `requestedAt` is within the 24
hours before `generatedAt`. `unmatchedMembers` is the sum of positive differences
between each platform's reported and matched member counts.

When Alertmanager or its metrics integration is missing or unreachable, the endpoint
still returns `200`: `monitoring.available` is `false`, items and counts are empty,
and `monitoring.error` contains the common `provider_unavailable` code and a
human-readable message. Durable repository failures are not partial and use the common
error envelope.

## Errors

The common envelope applies. Unknown Site is `not_found`; unexpected repository or
mapping failure is `internal_error`; authentication and authorization use the common
codes.

## Compatibility Notes

This endpoint is additive. New optional fields may be added to existing sections; section
removal, renaming, or semantic changes are breaking.

Following the Cluster to Platform rename, `inventory.clustered`, the `clusters` section,
and the `clusterId` field on recent operations are deprecated one-release aliases that
mirror `inventory.platformed`, the `platforms` section, and `platformId` respectively.
New consumers must read the platform-named fields; the aliases are removed after the
deprecation window (see
[ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)).
