# OS Provisioning

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)
- swallow installation tooling (reads the OS Image catalog to confirm `ubuntu/noble`)

## Purpose

List, upload, and delete provider-owned OS Images, manage swallow-owned Deployment
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
DELETE /api/v1/provisioning/images?integrationId={integrationId}&imageId={imageId}&architecture={architecture}
POST   /api/v1/provisioning/image-uploads
PUT    /api/v1/provisioning/image-uploads/{id}/content                          (application/octet-stream)
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

## OS Image Catalog

`GET /images` requires `integrationId` and reads the named provisioner live, then merges
swallow's OS Image metadata (see "OS Image Metadata" below) onto it. Each image is the provider's
artifact with the metadata swallow owns for it as `spec` and what is effectively shown and used,
with the provider's own values and swallow's verification, as `status`
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md) (provisioner realized desired state)). The
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

`spec` is swallow's metadata: `name`, `osSystem`, and `release` are `""` when swallow keeps no
value and the provider's applies; `tags` is a swallow-owned list of labels with no provider
counterpart, always an array; `defaultUser` is the default login user swallow sets for the image,
`""` when none is set. `id` and `architecture` are never overridable — they identify the deployable
artifact.

`status.name`, `status.osSystem`, and `status.release` are the effective values a client displays:
the `spec` value when set, otherwise the provider value. The `status.provider*` fields
(`providerName`, `providerOsSystem`, `providerRelease`) always carry the provider's own values.

`status.defaultUser` is the effective login user swallow automation uses on Servers deployed with
this image (see the glossary OS Image and
[decision 039](../../../../../docs/decisions/039-ssh-key-management-and-default-user.md) (ssh key management and default user)):
`spec.defaultUser` when set, otherwise a swallow built-in derived from `providerOsSystem`
(`ubuntu` → `ubuntu`, `centos` → `centos`, `rhel` → `cloud-user`). It is omitted when no value
applies, which is the usual case for an uploaded custom image until a user sets one;
automation then falls back to the Site SSH user and built-in candidates.

The `status` fields below are described without their `status.` prefix.

`sizeBytes`, when present, is a positive integer containing the provider-reported
total bytes of the newest complete resource set that backs the image. It is omitted
when the provider does not expose a complete size or its detail is temporarily
unavailable. Providers may expose multiple kernel or subarchitecture variants that
collapse into the same `id` + `architecture` row; in that case the catalog returns
the largest current complete-set size rather than summing mutually exclusive
variants. Size is provider-owned and cannot be changed by swallow's metadata.

