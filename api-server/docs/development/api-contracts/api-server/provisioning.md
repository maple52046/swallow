# OS Provisioning

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

List, upload, and delete provider-owned OS Images, manage Swallow-owned Deployment
Templates, and submit one OS deployment configuration to one or more eligible Servers.

## Related Glossary Terms

- OS Image
- Provider Data Overlay
- OS Deployment
- Deployment Template
- Server
- Server Status
- Network Configuration
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
PATCH  /api/v1/provisioning/images/overlay?integrationId={integrationId}&imageId={imageId}&architecture={architecture}
DELETE /api/v1/provisioning/images/overlay?integrationId={integrationId}&imageId={imageId}&architecture={architecture}
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
```

The existing `POST /api/v1/servers/{id}/deploy` contract remains active and
unchanged for backward compatibility.

## OS Image Catalog

`GET /images` requires `integrationId` and reads the named provisioner live,
then merges the Swallow-owned overlay (see "OS Image Overlay" below) onto it. It
returns:

```json
[
  {
    "id": "ubuntu/jammy",
    "name": "Golden Ubuntu",
    "providerName": "Ubuntu 22.04 LTS",
    "customName": "Golden Ubuntu",
    "osSystem": "Ubuntu LTS",
    "providerOsSystem": "ubuntu",
    "customOsSystem": "Ubuntu LTS",
    "release": "22.04",
    "providerRelease": "jammy",
    "customRelease": "22.04",
    "tags": ["gpu", "ml"],
    "architecture": "amd64",
    "sizeBytes": 5368709120,
    "verifiedDeployTargets": ["disk", "ram"],
    "failedDeployTargets": []
  }
]
```

`name`, `osSystem`, and `release` are the effective values a client displays: the
Swallow overlay value when one is set, otherwise the provider value. The
`provider*` fields (`providerName`, `providerOsSystem`, `providerRelease`) always
carry the provider's own values. The `custom*` fields (`customName`,
`customOsSystem`, `customRelease`) carry the Swallow overrides and are each omitted
when that field has no override, in which case the effective value equals the
provider value. `id` and `architecture` are never overridable — they identify the
deployable artifact. `tags` is a Swallow-owned list of labels with no provider
counterpart, always returned as an array (empty when the image has no tags).

`sizeBytes`, when present, is a positive integer containing the provider-reported
total bytes of the newest complete resource set that backs the image. It is omitted
when the provider does not expose a complete size or its detail is temporarily
unavailable. Providers may expose multiple kernel or subarchitecture variants that
collapse into the same `id` + `architecture` row; in that case the catalog returns
the largest current complete-set size rather than summing mutually exclusive
variants. Size is provider-owned live metadata and cannot be changed by the overlay.

`verifiedDeployTargets` is a Swallow-owned array of the deploy targets (`"disk"`
and/or `"ram"`) a Swallow verification has proven this image can deploy in. It is
always present, defaulting to an empty array, so a client can render a per-mode
"verified" indicator without a null check. An empty array means the image has not
been verified for any target. For a custom image (`providerOsSystem: "custom"`) a
Deploy Target absent from this array is blocked for a normal deploy (see "Custom
image verification gate"); synced provider images are provider-trusted and are
never gated regardless of this array. It is keyed by the same
`integrationId` + `imageId` + `architecture` identity as the overlay but stored
separately, so clearing an overlay never clears verification.

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

The image artifact is provider-owned and is not persisted by Swallow; only the
overlay is Swallow-owned (see ADR 025). Missing `integrationId` is
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
transport failure is `503 provider_unavailable`. Deleting an image also removes any
Swallow overlay for it, so a deleted image leaves no orphan override behind.

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
- `content` (required): the image file. It is streamed; Swallow never buffers the whole artifact
  in memory and keeps no copy after the provider accepts it.

On success it returns `201` with the created image in the same shape as one `GET /images` row:

```json
{
  "id": "custom/ubuntu-24.04-rocm",
  "name": "Ubuntu 24.04 ROCm",
  "providerName": "Ubuntu 24.04 ROCm",
  "osSystem": "custom",
  "providerOsSystem": "custom",
  "release": "ubuntu-24.04-rocm",
  "providerRelease": "ubuntu-24.04-rocm",
  "tags": [],
  "architecture": "amd64",
  "sizeBytes": 5368709120
}
```

`providerOsSystem` reflects the provider's classification of the uploaded artifact (for MAAS,
`custom`); the caller never sends it. A freshly uploaded image has no Swallow overlay, so its
`custom*` fields are absent and each effective field equals its provider value. `sizeBytes` is
present when the provider reports the completed artifact size.

A missing `integrationId`, `name`, `architecture`, or `content`, an unsupported `filetype`, a
name that duplicates an existing provider image, or a provisioner whose adapter does not
implement upload is `400 validation_error` — the provider's own wording is preserved for a
provider-side refusal. An unknown integration is `404 not_found`. A provider transport failure
or 5xx during upload is `503 provider_unavailable`. Because the artifact can be large, this
endpoint is not bounded by the short provider read timeout used for catalog reads.

## OS Image Overlay

No supported provider exposes an operation to rename an image or relabel its OS and
release, and these display strings have no external owner, so Swallow owns an overlay
merged onto the provider catalog at read as a Provider Data Overlay (see ADR 025). The
overlay is Swallow-owned data: it never changes the provider or the deployable image
identity, and it is keyed by the same `integrationId` + `imageId` + `architecture`
identity the catalog returns, passed as query parameters because an `imageId` can contain
a slash and one image name can back several architectures.

`PATCH /images/overlay` sets the overlay. The body carries the overridable display fields
and the Swallow-owned tag list, each optional:

```json
{ "name": "Golden Ubuntu", "osSystem": "Ubuntu LTS", "release": "22.04", "tags": ["gpu", "ml"] }
```

Each override field is trimmed before storage. A non-empty field becomes the effective value
on subsequent `GET /images` responses while the corresponding `provider*` field continues to
show the provider value; an empty or omitted field clears that override so the image shows its
provider value. `tags` are trimmed, blanks dropped, and duplicates removed while preserving
order; an empty or omitted list clears them. When no override field and no tag remains after
normalization, the overlay is removed entirely, so an all-empty `PATCH` reverts the image to
its provider values (the same effect as `DELETE`). It returns `204 No Content` on success. A
field or tag longer than 200 characters, or more than 50 tags, is `400 validation_error`. Any
missing query parameter or an invalid body is also `400 validation_error`.

`DELETE /images/overlay` removes the overlay, reverting every field to its provider value.
It uses the same query-parameter identity and returns `204 No Content` even when no overlay
existed, because the requested end state already holds. Any missing query parameter is
`400 validation_error`.

Setting or clearing an overlay does not validate the image against the live provider
catalog: an overlay for an image that later disappears is simply not merged, so these
endpoints stay Swallow-local with no provider round trip and do not return
`503 provider_unavailable`.

A successful `PATCH` or `DELETE` propagates the new effective name to the Server
projections immediately: the effective display name mirrored onto each deployed Server
of that integration (the `provisioning.deployedImageName` field on the Servers List and
Server detail) is re-resolved and updated for any server whose deployed image is the one
that was renamed, and each updated server emits a change on the Servers event stream. So
a rename is reflected on the fleet list and Server detail without waiting for the next
reconcile pass. This propagation is best-effort and does not affect the endpoint result:
it never changes the `204 No Content` outcome or the provider, and if it cannot run the
periodic reconcile still re-mirrors the name.

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
`ram ↔ true`) and wins when both are sent. `network.mode` is `automatic` or `static`;
missing network intent defaults to Automatic. `dhcp` is accepted as a deprecated
one-release alias of `automatic` and is normalized on write. Automatic addressing is
realized by provider auto-assign (a stable, provider-recorded address), not raw DHCP
(see [ADR 018](../../../../../docs/decisions/018-automatic-addressing-provider-auto-assign.md)).
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
List filters are conjunctive. An unknown Site, Integration, or template is
`404 not_found`; a non-provisioner Integration or invalid/missing field is
`400 validation_error`; duplicate names and deletion of an Integration still
referenced by a template are `409 conflict`.

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
              "managed": true
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

Configuration states are `dhcp`, `static`, `link_only`, `unconfigured`,
`provider_managed`, or `unknown`. Provider-specific values are diagnostic and
must not be sent back as writable intent. The boot interface is suggested by
default. One explicit Static link on that NIC is preserved as the suggested
deployment mode, subnet, IP address, and default-gateway intent. Multiple Static
links remain ambiguous and require operator input. Without an explicit Static
link, Swallow suggests Automatic and uses an existing linked subnet when unambiguous,
or the sole compatible managed subnet. Multiple compatible subnets produce
`network_selection_required` instead of a silent choice.

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

Issue codes are `integration_mismatch`, `absent`, `not_ready`, or `locked`. An
empty or duplicate target list, or a list longer than 100, is
`400 validation_error`; a Server unknown to Swallow is `404 not_found`.

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
`automatic` or `static` (the deprecated `dhcp` alias still maps to `automatic`);
keep-current is not valid intent. Automatic is realized by the provider's auto-assign
capability, so the deployed address is stable and always provider-recorded. Each
target resolves its boot NIC by default and can override `interfaceId` and
`subnetId`. Static requires one valid, unique per-target `ipAddress`.
`defaultGateway` is valid only for Static. Templates may supply mode, subnet,
and gateway intent but never a NIC ID or static IP.
`userData.mode` is one of:

- `inherit`: use the template's sealed user data; valid only with a template.
- `replace`: use the non-empty write-only `value` for this request only.
- `omit`: send no user data for this request.

Before any provider write, every Server must exist, be present, have provisioning
state `ready`, be unlocked, belong to the same Integration, and match the
template Integration when one is used. The lock preflight reads each target live from the provider before dispatch. The
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

Preflight validation uses `400 validation_error` for malformed input,
`failed[].stage` is `network_configuration` or `deployment`.
`404 not_found` for missing resources, `409 conflict` for target state or
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

Retry observes before writing. If the expected image is now SSH-reachable, the Step
succeeds without repeating provider work. If MAAS installed the image but reports no
address, explicit Retry releases that unusable installation, waits for Ready, reapplies
the frozen automatic/static intent, and redeploys the same image and cloud-init. This
destructive recovery is never automatic. If an address exists but SSH remains
unreachable, Retry only observes so routing, firewall, image, or service remediation does
not discard an installed OS. Lost provider responses enter observation first; Swallow
does not immediately submit a second deployment. An unknown outcome becomes
`requires_attention`. Cloud-init is removed from the intent snapshot and stored as an
encrypted opaque Step reference.

`POST /release-operations` accepts 1-100 Servers:

```json
{
  "serverIds": ["server-1"],
  "erase": false,
  "secureErase": false,
  "quickErase": false,
  "comment": "Return machines to the ready pool",
  "unbindStaticIPs": true
}
```

It performs complete presence, recoverable-state, Site, duplicate-target, active-work, and
live lock validation before persistence, then creates one `release-os` MAAS Step per
Server. A Step succeeds only after Ready is observed. When static cleanup is requested,
the same Step also waits for its durable Provisioning Task; Step Retry after a cleanup
failure retries cleanup only and never sends Release again. Provider cancellation is
best-effort and confirmed prior effects are preserved.

Release is the primary provider-recovery path and is not restricted to `deployed`. The
Swallow-owned recovery policy (decision 033, extended by decision 036) allows Release from
`deployed`, `allocated`, `failed`, `broken`, and `rescue`, always converging the Machine to
`ready`. `allocated` is included because MAAS parks a machine there when a deployment was
reserved but never finished, and the provider allows Release from it. A target in any other
state (for example `releasing`, `deploying`, `new`, or already `ready`) is rejected with a
Swallow-authored reason rather than the provider's message.

Both endpoints return `202` with an Operation reference and preserve the request ID as
`requestCorrelation`. Progress, target-specific normalized errors, Cancel, and safe Retry
are read and controlled through the [Operations](operations.md) contract.

Acceptance-time rejection is reported with the shared error envelope, never as an opaque
`500 internal_error`: malformed input (missing or out-of-range `serverIds`, a target in a
state Release is not allowed from, a duplicate target, or a cross-Site batch) is
`400 validation_error`; a Server unknown to Swallow is `404 not_found`; a target already
inside an unfinished Operation or a locked target is `409 conflict`; and a build without
durable provisioning wired up is `503 provider_unavailable`.

`POST /recover-operations` returns a Server to `ready` from a not-usable state and accepts
the same bounded batch shape as Release, without the disk-erase controls:

```json
{
  "serverIds": ["server-1"],
  "comment": "Return failed nodes to the ready pool",
  "unbindStaticIPs": true
}
```

It performs the same presence, Site, duplicate-target, active-work, and live lock
validation, then creates one `recover-server` MAAS Step per Server. Recover is allowed
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
with `{ "operationId": "..." }`.

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
`POST /tasks/{id}/retry` accepts only that retryable state, returns `202`, and
requeues cleanup without repeating Release. A retry is rejected while the
Server is locked; the worker also reads lock state again immediately before cleanup, so an external lock makes the task
fail retryably without changing network links. A task retained after the provider
refused Release remains diagnostic and is not retryable. Unknown tasks are
`404 not_found`; a task that is not retryable is `409 conflict`.

## Compatibility Notes

`POST /provisioning/deployments`, `POST /servers/{id}/deploy`, and
`POST /servers/{id}/release` preserve their existing wire behavior for one release and
return `Deprecation: true` plus a successor `Link` header. Dashboard command flows use the
durable Operation endpoints. Existing Server projections, authorization, and provider
action behavior are unchanged.
