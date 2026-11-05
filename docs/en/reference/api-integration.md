# API integration

[繁體中文](../../zh-TW/reference/api-integration.md) · [Documentation home](../README.md)

The `api-server` component owns swallow's HTTP API. External clients must use
the active provider-owned contracts and must not infer fields from private Go
types or Dashboard traffic.

## Base URL and authentication

Routes are served under `/api/v1`. Login exchanges a username and password for
an opaque bearer token:

```bash
API=https://swallow.example/api/v1
TOKEN=$(curl --fail --silent \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"REDACTED"}' \
  "$API/auth/login" | jq -r .accessToken)

curl --fail --silent \
  -H "Authorization: Bearer $TOKEN" \
  "$API/auth/me"
```

Treat the JWT as opaque. Read identity and role from `/auth/me`, and
reauthenticate after `401`.

Machine endpoints such as Prometheus discovery may accept the configured
machine bearer token. That token is not an alternative login for user
endpoints.

## Errors

Errors use one envelope:

```json
{
  "error": {
    "code": "validation_error",
    "message": "human-readable detail",
    "requestId": "opaque-correlation-id"
  }
}
```

Branch on `code`, never parse `message`, and preserve `requestId` for logs.
The shared contract maps codes to HTTP statuses.

## Pagination and timestamps

Large list endpoints accept `page` and `pageSize` and return `items`,
`total`, `page`, and `pageSize`. Timestamps are ISO 8601 UTC. Individual
contracts state when a bounded collection returns a plain array instead.

## Identity and staleness

- Use opaque resource IDs. Do not key integrations on hostname or address.
- Treat unknown enum values conservatively.
- Preserve `null` as unknown.
- Read sync and observation timestamps before making a decision from cached
  provider data.
- Credentials are write-only; use `hasCredential` (and, on a Kubernetes
  Platform's Integration, `hasOperatorCredential`) rather than expecting a
  redacted value.

## OS provisioning compatibility

Branch only on the swallow-defined
[`provisioning.state`](../../development/glossaries/terms/os-provisioning-state.md).
Never branch on `providerState`: it is a display-only provider label, such as
MAAS *Commissioning*, and can differ between provisioners.

The 2026-10-04 provisioning vocabulary change is breaking for API clients:

- State value `commissioning` was renamed to `inspecting`.
- `POST /api/v1/servers/{id}/commission` was renamed to
  `POST /api/v1/servers/{id}/inspect`. The old route was removed and has no
  alias.
- The field `commissioningStatus` keeps its name because it contains the
  provider's last inspection result label.

Stored projections written before the rename are returned as `inspecting`.
Filtering with `provisioningState=inspecting` also matches those records until
the next reconcile rewrites them.

The optional RFC 3339 `provisioning.stateSince` field is when swallow first
observed the current `provisioning.state`. It remains unchanged while that state
is unchanged and moves to the new observation time when the state changes. It
is omitted when unknown, including on an older projection until the state is
observed again; clients must accept its absence. This is a swallow observation
timestamp, not the provider's exact transition time.

Since 2026-10-06, `POST /api/v1/servers/{id}/inspect` runs an `inspect-hardware`
Workflow instead of a single provider call. The `202` response keeps the
provisioning snapshot and adds `workflowId` and `resumed`; follow the Workflow
rather than polling the Server state alone. Inspect from `deployed`,
`allocated`, `rescue`, `retired`, or an in-progress state, and Inspect while
another Workflow holds the Server, now return `409 conflict`. A provisioner
Integration's `settings.autoInspect: "false"` turns automatic inspection of newly
enrolled Servers off. See the
[Server Enrollment contract](../../../api-server/docs/development/api-contracts/api-server/server-enrollment.md),
which also defines the existing-OS enrollment bundle.

Since 2026-11-03 a `deploy-os` Workflow also installs the Node Exporter, with an
`install-node-exporter-<serverId>` and a `record-node-exporter-<serverId>` Task per
Server, and the `install-exporters` and `uninstall-exporters` Workflow kinds are
retired: `POST /api/v1/workflows` and a Site's `playbookMappings` refuse them with
`400 validation_error`. Install the Node Exporter on other Servers through the
[Managed Software contract](../../../api-server/docs/development/api-contracts/api-server/software.md)
(kind `node-exporter`).

Since 2026-11-04 swallow also keeps the Node Exporter installed by itself on every deployed,
unlocked Server of a Site with automation enabled. A client sees `configure-node-exporter`
Workflows whose `requestedBy` is `swallow:node-exporter-controller`; swallow cancels such a
Workflow itself when it stops in `requires_attention`, and never cancels any other. Uninstalling
`node-exporter` from such a Server is followed by a new install.

## Contract directory

Start with:

1. [HTTP conventions](../../../api-server/docs/development/api-contracts/api-server/conventions.md)
2. [Active contract outline](../../../api-server/docs/development/api-contracts/api-server/outline.md)
3. The specific active contract for the resource being integrated.

Do not implement against entries marked Planned. The old `/operations` and
`/clusters` aliases were removed; use `/workflows` and `/platforms`.

For shell automation, the [swallow CLI](cli.md) implements broad user
coverage and emits lossless JSON or YAML. Managed Software currently requires
the Dashboard or HTTP API.
