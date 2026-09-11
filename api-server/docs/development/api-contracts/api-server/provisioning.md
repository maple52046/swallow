# OS Provisioning

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

List provider-owned OS Images, manage Swallow-owned Deployment Templates, and
submit one OS deployment configuration to one or more eligible Servers.

## Related Glossary Terms

- OS Image
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
DELETE /api/v1/provisioning/images?integrationId={integrationId}&imageId={imageId}&architecture={architecture}
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
POST   /api/v1/provisioning/networks/inspect
GET    /api/v1/provisioning/tasks/{id}
POST   /api/v1/provisioning/tasks/{id}/retry
```

The existing `POST /api/v1/servers/{id}/deploy` contract remains active and
unchanged for backward compatibility.

## OS Image Catalog

`GET /images` requires `integrationId` and reads the named provisioner live.
It returns:

```json
[
  {
    "id": "ubuntu/jammy",
    "name": "Ubuntu 22.04 LTS",
    "osSystem": "ubuntu",
    "release": "jammy",
    "architecture": "amd64"
  }
]
```

The catalog contains only resources the provisioner accepts for OS deployment.
For MAAS this includes synced operating systems and uploaded custom images, but
excludes PXE and bootloader artifacts returned by the same boot-resources API.
An uploaded image keeps its provider resource name as `id` and is returned with
`osSystem: "custom"`.

OS Images are provider-owned and are not persisted by Swallow. Missing
`integrationId` is `400 validation_error`; an unknown integration is
`404 not_found`; a provider failure is `503 provider_unavailable`.

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
transport failure is `503 provider_unavailable`. Rename is intentionally absent: no
supported provider exposes an image-rename operation.

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
`description`, required `imageId`, optional `ephemeral`, optional `network`,
and optional write-only `userData`. `network.mode` is `automatic` or `static`;
missing network intent defaults to Automatic. `dhcp` is accepted as a deprecated
one-release alias of `automatic` and is normalized on write. Automatic addressing is
realized by provider auto-assign (a stable, provider-recorded address), not raw DHCP
(see [ADR 018](../../../../../docs/decisions/018-automatic-addressing-provider-auto-assign.md)).
`subnetId` names a live provider subnet and `defaultGateway` defaults to false. A default
gateway can be requested only for Static intent. It returns `201` and never echoes `userData`.

`PATCH /templates/{id}` accepts any subset of `name`, `description`,
`imageId`, `ephemeral`, and `network`. It cannot change `integrationId` and
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
    "ephemeral": false
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
`settings.imageId` is required, `ephemeral` defaults to false, and user data
defaults to `omit`. With a template, omitted settings use the template and
omitted user data defaults to `inherit`.


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

It performs complete presence, deployed-state, Site, duplicate-target, active-work, and
live lock validation before persistence, then creates one `release-os` MAAS Step per
Server. A Step succeeds only after Ready is observed. When static cleanup is requested,
the same Step also waits for its durable Provisioning Task; Step Retry after a cleanup
failure retries cleanup only and never sends Release again. Provider cancellation is
best-effort and confirmed prior effects are preserved.

Both endpoints return `202` with an Operation reference and preserve the request ID as
`requestCorrelation`. Progress, target-specific normalized errors, Cancel, and safe Retry
are read and controlled through the [Operations](operations.md) contract.

Acceptance-time rejection is reported with the shared error envelope, never as an opaque
`500 internal_error`: malformed input (missing or out-of-range `serverIds`, an
undeployed target, a duplicate target, or a cross-Site batch) is `400 validation_error`;
a Server unknown to Swallow is `404 not_found`; a target already inside an unfinished
Operation or a locked target is `409 conflict`; and a build without durable provisioning
wired up is `503 provider_unavailable`.

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
