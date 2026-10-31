# Servers List

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Lists the complete, secret-free Servers with optional filtering and pagination. Each Server
carries its spec (the provisioner-realized fields swallow owns) and its status: the independently
observed provisioning, platform-membership, and health axes, and the conditions that say whether
the provisioner matches the spec.

## Related Glossary Terms

- `Server`
- `Server Status`
- `Server Origin`
- `Condition`
- `Hold`
- `OS Provisioning State`
- `GPU Inventory`

## Endpoint / RPC

```text
GET /api/v1/servers
```

The route is also served with a trailing slash (`/api/v1/servers/`).

The wire shape — every field, its type, and whether it is required or nullable — is the
`listServers` operation and the `Server` schema of the OpenAPI contract
([`../openapi/areas/servers.yaml`](../openapi/areas/servers.yaml)). This document describes
what the fields mean.

## Authentication

Bearer token required. See [`conventions.md`](conventions.md).

## Authorization

`admin` only. A non-admin authenticated caller receives `403 forbidden`.

## Request

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Authorization` | Yes | Bearer token. |

### Query Parameters

| Name | Type | Required | Description |
| --- | --- | -------: | --- |
| `page` | integer | No | Page number; default `1`. See [`conventions.md`](conventions.md). |
| `pageSize` | integer | No | Items per page; default `20`, max `100`. |
| `siteId` | string | No | Return Servers observed through integrations at this Site. |
| `integrationId` | string | No | Return Servers observed through this integration. |
| `provisioningState` | string | No | Filter on the provisioning axis state; one of the values listed under [Provisioning state](#provisioning-state). |
| `platformId` | string | No | Return Servers whose membership axis names this Platform. |
| `clusterId` | string | No | Deprecated one-release alias for `platformId`; ignored when `platformId` is also present. |
| `keyword` | string | No | Case-insensitive match on hostname, FQDN, address, serial number, or system UUID. |
| `includeAbsent` | boolean | No | Include projections absent from the latest provider inventory; default `false`. Only the literal `true` enables it. |

Filters are conjunctive. `keyword` is a search convenience, not a structured
query language. `includeAbsent=true` changes visibility only.

Query values are not validated: an unknown filter value (for example an undocumented
`provisioningState`) matches nothing and returns `200` with no items, and a `page` or
`pageSize` that is not a positive integer, or a `pageSize` above `100`, falls back to its
default (`1` and `20`) instead of being refused.

## Response

### Success Response

`200 OK`, using the shared pagination envelope:

```json
{
  "items": [
    {
      "id": "server-id",
      "origin": "discovered",
      "generation": 2,
      "spec": {"tags": ["rack-a"], "zoneId": "zone-id", "poolId": null},
      "status": {
        "observedGeneration": 2,
        "conditions": [
          {"type": "realized", "status": "true", "reason": "converged", "message": "", "lastTransitionTime": "2026-05-02T15:00:00Z", "observedGeneration": 2},
          {"type": "drifted", "status": "false", "reason": "in_sync", "message": "", "lastTransitionTime": "2026-05-02T15:00:00Z", "observedGeneration": 2},
          {"type": "requires_attention", "status": "false", "reason": "none", "message": "", "lastTransitionTime": "2026-05-02T15:00:00Z", "observedGeneration": 2},
          {"type": "held", "status": "false", "reason": "not_held", "message": "", "lastTransitionTime": "2026-05-02T15:00:00Z", "observedGeneration": 2}
        ],
        "source": {
          "siteId": "site-id",
          "integrationId": "integration-id",
          "providerMachineId": "machine-42"
        },
        "hostname": "gpu-42",
        "fqdn": "gpu-42.maas",
        "addresses": ["192.0.2.42"],
        "architecture": "amd64/generic",
        "cpuCores": 192,
        "cpuModel": "AMD EPYC 9654",
        "memoryMiB": 1572864,
        "storageGB": 7680,
        "gpus": [{"vendor": "AMD", "model": "MI300X", "count": 8, "kind": "compute"}],
        "systemVendor": "Supermicro",
        "systemProduct": "AS-8125GS-TNMR2",
        "providerZone": "default",
        "providerResourcePool": "default",
        "providerPod": "",
        "tags": ["amd-gpu", "rack-a"],
        "automaticTags": ["amd-gpu"],
        "hardware": {"systemUuid": "uuid", "serialNumber": "serial", "macAddresses": []},
        "deployment": null,
        "provisioning": {
          "state": "deployed",
          "stateSince": "2026-05-02T14:12:09Z",
          "providerState": "Deployed",
          "powerState": "on",
          "osSystem": "ubuntu",
          "distroSeries": "jammy",
          "deployedImageName": "Ubuntu 22.04 LTS",
          "deployedImageDefaultUser": "ubuntu",
          "ephemeral": false,
          "hweKernel": "",
          "locked": false,
          "commissioningStatus": "Passed",
          "testingStatus": "Passed",
          "integrationId": "integration-id",
          "observedAt": "2026-05-02T15:00:00Z"
        },
        "membership": {
          "platformId": "platform-id",
          "clusterId": "platform-id",
          "nodeName": "gpu-42",
          "role": "worker",
          "state": "ready",
          "observedAt": "2026-05-02T15:00:00Z"
        },
        "health": {"state": "up", "observedAt": "2026-05-02T15:00:00Z"},
        "platformClaim": {"platformId": "platform-id", "claimedAt": "2026-05-02T13:40:00Z"},
        "defaultUser": {"user": "ubuntu", "source": "os_image"},
        "absent": false,
        "lastSeenAt": "2026-05-02T15:00:00Z",
        "createdAt": "2026-05-02T15:00:00Z",
        "updatedAt": "2026-05-02T15:00:00Z"
      }
    }
  ],
  "total": 1,
  "page": 1,
  "pageSize": 20
}
```

This is the same complete Server representation returned by the detail endpoint.
Optional observations use `null`; an unavailable axis is not assigned a default
state. No BMC, SSH, integration, or automation credential is returned.

### Spec and status

A Server is a migrated resource ([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)):
the provisioner-realized fields swallow owns are in `spec`, and everything observed, derived, or
judged is in `status`. Every field described in the sections below is relative to `status`.

| Field | Meaning |
| --- | --- |
| `origin` | How the Server arrived: `discovered` (its provisioner enlisted it, for example PXE enlistment in MAAS), `enrolled` (swallow registered it, [server-enrollment.md](server-enrollment.md)), or `imported` (registered with an OS already installed). Display only; after adoption swallow owns the spec whatever the origin. Ignore an unknown value. |
| `generation` | Counts spec changes. A Server's first observation is adopted as its spec (generation `1`) without changing the provisioner. |
| `spec.tags` | The complete set of manual tags the Server must carry ([server-tags.md](server-tags.md)). |
| `spec.zoneId`, `spec.poolId` | The Zone and Pool the Server must be in ([infrastructure.md](infrastructure.md#server-placement)); `null` for the provisioner's default group. |
| `status.observedGeneration` | The generation the conditions were judged for. Lower than `generation` while a spec change is converging. |
| `status.conditions` | The Server controller's judgement, one entry per type. Empty until the controller has judged the Server. |

The condition types and reasons a client may branch on:

| Type | `true` means | Reasons |
| --- | --- | --- |
| `realized` | The provisioner matches the spec. | `converged`; when `false`: `converging` (a spec change or drift correction is pending), `capability_missing` (the provisioner cannot set a field; the spec is kept), `provider_unreachable` (retrying), `provider_missing` (the provisioner no longer reports the machine), `failed` (the provisioner rejected the change). |
| `drifted` | The provisioner departed from a realized spec, for example a tag or the zone changed in the MAAS UI. Swallow corrects it at its next periodic resync (default every 10 minutes), not at once. | `drift_detected`; when `false`: `in_sync`. |
| `requires_attention` | Convergence stopped and only a spec change retries it. | `provider_rejected`; when `false`: `none`. |
| `held` | Swallow is deliberately not changing the machine. A spec change waits. | `server_locked`, `rescue_mode`, `workflow_lease` (a running Workflow holds the Server); when `false`: `not_held`. |

`message` is display text; never branch on it. A condition's `lastTransitionTime` changes only when
its `status` changes.

`deployment` is Swallow's own outcome of its latest durable OS deployment on the Server
(`deploying | verifying | succeeded | failed | requires_attention | canceled`, with the
Workflow and Task that ran it, a failed Task's `code`, and a progress `stage`), `null` when
none is recorded; see [server-detail-actions.md](server-detail-actions.md). `tags` is every tag the
provisioner reports on the machine, and `automaticTags` the subset it computes itself (a MAAS tag
with a definition), which is never part of the spec ([server-tags.md](server-tags.md)). `providerZone` and
`providerResourcePool` are the group names the provisioner reports for the machine (the
observation of `spec.zoneId` and `spec.poolId`), and `providerPod` the provisioner VM host of a
virtual machine (empty for bare metal); all are display only. `membership.nodeName`
is the platform's own name for the machine.

### GPU inventory

`gpus` is the complete provisioner-observed GPU Inventory, including workload accelerators and
local-console or BMC graphics controllers. Identical devices are grouped; every item contains:

| Field | Meaning |
| --- | --- |
| `vendor` | Provider-reported vendor; may be empty when the provider cannot name it. |
| `model` | Provider-reported model; may be empty when the provider cannot name it. |
| `count` | Positive number of physical devices in this vendor/model/kind group. |
| `kind` | `compute` for workload accelerator capacity, or `display` for a management/local-console graphics controller. |

MAAS exposes both kinds through its generic GPU device type. Swallow classifies known server
display controllers (including ASPEED, Cirrus Logic GD 5446, Matrox G200, and XGI BMC graphics)
and virtual-machine console adapters (virtio-gpu, QXL, Bochs/QEMU standard VGA, VMware SVGA II,
and Hyper-V video) as `display`. All other provider-reported GPU devices remain `compute` when
the provider supplies no stronger class. MAAS gives a processing accelerator its GPU type only
when a driver claimed it during commissioning, so swallow also counts known compute accelerators
that MAAS left untyped (AMD Instinct MI300 and MI350 series, physical and SR-IOV virtual
functions) as `compute`.
Clients must use `kind` rather than vendor-name heuristics when calculating accelerator capacity.

### Provisioning state

`provisioning.state` is the swallow-defined OS Provisioning State, never a provider's own
word. The provider adapter maps its lifecycle onto these values and puts its own label in the
display-only `providerState` (for example `inspecting` with `providerState` `Commissioning`
from MAAS). Clients branch on `state` only.

| Value | Meaning |
| --- | --- |
| `new` | Discovered, hardware not inspected yet; not deployable. |
| `inspecting` | The provider is inventorying the hardware (MAAS Commissioning). |
| `ready` | In the provider's available pool; can accept an OS Deployment. |
| `allocated` | Reserved but not deployed. |
| `deploying` | An operating system is being installed. |
| `deployed` | An operating system is installed and running. |
| `releasing` | Being returned to the available pool, including disk erasure. |
| `testing` | The provider is running hardware tests. |
| `rescue` | In the provider's diagnostic environment. |
| `broken` | The provider marked the machine unusable. |
| `failed` | The last lifecycle action failed. |
| `retired` | Withdrawn from service. |
| `unknown` | The provider reported a state swallow does not recognise. |

`inspecting`, `deploying`, `releasing`, and `testing` are in progress and change without
operator action.

`stateSince` is when Swallow first observed the current `state`: it stays the same while the
state is unchanged and moves to the observation time when the state changes. It is Swallow's
own record, not the provider's transition time, so its precision is the observation cadence
(the inventory reconcile interval, or a targeted refresh of the Server). Clients use it to show
how long a Server has been in its state, for example the running time of `releasing`. It is
omitted when unknown, which includes a projection stored before the field existed until its
state is observed again.

### Platform Claim

`platformClaim` is the Swallow-owned **Platform Claim**
([decision 067](../../../../../docs/decisions/067-platform-claim-on-server.md)): the Platform
whose accepted deployment targets this Server. It is `null` when no Platform claims the Server.

| Field | Meaning |
| --- | --- |
| `platformId` | The claiming Platform. Its name is not included; read the Platform to show it. |
| `claimedAt` | When that deployment was accepted (or, for a Platform deployed before claims existed, when its deployment was requested). |

It is written when a Platform deployment is accepted and cleared when that Platform's uninstall
succeeds or its record is deleted ([platforms.md](platforms.md)); a failed deployment or a failed
uninstall keeps it. It is intent, not observation: `membership` is the platform runtime's own
answer and can be missing exactly when the claim matters (a deployment that never joined, a
failing sync). A claimed Server cannot be released directly, and a deployed one cannot be
recovered, through [provisioning.md](provisioning.md#durable-os-operations), nor can it be
deleted ([server-detail-actions.md](server-detail-actions.md)); its Servers are released
through the Platform's uninstall with `releaseServers`.

### Provisioning axis fields

On the provisioning axis, `deployedImageName` is the effective display name of the
currently deployed OS image — the provider catalog name overlaid with any Swallow
custom name. It is mirrored when a deploy completes and refreshed by reconcile (and by
an image rename), so a freshly deployed Server shows the friendly name promptly rather
than only after the next reconcile pass. It is display only and is empty when nothing is
deployed or the image cannot be resolved from the catalog; clients fall back to
`osSystem`/`distroSeries` in that case.

`deployedImageDefaultUser` is the effective default login user of the currently deployed
OS image (the image's Swallow `defaultUser` overlay, else the built-in derived from the OS
family), mirrored the same way as `deployedImageName`. It is the account an operator's
Access Key authorizes on Servers the provisioner deployed, and the account automation logs
in as unless the Server has its own default user (automation uses `defaultUser` below). It
is omitted when nothing is deployed or no default user applies. It stays the image's value
even when the Server has its own default user.

`defaultUser` is the Server's effective **Server Default User** — the account swallow
automation logs in as with the Deployment Key and that Docker CE adds to the `docker` group:

| Field | Meaning |
| --- | --- |
| `user` | The account name. |
| `source` | `server` when an operator set it on this Server ([Default User](server-detail-actions.md#default-user)); `os_image` when it is `deployedImageDefaultUser`. |

It is omitted when neither applies, in which case automation falls back to the Site SSH user
and built-in candidates. A value with source `server` is cleared when swallow starts a new
OS deployment on the Server and when the Server is observed `ready` or `allocated`.

Also on the provisioning axis, `errorDescription` is the provisioner's own
machine-level failure reason (for example `"Failed to erase disks."`), mirrored
only for the failure states (`failed`, `broken`, `rescue`) and **omitted** for every
other state so a healthy Server never carries a stale error. It is display and
diagnostics only — never branch on it — and lets a client show why a lifecycle
action failed without reading the provider event log.

`inspectionRequired` is `true` when the provisioner's storage layout for the Server cannot
take a disk deployment until the Server is inspected again, and `inspectionRequiredReason`
is the Swallow-authored explanation to show with it. Both are **omitted** when the flag is
false. Swallow refuses a disk deployment of such a Server with the deployment-target issue
`inspection_required` ([provisioning.md](provisioning.md#multi-server-deployment)); a RAM
deployment does not partition the disk and is not affected. A new inspection that rebuilds
the layout clears it. The flag is set only on proof: for MAAS, a machine that boots UEFI
(`bios_boot_method=uefi`) while its boot disk has partitions but no EFI system partition
(FAT mounted at `/boot/efi`) — the layout of a Server inspected while it booted BIOS
([decision 057](../../../../../docs/decisions/057-uefi-boot-iso-hand-off.md)). A Server without a
layout, or whose layout MAAS did not report, is never flagged.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `unauthorized` | 401 | Token is missing, malformed, or expired. |
| `forbidden` | 403 | Caller's role is not `admin`. |

No query value is refused (see [Query Parameters](#query-parameters)); consumers should still
send only documented values, because an unknown filter value silently matches nothing.

## Compatibility Notes

Adding an optional projection field is backward compatible. Adding a credential
field is forbidden. The three state axes and `absent` remain independent.

On 2026-10-04 the provisioning state `commissioning` was renamed to `inspecting`
([decision 048](../../../../../docs/decisions/048-os-provisioning-generic-states.md)). This
is a breaking change for clients that branch on or filter by `commissioning`. A projection
stored before the rename is returned as `inspecting`, and `provisioningState=inspecting`
also matches it until the next reconcile rewrites it.

`provisioning.stateSince` was added on 2026-10-04 as an optional field; clients must accept
its absence.

`provisioning.inspectionRequired` and `provisioning.inspectionRequiredReason` were added on
2026-10-19 as optional fields; their absence means false. A projection stored before then
gains them at the next reconcile or refresh of the Server.

`gpus[].kind` was added on 2026-10-06. It is additive for clients that ignore unknown fields;
the server always emits it, including for inventory stored before the field existed by deriving
the classification while reading that legacy projection.

On 2026-10-13 the GPU Inventory widened without a shape change: virtual-machine console adapters
became `display`, and untyped MAAS accelerators started to appear as `compute`. A projection
stored before then keeps its stored kinds until the next inventory sweep rewrites it.

On 2026-10-30 this contract was aligned with the implementation when the OpenAPI wire shape
was introduced, with no wire change: the path is written without its trailing slash, query
values are documented as never refused (previously "a client error"), and the example lists
every field the projection has always carried.

On 2026-10-31 the Server representation was restructured into spec and status
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)). This is
a breaking change with no alias: every former top-level field except `id` moved unchanged under
`status`, and `origin`, `generation`, `spec`, `status.observedGeneration`, `status.conditions`,
and `status.automaticTags` were added; the same day `spec.zoneId` and `spec.poolId` joined the
spec. `status.tags` is now the provisioner's observation only; a
tag swallow kept for a provisioner without tag support is in `spec.tags`.

Following the Cluster to Platform rename, the `clusterId` query parameter and the
membership `clusterId` response field are deprecated one-release aliases for
`platformId`. When both are sent, `platformId` wins; the aliases are removed after the
deprecation window (see
[ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)).

## Implementation Notes

Filtering and pagination are pushed to the repository port; the handler only
parses and shapes.
