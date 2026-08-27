# Monitoring Alerts

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Expose firing/resolved Alertmanager alerts and create label-matched silences.

## Endpoints

```text
GET  /api/v1/monitoring/alerts?siteId=<optional>&serverId=<optional>&severity=<optional>&state=<optional>
POST /api/v1/monitoring/alerts/{fingerprint}/acknowledge?siteId=<optional>
```

Both routes require an admin bearer token. Alert items contain `fingerprint`,
`name`, `severity`, `state`, `summary`, `description`, `labels`,
`startsAt`, and optional correlated `serverId`, `siteId`, and `clusterId`.

Acknowledge accepts `matchers`, optional positive Go `duration`, and
`comment`; success returns `{"silenceId": "string"}`. The caller identity is
recorded as the actor. Missing provider configuration or query failures return
`503 provider_unavailable`; invalid filters, matchers, or duration return
`400 validation_error`.
