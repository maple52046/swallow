# Server Detail and Actions

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

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

All routes require an admin bearer token.

## Server Lock Protection

`POST /servers/{id}/lock` and `/unlock` return `202` with the accepted provisioning
snapshot and immediately update `provisioning.locked`. MAAS remains the source and
executor of that state; Swallow does not persist a second lock. MAAS only accepts Lock for a deployed Machine, so Swallow exposes Lock only in the `deployed` state and returns a clear `409 conflict` before calling the provider in every other state. Commissioning, deploying, releasing, testing, an Operation, or a Provisioning Task are also explicit conflicts. Unlock is always explicit and never resumes work.

Every state-changing action except Unlock performs a live lock check before touching
the provider. A locked Server returns `409 conflict` naming the Server and directing the
operator to Unlock it. An unavailable lock read fails closed with
`503 provider_unavailable`. Refresh, power query, event and network reads remain
available. Delete also retains its compatible stale-record behavior when the provider
explicitly reports that its Machine is already absent.

## Operator State and Provider Recovery

`mark-broken`, `mark-fixed`, `rescue-mode`, and `exit-rescue-mode` are advanced provider
primitives. Swallow gates each on the Server's live provisioning state before calling the
provisioner and returns a Swallow-authored `409 conflict` when the state does not allow it,
instead of forwarding the provider's own rejection text (decision 033):

- `mark-fixed` is accepted only from `broken`. From `failed` or any other state it is
  refused with a reason directing the operator to Recover or Release.
- `mark-broken` is accepted only when the Machine is not already `broken` and has no
  active provider lifecycle work.
- `rescue-mode` (enter) is a diagnostic action accepted from `deployed`, `broken`, and
  `failed`. Rescue Mode does not converge the Server to `ready`.
- `exit-rescue-mode` is accepted only from `rescue`, and restores the state the Machine
  had before entering rescue (commonly still `deployed`, `broken`, or `failed`); it is not
  a path to `ready`.

The two operator-facing recovery verbs are the durable Operations documented in
[provisioning.md](provisioning.md): `release-operations` (allowed from `deployed`,
`failed`, `broken`, `rescue`) and `recover-operations` ("Return to Ready"). A client
should present Recover and Release as the primary way to make a `failed`, `broken`, or
`rescue` Server usable again, and keep the raw operator-state primitives as advanced
controls.

`GET /servers/{id}` returns the same complete Server projection documented in [servers-list.md](servers-list.md).
Provisioner detail is a live provider-neutral view with a capability set. The
`machineRemoval` flag tells clients whether provider-backed deletion is available, and
`releaseOptions` tells clients whether release can carry disk-erasure controls. Power
state is read-only.

For a physical Machine, the live detail may include a `BMC` section. MAAS-backed
detail reads its connection configuration from the admin-only `power_parameters`
operation rather than expecting it in the ordinary Machine response. Its allowlisted
fields are `Protocol`, `Address`, `Username`, `Password`, `Node ID`, `Driver`, `Boot type`,
`Privilege level`, `Cipher suite`, and `Power MAC`; absent provider facts are omitted.
`Connection details` communicates an unavailable or empty parameter response. `Password`
is the only allowlisted secret and exists solely for manual administration through this
admin-only live-detail route. Consumers must mask it by default, reveal it only on an
explicit operator action, and must not persist or log it. Provisioner adapters must never
return K_g values, tokens, keys, private keys, or unrecognised power parameters through this
endpoint. URL userinfo, query parameters, and fragments are removed from `Address`.

`POST /servers/{id}/refresh` performs one targeted live read from the Server's
provisioner and updates its provisioning axis plus observed addresses. It returns `200`
with the same `ProvisioningStateItem` shape used by accepted lifecycle actions. Clients
use this bounded endpoint to follow an asynchronous action such as Release without
waiting for the fleet inventory interval. It is not a full inventory reconciliation: it
does not create Servers, mark missing machines absent, or rewrite identity and hardware
fields.

Full inventory reconciliation additionally clears a Server's terminal deployment outcome
(`succeeded`, `failed`, or `canceled`) once the machine is observed back in the provider's
`ready` pool, so an available Server never keeps a stale deployment result from a canceled
or abandoned run. An active (`deploying`/`verifying`) or operator-pending
(`requires_attention`) deployment is left untouched.

A failed or attention-needing deployment axis carries the failed Step's stable error
`code` (for example `deployment_address_unavailable`) alongside the verbose `statusReason`,
so a client can render a concise root cause without parsing the message. `code` is empty
for a non-failed deployment.

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
`comment`, and the deploy mode — `deployTarget` (`"disk"` | `"ram"`, preferred)
or its deprecated `ephemeral` boolean alias (`disk ↔ false`, `ram ↔ true`;
`deployTarget` wins when both are sent, and an unknown value is a `400`). This
single-server endpoint is deprecated in favour of
`POST /provisioning/deployment-operations`, which additionally applies the
custom-image verification gate. Other provider actions return `202` with the
accepted provisioning snapshot. Unsupported capabilities and provider refusals use
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
