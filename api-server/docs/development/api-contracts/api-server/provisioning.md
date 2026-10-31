# OS Provisioning

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)
- Swallow installation tooling (reads the OS Image catalog to confirm `ubuntu/noble`)

## Purpose

List, upload, and delete provider-owned OS Images, manage Swallow-owned Deployment
Templates, and submit one OS deployment configuration to one or more eligible Servers.

## Related Glossary Terms

- OS Image
- OS Deployment
- Deployment Template
- Server
- Server Status
- Network Configuration
- Subnet DNS
- IP Binding
- Provisioning Task
- Server Lock

## Authentication and Authorization

Every endpoint requires an admin bearer token. Shared error envelopes, codes,
timestamps, and authentication behavior follow [conventions.md](conventions.md).

## Endpoints

```text
GET    /api/v1/provisioning/images?integrationId={integrationId}
POST   /api/v1/provisioning/images                                              (multipart/form-data)
DELETE /api/v1/provisioning/images?integrationId={integrationId}&imageId={imageId}&architecture={architecture}
PUT    /api/v1/provisioning/images/metadata?integrationId={integrationId}&imageId={imageId}&architecture={architecture}
DELETE /api/v1/provisioning/images/metadata?integrationId={integrationId}&imageId={imageId}&architecture={architecture}
GET    /api/v1/provisioning/templates?siteId={optional}&integrationId={optional}
POST   /api/v1/provisioning/templates
GET    /api/v1/provisioning/templates/{id}
PATCH  /api/v1/provisioning/templates/{id}
DELETE /api/v1/provisioning/templates/{id}
PUT    /api/v1/provisioning/templates/{id}/user-data
DELETE /api/v1/provisioning/templates/{id}/user-data
POST   /api/v1/provisioning/deployments/preflight
POST   /api/v1/provisioning/deployments
POST   /api/v1/provisioning/deployment-operations
POST   /api/v1/provisioning/release-operations
POST   /api/v1/provisioning/recover-operations
POST   /api/v1/provisioning/image-verifications
POST   /api/v1/provisioning/networks/inspect
GET    /api/v1/provisioning/tasks/{id}
POST   /api/v1/provisioning/tasks/{id}/retry
POST   /api/v1/provisioning/reconcile
POST   /api/v1/provisioning/integrations/{id}/reconcile
```

The existing `POST /api/v1/servers/{id}/deploy` contract remains active and
unchanged for backward compatibility.

## OS Image Catalog

`GET /images` requires `integrationId` and reads the named provisioner live, then merges
Swallow's OS Image metadata (see "OS Image Metadata" below) onto it. Each image is the provider's
artifact with the metadata Swallow owns for it as `spec` and what is effectively shown and used,
with the provider's own values and Swallow's verification, as `status`
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)). The
metadata needs no provider realization, so an image has no `generation` or conditions. It returns:

```json
[
  {
    "id": "ubuntu/jammy",
    "architecture": "amd64",
    "spec": {
      "name": "Golden Ubuntu",
      "osSystem": "Ubuntu LTS",
      "release": "22.04",
      "tags": ["gpu", "ml"],
      "defaultUser": ""
    },
    "status": {
      "name": "Golden Ubuntu",
      "osSystem": "Ubuntu LTS",
      "release": "22.04",
      "defaultUser": "ubuntu",
      "providerName": "Ubuntu 22.04 LTS",
      "providerOsSystem": "ubuntu",
      "providerRelease": "jammy",
      "sizeBytes": 5368709120,
      "verifiedDeployTargets": ["disk", "ram"],
      "failedDeployTargets": []
    }
  }
]
```

`spec` is Swallow's metadata: `name`, `osSystem`, and `release` are `""` when Swallow keeps no
value and the provider's applies; `tags` is a Swallow-owned list of labels with no provider
counterpart, always an array; `defaultUser` is the default login user Swallow sets for the image,
`""` when none is set. `id` and `architecture` are never overridable — they identify the deployable
artifact.

`status.name`, `status.osSystem`, and `status.release` are the effective values a client displays:
the `spec` value when set, otherwise the provider value. The `status.provider*` fields
(`providerName`, `providerOsSystem`, `providerRelease`) always carry the provider's own values.

`status.defaultUser` is the effective login user Swallow automation uses on Servers deployed with
this image (see the glossary OS Image and
[decision 039](../../../../../docs/decisions/039-ssh-key-management-and-default-user.md)):
`spec.defaultUser` when set, otherwise a Swallow built-in derived from `providerOsSystem`
(`ubuntu` → `ubuntu`, `centos` → `centos`, `rhel` → `cloud-user`). It is omitted when no value
applies, which is the usual case for an uploaded custom image until an operator sets one;
automation then falls back to the Site SSH user and built-in candidates.

The `status` fields below are described without their `status.` prefix.

`sizeBytes`, when present, is a positive integer containing the provider-reported
total bytes of the newest complete resource set that backs the image. It is omitted
when the provider does not expose a complete size or its detail is temporarily
unavailable. Providers may expose multiple kernel or subarchitecture variants that
collapse into the same `id` + `architecture` row; in that case the catalog returns
the largest current complete-set size rather than summing mutually exclusive
variants. Size is provider-owned and cannot be changed by Swallow's metadata.