`verifiedDeployTargets` is a swallow-owned array of the deploy targets (`"disk"`
and/or `"ram"`) a swallow verification has proven this image can deploy in. It is
always present, defaulting to an empty array, so a client can render a per-mode
"verified" indicator without a null check. An empty array means the image has not
been verified for any target. For a custom image (`providerOsSystem: "custom"`) a
Deploy Target absent from this array is blocked for a normal deploy (see "Custom
image verification gate"); synced provider images are provider-trusted and are
never gated regardless of this array. It is keyed by the same
`integrationId` + `imageId` + `architecture` identity as the metadata but stored
separately, so clearing the metadata never clears verification.

`failedDeployTargets` is the mirror swallow-owned array of deploy targets whose
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

The image artifact is provider-owned and is not persisted by swallow; only the metadata is
swallow-owned. Missing `integrationId` is
`400 validation_error`; an unknown integration is `404 not_found`; a provider
failure is `503 provider_unavailable`.

`DELETE /images` removes one provider-owned OS Image. It requires `integrationId`,
`imageId`, and `architecture` as query parameters — the same identity `GET /images`
returns — because an `imageId` can contain a slash and cannot be a path segment, and
one image name can back several architectures. It returns `204 No Content` on success.

Only user-uploaded custom images are removable. A synced OS release is a
provider-owned mirror the provider would immediately re-sync, so the provider refuses
it — as it refuses an `imageId`/`architecture` that matches no deletable image — with a
`400 validation_error` carrying the provider's own wording. A provisioner whose adapter
does not implement image deletion is refused the same way. Any missing query parameter is
also `400 validation_error`; an unknown integration is `404 not_found`; a provider
transport failure is `503 provider_unavailable`. Deleting an image also removes swallow's
metadata for it, so a deleted image leaves no orphan metadata behind.

## OS Image Upload

Uploading a new provider-owned OS Image to one provisioner takes two requests: the caller
first creates an **OS Image upload** that states the intent and the content's size and sha256,
then streams the content to it. swallow drives the provider upload but does not own, mirror, or
keep a copy of the artifact: the content is forwarded to the provider as it arrives, never staged
on swallow's disk or held whole in memory, and the uploaded image is provider-owned exactly like
a synced one. **Whether it is classified as a custom image is determined by the provisioner, not
by the caller** (see [decision 027](../../../../../docs/decisions/027-os-image-upload.md) (os image upload)). Upload is an optional provider capability, so a
provisioner whose adapter does not implement it is refused.

### Create an upload

`POST /image-uploads` validates the intent and records it; it has no effect on the provider. The
JSON body has these fields:

- `integrationId` (required): the target provisioner Integration.
- `name` (required): the user-chosen image name. The adapter maps it into the provider's
  own namespace (for MAAS, the custom-image namespace).
- `architecture` (required): the CPU architecture, e.g. `amd64`. The adapter expands it to the
  provider's form (for MAAS, `amd64/generic`).
- `title` (optional): a human-readable label.
- `filetype` (optional): the artifact format, validated by the provider adapter. When omitted the
  provider's default is used (for MAAS, `tgz`).
- `defaultUser` (optional): the image's default login user, stored as the image's metadata
  `defaultUser` once the upload succeeds. It must be a POSIX login name
  (`^[a-z_][a-z0-9_-]{0,31}$`).
- `sizeBytes` (required): the exact length of the content, a positive integer.
- `sha256` (required): the lowercase hex SHA-256 of the content. The provider verifies the
  content against it, so a wrong value fails the upload when the content is complete.

It returns `201` with the upload's swallow-issued `id` and the time it expires if no content is
sent:

```json
{ "id": "8b1f3c2e-4d5a-4b6c-9e7f-0a1b2c3d4e5f", "expiresAt": "2026-11-05T08:00:00Z" }
```

An upload expires one hour after it is created. A missing `integrationId`, `name`,
`architecture`, `sizeBytes`, or `sha256`, a `name`, `architecture`, `title`, or `filetype` longer
than 200 characters, a `sizeBytes` that is not a positive integer, a `sha256` that is not 64
lowercase hex characters, an invalid `defaultUser`, an Integration that is not a provisioner, a
provisioner whose adapter does not implement upload, or an intent the adapter refuses (for
example an unsupported `filetype`) is `400 validation_error`. An unknown integration is
`404 not_found`. A `sizeBytes` larger than the installation's upload limit
(`api.imageUploadMaxBytes`) is `413 payload_too_large`. An Integration with no credential is
`503 provider_unavailable`.

### Send the content

`PUT /image-uploads/{id}/content` streams the content of one upload. The body is the raw content
with `Content-Type: application/octet-stream` and a `Content-Length` equal to the upload's
`sizeBytes`; chunked transfer encoding is refused. swallow creates the image at the provider,
forwards the content to it as it arrives, and waits — for up to five minutes after the last byte
— until the provider reports the image fully staged, so a `201` means the image can be deployed
(subject to the custom image verification gate). An upload is single-use: it is removed when this
request finishes, whether it succeeded or failed, and a failed upload is retried by creating a
new one. When the request fails after the provider created the image, swallow removes the
incomplete image from the provider on a best-effort basis.

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

A `Content-Type` other than `application/octet-stream`, a missing `Content-Length` or one that
differs from `sizeBytes`, a body that ends before `sizeBytes` bytes, or a provider refusal (for
example a name that duplicates an existing provider image, an unsupported `filetype`, or content
that does not match `sha256`) is `400 validation_error` — the provider's own wording is preserved
for a provider-side refusal. An unknown or expired upload is `404 not_found`; an upload whose
content is already being sent is `409 conflict`. A body larger than the installation's upload
limit is `413 payload_too_large`. A provider transport failure or 5xx during the upload, an
Integration with no credential, or an image the provider has not fully staged within the wait is
`503 provider_unavailable`; in the last case the image exists at the provider and deployments of it
are refused until it is staged. Because the artifact can be large, this request is not bounded by
the short provider read timeout used for catalog reads.

## OS Image Metadata

No supported provider exposes an operation to rename an image or relabel its OS and release, and
these values have no external owner, so swallow keeps them as the image's metadata: desired state
that needs no provider realization
(decision 071). It never
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
swallow-local with no provider round trip and do not return `503 provider_unavailable`.

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
default `disk`), optional `network`, and optional write-only `userData`. The stored template
carries the `ephemeral` boolean; `deployTarget` maps onto it (`disk ↔ false`,
`ram ↔ true`). `network.mode` is `automatic`, `dhcp`, or
`static` (glossary Network Configuration); missing network intent defaults to Automatic, on
every subnet. Automatic addressing is realized by provider auto-assign (a stable, provider-recorded
address; see [decision 058](../../../../../docs/decisions/058-deployment-dhcp-and-subnet-dns.md) (deployment dhcp and subnet dns));
DHCP leaves the address, gateway, and DNS to the network's own DHCP server and is only ever the
caller's explicit choice (see decision 058). `dhcp` is no
longer an alias of `automatic`, and a template stored with `dhcp` reads as DHCP.
`subnetId` names a live provider subnet and `defaultGateway` defaults to false. A default
gateway can be requested only for Static intent. It returns `201` and never echoes `userData`.

