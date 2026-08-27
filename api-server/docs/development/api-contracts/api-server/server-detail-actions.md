# Server Detail and Actions

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Read one Server projection and execute provider-backed machine actions without
exposing provider credentials.

## Endpoints

```text
GET  /api/v1/servers/{id}
GET  /api/v1/servers/{id}/provisioner-detail
GET  /api/v1/servers/{id}/power-state
POST /api/v1/servers/{id}/deploy
POST /api/v1/servers/{id}/release
POST /api/v1/servers/{id}/{power-on|power-off|commission|test|abort}
POST /api/v1/servers/{id}/{override-failed-testing|lock|unlock}
POST /api/v1/servers/{id}/{mark-broken|mark-fixed|rescue-mode|exit-rescue-mode}
```

All routes require an admin bearer token. `GET /servers/{id}` returns the same
complete Server projection documented in [servers-list.md](servers-list.md).
Provisioner detail is a live provider-neutral view with a capability set. Power
state is read-only.

Deploy requires `distroSeries` and accepts `osSystem`, `userData`,
`comment`, and `ephemeral`. Provider actions return `202` with the accepted
provisioning snapshot. Unsupported capabilities and provider refusals use
`400 validation_error`; unavailable providers use
`503 provider_unavailable`; missing resources use `404 not_found`.
