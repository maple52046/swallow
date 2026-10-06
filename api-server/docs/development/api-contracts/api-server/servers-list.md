# Servers List

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Lists the complete, secret-free Server projections with optional filtering and
pagination. This inventory surface includes the independently observed
provisioning, platform-membership, and health axes.

## Related Glossary Terms

- `Server`
- `Server Status`
- `OS Provisioning State`
- `GPU Inventory`

## Endpoint / RPC

```text
GET /api/v1/servers/
```

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
| `includeAbsent` | boolean | No | Include projections absent from the latest provider inventory; default `false`. |

Filters are conjunctive. `keyword` is a search convenience, not a structured
query language. `includeAbsent=true` changes visibility only.

## Response

### Success Response

`200 OK`, using the shared pagination envelope:

```json
{
  "items": [
    {
      "id": "server-id",
      "source": {
        "siteId": "site-id",
        "integrationId": "integration-id",
        "providerMachineId": "machine-42"
      },
      "hostname": "gpu-42",
      "addresses": ["192.0.2.42"],
      "gpus": [{"vendor": "AMD", "model": "MI300X", "count": 8, "kind": "compute"}],
      "hardware": {"systemUuid": "uuid", "serialNumber": "serial", "macAddresses": []},
      "provisioning": {
        "state": "deployed",
        "stateSince": "2026-05-02T14:12:09Z",
        "powerState": "on",
        "osSystem": "ubuntu",
        "distroSeries": "jammy",
        "deployedImageName": "Ubuntu 22.04 LTS",
        "deployedImageDefaultUser": "ubuntu",
        "observedAt": "2026-05-02T15:00:00Z"
      },
      "membership": {
        "platformId": "platform-id",
        "clusterId": "platform-id",
        "role": "worker",
        "state": "ready",
        "observedAt": "2026-05-02T15:00:00Z"
      },
      "health": {"state": "up", "observedAt": "2026-05-02T15:00:00Z"},
      "defaultUser": {"user": "ubuntu", "source": "os_image"},
      "absent": false,
      "lastSeenAt": "2026-05-02T15:00:00Z",
      "createdAt": "2026-05-02T15:00:00Z",
      "updatedAt": "2026-05-02T15:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "pageSize": 20
}
```

This is the same complete Server projection returned by the detail endpoint.
Optional observations use `null`; an unavailable axis is not assigned a default
state. No BMC, SSH, integration, or automation credential is returned.

### GPU inventory

`gpus` is the complete provisioner-observed GPU Inventory, including workload accelerators and
local-console or BMC graphics controllers. Identical devices are grouped; every item contains:

| Field | Meaning |
| --- | --- |
| `vendor` | Provider-reported vendor; may be empty when the provider cannot name it. |
| `model` | Provider-reported model; may be empty when the provider cannot name it. |
| `count` | Positive number of physical devices in this vendor/model/kind group. |
| `kind` | `compute` for workload accelerator capacity, or `display` for a management/local-console graphics controller. |

MAAS exposes both kinds through its generic GPU device filter. Swallow classifies known server
display controllers (including ASPEED, Cirrus Logic GD 5446, Matrox G200, and XGI BMC graphics)
as `display`. All other provider-reported GPU devices remain `compute` when the provider supplies
no stronger class.
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
family), mirrored the same way as `deployedImageName`. Swallow automation logs in to the
Server as this user; it is also the account an operator's Access Key authorizes on
Servers the provisioner deployed. It is omitted when nothing is deployed or no default
user applies. It stays the image's value even when the Server has its own default user.

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

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `unauthorized` | 401 | Token is missing, malformed, or expired. |
| `forbidden` | 403 | Caller's role is not `admin`. |

An unknown filter value or an out-of-range `pageSize` is a client error; the
contract does not currently define a distinct code for it, so consumers must send
only documented values.

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

`gpus[].kind` was added on 2026-10-06. It is additive for clients that ignore unknown fields;
the server always emits it, including for inventory stored before the field existed by deriving
the classification while reading that legacy projection.

Following the Cluster to Platform rename, the `clusterId` query parameter and the
membership `clusterId` response field are deprecated one-release aliases for
`platformId`. When both are sent, `platformId` wins; the aliases are removed after the
deprecation window (see
[ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)).

## Implementation Notes

Filtering and pagination are pushed to the repository port; the handler only
parses and shapes.
