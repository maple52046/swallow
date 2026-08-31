# Server Detail and Actions

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Read one Server projection, execute provider-backed machine actions, and permanently
delete a Server together with its backing provisioner Machine without exposing provider
credentials.

## Endpoints

```text
GET  /api/v1/servers/{id}
DELETE /api/v1/servers/{id}
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
Provisioner detail is a live provider-neutral view with a capability set. The
`machineRemoval` flag tells clients whether provider-backed deletion is available. Power
state is read-only.

`DELETE /servers/{id}` is provider-first and synchronous. It resolves the Server's
provisioner, deletes the backing Machine, and only then removes the Swallow projection.
If the provisioner reports that the Machine is already missing, Swallow removes the stale
projection and still succeeds. Provider refusal, authentication failure, or unavailability
leaves the projection intact. Swallow never adds a provider-specific force flag implicitly;
an operator must resolve any provider safeguard before retrying. Success is `204 No Content`.

Deploy requires `distroSeries` and accepts `osSystem`, `userData`,
`comment`, and `ephemeral`. Other provider actions return `202` with the accepted
provisioning snapshot. Unsupported capabilities and provider refusals use
`400 validation_error`; unavailable providers use
`503 provider_unavailable`; missing resources use `404 not_found`.
