# Operations

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Accept durable operator intent, expose Swallow-owned multi-Step workflow state, and read
normalized timeline, runner events, logs, and artifact metadata.

## Related Glossary Terms

- [Operation](../../../../../docs/development/glossaries/terms/operation.md)
- [Automation Configuration](../../../../../docs/development/glossaries/terms/automation-configuration.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)

## Endpoints

```text
POST /api/v1/operations/
GET  /api/v1/operations/
GET  /api/v1/operations/{operationId}
POST /api/v1/operations/{operationId}/cancel
POST /api/v1/operations/{operationId}/steps/{stepId}/retry
GET  /api/v1/operations/{operationId}/timeline
GET  /api/v1/operations/{operationId}/steps/{stepId}/logs
GET  /api/v1/operations/{operationId}/steps/{stepId}/events
GET  /api/v1/operations/{operationId}/steps/{stepId}/artifacts

# schema-v2 compatibility
GET  /api/v1/operations/{operationId}/logs
GET  /api/v1/operations/{operationId}/events
POST /api/v1/operations/{operationId}/retry
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Create Automation Request

```json
{
  "kind": "custom",
  "intent": "verify SSH connectivity",
  "targetServerIds": ["server-id"],
  "platformId": null,
  "playbookName": "diagnostic-ping",
  "extraVars": {}
}
```

`playbookName` is required for `custom`; built-in kinds use the Site mapping unless an
explicit release-manifest playbook is supplied by a trusted use case. Targets are frozen,
must belong to one Site, and must pass current provisioning, Platform policy, active-work,
and live Server Lock checks. Success is `202 Accepted` after schema-v3 intent is persisted;
Temporal may start after the response through starter reconciliation.

## Schema-v3 Response

```json
{
  "id": "operation-id",
  "schemaVersion": 3,
  "kind": "deploy-kubernetes",
  "intent": "Deploy k0s Platform lab-k0s",
  "intentSnapshot": {},
  "definition": "platform-deployment",
  "definitionVersion": 1,
  "status": "waiting_external",
  "statusReason": "Waiting for an external executor or provider.",
  "startState": "started",
  "temporal": {
    "workflowId": "swallow-operation/operation-id",
    "runId": "temporal-run-id"
  },
  "siteId": "site-id",
  "platformId": "platform-id",
  "clusterId": "platform-id",
  "targetResources": [
    { "kind": "platform", "id": "platform-id" },
    { "kind": "server", "id": "server-id" }
  ],
  "targetServerIds": ["server-id"],
  "steps": [
    {
      "id": "install-platform",
      "kind": "ansible-playbook",
      "name": "Install k0s Platform",
      "executor": "ansible",
      "dependsOn": ["wait-for-ssh"],
      "targets": [{ "kind": "server", "id": "server-id" }],
      "status": "waiting_external",
      "attempt": 1,
      "progress": 0,
      "waitingReason": "Waiting for ansible execution.",
      "error": null,
      "externalExecution": {
        "provider": "ansible-runner",
        "id": "run-id",
        "generation": 1
      },
      "artifacts": [],
      "startedAt": "2026-09-03T00:00:00Z",
      "finishedAt": null
    }
  ],
  "leases": [
    {
      "resourceKey": "server:server-id",
      "owner": "swallow-operation/operation-id",
      "fencingToken": 4,
      "expiresAt": "2026-09-03T00:01:30Z",
      "updatedAt": "2026-09-03T00:00:00Z"
    }
  ],
  "retryOfOperationId": null,
  "requestedBy": "admin",
  "requestCorrelation": "request-id",
  "requestedAt": "2026-09-03T00:00:00Z",
  "startedAt": "2026-09-03T00:00:00Z",
  "finishedAt": null,
  "updatedAt": "2026-09-03T00:00:00Z",
  "execution": {
    "runId": "temporal-run-id",
    "playbook": "",
    "status": "waiting_external",
    "statusReason": "Waiting for an external executor or provider.",
    "startedAt": "2026-09-03T00:00:00Z",
    "finishedAt": null
  }
}
```

The canonical Operation status is exactly `pending | running | waiting_external |
waiting_dependency | canceling | succeeded | failed | partially_succeeded | canceled |
requires_attention`. Step status is `pending | running | waiting_external |
waiting_dependency | succeeded | failed | canceled | skipped | requires_attention`.
`execution` is a compatibility projection and must not be used to infer Step state.
`clusterId` is a deprecated one-release alias of `platformId`.

`intentSnapshot` and Step parameters never contain credential, cloud-init, or Kubernetes
secret values. Opaque secret references are internal and are not returned. Provider raw
state and raw runner events are diagnostics, not lifecycle values.

## List Query

Optional filters are `siteId`, `platformId`, deprecated `clusterId`, `serverId`, `kind`,
`status`, `active=true`, and common pagination. Results merge schema-v2 and schema-v3
history and sort by `requestedAt` descending.

## Timeline And Step Diagnostics

`GET /timeline` returns normalized immutable events ordered by `createdAt` and ID. Events
name Operation and Step state transitions and contain no secret material.

Step logs are UTF-8 `text/plain`. A non-Ansible Step or a Step without output returns an
empty body. Step events return the retained runner task-event projection for Ansible and
an empty event list for other executors. Step artifacts return metadata only; server-side
paths and bytes are never exposed by this endpoint.

`dependsOn`, `targets`, `artifacts`, timeline results, and Step artifact results are JSON
arrays and never `null`, including when reading older schema-v3 Mongo records.

## Cancel

`POST /{id}/cancel` returns `202` after Temporal accepts the request. The workflow writes
`canceling` and then `canceled`; the HTTP handler never writes Mongo status. Unstarted
Steps become canceled. Active MAAS or Ansible execution receives best-effort abort/cancel
and remains observed when the provider cannot stop it. Confirmed prior effects are
preserved.

## Retry Step

`POST /{id}/steps/{stepId}/retry` returns `202` only when the Step is `failed` or
`requires_attention` and its normalized error has `retryable=true`. Temporal increments
the Step attempt in the same Operation. Successful dependencies are not repeated. A
provider or Ansible idempotency identity includes Operation, Step, target, and attempt.
For a `provision-os` Step whose installed image has no provider address, Retry is an
explicit recovery command: it may release and redeploy that failed target from frozen
intent. The response error and Dashboard confirmation disclose this behavior; no
destructive recovery occurs without the retry command.

## Durability And Protection

The stable Temporal Workflow ID is `swallow-operation/<operationId>`. The starter retries
records with `startState=pending`, including after an API restart, and duplicate starts are
rejected by Temporal.

Resource mutations acquire sorted `server:<id>` and `platform:<id>` leases. Each lease has
a monotonically increasing fencing token, is renewed while waiting, and is verified before
side effects. Provider-owned Server Lock is checked independently before acceptance and
again by the provider or Ansible executor.

Ansible Steps use a durable `ansible_executions` queue. A Step attempt is enqueued exactly
once, freezes non-secret inventory and SSH policy, and is owned by the standalone executor.
Executor loss produces `requires_attention`; it never automatically starts a second
playbook.

## Schema-v2 Compatibility

Historical schema-v2 Operations retain the status set `pending | running | succeeded |
failed | canceled | indeterminate` and are returned with `schemaVersion=2` plus a synthetic
single Ansible Step. Their `GET /logs` and `GET /events` endpoints remain available.
`POST /{id}/retry` creates a new Operation only for terminal schema-v2 history. Calling it
for v3 returns `409 conflict`; v3 uses Step Retry.

## Errors

Target overlap, lock, policy, and invalid control state return `conflict`. Invalid target
state, Step retry eligibility, or unregistered playbook returns `validation_error`.
Disabled or missing Site automation and credential return `provider_unavailable`. Unknown
Operation or Step returns `not_found`.
