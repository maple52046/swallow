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
POST /api/v1/servers/{id}/{power-on|power-off|inspect|test|abort}
POST /api/v1/servers/{id}/{override-failed-testing|lock|unlock}
POST /api/v1/servers/{id}/{mark-broken|mark-fixed|rescue-mode|exit-rescue-mode}
PUT  /api/v1/servers/{id}/default-user
DELETE /api/v1/servers/{id}/default-user
GET  /api/v1/servers/{id}/boot-media
PUT  /api/v1/servers/{id}/boot-media
POST /api/v1/servers/{id}/redfish/probe
GET  /boot-media/ipxe/{isoId}/swallow-ipxe.iso
```

All routes require an admin bearer token, except `GET /boot-media/ipxe/{isoId}/swallow-ipxe.iso`, which
is deliberately unauthenticated (see [Boot Media](#boot-media)).

## Server Lock Protection

`POST /servers/{id}/lock` and `/unlock` return `202` with the accepted provisioning
snapshot and immediately update `provisioning.locked`. MAAS remains the source and
executor of that state; Swallow does not persist a second lock. MAAS only accepts Lock for a deployed Machine, so Swallow exposes Lock only in the `deployed` state and returns a clear `409 conflict` before calling the provider in every other state. The in-progress provisioning states `inspecting`, `deploying`, `releasing`, and `testing`, an Operation, or a Provisioning Task are also explicit conflicts. Unlock is always explicit and never resumes work.

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
for a non-failed deployment. An active deployment may still carry a non-terminal `stage` and
`statusReason` that explain what it is waiting on: while `deploying`, the provider's newest
installation stage (for example `configuring_os` / `Provider stage: Configuring OS (since …).`);
while `verifying`, the SSH-readiness wait. Clients show these as progress, never as a failure,
and must not branch on the human-readable `statusReason`.

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

`inspect` asks the provisioner to re-inventory the machine's hardware (MAAS calls this
Commission; Ironic calls it introspection). While it runs the Server's provisioning state is
`inspecting`. `test` runs the provider's hardware tests (`testing`), and `abort` stops an
in-progress inspection, test, or deployment. All three need the `hardwareValidation`
capability.

## Default User

The Server Default User is the account swallow automation logs in as with the Deployment Key
(wait-for-ssh, Ansible) and that Docker CE adds to the `docker` group. The Server projection
reports the effective value as `defaultUser {user, source}` (see
[servers-list.md](servers-list.md)). These routes set or clear the value on one Server; without
one, the deployed OS Image's default user applies.

### Set

`PUT /api/v1/servers/{id}/default-user`

```json
{ "user": "amd", "password": "optional one-time password" }
```

| Field | Required | Meaning |
| --- | --- | --- |
| `user` | Yes | A POSIX login name: lowercase letters, digits, `_` or `-`, starting with a letter or `_`, up to 32 characters. `root` is allowed. |
| `password` | No | The account's password, used once. api-server logs in over SSH with it (password or keyboard-interactive authentication) and appends the Deployment Key's public key to the account's `~/.ssh/authorized_keys` if it is not already there. It is never stored, logged, or returned. |

The Server must exist, be `deployed` with a primary address, and not be locked, and the
installation must have a Deployment Key. api-server connects to the primary address on the
Site's SSH port (default `22`). Host keys are not verified (like the login-user probe). In
every case — with or without a password — it then logs in **with the Deployment Key** as `user`
and runs `sudo -n true`; the value is saved only after that login succeeds. Without a password
the operator must have installed the key already, for example:

```sh
mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '<Deployment Key public key>' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
```

On success the response is `200 OK`:

```json
{
  "defaultUser": { "user": "amd", "source": "server" },
  "keyInstalled": true,
  "sudo": "passwordless"
}
```

- `keyInstalled` is `true` when a password was given and the key was installed (or already
  present); `false` when no password was given.
- `sudo` is `passwordless` (`sudo -n` works), `password_required` (automation uses the Site
  become password), or `unavailable` (the account cannot use sudo, so automation that needs
  root fails).

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | `user` is missing or not a login name, or the host rejected the password. |
| 404 | `not_found` | The Server does not exist. |
| 409 | `conflict` | The Server is not `deployed`, has no address, or is locked; the installation has no Deployment Key; or the host rejected the Deployment Key for `user` (no password was given, or the key did not work after installing it). |
| 503 | `provider_unavailable` | The host could not be reached over SSH, or the Server Lock state is unavailable. |

### Clear

`DELETE /api/v1/servers/{id}/default-user` removes the value set on the Server, so the deployed
OS Image's default user applies again. It is `204 No Content`, also when nothing was set. It
does not touch the host (the key stays authorized). An unknown Server is `404 not_found`; a
locked Server is `409 conflict`.

A value set on the Server belongs to one OS installation: it is cleared automatically when
swallow starts a new OS deployment on the Server and when the Server is observed `ready` or
`allocated`.

## Boot Media

Boot Media is a Server's setting that its BMC mounts a Boot ISO as Redfish virtual media and
boots it first, so a Server on a network whose DHCP is not the provisioner's still reaches the
provisioner ([decision 047](../../../../../docs/decisions/047-redfish-boot-media.md),
[decision 049](../../../../../docs/decisions/049-boot-iso-builder.md)). Boot ISOs are built in
swallow per provisioner Integration ([boot-isos.md](boot-isos.md)); per Server, swallow owns
whether Boot Media is enabled, which Boot ISO it uses, and the outcome of the last apply. swallow
reads the BMC's address and account from the provisioner (MAAS `power_parameters`) for each call
and never stores, logs, or returns them on these routes.

### The ISO

`GET /boot-media/ipxe/{isoId}/swallow-ipxe.iso` (and `HEAD`) serves a Boot ISO's file without
authentication, because a BMC mounts the URL with no way to send a token, and it re-reads the
ISO at every boot. It supports byte ranges (`206 Partial Content`, `Accept-Ranges: bytes`), which
BMC HTTP virtual media requires. It is `404` for an unknown or malformed `isoId`. The URL BMCs
mount is `<api.bootMedia.baseURL>/boot-media/ipxe/{isoId}/swallow-ipxe.iso`, reported as the
Boot ISO's `url`; many BMCs accept only `http://` on port 80 and an image under a directory, so
installations publish `/boot-media/` there. The former fixed route
`/boot-media/ipxe/swallow-ipxe.iso` of the installation-supplied ISO is removed.

