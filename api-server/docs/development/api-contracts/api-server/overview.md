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
    "configured": true,
    "available": true,
    "error": null,
    "firing": { "critical": 1, "warning": 2, "items": [] }
  }
}
```

`inventory.gpuDevices` counts only GPU Inventory items whose kind is `compute`. Display
controllers remain visible on their Server projections but do not contribute accelerator
capacity to the overview.

Integration items contain `id`, `siteId`, `name`, `kind`, `providerKind`,
`enabled`, `lastSucceededAt`, and `lastError`. Recent operations reuse the public
Operation summary fields and contain at most eight entries ordered by `requestedAt`
descending. Firing alert items reuse the Monitoring Alert shape and contain at most ten
entries ordered by severity (`critical`, `warning`, then other) and `startsAt`
descending.

`failedLast24Hours` counts failed operations whose `requestedAt` is within the 24
hours before `generatedAt`. `unmatchedMembers` is the sum of positive differences
between each platform's reported and matched member counts.

`monitoring` summarizes firing alerts read from Alertmanager. Alertmanager is optional, so
the section distinguishes "no alert source" from "alert source failing":

| State | `configured` | `available` | `error` | Meaning |
| --- | --- | --- | --- | --- |
| Alerts read | `true` | `true` | `null` | Counts and items are current. |
| Not configured | `false` | `false` | `null` | The scope has no enabled metrics Integration, or it sets no Alertmanager address. This is a normal state, not a failure. |
| Unavailable | `true` | `false` | `provider_unavailable` | An Alertmanager is configured but could not be read (unreachable, rejected credential, or an error response). |

In both non-read states the endpoint still returns `200` with empty items and zero counts,
which a consumer must not present as "no alerts". `error.message` is human-readable. Usage
charts and health use the metrics backend, not Alertmanager, and are unaffected by this
section. Durable repository failures are not partial and use the common error envelope.

## Errors

The common envelope applies. Unknown Site is `not_found`; unexpected repository or
mapping failure is `internal_error`; authentication and authorization use the common
codes.

## Compatibility Notes

This endpoint is additive. New optional fields may be added to existing sections; section
removal, renaming, or semantic changes are breaking.

On 2026-10-12 `monitoring.configured` was added, and a scope without an alert source
(no enabled metrics Integration, or no Alertmanager address) now reports `error: null`
instead of `provider_unavailable`. This is a breaking semantic change for a consumer that
treated a non-null `error` as the only signal: such a consumer stops warning for a Site that
simply has no Alertmanager, which is the intended behavior. `available` keeps its meaning
(alerts were read).

Following the Cluster to Platform rename, `inventory.clustered`, the `clusters` section,
and the `clusterId` field on recent operations are deprecated one-release aliases that
mirror `inventory.platformed`, the `platforms` section, and `platformId` respectively.
New consumers must read the platform-named fields; the aliases are removed after the
deprecation window (see
[ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)).
