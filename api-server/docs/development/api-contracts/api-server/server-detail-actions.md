# Server Detail and Actions

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Read one Server projection, execute provider-backed machine actions, read and write the
provisioner-owned Power Configuration, and permanently delete a Server together with its
backing provisioner Machine without exposing provider credentials.

## Endpoints

```text
GET  /api/v1/servers/{id}
POST /api/v1/servers/{id}/refresh
DELETE /api/v1/servers/{id}
GET  /api/v1/servers/{id}/provisioner-detail
GET  /api/v1/servers/{id}/events?limit=50
GET  /api/v1/servers/{id}/power-state
GET  /api/v1/servers/{id}/power-configuration
PUT  /api/v1/servers/{id}/power-configuration
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
`machineRemoval` flag tells clients whether provider-backed deletion is available,
`releaseOptions` tells clients whether release can carry disk-erasure controls, and
`powerConfiguration` tells clients whether the Power Configuration routes below are
available. Power state is read-only; the Power Configuration that lets the provisioner
switch and read it is written through its own route.

The live detail describes the Machine's power driver by its driver family
([decision 054](../../../../../docs/decisions/054-provisioner-power-configuration.md)).
A Machine whose driver belongs to the `bmc` family (`ipmi`, `redfish`) and that is not a member
of a provisioner VM host may include a `BMC` section. MAAS-backed detail reads its connection configuration from the admin-only
`power_parameters` operation rather than expecting it in the ordinary Machine response. Its
allowlisted fields are `Protocol`, `Address`, `Username`, `Password`, `Node ID`, `Driver`,
`Boot type`, `Privilege level`, `Cipher suite`, and `Power MAC`; absent provider facts are
omitted. A Machine with any other configured driver (for example `virsh`) has no BMC and
gets a `Power` section instead, with only `Driver`, `Address`, and `Power ID`; it never
carries a password. A Machine without a power driver has neither section.
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

### Inspect

`POST /servers/{id}/inspect` runs hardware inspection as an `inspect-hardware` Workflow
([server-enrollment.md](server-enrollment.md), [workflows.md](workflows.md)) instead of a single
provider call, so a commission that cannot reach the provisioner is retried, bounded, and
reported for attention. It takes no body.

- When no `inspect-hardware` Workflow is active for the Server, a new one starts. A request from
  this route skips the enrollment wait: the operator asserts the Server may boot now. It still
  stops in `requires_attention` with `power_configuration_required` when the Machine has no power
  driver at all, because the provisioner could not power it on to inspect it.
- When the Server's `inspect-hardware` Workflow is waiting in `requires_attention`, the request
  retries it (the same Workflow, its whole `ensure-inspected` Job) instead of starting another,
  so a Power Configuration or Boot Media set since is used by the next commission. Like any
  retry it runs the enrollment wait again ([server-enrollment.md](server-enrollment.md)).
- An `inspect-hardware` Workflow that is still running, or any other active Workflow on the
  Server, is `409 conflict`.

The Server must be present, unlocked, and in provisioning state `new`, `ready`, `failed`,
`broken`, or `unknown`; any other state, or no observed state, is `409 conflict` before the
provider is called. The response is `202` with the provisioning snapshot as stored (inspection has
not started yet) plus the Workflow:

```json
{
  "serverId": "server-id",
  "state": "new",
  "providerState": "New",
  "powerState": "off",
  "osSystem": "",
  "distroSeries": "",
  "ephemeral": false,
  "hweKernel": "",
  "locked": false,
  "commissioningStatus": "",
  "testingStatus": "",
  "observedAt": "2026-10-06T03:00:00Z",
  "workflowId": "workflow-id",
  "resumed": false
}
```

`resumed` is `true` when the request retried the Workflow waiting for attention. A deployment
without a Workflow engine falls back to the direct provider action and omits both fields.

## Power Configuration

A Server's Power Configuration is the power driver its provisioner uses to switch and read the
Server's power, and that driver's connection parameters
([decision 054](../../../../../docs/decisions/054-provisioner-power-configuration.md)). The
provisioner owns it (for MAAS, the Machine's `power_type` and `power_parameters`); swallow reads
it live on every request and writes it through to the provisioner. swallow never stores it, and
never returns or logs a password. Both routes need the `powerConfiguration` capability; a
provisioner without it answers `400 validation_error`. Responses carry `Cache-Control: no-store`.

Drivers are grouped into families. A family decides which parameters apply and which functions a
Server has beyond switching power:

| Driver | Family | Parameters | Notes |
| --- | --- | --- | --- |
| `ipmi` | `bmc` | `address` (required), `username`, `password` | A BMC. Redfish Boot Media is a function of the BMC, probed independently of the driver (decision 047). |
| `redfish` | `bmc` | `address` (required), `username`, `password` | The provisioner switches power over Redfish. Not the same as Redfish Boot Media. |
| `virsh` | `virsh` | `address` (required), `powerId` (required), `password` | A libvirt virtual machine. No BMC; Boot Media uses the `libvirt` method when its hypervisor is a swallow Server. |

`control` classifies what the driver lets the provisioner do: `none` (no driver: the provisioner
can neither switch nor read power, so it cannot inspect or deploy the Server), `manual` (a person
switches power and the state cannot be read, MAAS `manual`), or `automatic` (the provisioner
switches and reads power itself; every driver in the table, and any other driver the provisioner
reports, such as a VM host's own driver).

### Read

`GET /api/v1/servers/{id}/power-configuration`

```json
{
  "serverId": "server-id",
  "driver": "virsh",
  "family": "virsh",
  "control": "automatic",
  "address": "qemu+ssh://maas@tainan-ci.lab/system",
  "powerId": "simple-pig",
  "username": "",
  "passwordSet": false,
  "editable": true,
  "readOnlyReason": "",
  "drivers": [
    { "driver": "ipmi", "family": "bmc" },
    { "driver": "redfish", "family": "bmc" },
    { "driver": "virsh", "family": "virsh" }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `driver` | The provisioner's power driver, `""` when none is configured. A driver outside the table is reported verbatim. |
| `family` | `bmc`, `virsh`, or `""` when there is no driver or swallow does not know its family. |
| `control` | `none`, `manual`, or `automatic`, as above. |
| `address` | The BMC address or libvirt URI as the provisioner holds it, with any password in the URL, query, and fragment removed. A user name in the URL is kept: it is the account the provisioner connects as, not a secret. |
| `powerId` | The libvirt domain name or UUID of a `virsh` driver; `""` otherwise. |
| `username` | The BMC account of a `bmc` driver; `""` otherwise. |
| `passwordSet` | Whether the provisioner holds a password for the driver. The password itself is write-only. |
| `editable` | `false` when the provisioner manages this Server's power elsewhere: a virtual machine that belongs to a provisioner VM host takes its power from that VM host. |
| `readOnlyReason` | Why `editable` is `false`, in operator terms; `""` otherwise. |
| `drivers` | The drivers a write may choose, with their family. |

### Write

`PUT /api/v1/servers/{id}/power-configuration` replaces the Server's Power Configuration and
returns `200 OK` with the Read shape, read back from the provisioner after the write.

```json
{
  "driver": "virsh",
  "address": "qemu+ssh://maas@tainan-ci.lab/system",
  "powerId": "simple-pig",
  "password": "optional"
}
```

| Field | Required | Meaning |
| --- | --- | --- |
| `driver` | Yes | One of `drivers`. |
| `address` | Yes | For `bmc`: the BMC host, IP, or URL. For `virsh`: `qemu+ssh://[user@]host[:port]/system`. A libvirt URI with a password, a query (`?keyfile=…`), a fragment, another scheme, or another path is refused: the provisioner cannot use one, and SSH settings belong in the provisioner's own SSH configuration. |
| `powerId` | `virsh` only | The libvirt domain name or UUID on that hypervisor, without whitespace. Refused for `bmc`. |
| `username` | No (`bmc` only) | The BMC account. Refused for `virsh`. |
| `password` | No | Write-only. Omitted keeps the password the provisioner holds when `driver` is unchanged and clears it when `driver` changes; `""` clears it; any other value replaces it. |

swallow validates the body against the driver's family before calling the provisioner; the
provisioner then validates it again and may refuse it. The provisioner itself connects with these
values, so the provisioner — not swallow — must reach the address: for `virsh`, the MAAS rack
controller needs SSH access to the hypervisor account (its key in the MAAS snap's
`/var/snap/maas/current/root/.ssh`). A write does not test that access; read `GET
/servers/{id}/power-state` afterwards.

A write changes only the Power Configuration; it never switches power, and it does not resume
an `inspect-hardware` Workflow waiting for attention. Retry that Workflow (or `POST
/servers/{id}/inspect`) after the write.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | The body is not a JSON object; `driver` is missing or not one of `drivers`; a parameter is missing, malformed, or does not apply to the driver; the provisioner lacks the capability; or the provisioner refused the configuration (the message carries its explanation). |
| 404 | `not_found` | The Server does not exist, or the provisioner no longer has its Machine. |
| 409 | `conflict` | The Server is locked, or its power is managed by a provisioner VM host (`editable` is `false`). |
| 503 | `provider_unavailable` | The provisioner could not be reached, its credential may not read or write power parameters (a MAAS account that is not an administrator), or the Server Lock state is unavailable. |

The Read answers `404` and `503` the same way, and `400` only for a provisioner without the
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

Boot Media is a Server's setting that a Boot ISO is attached to it and booted first, so a Server
on a network whose DHCP is not the provisioner's still reaches the provisioner
([decision 047](../../../../../docs/decisions/047-redfish-boot-media.md),
[decision 049](../../../../../docs/decisions/049-boot-iso-builder.md),
[decision 055](../../../../../docs/decisions/055-libvirt-virtual-machine-enrollment.md)). Boot ISOs
are built in swallow per provisioner Integration ([boot-isos.md](boot-isos.md)); per Server, swallow
owns whether Boot Media is enabled, which Boot ISO it uses, and the outcome of the last apply.

How the ISO is attached is the Boot Media **method**, decided by the Server's Power Configuration:

- `redfish` — a Server with a BMC (`bmc`-family driver, not a VM-host member): the BMC mounts the
  Boot ISO as Redfish virtual media. swallow reads the BMC's address and account from the
  provisioner (MAAS `power_parameters`) for each call and never stores, logs, or returns them on
  these routes.
- `libvirt` — a libvirt virtual machine (`virsh` driver) whose hypervisor is a swallow Server: the
  Power Configuration's address `qemu+ssh://<account>@<host>/system` names the hypervisor (its host
  matches a Server of the same Site by address, hostname, or FQDN) and the account, and the power ID
  names the domain. swallow logs in to the hypervisor with the Deployment Key, as for
  [virtual-machine enrollment](server-enrollment.md#virtual-machines-libvirt), uploads the Boot ISO as
  the volume `swallow-ipxe-<isoId>.iso` in the hypervisor's `default` storage pool (once per Boot
  ISO; like `virt-install`, swallow defines `default` as a directory pool at
  `/var/lib/libvirt/images` when the hypervisor has none, and starts it when it is inactive), puts
  it on the domain's CD-ROM (adding a CD-ROM when the domain has none), and makes the CD-ROM boot
  first. It changes the domain's persistent definition, which a running domain uses from
  its next start. The provisioner's `virsh` driver starts and stops the domain without touching its
  boot order, so nothing undoes it.

Any other Server has no Boot Media method.

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

`GET /api/v1/servers/{id}/boot-media` — add `?live=true` to also read the BMC or the hypervisor.

```json
{
  "serverId": "4f9ee382-…",
  "method": "redfish",
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
  "libvirt": null,
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
  address, or it rejected the provisioner's BMC account), or `no_bmc` (a virtual machine, a
  driver outside the `bmc` family, or the provisioner holds no BMC address). `reason` explains any
  value but `supported`. The probe is made against the BMC address whatever `bmc`-family driver
  the provisioner uses (an IPMI-driven BMC may offer Redfish). The API process probes every present Server whose capability is missing — a
  newly enrolled Server — or older than a day, every ten minutes by default.
- `method` is `redfish`, `libvirt`, or `null` (no method, or not probed yet), derived from the
  stored probes: `libvirt` when the libvirt probe found a hypervisor, else `redfish` when the
  Redfish probe found a BMC.
- `libvirt` is `null` unless the Server's driver is `virsh` and it was probed (the same probe and
  sweep as `redfish`, which records `no_bmc` for such a Server):

  ```json
  {
    "support": "supported",
    "hypervisorServerId": "server-id",
    "account": "ubuntu",
    "domain": "lab-afde-mi308-1",
    "pool": "default",
    "cdrom": true,
    "probedAt": "2026-10-09T03:00:00Z"
  }
  ```

  `support` is `supported` (the domain exists), `unsupported` (no such domain on the hypervisor),
  `unreachable` (the hypervisor could not be reached or refused the Deployment Key or libvirt
  access), or `no_hypervisor` (the address's host is not a swallow Server of the Site). `reason`
  explains any value but `supported`. `pool` is the storage pool the Boot ISO goes to. `cdrom` says
  whether the domain already has a CD-ROM; enabling adds one when it does not.
- `live` is `null` unless `live=true` was requested and the BMC or hypervisor answered; then
  `liveError` explains a failed read and the response is still `200`. `mediaImage` is verbatim (BMCs
  rewrite URLs; for `libvirt` it is the CD-ROM's source path). `ready` means the next boot starts
  from the ISO. For `libvirt`, `overrideEnabled` is `Continuous` when the CD-ROM boots first and
  `overrideTarget` is `Cd`.
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

  `phase` is, in order: `probing` (re-probing the BMC's Redfish service or the hypervisor),
  `ejecting` (a switch ejects the previous Boot ISO), `mounting` (mounting the Boot ISO and waiting
  until the BMC reports it inserted; for `libvirt`, uploading it to the hypervisor and putting it on
  the CD-ROM), `settling` (the fixed wait a fresh BMC mount needs before the host may power on; never
  for `libvirt`), `directing` (directing the next boots at the virtual CD), `verifying` (reading both
  back). Phases that are not needed are skipped: no `ejecting` unless switching, no `mounting` or
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

For the `libvirt` method, enabling needs the Boot ISO's file but not a Boot Media base URL (the
file is uploaded to the hypervisor, not mounted by URL). The hypervisor Server must also be
unlocked, because the domain's definition lives on it. `bootOverride` is `Continuous`: the CD-ROM's
boot order persists until Boot Media is disabled. Disabling ejects the CD-ROM and removes its boot
order, leaving the domain's other boot devices in their order. There is no `settling` wait and no
recovery during an OS deployment, because nothing undoes a domain's persistent definition.

On success both return `200 OK` with the Read shape (plus `reverted` / `revertError` for a
disable).

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | The body is not a JSON object with a boolean `enabled`; enabling without an `isoId`; or the Boot ISO belongs to another provisioner Integration. |
| 404 | `not_found` | The Server does not exist, or the `isoId` names no Boot ISO. |
| 409 | `conflict` | A preflight is already running on the Server (enable or disable); the Server or its hypervisor is locked; the Boot ISO is not served (its file is missing or, for `redfish`, the installation has no Boot Media base URL); the Server has no Boot Media method (no BMC and no swallow hypervisor); the provisioner would not reveal the BMC connection (its account is not an administrator); the BMC does not support Redfish Boot Media, or the hypervisor has no such domain; or the BMC or hypervisor refused the ISO, the storage pool, or the boot order (the message carries its own explanation). |
| 503 | `provider_unavailable` | The BMC's Redfish service or the hypervisor could not be reached or stayed busy, the provisioner could not be reached, or the Server Lock state is unavailable. |

### Probe

`POST /api/v1/servers/{id}/redfish/probe` re-probes the Server's Boot Media method now, stores the
result, and returns `200 OK` with `{ "redfish": { … }, "libvirt": { … } | null }` (the Read shapes).
An unreachable or unsupported BMC or hypervisor is a successful probe with that `support`. An
unknown Server is `404 not_found`; a provisioner that cannot be reached is
`503 provider_unavailable`.

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

On 2026-10-06 `inspect` became an `inspect-hardware` Workflow
([decision 053](../../../../../docs/decisions/053-server-enrollment-and-automatic-inspection.md)).
The response keeps the provisioning snapshot and adds `workflowId` and `resumed`; the snapshot
now shows the state before inspection starts rather than the provider's accepted state. Inspect
from `deployed`, `allocated`, `rescue`, `retired`, or an in-progress state, and Inspect while
another Workflow holds the Server, are now `409 conflict` instead of being forwarded to the
provider.

On 2026-10-09 the Power Configuration routes were added and `capabilities` gained
`powerConfiguration`
([decision 054](../../../../../docs/decisions/054-provisioner-power-configuration.md)). The
provisioner detail now shows a `BMC` section only for a `bmc`-family driver; a Machine with a
`virsh` or other non-BMC driver gets a `Power` section without a password, where it used to get
a `BMC` section naming its libvirt URI. Both changes are additive for clients that ignore unknown
fields and sections. An Inspect that resumes a Workflow waiting for attention now runs the
enrollment wait again, and a new requested Inspect of a Machine without a power driver stops for
attention instead of failing at the provider.

Also on 2026-10-09 Boot Media gained the `libvirt` method
([decision 055](../../../../../docs/decisions/055-libvirt-virtual-machine-enrollment.md)): the Read
adds `method` and `libvirt`, the probe response adds `libvirt`, and a `virsh` Server whose
hypervisor is a swallow Server can enable Boot Media instead of being refused for having no BMC.
All additions are compatible with clients that ignore unknown fields.