### Read

`GET /api/v1/servers/{id}/boot-media` — add `?live=true` to also read the BMC.

```json
{
  "serverId": "4f9ee382-…",
  "image": {
    "id": "6b3f0c1e-…",
    "name": "tainan-rack",
    "url": "http://10.170.168.20/boot-media/ipxe/6b3f0c1e-…/swallow-ipxe.iso",
    "available": true
  },
  "setting": {
    "enabled": true,
    "isoId": "6b3f0c1e-…",
    "updatedAt": "2026-10-03T10:30:00Z",
    "lastAppliedAt": "2026-10-03T10:30:00Z",
    "lastAppliedBy": "preflight",
    "bootOverride": "Continuous",
    "lastErrorAt": null
  },
  "redfish": {
    "support": "supported",
    "serviceRoot": "https://10.170.168.230/redfish/v1",
    "vendor": "AMI",
    "product": "AMI Redfish Server",
    "redfishVersion": "1.15.1",
    "firmwareVersion": "13.06.10",
    "systemId": "Self",
    "virtualMedia": true,
    "bootOverrideModes": ["Once", "Continuous"],
    "probedAt": "2026-10-03T10:23:29Z"
  },
  "live": {
    "mediaInserted": true,
    "mediaImage": "//10.170.168.20/boot-media/ipxe/6b3f0c1e-…/swallow-ipxe.iso/swallow-ipxe.iso",
    "overrideEnabled": "Once",
    "overrideTarget": "UefiBootNext",
    "ready": true
  },
  "apply": null
}
```

- `image` is the Boot ISO the setting names, or `null` when it names none (never set, or a
  setting enabled before Boot ISOs existed). `available` is `false` with a `reason` when that Boot
  ISO no longer exists, its file is missing, or the installation has no Boot Media base URL.
- `setting.isoId` is the chosen Boot ISO, or `null`. An enabled setting with no `isoId` needs one
  chosen (enable again with an `isoId`); until then its deployments' ensure Task fails
  `boot_media_not_configured`.
- `setting` is `null` when Boot Media was never set on the Server. `lastAppliedBy` is
  `preflight` (the enable action) or `ensure` (the Task of an OS deployment). `bootOverride` is
  the persistence the BMC accepted: `Continuous` (survives reboots) or `Once` (the next boot
  only). `lastError` / `lastErrorAt` describe the most recent failed apply and are cleared by a
  successful one; a failure never changes `enabled`.
- `redfish` is `null` before the first probe. `support` is `supported` (Redfish answers and the
  host System has a virtual CD and boot override), `unsupported` (Redfish answers but lacks one of
  them, or the host System cannot be identified), `unreachable` (no Redfish service at the BMC
  address, or it rejected the provisioner's BMC account), or `no_bmc` (a virtual machine, or the
  provisioner holds no BMC address). `reason` explains any value but `supported`. The probe is
  made against the BMC address whatever the provisioner's power driver is (an IPMI-driven BMC may
  offer Redfish). The API process probes every present Server whose capability is missing — a
  newly enrolled Server — or older than a day, every ten minutes by default.
- `live` is `null` unless `live=true` was requested and the BMC answered; then `liveError`
  explains a failed read and the response is still `200`. `mediaImage` is verbatim (BMCs rewrite
  URLs). `ready` means the next boot starts from the ISO.