`verifiedDeployTargets` is a Swallow-owned array of the deploy targets (`"disk"`
and/or `"ram"`) a Swallow verification has proven this image can deploy in. It is
always present, defaulting to an empty array, so a client can render a per-mode
"verified" indicator without a null check. An empty array means the image has not
been verified for any target. For a custom image (`providerOsSystem: "custom"`) a
Deploy Target absent from this array is blocked for a normal deploy (see "Custom
image verification gate"); synced provider images are provider-trusted and are
never gated regardless of this array. It is keyed by the same
`integrationId` + `imageId` + `architecture` identity as the metadata but stored
separately, so clearing the metadata never clears verification.

`failedDeployTargets` is the mirror Swallow-owned array of deploy targets whose
most recent verification run failed. It is always present, defaulting to an empty
array. It lets a client show a failed verification distinctly from a
never-attempted one — a custom image with `"disk"` in `failedDeployTargets` was
proven not to deploy to disk, as opposed to simply not yet tried. A Deploy Target
is in `verifiedDeployTargets` or `failedDeployTargets` but never both: recording one
outcome clears the other, so the arrays reflect the latest run. A failed target is
still blocked by the verification gate exactly like an unverified one; the array
only affects display, not the gate.

The catalog contains only resources the provisioner accepts for OS deployment.
For MAAS this includes synced operating systems and uploaded custom images, but
excludes PXE and bootloader artifacts returned by the same boot-resources API.
An uploaded image keeps its provider resource name as `id` and is returned with
`providerOsSystem: "custom"`.

The image artifact is provider-owned and is not persisted by Swallow; only the metadata is
Swallow-owned. Missing `integrationId` is
`400 validation_error`; an unknown integration is `404 not_found`; a provider
failure is `503 provider_unavailable`.

`DELETE /images` removes one provider-owned OS Image. It requires `integrationId`,
`imageId`, and `architecture` as query parameters — the same identity `GET /images`
returns — because an `imageId` can contain a slash and cannot be a path segment, and
one image name can back several architectures. It returns `204 No Content` on success.

Only operator-uploaded custom images are removable. A synced OS release is a
provider-owned mirror the provider would immediately re-sync, so the provider refuses
it — as it refuses an `imageId`/`architecture` that matches no deletable image — with a
`400 validation_error` carrying the provider's own wording. A provisioner whose adapter
does not implement image deletion is refused the same way. Any missing query parameter is
also `400 validation_error`; an unknown integration is `404 not_found`; a provider
transport failure is `503 provider_unavailable`. Deleting an image also removes Swallow's
metadata for it, so a deleted image leaves no orphan metadata behind.

## OS Image Upload

`POST /images` uploads a new provider-owned OS Image to one provisioner. It is the only
endpoint on this surface that uses `multipart/form-data` rather than JSON, because it carries
a potentially multi-gigabyte image artifact. Swallow drives the provider upload but does not
own, mirror, or keep a durable copy of the artifact: the uploaded image is provider-owned
exactly like a synced one, and **whether it is classified as a custom image is determined by the
provisioner, not by the caller** (see ADR 027). Upload is an optional provider capability, so a
provisioner whose adapter does not implement it is refused.

The request is `multipart/form-data` with these parts:

- `integrationId` (required): the target provisioner Integration.
- `name` (required): the operator-chosen image name. The adapter maps it into the provider's
  own namespace (for MAAS, the custom-image namespace).
- `architecture` (required): the CPU architecture, e.g. `amd64`. The adapter expands it to the
  provider's form (for MAAS, `amd64/generic`).
- `title` (optional): a human-readable label.
- `filetype` (optional): the artifact format, validated by the provider. When omitted the
  provider's default is used (for MAAS, `tgz`).
- `defaultUser` (optional): the image's default login user, stored as the image's metadata
  `defaultUser` once the provider accepts the upload. It must be a POSIX login name
  (`^[a-z_][a-z0-9_-]{0,31}$`); an invalid value is rejected before anything is uploaded.
- `content` (required): the image file. It is streamed; Swallow never buffers the whole artifact
  in memory and keeps no copy after the provider accepts it.

On success it returns `201` with the created image in the same shape as one `GET /images` row:

```json
{
  "id": "custom/ubuntu-24.04-rocm",
  "architecture": "amd64",
  "spec": { "name": "", "osSystem": "", "release": "", "tags": [], "defaultUser": "" },
  "status": {
    "name": "Ubuntu 24.04 ROCm",
    "osSystem": "custom",
    "release": "ubuntu-24.04-rocm",
    "providerName": "Ubuntu 24.04 ROCm",
    "providerOsSystem": "custom",
    "providerRelease": "ubuntu-24.04-rocm",
    "sizeBytes": 5368709120,
    "verifiedDeployTargets": [],
    "failedDeployTargets": []
  }
}
```

`providerOsSystem` reflects the provider's classification of the uploaded artifact (for MAAS,
`custom`); the caller never sends it. A freshly uploaded image has no metadata, so its `spec` is
empty and each effective field equals its provider value. `sizeBytes` is present when the provider
reports the completed artifact size. When `defaultUser` was sent, the response carries it as
`spec.defaultUser` and `status.defaultUser`. If the provider accepted the artifact but storing the
default user failed, the endpoint returns `500 internal_error`; the image exists and the default
user can be set afterwards with `PUT /images/metadata`.

A missing `integrationId`, `name`, `architecture`, or `content`, an empty or unreadable
`content` file, a `name`, `architecture`, `title`, or `filetype` longer than 200 characters, an
unsupported `filetype`, a name that duplicates an existing provider image, or a provisioner whose
adapter does not implement upload is `400 validation_error` — the provider's own wording is
preserved for a provider-side refusal. An unknown integration is `404 not_found`. A provider transport failure
or 5xx during upload is `503 provider_unavailable`. Because the artifact can be large, this
endpoint is not bounded by the short provider read timeout used for catalog reads.

## OS Image Metadata

No supported provider exposes an operation to rename an image or relabel its OS and release, and
these values have no external owner, so Swallow keeps them as the image's metadata: desired state
that needs no provider realization
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)). It never
changes the provider or the deployable image identity, and it is keyed by the same
`integrationId` + `imageId` + `architecture` identity the catalog returns, passed as query
parameters because an `imageId` can contain a slash and one image name can back several
architectures.

`PUT /images/metadata` replaces the metadata. The body carries the name, OS, and release labels,
the tag list, and the default login user, each optional:

```json
{ "name": "Golden Ubuntu", "osSystem": "Ubuntu LTS", "release": "22.04", "tags": ["gpu", "ml"], "defaultUser": "ubuntu" }
```

`defaultUser` follows the same set/clear rule as the labels: a non-empty value becomes
`spec.defaultUser` and the effective `status.defaultUser`; an empty or omitted value clears it so
the built-in default, if any, applies again. A non-empty value that is not a POSIX login name
(`^[a-z_][a-z0-9_-]{0,31}$`) is `400 validation_error`. A `PUT` replaces the whole metadata, so a
client editing one field sends the current values of the others.

