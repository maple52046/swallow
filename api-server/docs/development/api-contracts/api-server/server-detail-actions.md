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
POST /api/v1/servers/{id}/refresh
DELETE /api/v1/servers/{id}
GET  /api/v1/servers/{id}/provisioner-detail
GET  /api/v1/servers/{id}/events?limit=50
GET  /api/v1/servers/{id}/power-state
GET  /api/v1/servers/{id}/network
POST /api/v1/servers/{id}/network/interfaces/{interfaceId}/links
PUT  /api/v1/servers/{id}/network/interfaces/{interfaceId}/links/{linkId}
DELETE /api/v1/servers/{id}/network/interfaces/{interfaceId}/links/{linkId}
GET  /api/v1/servers/{id}/provisioning-tasks
POST /api/v1/servers/{id}/deploy
POST /api/v1/servers/{id}/release
POST /api/v1/servers/{id}/{power-on|power-off|commission|test|abort}
POST /api/v1/servers/{id}/{override-failed-testing|lock|unlock}
POST /api/v1/servers/{id}/{mark-broken|mark-fixed|rescue-mode|exit-rescue-mode}
```

All routes require an admin bearer token. `GET /servers/{id}` returns the same
complete Server projection documented in [servers-list.md](servers-list.md).
Provisioner detail is a live provider-neutral view with a capability set. The
`machineRemoval` flag tells clients whether provider-backed deletion is available, and
`releaseOptions` tells clients whether release can carry disk-erasure controls. Power
state is read-only.

`POST /servers/{id}/refresh` performs one targeted live read from the Server's
provisioner and updates its provisioning axis plus observed addresses. It returns `200`
with the same `ProvisioningStateItem` shape used by accepted lifecycle actions. Clients
use this bounded endpoint to follow an asynchronous action such as Release without
waiting for the fleet inventory interval. It is not a full inventory reconciliation: it
does not create Servers, mark missing machines absent, or rewrite identity and hardware
fields.

`DELETE /servers/{id}` is provider-first and synchronous. It resolves the Server's
provisioner, deletes the backing Machine, and only then removes the Swallow projection.
If the provisioner reports that the Machine is already missing, Swallow removes the stale
projection and still succeeds. Provider refusal, authentication failure, or unavailability
leaves the projection intact. Swallow never adds a provider-specific force flag implicitly;
an operator must resolve any provider safeguard before retrying. Success is `204 No Content`.

`POST /servers/{id}/release` accepts an absent body for backwards compatibility,
or this optional JSON body:

```json
{
  "erase": true,
  "secureErase": true,
  "quickErase": true,
  "comment": "retire from test pool",
  "unbindStaticIPs": true
}
```

`erase=false` releases without wiping disks. `erase=true` with neither
refinement requests a full zero overwrite. `secureErase` requests the device
hardware erase command; `quickErase` wipes only the beginning and end of each
disk and is not secure. When both are true, MAAS prefers secure erase and uses
quick erase when secure erase is unavailable. `secureErase` and `quickErase`
require `erase=true`. Swallow never accepts or forwards MAAS `force`, so
provider safeguards remain authoritative. A provider without configurable
release support can still accept the parameterless form and refuses non-default
options with `400 validation_error`.

`unbindStaticIPs` defaults to false. When true, Swallow persists a
release-network-cleanup Provisioning Task before asking the provider to release
the Machine. The accepted provisioning response adds optional `taskId`. The task
waits until the Server is Ready, then removes only unchanged static links from
the pre-release snapshot; DHCP, provider-managed, link-only, and subsequently
changed links are preserved. A failed task can be retried without releasing the
Machine again.

## Structured Network Configuration

`GET /servers/{id}/network` returns the same typed target observation documented
by the provisioning network-inspection contract. The link mutation endpoints
accept `mode=dhcp|static|link_only`, required `subnetId`, optional
`defaultGateway` for Static, and an `ipAddress` required only for Static.
DELETE unbinds the addressed link and has no request body.

Network writes require a present, unlocked Server in provisioning state `ready`.
They mutate only the addressed link, never forward a provider force option, then
read and verify the resulting provider state before returning the full refreshed
Network Configuration. An unknown Server or provider Machine is `404 not_found`;
an unknown interface, link, or subnet is `400 validation_error`, as are an invalid
mode or address. Incompatible
Server state is `409 conflict`; provider unavailability is
`503 provider_unavailable`.

Deploy requires `distroSeries` and accepts `osSystem`, `userData`,
`comment`, and `ephemeral`. Other provider actions return `202` with the accepted
provisioning snapshot. Unsupported capabilities and provider refusals use
`400 validation_error`; unavailable providers use
`503 provider_unavailable`; missing resources use `404 not_found`. Every error
uses the shared envelope's `requestId`, which correlates the client-visible
detail with the structured provisioning log entry without exposing provider
credentials or request bodies.

## Provider Events

`GET /servers/{id}/events` reads the recent machine history retained by the
provisioner. `limit` defaults to 50 and accepts 1 through 100. It returns:

```json
{
  "supported": true,
  "events": [
    {
      "id": "4812",
      "level": "audit",
      "type": "Request from user",
      "message": "Started releasing machine.",
      "actor": "admin",
      "occurredAt": "2026-09-02T01:02:03.000000Z"
    }
  ]
}
```

An adapter without machine-event support returns `200` with `supported:false` and an
empty `events` array. A provider read failure uses the common error envelope and does
not alter the Server projection. These are provider events, not a claim that Swallow
owns a complete durable audit trail.
