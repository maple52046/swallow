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

## Authentication and Authorization

Every endpoint requires an admin bearer token. Shared error envelopes, codes,
timestamps, and authentication behavior follow [conventions.md](conventions.md).

## Endpoints

```text
GET    /api/v1/provisioning/images?integrationId={integrationId}
GET    /api/v1/provisioning/templates?siteId={optional}&integrationId={optional}
POST   /api/v1/provisioning/templates
GET    /api/v1/provisioning/templates/{id}
PATCH  /api/v1/provisioning/templates/{id}
DELETE /api/v1/provisioning/templates/{id}
PUT    /api/v1/provisioning/templates/{id}/user-data
DELETE /api/v1/provisioning/templates/{id}/user-data
POST   /api/v1/provisioning/deployments/preflight
POST   /api/v1/provisioning/deployments
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

OS Images are provider-owned and are not persisted by Swallow. Missing
`integrationId` is `400 validation_error`; an unknown integration is
`404 not_found`; a provider failure is `503 provider_unavailable`.

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
  "hasUserData": true,
  "createdAt": "2026-08-28T10:00:00Z",
  "updatedAt": "2026-08-28T10:00:00Z"
}
```

`POST /templates` accepts `integrationId`, required `name`, optional
`description`, required `imageId`, optional `ephemeral`, and optional
write-only `userData`. It returns `201` and never echoes `userData`.

`PATCH /templates/{id}` accepts any subset of `name`, `description`,
`imageId`, and `ephemeral`. It cannot change `integrationId` and never
accepts `userData`. Image creation or replacement validates the live provider
catalog before persistence.

`PUT /templates/{id}/user-data` requires a non-empty `userData` value and
returns `204 No Content`. `DELETE` clears the sealed value and also returns
`204 No Content`. Callers read template metadata again when they need the
updated `hasUserData` flag; neither mutation echoes secret content.

Template names are trimmed and case-insensitively unique within one Integration.
List filters are conjunctive. An unknown Site, Integration, or template is
`404 not_found`; a non-provisioner Integration or invalid/missing field is
`400 validation_error`; duplicate names and deletion of an Integration still
referenced by a template are `409 conflict`.

## Deployment Target Preflight

`POST /deployments/preflight` accepts only the intended targets:

```json
{
  "serverIds": ["server-1", "server-2"]
}
```

It performs the same local target checks and provider-owned deployment-readiness
checks that `POST /deployments` repeats immediately before dispatch. The operation
is read-only: it does not reserve a Server, choose or create provider network
configuration, validate an image, or start a deployment.

A completed check returns `200`, including when one or more targets are not ready:

```json
{
  "valid": false,
  "integrationId": "integration-id",
  "issues": [
    {
      "serverId": "server-1",
      "code": "provider_not_ready",
      "message": "No MAAS interface is linked to a subnet. Configure the machine's Network in MAAS, then check deployment readiness again."
    }
  ]
}
```

Issue codes are `integration_mismatch`, `absent`, `not_ready`,
`provider_machine_missing`, or `provider_not_ready`. An empty or duplicate target
list, or a list longer than 100, is `400 validation_error`; a Server unknown to
Swallow is `404 not_found`; inability to inspect the provisioner is
`503 provider_unavailable`. Provider-specific remediation remains in `message`
because the provider owns the prerequisite.

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
  }
}
```

`serverIds` must contain 1-100 unique values. Without `templateId`,
`settings.imageId` is required, `ephemeral` defaults to false, and user data
defaults to `omit`. With a template, omitted settings use the template and
omitted user data defaults to `inherit`.

`userData.mode` is one of:

- `inherit`: use the template's sealed user data; valid only with a template.
- `replace`: use the non-empty write-only `value` for this request only.
- `omit`: send no user data for this request.

Before any provider write, every Server must exist, be present, have provisioning
state `ready`, belong to the same Integration, and match the template
Integration when one is used. The resolved image must exist in the current live
catalog. Provider-owned target readiness is repeated even when a caller already
used `/deployments/preflight`, preventing a stale browser result from bypassing a
changed provider state. A preflight failure rejects the entire request without

After preflight, writes run with at most four concurrent provider calls. Individual
provider refusals do not roll back accepted deployments. Every dispatched batch
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
      "message": "Provider rejected deployment."
    }
  ]
}
```

The response is an acceptance report, not a durable job. Deployment progress is
read from each Server provisioning axis. Batch deployment provides no rollback.

Preflight validation uses `400 validation_error` for malformed input,
`404 not_found` for missing resources, `409 conflict` for target state or
Integration mismatch, and `503 provider_unavailable` when the provider cannot
be reached before dispatch. Secret values never appear in responses or errors.

## Compatibility Notes

All endpoints are additive under `/api/v1`. Existing Server projections,
single-Server deployment, authorization, and provider action behavior are
unchanged.