Each field is trimmed before storage. A non-empty label becomes the effective value on subsequent
`GET /images` responses while the corresponding `status.provider*` field continues to show the
provider value; an empty or omitted label clears it so the image shows its provider value. `tags`
are trimmed, blanks dropped, and duplicates removed while preserving order; an empty or omitted
list clears them. When no label, no tag, and no default user remains after normalization, the
metadata is removed entirely, so an all-empty `PUT` reverts the image to its provider values (the
same effect as `DELETE`). It returns `204 No Content` on success. A field or tag longer than 200
characters, or more than 50 tags, is `400 validation_error`. Any missing query parameter or an
invalid body is also `400 validation_error`.

`DELETE /images/metadata` removes the metadata, reverting every field to its provider value. It
uses the same query-parameter identity and returns `204 No Content` even when no metadata existed,
because the requested end state already holds. Any missing query parameter is
`400 validation_error`.

Setting or clearing metadata does not validate the image against the live provider catalog:
metadata for an image that later disappears is simply not merged, so these endpoints stay
Swallow-local with no provider round trip and do not return `503 provider_unavailable`.

A successful `PUT` or `DELETE` propagates the new effective name to the Server
projections immediately: the effective display name mirrored onto each deployed Server
of that integration (the `provisioning.deployedImageName` field on the Servers List and
Server detail) is re-resolved and updated for any server whose deployed image is the one
that was renamed, and each updated server emits a change on the Servers event stream. So
a rename is reflected on the fleet list and Server detail without waiting for the next
reconcile pass. The effective default user is propagated the same way, onto
`provisioning.deployedImageDefaultUser`. This propagation is best-effort and does not
affect the endpoint result: it never changes the `204 No Content` outcome or the provider,
and if it cannot run the periodic reconcile still re-mirrors the name and default user.

## Deployment Templates

A template response contains:

```json
{
  "id": "template-id",
  "siteId": "site-id",
  "integrationId": "integration-id",
  "name": "Ubuntu compute",
  "description": "Default disk deployment",
  "imageId": "ubuntu/jammy",
  "ephemeral": false,
  "network": {
    "mode": "automatic",
    "subnetId": "subnet-id",
    "defaultGateway": false
  },
  "hasUserData": true,
  "createdAt": "2026-08-28T10:00:00Z",
  "updatedAt": "2026-08-28T10:00:00Z"
}
```

`POST /templates` accepts `integrationId`, required `name`, optional
`description`, required `imageId`, optional `deployTarget` (`"disk"` | `"ram"`,
the preferred deploy-mode field) or its deprecated `ephemeral` boolean alias,
optional `network`, and optional write-only `userData`. The stored template
carries the `ephemeral` boolean; `deployTarget` maps onto it (`disk ↔ false`,
`ram ↔ true`) and wins when both are sent. `network.mode` is `automatic`, `dhcp`, or
`static` (glossary Network Configuration); missing network intent defaults to Automatic.
Automatic addressing is realized by provider auto-assign (a stable, provider-recorded
address; see [ADR 018](../../../../../docs/decisions/018-automatic-addressing-provider-auto-assign.md));
DHCP leaves the address, gateway, and DNS to the network's own DHCP server (see
[ADR 058](../../../../../docs/decisions/058-deployment-dhcp-and-subnet-dns.md)). `dhcp` is no
longer an alias of `automatic`, and a template stored with `dhcp` reads as DHCP.
`subnetId` names a live provider subnet and `defaultGateway` defaults to false. A default
gateway can be requested only for Static intent. It returns `201` and never echoes `userData`.

`PATCH /templates/{id}` accepts any subset of `name`, `description`,
`imageId`, the deploy mode (`deployTarget` or its deprecated `ephemeral` alias),
and `network`. It cannot change `integrationId` and
never accepts `userData`. Image or network changes validate the live provider
catalog before persistence. Templates never store a provider interface ID or a
target-specific static IP. Historical records without `network` read as Automatic.

`PUT /templates/{id}/user-data` requires a non-empty `userData` value and
returns `204 No Content`. `DELETE` clears the sealed value and also returns
`204 No Content`. Callers read template metadata again when they need the
updated `hasUserData` flag; neither mutation echoes secret content.

Template names are trimmed and case-insensitively unique within one Integration.
`GET /templates` returns a plain, unpaginated array; its filters are conjunctive, and a Site and Integration that do not match yield an empty array. An
unknown Site, Integration, or template is `404 not_found`; a non-provisioner Integration
or invalid/missing field is `400 validation_error`; duplicate names and deletion of an
Integration still referenced by a template are `409 conflict`. Create, and an update that
changes the network, require a provisioner with network configuration: on one without it
they are `409 conflict`. A provisioner catalog that cannot be read is
`503 provider_unavailable`.

## Network Inspection

`POST /networks/inspect` accepts 1-100 unique `serverIds` and returns one typed
live network observation per target. It is read-only and bounded independently
from deployment preflight:

```json
{
  "targets": [
    {
      "serverId": "server-1",
      "editable": true,
      "disabledReason": "",
      "suggestion": {
        "mode": "static",
        "interfaceId": "23",
        "subnetId": "11",
        "ipAddress": "192.168.100.20",
        "defaultGateway": true
      },
      "network": {
        "interfaces": [
        {
          "id": "23",
          "name": "eth0",
          "macAddress": "52:54:00:50:cd:84",
          "boot": true,
          "physicalState": "unknown",
          "configurationState": "provider_managed",
          "rawProviderMode": "AUTO",
          "links": [
            {
              "id": "91",
              "configurationState": "provider_managed",
              "rawProviderMode": "AUTO",
              "subnetId": "11",
              "subnetName": "management",
              "cidr": "192.168.100.0/24",
              "ipAddress": "",
              "defaultGateway": true
            }
          ],
          "availableSubnets": [
            {
              "id": "11",
              "name": "management",
              "cidr": "192.168.100.0/24",
              "gatewayAddress": "192.168.100.1",
              "managed": true,
              "provisionerDhcp": true
            }
          ]
        }
        ]
      }
    }
  ],
  "issues": []
}
```