- `apply` is the enable preflight running now, or `null` when none is. Clients poll this read
  (every few seconds) for progress while the enable request is outstanding, and after a page
  reload:

  ```json
  {
    "isoId": "6b3f0c1e-…",
    "phase": "settling",
    "startedAt": "2026-10-05T01:32:33Z",
    "phaseStartedAt": "2026-10-05T01:33:25Z",
    "phaseEndsAt": "2026-10-05T01:36:25Z"
  }
  ```

  `phase` is, in order: `probing` (re-probing the BMC's Redfish service), `ejecting` (a switch
  ejects the previous Boot ISO), `mounting` (mounting the Boot ISO and waiting until the BMC
  reports it inserted), `settling` (the fixed wait a fresh mount needs before the host may power
  on), `directing` (directing the next boots at the virtual CD), `verifying` (reading both back).
  Phases that are not needed are skipped: no `ejecting` unless switching, no `mounting` or
  `settling` when the BMC already holds the Boot ISO. A future phase value may appear; treat an
  unknown one as in progress. `phaseEndsAt` is set only for a phase with a known end
  (`settling`), else `null`. An apply whose API process stopped is no longer reported once it is
  older than any preflight can run (ten minutes).

An unknown Server is `404 not_found`.

### Enable or disable

`PUT /api/v1/servers/{id}/boot-media`

```json
{ "enabled": true, "isoId": "6b3f0c1e-…" }
```

`isoId` is required to enable and names a Boot ISO built for the Server's own provisioner
Integration ([boot-isos.md](boot-isos.md)); it is ignored when disabling.

Enabling is a **preflight**: the Boot ISO must exist and be served and the Server must be
unlocked; swallow re-probes the BMC, mounts the Boot ISO on a virtual CD (enabling the BMC's
remote media service first when a vendor requires it), directs the next boots at that CD, reads
both back, and only then saves `enabled: true` with that `isoId`. It can take a few minutes. A
failure is recorded in `setting.lastError` and returned; the setting stays as it was.
Re-enabling an enabled Server re-applies it; enabling with a different `isoId` switches to that
Boot ISO the same way, ejecting the previous one first. The request stays open until the
preflight ends; its progress is the Read's `apply`. Only one preflight runs per Server at a time.

How the boot is directed depends on the BMC and is reported as `bootOverride`: on AMI Aptio
firmware swallow puts the USB device group (where BMC virtual media lives) first in the BIOS
boot order, effective from the next POST (`Continuous`); a BIOS that lists the virtual CD as a
UEFI boot option without such an order gets it first in `BootOrder` plus a one-time boot to it
(`Once`); other BMCs get the Redfish `Cd` override, `Continuous` when allowed. Because a BMC can
lose any of this (a BMC restart unmounts the ISO; a BIOS can re-sort its boot order; a
provisioner's own power-on overrides one-time settings), every OS deployment of an enabled
Server re-applies it first (see [provisioning.md](provisioning.md)).

Disabling (`"enabled": false`) saves the setting first, then makes a best-effort attempt to
eject the ISO and clear the boot override. The response adds `reverted` (`true` when the BMC
was reset) and, when it was not, `revertError`. A disabled Server's OS deployments do not touch
its BMC.

On success both return `200 OK` with the Read shape (plus `reverted` / `revertError` for a
disable).

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | The body is not a JSON object with a boolean `enabled`; enabling without an `isoId`; or the Boot ISO belongs to another provisioner Integration. |
| 404 | `not_found` | The Server does not exist, or the `isoId` names no Boot ISO. |
| 409 | `conflict` | A preflight is already running on the Server (enable or disable); the Server is locked; the Boot ISO is not served (its file is missing or the installation has no Boot Media base URL); the Server has no BMC; the provisioner would not reveal the BMC connection (its account is not an administrator); the BMC does not support Redfish Boot Media; or the BMC refused the ISO or the override (the message carries the BMC's own explanation). |
| 503 | `provider_unavailable` | The BMC's Redfish service could not be reached or stayed busy, the provisioner could not be reached, or the Server Lock state is unavailable. |

### Probe

`POST /api/v1/servers/{id}/redfish/probe` re-probes the BMC now, stores the result, and returns
`200 OK` with `{ "redfish": { … } }` (the Read `redfish` shape). An unreachable or unsupported
BMC is a successful probe with that `support`. An unknown Server is `404 not_found`; a
provisioner that cannot be reached is `503 provider_unavailable`.

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

## Compatibility Notes

On 2026-10-04 `POST /servers/{id}/commission` was renamed to `POST /servers/{id}/inspect`
with the same behavior, and the provisioning state `commissioning` became `inspecting`
([decision 048](../../../../../docs/decisions/048-os-provisioning-generic-states.md)). The old
route is removed rather than aliased; clients move to `inspect` in the same release.

On 2026-10-05 the Boot Media Read gained `apply` (the running preflight's progress), and a second
Boot Media write while a preflight runs is refused with `409`. Both are additive for clients
that ignore unknown fields.