`PATCH /templates/{id}` accepts any subset of `name`, `description`,
`imageId`, the deploy mode (`deployTarget`), and `network`. It cannot change `integrationId` and
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
they are `400 validation_error`, like any unsupported capability. A provisioner catalog that cannot be read is
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
on for the subnet's VLAN); false means any DHCP there is the site's. It does not change the
suggestion; clients use it to warn about Automatic addressing on that subnet (below).

Configuration states are `dhcp`, `static`, `link_only`, `unconfigured`,
`provider_managed`, or `unknown`. Provider-specific values are diagnostic and
must not be sent back as writable intent. The boot interface is suggested by
default. One explicit Static link on that NIC is preserved as the suggested
deployment mode, subnet, IP address, and default-gateway intent. Multiple Static
links remain ambiguous and require user input. Without an explicit Static
link, swallow uses an existing linked subnet when unambiguous, or the sole compatible
managed subnet, and suggests Automatic. Multiple compatible subnets produce
`network_selection_required` instead of a silent choice and also suggest Automatic.

The suggestion is never `dhcp`, including on a subnet without provisioner DHCP
(`provisionerDhcp` false). There provider auto-assign does not know the site DHCP server's
pool, so Automatic may hand out an address that pool also leases. A client that offers
Automatic for such a subnet shows a visible warning that says so and that `dhcp` lets the
site DHCP server assign the address, gateway, and DNS; the user decides (decision 058).

`issues[]` entries carry `serverId`, `code`, and `message`. Codes are
`network_interface_required` (no boot NIC could be chosen), `network_selection_required`
(no subnet could be chosen), and `integration_mismatch`: targets of different Integrations
are not read, and the response then has an empty `targets` array with one
`integration_mismatch` issue per mismatched target. An empty or duplicate target list, or one
longer than 100, is `400 validation_error`, as is a non-provisioner Integration; an unknown
Server or provider Machine is `404 not_found`; a provisioner without network configuration
is `400 validation_error`; and an unreachable provisioner is `503 provider_unavailable`.

## Deployment Target Preflight

`POST /deployments/preflight` accepts only the intended targets:

```json
{
  "serverIds": ["server-1", "server-2"]
}
```

It performs the target identity, presence, Ready, unlocked, and same-Integration
checks that `POST /deployment-operations` repeats at acceptance. Lock is
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
a list longer than 100, is `400 validation_error`; a Server unknown to swallow is
`404 not_found`.