`provisionerDhcp` is true when the provisioner itself serves DHCP on the subnet (MAAS: DHCP
on for the subnet's VLAN); false means any DHCP there is the site's.

Configuration states are `dhcp`, `static`, `link_only`, `unconfigured`,
`provider_managed`, or `unknown`. Provider-specific values are diagnostic and
must not be sent back as writable intent. The boot interface is suggested by
default. One explicit Static link on that NIC is preserved as the suggested
deployment mode, subnet, IP address, and default-gateway intent. Multiple Static
links remain ambiguous and require operator input. Without an explicit Static
link, Swallow uses an existing linked subnet when unambiguous, or the sole compatible
managed subnet, and suggests DHCP when that subnet has no provisioner DHCP
(`provisionerDhcp` false), Automatic otherwise. Multiple compatible subnets produce
`network_selection_required` instead of a silent choice and suggest Automatic.

`issues[]` entries carry `serverId`, `code`, and `message`. Codes are
`network_interface_required` (no boot NIC could be chosen), `network_selection_required`
(no subnet could be chosen), and `integration_mismatch`: targets of different Integrations
are not read, and the response then has an empty `targets` array with one
`integration_mismatch` issue per mismatched target. An empty or duplicate target list, or one
longer than 100, is `400 validation_error`, as is a non-provisioner Integration; an unknown
Server or provider Machine is `404 not_found`; a provisioner without network configuration
is `409 conflict`; and an unreachable provisioner is `503 provider_unavailable`.

## Deployment Target Preflight

`POST /deployments/preflight` accepts only the intended targets:

```json
{
  "serverIds": ["server-1", "server-2"]
}
```

It performs the target identity, presence, Ready, unlocked, and same-Integration
checks that `POST /deployments` repeats immediately before dispatch. Lock is
read live from the provisioner; unavailable lock state returns
`503 provider_unavailable` rather than accepting mutation work. The operation
is read-only: it does not inspect or mutate network configuration, validate an
image, reserve a Server, or start a deployment.

A completed check returns `200`, including when one or more targets are not ready:

```json
{
  "valid": false,
  "integrationId": "integration-id",
  "issues": [
    {
      "serverId": "server-1",
      "code": "locked",
      "message": "Unlock the Server before deployment."
    }
  ]
}
```

Issue codes are `integration_mismatch`, `absent`, `not_ready`, `locked`,
`provider_machine_missing` (the provisioner no longer has the Server's Machine), or
`provider_not_ready` (a provider without network configuration refused the target's
deployment readiness; the message is the provider's). An empty or duplicate target list, or
a list longer than 100, is `400 validation_error`; a Server unknown to Swallow is
`404 not_found`.

The request names no deploy target, so this check does not report `inspection_required`,
which blocks only a disk deployment: `POST /deployments`, `POST /deployment-operations`, and
Platform deployment acceptance report it once the target is resolved (see
[Multi-Server Deployment](#multi-server-deployment)).

## Multi-Server Deployment

`POST /deployments` accepts:

```json
{
  "serverIds": ["server-1", "server-2"],
  "templateId": "template-id",
  "settings": {
    "imageId": "ubuntu/noble",
    "deployTarget": "disk"
  },
  "userData": {
    "mode": "inherit",
    "value": ""
  },
  "network": {
    "mode": "static",
    "subnetId": "11",
    "defaultGateway": true,
    "assignments": [{
      "serverId": "server-1",
      "interfaceId": "23",
      "subnetId": "11",
      "ipAddress": "192.168.100.20"
    }]
  }
}
```

`serverIds` must contain 1-100 unique values. Without `templateId`,
`settings.imageId` is required, the deploy target defaults to `disk`, and user
data defaults to `omit`. With a template, omitted settings use the template and
omitted user data defaults to `inherit`.

`settings.deployTarget` (`"disk"` | `"ram"`) is the preferred way to choose the
deploy mode; `"ram"` runs the OS from memory (what the provider calls ephemeral).
`settings.ephemeral` (boolean) remains accepted as a deprecated alias and maps
one-to-one (`disk ↔ false`, `ram ↔ true`); when both are sent, `deployTarget`
wins. An unknown `deployTarget` value is a `400`. This applies identically to
`POST /deployment-operations` and, through the platform contract, to platform
deploys.

**Deployment Key gate.** Swallow logs in to deployed Servers with the installation's Deployment
Key (see [ssh-keys.md](ssh-keys.md)), so every OS deployment — `POST /deployments`,
`POST /deployment-operations`, `POST /image-verifications`, the deprecated
`POST /servers/{id}/deploy`, and Platform deploys with `provision_os` — is refused at acceptance
with `409 conflict` when no Deployment Key exists. The message names the installation command
that creates it (`swallow-api deployment-key ensure`). Nothing is written to the provider.

**Custom image verification gate.** When the resolved image is a *custom* image
(`providerOsSystem: "custom"`) that has not been verified for the requested
Deploy Target, the deploy is refused at acceptance with `409` and a Swallow
reason ("this custom image is not verified for `<disk|ram>` deployment; verify it
on a ready Server first"). Synced provider images bypass this gate. A known
Server/image architecture mismatch is likewise refused with `409`. An image the
provisioner has not fully staged — for example a custom upload whose boot or kernel
resources are still missing — is also refused at acceptance with `409` and a Swallow
reason ("the `<name>` image is not fully staged by the provisioner and cannot be
deployed"), so an incomplete image is never handed to the provider only to fail
opaquely mid-install. These gates are applied by the shared deploy resolve, so they
cover `POST /deployments`, `POST /deployment-operations`, and platform deploys. See
"Image Verification".


Missing `network` resolves to Swallow's Automatic default. `network.mode` accepts
`automatic`, `dhcp`, or `static`; keep-current is not valid intent. Automatic is realized
by the provider's auto-assign capability, so the deployed address is stable and always
provider-recorded; its DNS servers are the subnet's Subnet DNS
([provisioner-subnet-dns.md](provisioner-subnet-dns.md)) or, when that is empty, the
provisioner's own DNS. DHCP links the NIC for the network's own DHCP server, which assigns
the address, gateway, and DNS; the provider records only the address it observes. Each
target resolves its boot NIC by default and can override `interfaceId` and
`subnetId`. Static requires one valid, unique per-target `ipAddress` and takes DNS like
Automatic. A per-target `ipAddress` and `defaultGateway` are valid only for Static.
Templates may supply mode, subnet, and gateway intent but never a NIC ID or static IP.
`userData.mode` is one of:

- `inherit`: use the template's sealed user data; valid only with a template.
- `replace`: use the non-empty write-only `value` for this request only.
- `omit`: send no user data for this request.

Before any provider write, every Server must exist, be present, have provisioning
state `ready`, be unlocked, belong to the same Integration, and match the
template Integration when one is used. The lock preflight reads each target live from the provider before dispatch. The
same live read refuses a **disk** deployment (`deployTarget` `disk`, or `ephemeral` false or
omitted) of a Server whose storage layout needs a new inspection (`provisioning.inspectionRequired`,
see [servers-list.md](servers-list.md#provisioning-axis-fields)) with `409 conflict` naming the
Server and its `inspection_required` reason; a RAM deployment of that Server is allowed. The
resolved image must exist in the
current live catalog. Swallow inspects every target's live network capability,
NICs, subnets, existing links, and Static address before the first write. A
preflight failure rejects the entire request without writes.

After preflight, each target configures and verifies its network, then starts OS
deployment. Target dispatch uses at most four workers. Individual provider
refusals do not roll back accepted deployments or network changes. Every dispatched batch
returns `202`:

```json
{
  "requested": 2,
  "accepted": [
    {
      "serverId": "server-1",
      "state": "deploying",
      "providerState": "Deploying",
      "powerState": "on",
      "osSystem": "ubuntu",
      "distroSeries": "jammy",
      "ephemeral": false,
      "hweKernel": "",
      "locked": false,
      "commissioningStatus": "",
      "testingStatus": "",
      "observedAt": "2026-08-28T10:00:00Z"
    }
  ],
  "failed": [
    {
      "serverId": "server-2",
      "code": "provider_rejected",
      "message": "Provider rejected deployment.",
      "stage": "deployment"
    }
  ]
}
```

The response is an acceptance report, not a durable job. Deployment progress is
read from each Server provisioning axis. Batch deployment provides no rollback.

Each `accepted[]` entry is the provisioning snapshot the provider returned on
acceptance; it also carries `inspectionRequired` and `inspectionRequiredReason` when the
Server's layout needs a new inspection, and omits them otherwise. `failed[].stage` is
`network_configuration` or `deployment`; `failed[].code` is `provider_rejected` (the
provider refused, or no longer has the Machine), `provider_unavailable` (the provider could
not be reached during dispatch), or `provider_error` (any other dispatch failure).

Preflight validation uses `400 validation_error` for malformed input (including an
image missing from the live catalog), `404 not_found` for an unknown Server or template,
`409 conflict` for target state or
Integration mismatch, and `503 provider_unavailable` when the provider cannot
be reached before dispatch. Secret values never appear in responses or errors.
Dispatched provider refusals preserve actionable provider validation detail in
`failed[].message`; for example, a Static IP allocation conflict remains a
`network_configuration` failure rather than being reported as a missing Server.

## Durable OS Operations

`POST /deployment-operations` accepts the same request as `/deployments`, runs the same
complete side-effect-free preflight, and persists one schema-v3 Operation:

```json
{ "operationId": "operation-id" }
```

It creates one `provision-os` MAAS Step per Server, with a maximum of four concurrent
Steps. HTTP acceptance and the provider's `deployed` state are not Swallow completion.
Each Step observes the requested image, refreshes the Server address projection, and
requires the Site's configured SSH port (default 22) to become reachable. A missing
provider address or unreachable SSH endpoint therefore fails the Step and the Operation;
MAAS's `deployed` value remains available only as provider-owned lifecycle diagnostics.
While the provider remains in `deploying`, Swallow checks live BMC power after ten
minutes. A machine that is still powered off becomes `requires_attention` with code
`deployment_power_on_timeout` and stage `deployment_power_on`; Swallow does not abort,
power on, or resubmit it automatically. Providers without live power inspection retain
the general two-hour observation timeout.

**Time synchronization.** Every OS provisioning configures time
([decision 065](../../../../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md)):
the Operation also has one `ansible` Step `configure-ntp-<serverId>` per Server, depending only
on that Server's `provision-<serverId>`. It runs the release playbook `configure-ntp` against
the installation's NTP server (the api-server setting `SWALLOW_API_NTP_SERVER`, which the
production installation sets to `SWALLOW_ADDRESS`), frozen at acceptance as the trusted variable
`swallow_ntp_servers`. The playbook configures the time client the OS already has, in the order
`systemd-timesyncd`, `chrony`, `ntpd`, installs chrony only when there is none, and stops every
other client; the run output names the client used. The Step fails with `ansible_failed` whose run
output carries `[ntp_not_synchronized]` when the client gets no reply within two minutes.
`systemd-timesyncd` still prefers a server a DHCP lease offers; on a subnet MAAS serves that is the
MAAS rack, which is the swallow host too. The Server's `deployment` result still reflects only its
`provision-os` Step. Acceptance fails with `409 conflict` when the installation has no NTP server
(the message names `SWALLOW_API_NTP_SERVER`), and, because these Steps are Ansible, with
`503 provider_unavailable` while the Site's automation is not runnable (see
[site-automation.md](site-automation.md)). OS image verification does not configure time: it
returns the Server afterwards.

**Boot Media ensure.** For each target Server whose Boot Media is enabled when the Operation is
accepted (see [server-detail-actions.md](server-detail-actions.md#boot-media) and
[decision 047](../../../../../docs/decisions/047-redfish-boot-media.md)), the Operation also has
an `internal` Step `ensure-boot-media-<serverId>` of kind `ensure-boot-media`, and that Server's
`provision-os` Step depends on it. The Step's parameters carry the URL of that Server's chosen
Boot ISO ([boot-isos.md](boot-isos.md), [decision 049](../../../../../docs/decisions/049-boot-iso-builder.md))
as accepted (`isoUrl`). When it runs it re-reads the Server's setting — a Server whose Boot
Media was disabled meanwhile is left alone — then reads the BMC and re-applies the ISO mount and
boot direction unconditionally, because a BMC can lose either between deployments. A BMC that
cannot be reached or refuses fails the Step as retryable (`boot_media_ensure_failed`), and the
dependent `provision-os` Step does not run; an Operation accepted while the Server's Boot Media
named no served Boot ISO (none chosen, the Boot ISO's file missing, or no Boot Media base URL)
fails it as not retryable (`boot_media_not_configured`). Servers without Boot Media get
exactly the Steps described above. The same Step is added in front of `provision-os` in OS image
verification and in a Platform's `ensure-os` Job. Hardware inspection always has the Step and
resolves the Boot ISO when it runs instead (`resolveLive`, no `isoUrl`); see
[server-enrollment.md](server-enrollment.md).

Such a Server's `provision-os` Step carries the same URL as `bootMediaIsoUrl`, next to its
`request`, and watches the deployment boot, timed from its first `deploying` reading. Two minutes
in, if the BMC no longer holds the ISO (some BMCs drop a fresh mount at the provider's power-on),
the Step mounts it again. Ten minutes in, if the provider's newest event is still the deployment
start (the host never network-booted, for example because its BIOS booted the disk), it
re-applies Boot Media and restarts the host through Redfish so the deployment boot starts from
the ISO; the stage-stall rule below then measures from that restart. The host is restarted at
most once per observation; the watch does not change the Step's status, and a failure is only
logged. A deployment that still makes no progress is reported by the stage-stall rule.

While the provider remains in `deploying`, Swallow also reads the provider's machine event
stream on every observation
([decision 046](../../../../../docs/decisions/046-os-deployment-progress-and-stall.md)).
The newest event recorded since the current attempt started is projected onto the Server
deployment axis as non-terminal progress — `state` stays `deploying`, `code` stays empty,
`stage` is the event type in snake_case (for example `configuring_os`), and `statusReason` names
it, for example `Provider stage: Configuring OS (since 2026-10-02T16:48:45Z).` — and is rewritten
only when the provider advances. When no newer event arrives for 25 minutes, the Step becomes
`requires_attention` with code `deployment_provider_stage_stall`, stage set to the stuck event's
snake_case type (or `deployment_progress` when no event was recorded since the attempt started),
and a message naming the stage and since when. Progress is measured from provider event
timestamps, so a worker restart or activity retry does not grant a fresh window; an explicit Step
Retry starts a new attempt and therefore a new window. Swallow does not abort the provider
deployment. A provider without an event stream, or an event read that fails, never produces a
stall; the power-on check and the two-hour observation timeout stay authoritative.

A Machine that the provider reserved (`allocated`) after the deployment was accepted but that
never starts installing within the power-on window (10 minutes) makes the Step
`requires_attention` with code `deployment_reserved_not_started` (stage `deployment`). The Machine
is left `allocated`, which Recover and Release accept, so the operator can return it to Ready
before retrying.

Retry observes before writing. If the expected image is now SSH-reachable, the Step
succeeds without repeating provider work. If MAAS installed the image but reports no
address, explicit Retry releases that unusable installation, waits for Ready, reapplies
the frozen automatic/static intent, and redeploys the same image and cloud-init. If MAAS
reports the Machine `failed`, `allocated`, `broken`, or `rescue`, explicit Retry first
returns it to Ready with the same state-driven plan as Recover (static IP bindings are
kept), and then redeploys the same frozen intent; failures on that path carry a stage
prefixed `deployment_recovery_`. When the frozen intent is a disk deployment and the
Server's layout needs a new inspection, Retry does neither: the Step fails before Recover
with code `inspection_required` (stage `deployment_preflight`, not retryable, message is the
Server's `inspectionRequiredReason`), so the Workflow ends and the Server can be inspected. A Machine already deployed with a different image is not
replaced: the Step fails with `deployment_image_mismatch` (not retryable) so the Workflow
ends and the Server can be released explicitly. This destructive recovery is never
automatic: a first attempt, or a Temporal activity retry of it after worker turnover,
reports a provider failure as `deployment_failed` instead. If an address exists but SSH
remains unreachable, Retry only observes so routing, firewall, image, or service
remediation does not discard an installed OS.

A provider-reported deployment failure (`failed` or `broken` while observing) fails the
Step with `deployment_failed` (stage `deployment`, retryable). Its message names the cause
when the provisioner can explain it:
`The provisioner reported that OS deployment failed: <provider error description>. Installation error: <line>`.
The description is the Server's `provisioning.errorDescription`; the installation error is the
most specific line of the provider's newest failed installation log that started after the
Step attempt started (for MAAS: the curtin handler error, else the exception that ended its
traceback, else the first failed stage), at most 300 characters, with URL credentials removed
and any line that mentions a password, token, or secret never used. Each part is omitted when
unavailable, down to `The provisioner reported that OS deployment failed.`; reading the
installation log never changes the Step outcome. Lost provider responses enter observation
first; Swallow does not immediately submit a second deployment. An unknown outcome becomes
`requires_attention`. A deployment Swallow's own preflight refuses before any provider
write (for example a target that is not ready, an image that is not staged or verified, or
a network conflict) fails with `deployment_preflight_rejected` (not retryable), so the
Workflow ends instead of parking on a retry that repeats the refusal. Cloud-init is
removed from the intent snapshot and stored as an encrypted opaque Step reference.

`POST /release-operations` accepts 1-100 Servers:

```json
{
  "serverIds": ["server-1"],
  "erase": false,
  "secureErase": false,
  "quickErase": false,
  "comment": "Return machines to the ready pool",
  "unbindStaticIPs": true,
  "cancelActiveWorkflows": false
}
```

It performs complete presence, recoverable-state, Site, duplicate-target, active-work, and
live lock validation before persistence, then creates one `release-os` MAAS Step per
Server. A Step succeeds only after Ready is observed. When static cleanup is requested,
the same Step also waits for its durable Provisioning Task; Step Retry after a cleanup
failure retries cleanup only and never sends Release again. Provider cancellation is
best-effort and confirmed prior effects are preserved.

Every attempt of a `release-os` Step reads the Server first. A Server already `releasing` is
followed to Ready, and one already `ready` meets the Step's goal without a provider Release.
When static cleanup was requested and this Step did not send the Release, nothing captured the
bindings to remove: the Step fails with `release_cleanup_not_applied` (stage `network_cleanup`,
not retryable) and its message says the bindings remain.

`cancelActiveWorkflows` (optional, default `false`) supersedes the unfinished Workflows that
hold any target instead of refusing the request as busy
([decision 061](../../../../../docs/decisions/061-recovery-supersedes-blocking-work.md)). After
every other validation passes, Swallow requests cancellation of each such Workflow (see the
Workflows contract's Cancel) and creates the Operation at once, so the response returns
within the request. The Operation waits in `waiting_dependency` ("Waiting for resource leases.")
until those Workflows end and release their leases, then runs; a canceled deployment may have
returned its Server to Ready by then, which the Step accepts as above. A cancel Swallow refuses
for a Workflow that is still running fails the request with `409 conflict` naming that Workflow,
and nothing is created.

A Server a Platform claims (its `platformClaim`, see
[servers-list.md](servers-list.md#platform-claim) and
[decision 067](../../../../../docs/decisions/067-platform-claim-on-server.md)) is refused:
releasing it would take a node out from under that Platform. The whole batch fails with
`409 conflict` before anything is created or canceled, so `cancelActiveWorkflows` does not get
around it, and the message names the Server and the Platform, for example `Server gpu-node-01
belongs to kubernetes Platform production-k0s; uninstall the Platform (with releaseServers to
also release its Servers) instead of releasing the Server directly`. A Platform's Servers are
released through its uninstall with `releaseServers`
([platforms.md](platforms.md#uninstall-a-deployed-platform)), which is not subject to this rule.

Release is the primary provider-recovery path and is not restricted to `deployed`. The
Swallow-owned recovery policy (decision 033, extended by decision 036) allows Release from
`deployed`, `allocated`, `failed`, `broken`, and `rescue`, always converging the Machine to
`ready`. `allocated` is included because MAAS parks a machine there when a deployment was
reserved but never finished, and the provider allows Release from it. A target in any other
state (for example `releasing`, `deploying`, `new`, or already `ready`) is rejected with a
Swallow-authored reason rather than the provider's message.

Both endpoints return `202` with an Operation reference, `{ "operationId": "..." }`, and
nothing else; the request ID is recorded on the Workflow as its `requestCorrelation`, read
through the Workflow, not returned here.
Progress, target-specific normalized errors, Cancel, and safe Retry are read and controlled
through the [Operations](operations.md) contract.

Acceptance-time rejection is reported with the shared error envelope, never as an opaque
`500 internal_error`: malformed input (missing or out-of-range `serverIds`, a target in a
state Release is not allowed from, a duplicate target, or a cross-Site batch) is
`400 validation_error`; a Server unknown to Swallow is `404 not_found`; a target already
inside an unfinished Operation, a locked target, or a target a Platform claims is
`409 conflict`; and a build without durable provisioning wired up is
`503 provider_unavailable`.

`POST /recover-operations` returns a Server to `ready` from a not-usable state and accepts
the same bounded batch shape as Release, without the disk-erase controls:

```json
{
  "serverIds": ["server-1"],
  "comment": "Return failed nodes to the ready pool",
  "unbindStaticIPs": true,
  "cancelActiveWorkflows": false
}
```

It performs the same presence, Site, duplicate-target, active-work, and live lock
validation, then creates one `recover-server` MAAS Step per Server. `cancelActiveWorkflows`
behaves as it does for Release. Recover is allowed
from `failed`, `broken`, `rescue`, `deployed`, and `allocated`; a target already `ready` is
accepted as an immediate success (no-op) and any other state is a `400 validation_error`
with a Swallow reason. Each Step chooses the provider primitive by observed state — `broken`
is returned with Mark fixed, `rescue` exits rescue and then Releases if it is still not
`ready`, and `failed` (or `deployed`/`allocated`) is Released — and succeeds only after
`ready` is observed. A Machine that fails to leave rescue (a settled "failed to exit rescue" state, or
one that hangs in the transition past a grace period) is escalated to Mark broken and then
Mark fixed, which returns it to `ready` without a disk erase; this is the only path out for
a Machine whose exit-rescue and disk-erase both fail on the provider. `unbindStaticIPs`
behaves as it does for Release when the recovery path performs a Release. Error mapping
matches `/release-operations`.

Recovering a `deployed` Server releases it, so a `deployed` target a Platform claims is refused
with `409 conflict` exactly like a Release of it. A Platform's Server in a not-usable state
(`failed`, `broken`, `rescue`, `allocated`) can still be recovered: returning it to `ready` is
how a broken deployment is repaired.

## Image Verification

`POST /image-verifications` launches a durable `verify-os-image` Operation that
proves one custom OS Image works for one Deploy Target by running a real deploy on
an operator-chosen ready Server, recording the Swallow-owned verification on
success, then auto-releasing the Server. It accepts:

```json
{
  "integrationId": "integration-id",
  "imageId": "custom/rocky-10.2",
  "architecture": "amd64",
  "deployTarget": "ram",
  "serverId": "server-1",
  "keepServer": false
}
```

`integrationId`, `imageId`, `architecture`, and `serverId` are required;
`deployTarget` is `"disk"` or `"ram"` (an unknown value is a `400`). `keepServer`
is optional and defaults to `false`: when `false` the borrowed Server is returned
to the ready pool after the verification (see the return Step below); when `true`
the return Step is omitted, leaving the Server deployed on success (for an operator
who wants to keep the verified deployment) and in its failed state on failure.
Acceptance preflight requires the Server to exist, be `ready`, be unlocked, belong
to the image's Integration, and match the image architecture. It responds `202`
with `{ "operationId": "..." }`. A missing field, an unknown `deployTarget`, a Server that
is absent, not `ready`, of another Integration, or of another architecture, and an image not
in the provisioner's live catalog are `400 validation_error`; an unknown Server or Integration
is `404 not_found`; a locked or busy Server, an image the provisioner has not fully staged, or a
missing Deployment Key is `409 conflict`.

The Operation runs Steps: `provision-os` (the real deploy in the target mode; this
proving deploy is exempt from the custom-image verification gate), one of two
mutually exclusive internal finalize Steps — `record-image-verification` when the
proving deploy succeeded, or `record-image-verification-failure` when it failed —
and, unless `keepServer` is `true`, `recover-server` (return the borrowed Server to
`ready`). Verification is a borrow-and-return contract: by default the return Step
runs whether the proving deploy succeeded or failed, so the borrowed Server is
always given back; with `keepServer: true` the return Step is omitted and the Server
is left deployed. Exactly one record Step runs; the other is skipped. A failed proving deploy is terminal (not a
retry-park), so the Operation reaches `partially_succeeded` after the Server is
returned, rather than holding the Server and showing a perpetual "verifying". On
success the image's `verifiedDeployTargets` gains the target; on failure its
`failedDeployTargets` gains the target and the provider's reason is on the failed
`provision-os` Step. See
[decision 035](../../../../../docs/decisions/035-os-image-verification-and-deploy-target.md)
and [decision 036](../../../../../docs/decisions/036-provisioning-lifecycle-integrity.md).

## Provisioning Tasks

`GET /tasks/{id}` and `GET /servers/{serverId}/provisioning-tasks` expose
Swallow-owned provider coordination without returning the saved static-link
snapshot:

```json
{
  "id": "task-id",
  "kind": "release_network_cleanup",
  "serverId": "server-1",
  "status": "failed",
  "phase": "cleaning_network",
  "attempt": 2,
  "error": "MAAS refused to unlink the captured Static address.",
  "requestId": "request-id",
  "retryable": true,
  "createdAt": "2026-09-02T01:00:00Z",
  "updatedAt": "2026-09-02T01:05:00Z"
}
```

Statuses are `pending`, `running`, `succeeded`, or `failed`. Phases are
`waiting_for_release`, `waiting_for_ready`, `cleaning_network`, or `complete`.
`retryable` is true only for a failed task after its Release was accepted.
`error` and `requestId` are omitted when empty.
`POST /tasks/{id}/retry` accepts only that retryable state, returns `202` with the task as
stored after the requeue, and requeues cleanup without repeating Release. A retry is rejected
with `409 conflict` while the Server is locked, and with `503 provider_unavailable` when its
live lock state cannot be read; the worker also reads lock state again immediately before
cleanup, so an external lock makes the task fail retryably without changing network links. A
task retained after the provider refused Release remains diagnostic and is not retryable.
Unknown tasks, and a retry of a task whose Server no longer exists, are `404 not_found`; a task
that is not retryable is `409 conflict`.

## Provisioner Reconciliation

Reconciliation projects a provisioner's Machine inventory onto Server records; a background
loop runs it at the reconcile interval, and these endpoints run one pass immediately. Neither
takes a body.

`POST /reconcile` reconciles every enabled provisioner Integration and returns a plain array
with one report per Integration. One provisioner failing does not stop the others.

`POST /integrations/{id}/reconcile` reconciles one provisioner Integration, enabled or not,
and returns its report:

```json
{
  "integrationId": "integration-id",
  "integrationName": "maas-east",
  "siteId": "site-id",
  "machines": 12,
  "created": 1,
  "updated": 10,
  "relinked": 0,
  "markedAbsent": 1,
  "conflicts": [
    {
      "providerMachineId": "abc123",
      "hostname": "gpu-07",
      "reason": "hardware identifiers match more than one existing server; refusing to guess which one this is",
      "candidateServerIds": ["server-1", "server-2"]
    }
  ],
  "error": null
}
```

`conflicts` lists the Machines the pass refused to project, because guessing which Server
they are could merge two Servers; they need an operator, which is why the report is returned
rather than a bare acknowledgement. `error` is `null` for a completed pass and otherwise says
why the pass stopped (the provisioner could not be read, for example); a failed pass still
answers `200`, and the same message is recorded as the Integration's last sync error. An
unknown Integration is `404 not_found` and a non-provisioner Integration `400 validation_error`.

## Compatibility Notes

On 2026-10-31 the OS Image overlay became OS Image metadata
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)). Breaking,
with no alias: a catalog image is `{id, architecture, spec, status}` — the `custom*` fields and
`tags` became `spec`, and the effective and `provider*` values, `sizeBytes`, and the verification
arrays moved under `status` — and `PATCH /images/overlay` / `DELETE /images/overlay` became
`PUT /images/metadata` / `DELETE /images/metadata` with the same body and responses.

`POST /provisioning/deployments`, `POST /servers/{id}/deploy`, and
`POST /servers/{id}/release` preserve their existing wire behavior for one release and
return `Deprecation: true` plus a successor `Link` header. Dashboard command flows use the
durable Operation endpoints. Existing Server projections, authorization, and provider
action behavior are unchanged.

On 2026-10-19 `POST /release-operations` and `POST /recover-operations` gained the optional
`cancelActiveWorkflows` field, and a first `release-os` attempt began observing a Server that is
already Ready or Releasing instead of sending another Release; clients that omit the field are
unaffected ([decision 061](../../../../../docs/decisions/061-recovery-supersedes-blocking-work.md)).

On 2026-10-19 the deployment preflight began refusing a disk deployment of a Server whose
layout needs a new inspection (`inspection_required`), and `deployment_failed` messages began
carrying the provider's error description and installation error line. Both keep their status
codes and error codes; clients that branch on codes are unaffected
([decision 057 amendment](../../../../../docs/decisions/057-uefi-boot-iso-hand-off.md)).

On 2026-10-29 `POST /release-operations`, the deprecated `POST /servers/{id}/release`, and
`POST /recover-operations` for a `deployed` target began refusing a Server a Platform claims
with `409 conflict` (see Durable OS Operations). Before, such a request was accepted and took
the node out from under its Platform. Release a Platform's Servers through its uninstall
([decision 067](../../../../../docs/decisions/067-platform-claim-on-server.md)).
