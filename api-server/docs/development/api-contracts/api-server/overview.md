# User Overview

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

Return one site-scoped, point-in-time read model for the user landing screen. The
response derives facts from their owning contexts and stores no overview state.

## Related Glossary Terms

- `Server`
- `Site`
- `Integration`
- `Platform`
- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)

## Endpoint

```text
GET /api/v1/overview
```

Requires the `admin` role, with either bearer credential (a Session access token or an API
Key) according to [conventions](conventions.md). The wire shape is published in
[`openapi/areas/monitoring.yaml`](../openapi/areas/monitoring.yaml) (`getOverview`).

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
    "gpuDevices": 64,
    "health": { "up": 37, "down": 1, "unknown": 2, "notApplicable": 8 }
  },
  "integrations": { "total": 4, "failing": 1, "items": [] },
  "platforms": { "total": 3, "unreachable": 1, "unmatchedMembers": 2 },
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

`inventory.health` counts every scoped Server once by its health state, classified exactly as
on the Server itself ([servers-list.md](servers-list.md#health)): `up`, `down`, `unknown`, and
`notApplicable` (the `not_applicable` state). Absent Servers count as `notApplicable`, so the four
counts add up to `inventory.servers`. A metrics read that fails leaves the Servers it could not
read `unknown` and does not fail the overview.

`scope.siteId` is `null` for the swallow-wide view.

Integration items contain `id`, `siteId`, `name`, `kind`, `providerKind`,
`enabled`, `lastSucceededAt`, and `lastError`; `lastSucceededAt` and `lastError` are `null`
when no sync has succeeded or failed. No credential is ever included.

Recent operations are the overview's own secret-free summary, projected from Workflows, and
contain at most eight entries ordered by `requestedAt` descending. Each carries `id`, `kind`,
`intent`, `siteId`, `platformId`, `targetServerIds`, `retryOfOperationId`, `requestedBy`,
`requestedAt`, `updatedAt`, and an `execution` compatibility projection with `runId`,
`playbook`, `status`, `statusReason`, `startedAt`, and `finishedAt`. `platformId`,
`retryOfOperationId`, `statusReason`, `startedAt`, and `finishedAt` are `null` when unset, and
`targetServerIds` is `null` (not `[]`) when the operation has no targets. `execution.status`
is a Workflow status ([workflows.md](workflows.md)). This is not the `GET /api/v1/workflows` item
shape.

Firing alert items reuse the Monitoring Alert shape of
[monitoring-alerts.md](monitoring-alerts.md) and
contain at most ten entries ordered by severity (`critical`, `warning`, then other) and
`startsAt` descending.

`operations.active` counts operations that have not finished: every status other than
`succeeded`, `failed`, `partially_succeeded`, and `canceled`.
Waiting, canceling, and `requires_attention` work is active, as it still holds its Servers.
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
which a consumer must not present as "no alerts". The embedded `error` carries only `code` and
`message` (no `requestId`, since the request succeeded); `error.message` is human-readable. Usage
charts and health use the metrics backend, not Alertmanager, and are unaffected by this
section. Durable repository failures are not partial and use the common error envelope.

## Errors

The common envelope applies. Unknown Site is `404 not_found`; unexpected repository or
mapping failure is `500 internal_error`; a missing or invalid credential is `401
unauthorized` and a non-admin caller is `403 forbidden`.

## Compatibility Notes

This endpoint is additive. New optional fields may be added to existing sections; section
removal, renaming, or semantic changes are breaking.

On 2026-10-12 `monitoring.configured` was added, and a scope without an alert source
(no enabled metrics Integration, or no Alertmanager address) now reports `error: null`
instead of `provider_unavailable`. This is a breaking semantic change for a consumer that
treated a non-null `error` as the only signal: such a consumer stops warning for a Site that
simply has no Alertmanager, which is the intended behavior. `available` keeps its meaning
(alerts were read).

Following the Cluster to Platform rename
([decision 015](../../../../../docs/decisions/015-platform-term-disambiguation.md) (platform term disambiguation)), the one-release
aliases `inventory.clustered`, the `clusters` section, and `clusterId` on recent operations and
firing alert items were removed on 2026-10-31
([decision 074](../../../../../docs/decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases)),
which is breaking; read `inventory.platformed`, `platforms`, and `platformId`. The schema-v2
Operation records no longer appear among recent operations, and no status is `indeterminate`.

On 2026-10-31 `operations.active` began counting every unfinished operation. It counted
only `pending` and `running`, so waiting, canceling, and `requires_attention` work, which still
holds Servers, was missing. This is a semantic change; the field and its type are unchanged.

On 2026-11-04 `inventory.health` gained the required `notApplicable` count, and `unknown`
narrowed to Servers swallow monitors but has no answer for. A Server swallow does not monitor
(for example `ready`, locked, absent, or at a Site whose automation is off) moved from `unknown`
to `notApplicable`, and a Server released by swallow is no longer counted `down` from its last
series. This is a breaking semantic change for a consumer that read `unknown` as every Server
without an answer.