The request names no deploy target, so this check does not report `inspection_required`,
which blocks only a disk deployment: `POST /deployment-operations` and Platform deployment
acceptance report it once the target is resolved (see
[Multi-Server Deployment](#multi-server-deployment)).

## Multi-Server Deployment

`POST /deployment-operations` ([Durable OS Workflows](#durable-os-workflows)) accepts:

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

`settings.deployTarget` (`"disk"` | `"ram"`) chooses the deploy mode; `"ram"` runs the
OS from memory (what the provider calls ephemeral). An unknown `deployTarget` value is a
`400`. This applies identically, through the platform contract, to platform deploys.

**Deployment Key gate.** swallow logs in to deployed Servers with the installation's Deployment
Key (see [ssh-keys.md](ssh-keys.md)), so every OS deployment — `POST /deployment-operations`,
`POST /image-verifications`, and Platform deploys with `provision_os` — is refused at acceptance
with `409 conflict` when no Deployment Key exists. The message names the installation command
that creates it (`swallow-api deployment-key ensure`). Nothing is written to the provider.

**Custom image verification gate.** When the resolved image is a *custom* image
(`providerOsSystem: "custom"`) that has not been verified for the requested
Deploy Target, the deploy is refused at acceptance with `409` and a swallow
reason ("this custom image is not verified for `<disk|ram>` deployment; verify it
on a ready Server first"). Synced provider images bypass this gate. A known
Server/image architecture mismatch is likewise refused with `409`. An image the
provisioner has not fully staged — for example a custom upload whose boot or kernel
resources are still missing — is also refused at acceptance with `409` and a swallow
reason ("the `<name>` image is not fully staged by the provisioner and cannot be
deployed"), so an incomplete image is never handed to the provider only to fail
opaquely mid-install. These gates are applied by the shared deploy resolve, so they
cover `POST /deployment-operations` and platform deploys. See
"Image Verification".


Without a template, a missing `network`, or a `network` without `mode`, resolves to swallow's
Automatic default on every subnet, including one without provisioner DHCP; swallow never switches a
deployment to `dhcp` by itself (see [Network Inspection](#network-inspection)). With a template, a
missing `network` takes the template's network intent, and a `network` without `mode` keeps the
template's mode while its `subnetId`, `defaultGateway`, and `assignments` apply as sent, so a
request can add the per-target static IPs a Static template cannot hold. The Workflow freezes the
resolved mode, so a later template change does not alter it. `network.mode` accepts
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
same live read refuses a **disk** deployment (`deployTarget` `disk` or omitted) of a Server whose storage layout needs a new inspection (`provisioning.inspectionRequired`,
see [servers-list.md](servers-list.md#provisioning-axis-fields)) with `409 conflict` naming the
Server and its `inspection_required` reason; a RAM deployment of that Server is allowed. The
resolved image must exist in the
current live catalog. swallow inspects every target's live network capability,
NICs, subnets, existing links, and Static address before the first write. A
preflight failure rejects the entire request without writes.

Preflight validation uses `400 validation_error` for malformed input (including an
image missing from the live catalog), `404 not_found` for an unknown Server or template,
`409 conflict` for target state or
Integration mismatch, and `503 provider_unavailable` when the provider cannot
be reached. Secret values never appear in responses or errors.

## Durable OS Workflows

`POST /deployment-operations` accepts the request above, runs the complete side-effect-free
preflight, and persists one Workflow:

```json
{ "operationId": "operation-id" }
```

It creates one `provision-os` MAAS Task per Server, with a maximum of four concurrent
Tasks. HTTP acceptance and the provider's `deployed` state are not swallow completion.
Each Task observes the requested image, refreshes the Server address projection, and
requires the Site's configured SSH port (default 22) to become reachable. A missing
provider address or unreachable SSH endpoint therefore fails the Task and the Workflow;
MAAS's `deployed` value remains available only as provider-owned lifecycle diagnostics.
While the provider remains in `deploying`, swallow checks live BMC power after ten
minutes. A machine that is still powered off becomes `requires_attention` with code
`deployment_power_on_timeout` and stage `deployment_power_on`; swallow does not abort,
power on, or resubmit it automatically. Providers without live power inspection retain
the general two-hour observation timeout.

**Time synchronization.** Every OS provisioning configures time
([decision 065](../../../../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md) (control plane ntp and post provision time sync)):
the Workflow also has one `ansible` Task `configure-ntp-<serverId>` per Server, depending only
on that Server's `provision-<serverId>`. It runs the release playbook `configure-ntp` against
the installation's NTP server (the api-server setting `SWALLOW_API_NTP_SERVER`, which the
production installation sets to `SWALLOW_ADDRESS`), frozen at acceptance as the trusted variable
`swallow_ntp_servers`. The playbook configures the time client the OS already has, in the order
`systemd-timesyncd`, `chrony`, `ntpd`, installs chrony only when there is none, and stops every
other client; the run output names the client used. The Task fails with `ansible_failed` whose run
output carries `[ntp_not_synchronized]` when the client gets no reply within two minutes.
`systemd-timesyncd` still prefers a server a DHCP lease offers; on a subnet MAAS serves that is the
MAAS rack, which is the swallow host too. The Server's `deployment` result still reflects only its
`provision-os` Task. Acceptance fails with `409 conflict` when the installation has no NTP server
(the message names `SWALLOW_API_NTP_SERVER`), and, because these Tasks are Ansible, with
`503 provider_unavailable` while the Site's automation is not runnable (see
[site-automation.md](site-automation.md)). OS image verification does not configure time: it
returns the Server afterwards.

**Local Repository.** An `ansible` Task whose playbook fetches its artifacts from the Site's Local
Repository is accepted only while that repository is ready; otherwise acceptance fails with
`409 conflict` and a message naming what to register or wait for
([local-repository.md](local-repository.md#deployments)).

**Node Exporter.** Every OS deployment also installs the Node Exporter as Managed Software
(kind `node-exporter`, see [software.md](software.md#node-exporter)): after each Server's
`configure-ntp-<serverId>`, the Workflow has an `ansible` Task `install-node-exporter-<serverId>`
that runs the release playbook `deploy-node-exporter`, and an `internal` Task
`record-node-exporter-<serverId>` (kind `record-software-assignment`) that marks the Server's
`node-exporter` Software Assignment `installed`. The assignment is written as `pending` when the
Workflow is accepted, and a failed install marks it `failed`. No Task depends on these two, so a
failed exporter install never holds back the OS, and the Server's `deployment` result still
reflects only `provision-os`; the Workflow ends `requires_attention` until the user retries the
failed Task. A `ram` deployment has the same Tasks. OS image verification does not install the
Node Exporter.

**Boot Media ensure.** For each target Server whose Boot Media is enabled when the Workflow is
accepted (see [server-detail-actions.md](server-detail-actions.md#boot-media) and
[decision 047](../../../../../docs/decisions/047-redfish-boot-media.md) (redfish boot media)), the Workflow also has
an `internal` Task `ensure-boot-media-<serverId>` of kind `ensure-boot-media`, and that Server's
`provision-os` Task depends on it. The Task's parameters carry the URL of that Server's chosen
Boot ISO ([boot-isos.md](boot-isos.md), decision 047)
as accepted (`isoUrl`). When it runs it re-reads the Server's setting — a Server whose Boot
Media was disabled meanwhile is left alone — then reads the BMC and re-applies the ISO mount and
boot direction unconditionally, because a BMC can lose either between deployments. A BMC that
cannot be reached or refuses fails the Task as retryable (`boot_media_ensure_failed`), and the
dependent `provision-os` Task does not run; a Workflow accepted while the Server's Boot Media
named no served Boot ISO (none chosen, the Boot ISO's file missing, or no Boot Media base URL)
fails it as not retryable (`boot_media_not_configured`). Servers without Boot Media get
exactly the Tasks described above. The same Task is added in front of `provision-os` in OS image
verification and in a Platform's `ensure-os` Job. Hardware inspection always has the Task and
resolves the Boot ISO when it runs instead (`resolveLive`, no `isoUrl`); see
[server-enrollment.md](server-enrollment.md).

Such a Server's `provision-os` Task carries the same URL as `bootMediaIsoUrl`, next to its
`request`, and watches the deployment boot, timed from its first `deploying` reading. Two minutes
in, if the BMC no longer holds the ISO (some BMCs drop a fresh mount at the provider's power-on),
the Task mounts it again. Ten minutes in, if the provider's newest event is still the deployment
start (the host never network-booted, for example because its BIOS booted the disk), it
re-applies Boot Media and restarts the host through Redfish so the deployment boot starts from
the ISO; the stage-stall rule below then measures from that restart. The host is restarted at
most once per observation; the watch does not change the Task's status, and a failure is only
logged. A deployment that still makes no progress is reported by the stage-stall rule.

While the provider remains in `deploying`, swallow also reads the provider's machine event
stream on every observation
([decision 046](../../../../../docs/decisions/046-os-deployment-progress-and-stall.md) (os deployment progress and stall)).
The newest event recorded since the current attempt started is projected onto the Server
deployment axis as non-terminal progress — `state` stays `deploying`, `code` stays empty,
`stage` is the event type in snake_case (for example `configuring_os`), and `statusReason` names
it, for example `Provider stage: Configuring OS (since 2026-10-02T16:48:45Z).` — and is rewritten
only when the provider advances. When no newer event arrives for 25 minutes, the Task becomes
`requires_attention` with code `deployment_provider_stage_stall`, stage set to the stuck event's
snake_case type (or `deployment_progress` when no event was recorded since the attempt started),
and a message naming the stage and since when. Progress is measured from provider event
timestamps, so a worker restart or activity retry does not grant a fresh window; an explicit Task
Retry starts a new attempt and therefore a new window. swallow does not abort the provider
deployment. A provider without an event stream, or an event read that fails, never produces a
stall; the power-on check and the two-hour observation timeout stay authoritative.

A Machine that the provider reserved (`allocated`) after the deployment was accepted but that
never starts installing within the power-on window (10 minutes) makes the Task
`requires_attention` with code `deployment_reserved_not_started` (stage `deployment`). The Machine
is left `allocated`, which Recover and Release accept, so the user can return it to Ready
before retrying.

Retry observes before writing. If the expected image is now SSH-reachable, the Task
succeeds without repeating provider work. If MAAS installed the image but reports no
address, explicit Retry releases that unusable installation, waits for Ready, reapplies
the frozen automatic/static intent, and redeploys the same image and cloud-init. If MAAS
reports the Machine `failed`, `allocated`, `broken`, or `rescue`, explicit Retry first
returns it to Ready with the same state-driven plan as Recover (static IP bindings are
kept), and then redeploys the same frozen intent; failures on that path carry a stage
prefixed `deployment_recovery_`. When the frozen intent is a disk deployment and the
Server's layout needs a new inspection, Retry does neither: the Task fails before Recover
with code `inspection_required` (stage `deployment_preflight`, not retryable, message is the
Server's `inspectionRequiredReason`), so the Workflow ends and the Server can be inspected. A Machine already deployed with a different image is not
replaced: the Task fails with `deployment_image_mismatch` (not retryable) so the Workflow
ends and the Server can be released explicitly. This destructive recovery is never
automatic: a first attempt, or a Temporal activity retry of it after worker turnover,
reports a provider failure as `deployment_failed` instead. If an address exists but SSH
remains unreachable, Retry only observes so routing, firewall, image, or service
remediation does not discard an installed OS.

A provider-reported deployment failure (`failed` or `broken` while observing) fails the
Task with `deployment_failed` (stage `deployment`, retryable). Its message names the cause
when the provisioner can explain it:
`The provisioner reported that OS deployment failed: <provider error description>. Installation error: <line>`.
The description is the Server's `provisioning.errorDescription`; the installation error is the
most specific line of the provider's newest failed installation log that started after the
Task attempt started (for MAAS: the curtin handler error, else the exception that ended its
traceback, else the first failed stage), at most 300 characters, with secrets masked as in Task
errors ([workflows.md](workflows.md#task-errors-and-diagnostics)) and, for MAAS, its API key and
OAuth consumer key and signature too; the rest of the line is kept. Each part is omitted when
unavailable, down to `The provisioner reported that OS deployment failed.`; reading the
installation log never changes the Task outcome. The Task's diagnostic carries the same
explanation as its cause and the provider events recorded since the attempt started.

While a deployment, Release, or Recover is observed, a read the provisioner rejects for its
credential makes the Task `requires_attention` with `provider_auth` at once, and a missing
Integration or machine with `provider_observation_failed`; any other failed read is tolerated
and makes the Task `requires_attention` with `provider_unavailable` only after 10 minutes of
consecutive failures. A resumed attempt whose read fails keeps reading under the same rule
rather than submitting the deployment again. Lost provider responses enter observation
first; swallow does not immediately submit a second deployment. An unknown outcome becomes
`requires_attention`. A deployment swallow's own preflight refuses before any provider
write (for example a target that is not ready, an image that is not staged or verified, or
a network conflict) fails with `deployment_preflight_rejected` (not retryable), so the
Workflow ends instead of parking on a retry that repeats the refusal. Cloud-init is
removed from the intent snapshot and stored as an encrypted opaque Task reference.

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
live lock validation before persistence, then creates one `release-os` MAAS Task per
Server. A Task succeeds only after Ready is observed. When static cleanup is requested,
the same Task then runs its durable Provisioning Task to completion under the Workflow's
resource lease; Task Retry after a cleanup failure retries cleanup only and never sends
Release again. Provider cancellation is
best-effort and confirmed prior effects are preserved. A provider-reported release failure
(`failed` or `broken`) fails the Task with `release_failed` (stage `release`, retryable); its
message names the provider's error description when there is one
(`The provisioner reported that release failed: <description>.`) and its diagnostic carries the
provider events of the attempt. A static cleanup still unfinished 30 minutes after Ready makes
the Task `requires_attention` with `network_cleanup_timeout` (stage `network_cleanup`).

Every attempt of a `release-os` Task reads the Server first. A Server already `releasing` is
followed to Ready, and one already `ready` meets the Task's goal without a provider Release.
When static cleanup was requested and this Task did not send the Release, nothing captured the
bindings to remove: the Task fails with `release_cleanup_not_applied` (stage `network_cleanup`,
not retryable) and its message says the bindings remain.

`cancelActiveWorkflows` (optional, default `false`) supersedes the unfinished Workflows that
hold any target instead of refusing the request as busy
([decision 061](../../../../../docs/decisions/061-recovery-supersedes-blocking-work.md) (recovery supersedes blocking work)). After
every other validation passes, swallow requests cancellation of each such Workflow (see the
Workflows contract's Cancel) and creates the Workflow at once, so the response returns
within the request. The Workflow waits in `waiting_dependency` ("Waiting for resource leases.")
until those Workflows end and release their leases, then runs; a canceled deployment may have
returned its Server to Ready by then, which the Task accepts as above. A cancel swallow refuses
for a Workflow that is still running fails the request with `409 conflict` naming that Workflow,
and nothing is created.

A Server a Platform claims (its `platformClaim`, see
[servers-list.md](servers-list.md#platform-claim) and
[decision 067](../../../../../docs/decisions/067-platform-claim-on-server.md) (platform claim on server)) is refused:
releasing it would take a node out from under that Platform. The whole batch fails with
`409 conflict` before anything is created or canceled, so `cancelActiveWorkflows` does not get
around it, and the message names the Server and the Platform, for example `Server gpu-node-01
belongs to kubernetes Platform production-k0s; uninstall the Platform (with releaseServers to
also release its Servers) instead of releasing the Server directly`. A Platform's Servers are
released through its uninstall with `releaseServers`
([platforms.md](platforms.md#delete-a-platform)), which is not subject to this rule.

Release is the primary provider-recovery path and is not restricted to `deployed`. The
swallow-owned recovery policy ([decision 033](../../../../../docs/decisions/033-provider-recovery-policy.md) (provider recovery policy), extended by [decision 036](../../../../../docs/decisions/036-provisioning-lifecycle-integrity.md) (provisioning lifecycle integrity)) allows Release from
`deployed`, `allocated`, `failed`, `broken`, and `rescue`, always converging the Machine to
`ready`. `allocated` is included because MAAS parks a machine there when a deployment was
reserved but never finished, and the provider allows Release from it. A target in any other
state (for example `releasing`, `deploying`, `new`, or already `ready`) is rejected with a
swallow-authored reason rather than the provider's message.

Both endpoints return `202` with a Workflow reference, `{ "operationId": "..." }`, and
nothing else; the request ID is recorded on the Workflow as its `requestCorrelation`, read
through the Workflow, not returned here.
Progress, target-specific normalized errors, Cancel, and safe Retry are read and controlled
through the [Workflows](workflows.md) contract.

Acceptance-time rejection is reported with the shared error envelope, never as an opaque
`500 internal_error`: malformed input (missing or out-of-range `serverIds`, a target in a
state Release is not allowed from, a duplicate target, or a cross-Site batch) is
`400 validation_error`; a Server unknown to swallow is `404 not_found`; a target already
inside an unfinished Workflow, a locked target, or a target a Platform claims is
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
validation, then creates one `recover-server` MAAS Task per Server. `cancelActiveWorkflows`
behaves as it does for Release. Recover is allowed
from `failed`, `broken`, `rescue`, `deployed`, and `allocated`; a target already `ready` is
accepted as an immediate success (no-op) and any other state is a `400 validation_error`
with a swallow reason. Each Task chooses the provider primitive by observed state — `broken`
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

When a `release-os` or `recover-server` Task succeeds, it clears the Server's `deployment`
result and marks every Software Assignment of the Server `absent` (see [software.md](software.md)),
because the software went with the OS. A machine whose OS is removed outside swallow gets the same
treatment from reconciliation once it is observed `ready` or `allocated`; a Server in `rescue` or
`broken` keeps its assignments, because its OS is still on disk.

## Image Verification

`POST /image-verifications` launches a durable `verify-os-image` Workflow that
proves one custom OS Image works for one Deploy Target by running a real deploy on
a user-chosen ready Server, recording the swallow-owned verification on
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
to the ready pool after the verification (see the return Task below); when `true`
the return Task is omitted, leaving the Server deployed on success (for a user
who wants to keep the verified deployment) and in its failed state on failure.
Acceptance preflight requires the Server to exist, be `ready`, be unlocked, belong
to the image's Integration, and match the image architecture. It responds `202`
with `{ "operationId": "..." }`. A missing field, an unknown `deployTarget`, a Server that
is absent, not `ready`, of another Integration, or of another architecture, and an image not
in the provisioner's live catalog are `400 validation_error`; an unknown Server or Integration
is `404 not_found`; a locked or busy Server, an image the provisioner has not fully staged, or a
missing Deployment Key is `409 conflict`.

The Workflow runs Tasks: `provision-os` (the real deploy in the target mode; this
proving deploy is exempt from the custom-image verification gate), one of two
mutually exclusive internal finalize Tasks — `record-image-verification` when the
proving deploy succeeded, or `record-image-verification-failure` when it failed —
and, unless `keepServer` is `true`, `recover-server` (return the borrowed Server to
`ready`). Verification is a borrow-and-return contract: by default the return Task
runs whether the proving deploy succeeded or failed, so the borrowed Server is
always given back; with `keepServer: true` the return Task is omitted and the Server
is left deployed, without the Node Exporter an OS deployment installs. A succeeded return Task
marks the Server's Software Assignments `absent`, like Recover. Exactly one record Task runs; the other is skipped. A failed proving deploy is terminal (not a
retry-park), so the Workflow reaches `partially_succeeded` after the Server is
returned, rather than holding the Server and showing a perpetual "verifying". On
success the image's `verifiedDeployTargets` gains the target; on failure its
`failedDeployTargets` gains the target and the provider's reason is on the failed
`provision-os` Task. See
[decision 035](../../../../../docs/decisions/035-os-image-verification-and-deploy-target.md) (os image verification and deploy target)
and [decision 036](../../../../../docs/decisions/036-provisioning-lifecycle-integrity.md).

## Provisioning Tasks

`GET /tasks/{id}` and `GET /servers/{serverId}/provisioning-tasks` expose
swallow-owned provider coordination without returning the saved static-link
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
`retryable` is true only for a failed task after its Release was accepted, on behalf of a
release Workflow (`requestId`).
`error` and `requestId` are omitted when empty.
`POST /tasks/{id}/retry` accepts only that retryable state and retries the `release-os` Task of
the Workflow that owns the cleanup, exactly as `POST /workflows/{id}/tasks/{taskId}/retry`
would; the cleanup runs again without repeating Release. It returns `202` with the task as
stored, which changes once that Task runs. A retry is rejected with `409 conflict` while the
Server is locked, and with `503 provider_unavailable` when its live lock state cannot be read;
the cleanup also reads lock state again immediately before unlinking, so an external lock makes
the task fail retryably without changing network links. A task retained after the provider
refused Release remains diagnostic and is not retryable. Unknown tasks, and a retry of a task
whose Server no longer exists, are `404 not_found`; a task that is not retryable, or whose
release Task cannot be retried (its Workflow is gone or the Task is not failed and retryable),
is `409 conflict`.

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
they are could merge two Servers; they need a user, which is why the report is returned
rather than a bare acknowledgement. `error` is `null` for a completed pass and otherwise says
why the pass stopped (the provisioner could not be read, for example); a failed pass still
answers `200`, and the same message is recorded as the Integration's last sync error. An
unknown Integration is `404 not_found` and a non-provisioner Integration `400 validation_error`.

## Compatibility Notes

On 2026-11-05 the installation error in a `deployment_failed` message began masking secrets
instead of skipping every line that mentions a password, token, or secret: such a line can now be
the one shown, with its secret values reading `[REDACTED]`, and URL userinfo reads `[REDACTED]@`
instead of being removed. The same day observation stopped ignoring failed provisioner reads
(`provider_auth`, `provider_observation_failed`, `provider_unavailable`), `release_failed` began
naming the provider's error description, and a static cleanup gained its 30-minute
`network_cleanup_timeout`. Failed and attention Tasks carry a `diagnostic` (the Workflows
contract). The request and response shapes are unchanged.

On 2026-11-05 a deployment with a template whose `network` has no `mode` began keeping the
template's mode, as [Multi-Server Deployment](#multi-server-deployment) says. It used to resolve to
Automatic whatever the template's mode, so a DHCP or Static template was deployed as Automatic, and
per-target static IPs sent for a Static template were refused as valid only for Static. This is a
bug fix: a request that names `mode`, or sends no `network`, is unaffected, and the request and
response shapes are unchanged.

On 2026-11-05 network inspection stopped suggesting `dhcp` for a subnet without provisioner
DHCP (`provisionerDhcp` false): `suggestion.mode` is now `static` for an explicit Static
binding on the boot NIC and `automatic` otherwise, and clients warn about the site DHCP pool
instead of preselecting DHCP (decision 058). Request and response shapes are unchanged, and
`suggestion.mode` keeps its `automatic | dhcp | static` enum; a client that adopted the
suggestion now starts such a deployment on Automatic.

On 2026-11-05 (breaking, with no alias) the single multipart `POST /images` upload was replaced
by `POST /image-uploads`, which takes the same fields as JSON plus the content's `sizeBytes` and
`sha256`, followed by `PUT /image-uploads/{id}/content` with the raw content. The created image is
returned in the same shape, and only once the provider reports it fully staged.

On 2026-11-03 a `deploy-os` Workflow gained the `install-node-exporter-<serverId>` and
`record-node-exporter-<serverId>` Tasks after each Server's `configure-ntp-<serverId>`, replacing
the separate exporter Workflow that used to follow a deployment, and a succeeded `release-os` or
`recover-server` Task began marking the Server's Software Assignments `absent` itself. A Server
in `rescue` or `broken` now keeps its assignments. The request and response shapes are unchanged.

On 2026-10-31 the OS Image overlay became OS Image metadata
(decision 071). Breaking,
with no alias: a catalog image is `{id, architecture, spec, status}` — the `custom*` fields and
`tags` became `spec`, and the effective and `provider*` values, `sizeBytes`, and the verification
arrays moved under `status` — and `PATCH /images/overlay` / `DELETE /images/overlay` became
`PUT /images/metadata` / `DELETE /images/metadata` with the same body and responses.

On 2026-10-31 ([decision 074](../../../../../docs/decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases),
breaking) the deprecated synchronous `POST /provisioning/deployments`, `POST /servers/{id}/deploy`,
and `POST /servers/{id}/release` were removed; deploy and release through `POST
/deployment-operations` and `POST /release-operations`. The removed single-Server deploy was the
only deployment that did not clear the Server Default User. The request `ephemeral` alias
of templates and deployment settings is no longer read; send `deployTarget`.

On 2026-10-19 `POST /release-operations` and `POST /recover-operations` gained the optional
`cancelActiveWorkflows` field, and a first `release-os` attempt began observing a Server that is
already Ready or Releasing instead of sending another Release; clients that omit the field are
unaffected (decision 061).

On 2026-10-19 the deployment preflight began refusing a disk deployment of a Server whose
layout needs a new inspection (`inspection_required`), and `deployment_failed` messages began
carrying the provider's error description and installation error line. Both keep their status
codes and error codes; clients that branch on codes are unaffected
([decision 047 amendment](../../../../../docs/decisions/047-redfish-boot-media.md)).

On 2026-10-29 `POST /release-operations`, the deprecated `POST /servers/{id}/release`, and
`POST /recover-operations` for a `deployed` target began refusing a Server a Platform claims
with `409 conflict` (see Durable OS Workflows). Before, such a request was accepted and took
the node out from under its Platform. Release a Platform's Servers through its uninstall
([decision 067](../../../../../docs/decisions/067-platform-claim-on-server.md)).

On 2026-10-31 (breaking) a provisioner without network configuration became
`400 validation_error` for templates, network inspection, and OS deployments (also inside a
deployment batch), like every other unsupported capability; it was `409 conflict`. A target not in
the provisioning state an OS deployment requires became `409 conflict`; it was `400`.
