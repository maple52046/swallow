# Monitoring Alerts

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Expose the alerts Alertmanager currently holds (firing and suppressed; resolved alerts are not
listed) and create label-matched silences.

swallow stores no alerts: they are read from Alertmanager on demand and correlated to swallow
resources from their label set, and acknowledging an alert creates an Alertmanager silence
([decision 003](../../../../../docs/decisions/003-metrics-label-contract.md), "alerts are read,
not received").

## Endpoints

```text
GET  /api/v1/monitoring/alerts?siteId=<optional>&serverId=<optional>&severity=<optional>&state=<optional>
POST /api/v1/monitoring/alerts/{fingerprint}/acknowledge?siteId=<optional>
```

Both routes require the `admin` role, with either bearer credential (a Session access token or
an API Key) according to [conventions](conventions.md). The wire shape is published in
[`openapi/areas/monitoring.yaml`](../openapi/areas/monitoring.yaml) (`listAlerts`,
`acknowledgeAlert`).

`siteId` selects the Site whose enabled metrics Integration supplies the Alertmanager address;
omitted, any enabled metrics Integration's is used. A `siteId` that names no Site is
`404 not_found`.

## List

The response is a plain JSON array (never paginated) of the alerts Alertmanager reports as
active, silenced, or inhibited, so an acknowledged alert stays visible as `suppressed` instead
of looking fixed. Items are ordered most severe first (`critical`, `warning`, `info`, then any
other severity), then by `startsAt` descending.

Filters narrow the array and are not validated: a value that matches no alert yields `[]`.

- `serverId` keeps alerts correlated to that Server.
- `severity` keeps alerts whose severity equals it, case-insensitively.
- `state` keeps alerts whose `state` equals it exactly.

Alert items contain `fingerprint`, `name`, `severity`, `state`, `summary`, `description`,
`labels`, `startsAt`, and the correlated `serverId`, `siteId`, `platformId`, and `clusterId`:

- `fingerprint` is Alertmanager's opaque identifier. `name` and `severity` are the `alertname`
  and `severity` labels; `summary` and `description` are the same-named annotations. Each is
  an empty string when the alert does not carry it.
- `state` is `firing` (active in Alertmanager), `suppressed` (silenced or inhibited), or
  `unknown`.
- `labels` is the alert's full label set, always an object.
- `startsAt` is a timestamp, or `null` when Alertmanager's value cannot be read.
- `serverId`, `siteId`, and `platformId` come from the `server_id`, `site`, and `platform_id`
  labels (`platformId` falls back to the legacy `platform`, then `cluster` label); each is
  `null` when the labels do not identify one. `clusterId` is a deprecated one-release alias
  that mirrors `platformId`
  ([ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)).

## Acknowledge

The path `fingerprint` names the alert being acknowledged and must be one the Site's
Alertmanager reports (active, silenced, or inhibited); otherwise the request is `404 not_found`.
The request body is optional JSON:

| Field | Type | Required | Meaning |
| --- | --- | ---: | --- |
| `matchers` | object of label name to string value | No | The labels the silence matches by exact equality. Each must be a label of the alert with the same value, so the silence covers it; `alertname` plus `server_id` also covers the alert's later instances. Omitted or empty, the silence matches the alert's full label set. |
| `duration` | string | No | A positive Go duration such as `"4h"`. Omitted or empty means `4h`. |
| `comment` | string | No | The silence comment. Omitted or empty means `Acknowledged in swallow`. |

Alertmanager silences match labels, not fingerprints, so the fingerprint selects the alert and
its labels, and the silence is built from them. The caller's username is recorded as the silence's creator. Success is `200` with
`{"silenceId": "string"}`, Alertmanager's id of the new silence.

## Errors

The common error envelope applies.

| Code | HTTP Status | When |
| --- | ---: | --- |
| `validation_error` | 400 | Acknowledge: the body is not valid JSON, a matcher is not a label of the alert, or `duration` is not a positive Go duration. Either route: Alertmanager rejected the request with a client error. |
| `unauthorized` / `forbidden` | 401 / 403 | Missing or invalid credential / a non-admin caller (conventions). |
| `not_found` | 404 | The `siteId` names no Site. Acknowledge: the Site's Alertmanager reports no alert with the fingerprint. |
| `provider_unavailable` | 503 | No metrics Integration is registered, it sets no Alertmanager address, or Alertmanager cannot be reached, rejects swallow's credential, or answers with a server error. |
| `internal_error` | 500 | An unexpected failure. |

## Compatibility Notes

- `clusterId` is a deprecated alias of `platformId` for one release; new consumers read
  `platformId`.
- Severity and state values are Alertmanager's and the alert rules'; the glossary has not yet
  defined an Alert value set (Pending Terms), so a consumer must tolerate any severity string.
- Since 2026-10-31 (DF-05) a `siteId` that names no Site is `404 not_found` on both routes; it was
  `503 provider_unavailable`, as if the Site had no metrics Integration.
- Since 2026-10-31 (DF-14) the acknowledge fingerprint names a reported alert (`404` otherwise),
  and `matchers` is optional and must be labels of that alert. Before, the fingerprint was not
  read and `matchers` was required, so a request could silence labels that did not cover the
  alert it named. A client that sends the alert's labels as `matchers`, as the dashboard does,
  keeps working.
