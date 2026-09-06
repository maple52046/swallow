# Workflows

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Accept durable operator intent and expose swallow's owned multi-Task Workflow state:
normalized timeline, runner events, logs, and artifact metadata. A Workflow is a DAG of
Tasks executed by Runners (`internal`, `ansible`, `provisioner`) and orchestrated by
Temporal ([ADR 016](../../../../../docs/decisions/016-temporal-operation-orchestration.md),
[ADR 017](../../../../../docs/decisions/017-workflow-job-task-runner-model.md)).

## Related Glossary Terms

- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)
- [Job](../../../../../docs/development/glossaries/terms/job.md)
- [Task](../../../../../docs/development/glossaries/terms/task.md)
- [Runner](../../../../../docs/development/glossaries/terms/runner.md)
- [Automation Configuration](../../../../../docs/development/glossaries/terms/automation-configuration.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)

## Endpoints

```text
POST /api/v1/workflows/
GET  /api/v1/workflows/
GET  /api/v1/workflows/{workflowId}
POST /api/v1/workflows/{workflowId}/cancel
GET  /api/v1/workflows/{workflowId}/timeline
POST /api/v1/workflows/{workflowId}/tasks/{taskId}/retry
GET  /api/v1/workflows/{workflowId}/tasks/{taskId}/logs
GET  /api/v1/workflows/{workflowId}/tasks/{taskId}/events
GET  /api/v1/workflows/{workflowId}/tasks/{taskId}/artifacts
```

All endpoints require an admin JWT according to [conventions](conventions.md).

### Deprecated `/operations` alias (one release)

For one release the former surface remains available and is served by the same handlers,
with a `Deprecation: true` response header and a `Link: </api/v1/workflows>;
rel="successor-version"` header:

```text
POST /api/v1/operations/                                   -> /workflows/
GET  /api/v1/operations/                                   -> /workflows/
GET  /api/v1/operations/{id}                               -> /workflows/{id}
POST /api/v1/operations/{id}/cancel                        -> /workflows/{id}/cancel
GET  /api/v1/operations/{id}/timeline                      -> /workflows/{id}/timeline
POST /api/v1/operations/{id}/steps/{stepId}/retry          -> /workflows/{id}/tasks/{taskId}/retry
GET  /api/v1/operations/{id}/steps/{stepId}/logs           -> /workflows/{id}/tasks/{taskId}/logs
GET  /api/v1/operations/{id}/steps/{stepId}/events         -> /workflows/{id}/tasks/{taskId}/events
GET  /api/v1/operations/{id}/steps/{stepId}/artifacts      -> /workflows/{id}/tasks/{taskId}/artifacts

# schema-v2 compatibility (legacy path only)
GET  /api/v1/operations/{id}/logs
GET  /api/v1/operations/{id}/events
POST /api/v1/operations/{id}/retry
```

### Wire field rename (in progress)

The route segments are canonical (`workflows`, `tasks`). The JSON body and Mongo still use
the field names `steps[]`, `executor`, `operationId`, and the `provisioner` Runner still
serializes as `maas`. Renaming these to `tasks[]`, `runner`, `workflowId`, and
`provisioner` is the one-release data/wire migration tracked by
[ADR 017](../../../../../docs/decisions/017-workflow-job-task-runner-model.md); consumers
must accept both during the window.

## Create Request

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
and live Server Lock checks. Success is `202 Accepted` after intent is persisted; Temporal
may start after the response through starter reconciliation.

## Response

The response carries the Workflow, its Tasks (currently serialized under `steps[]` with an
`executor` field, see the wire-rename note above), resource leases, and a compatibility
`execution` projection:

```json
{
  "id": "workflow-id",
  "schemaVersion": 3,
  "kind": "deploy-kubernetes",
  "status": "waiting_external",
  "temporal": { "workflowId": "swallow-operation/workflow-id", "runId": "temporal-run-id" },
  "siteId": "site-id",
  "platformId": "platform-id",
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
      "error": null,
      "externalExecution": { "provider": "ansible-runner", "id": "run-id", "generation": 1 },
      "artifacts": []
    }
  ],
  "leases": [
    { "resourceKey": "server:server-id", "owner": "swallow-operation/workflow-id", "fencingToken": 4 }
  ],
  "requestedBy": "admin",
  "requestedAt": "2026-09-03T00:00:00Z"
}
```

Canonical Workflow status is exactly `pending | running | waiting_external |
waiting_dependency | canceling | succeeded | failed | partially_succeeded | canceled |
requires_attention`. Task status is `pending | running | waiting_external |
waiting_dependency | succeeded | failed | canceled | skipped | requires_attention`.
`execution` is a compatibility projection and must not be used to infer Task state.
`intentSnapshot` and Task parameters never contain credential, cloud-init, or Kubernetes
secret values; opaque secret references are internal and never returned.

## List Query

Optional filters are `siteId`, `platformId`, deprecated `clusterId`, `serverId`, `kind`,
`status`, `active=true`, and common pagination. Results merge schema-v2 and schema-v3
history and sort by `requestedAt` descending.

## Timeline And Task Diagnostics

`GET /timeline` returns normalized immutable events ordered by `createdAt` and ID, naming
Workflow and Task state transitions with no secret material. Task logs are UTF-8
`text/plain`; a non-Ansible Task or one without output returns an empty body. Task events
return the retained runner task-event projection for Ansible and an empty list for other
Runners. Task artifacts return metadata only. `dependsOn`, `targets`, `artifacts`, and
timeline results are JSON arrays and never `null`.

## Cancel

`POST /{id}/cancel` returns `202` after Temporal accepts the request. The workflow writes
`canceling` then `canceled`; the handler never writes Mongo status. Unstarted Tasks become
canceled; active MAAS or Ansible execution receives best-effort abort and remains observed
when the provider cannot stop it. Confirmed prior effects are preserved.

## Retry Task

`POST /{id}/tasks/{taskId}/retry` returns `202` only when the Task is `failed` or
`requires_attention` and its normalized error has `retryable=true`. Temporal increments the
Task attempt in the same Workflow; successful dependencies are not repeated. A provider or
Ansible idempotency identity includes Workflow, Task, target, and attempt.

## Durability And Protection

The stable Temporal Workflow ID is `swallow-operation/<workflowId>` (the Temporal name is
history-tied and retained). The starter retries records with `startState=pending`, and
duplicate starts are rejected by Temporal. Resource mutations acquire sorted `server:<id>`
and `platform:<id>` leases, each with a monotonically increasing fencing token, renewed
while waiting and verified before side effects.

## Schema-v2 Compatibility

Historical schema-v2 records retain the status set `pending | running | succeeded | failed
| canceled | indeterminate` and are returned with `schemaVersion=2` plus a synthetic single
Task. Their legacy `GET /operations/{id}/logs`, `GET /operations/{id}/events`, and `POST
/operations/{id}/retry` remain available on the deprecated path only; the v3 equivalents are
the per-Task endpoints above.

## Errors

Target overlap, lock, policy, and invalid control state return `conflict`. Invalid target
state, Task retry eligibility, or unregistered playbook returns `validation_error`. Disabled
or missing Site automation and credential return `provider_unavailable`. Unknown Workflow or
Task returns `not_found`.
