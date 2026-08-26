# Operations

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Accept durable automation intent, expose Swallow-owned execution state, and read locally
retained runner logs.

## Related Glossary Terms

- [Operation](../../../../../docs/development/glossaries/terms/operation.md)
- [Automation Configuration](../../../../../docs/development/glossaries/terms/automation-configuration.md)

## Endpoints

```text
POST /api/v1/operations/
GET /api/v1/operations/
GET /api/v1/operations/{operationId}
GET /api/v1/operations/{operationId}/logs
GET /api/v1/operations/{operationId}/events
POST /api/v1/operations/{operationId}/retry
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Create Request

```json
{
  "kind": "custom",
  "intent": "verify SSH connectivity",
  "targetServerIds": ["server-id"],
  "clusterId": null,
  "playbookName": "diagnostic-ping",
  "extraVars": {}
}
```

`playbookName` is required for `custom`; built-in kinds use their site mapping unless
an explicit registered playbook is supplied. Target IDs are frozen at acceptance and
must belong to one site. Success is `202 Accepted` after the pending operation is
persisted, not after execution starts.

## Operation Response

```json
{
  "id": "operation-id",
  "kind": "custom",
  "intent": "verify SSH connectivity",
  "siteId": "site-id",
  "clusterId": null,
  "targetServerIds": ["server-id"],
  "retryOfOperationId": null,
  "execution": {
    "runId": "run-id",
    "playbook": "diagnostic-ping",
    "status": "pending",
    "statusReason": null,
    "startedAt": null,
    "finishedAt": null
  },
  "requestedBy": "admin",
  "requestedAt": "2026-08-25T00:00:00Z",
  "updatedAt": "2026-08-25T00:00:00Z"
}
```

Status is exactly `pending | running | succeeded | failed | canceled | indeterminate`.
A lease expiry becomes `indeterminate` and is never retried automatically. One
operation per site may run; different sites may run concurrently. `retryOfOperationId`
is the operation this one was created to retry, or `null` when it was requested directly.

## List Query

Optional filters are `siteId`, `clusterId`, `serverId`, `kind`, `status`,
`active=true`, and the common pagination query. The common pagination envelope applies.

## Logs

Logs are UTF-8 `text/plain` read from the persistent local artifact directory. Pending
runs and runs without output return an empty body. Credentials must never appear.

## Events

`GET /api/v1/operations/{operationId}/events` returns the run's task-level progress,
derived from the runner's own event stream, so that a long multi-phase run such as a
cluster deployment can be followed beyond a single status word:

```json
{
  "runId": "run-id",
  "status": "running",
  "okCount": 42,
  "changedCount": 12,
  "failedCount": 0,
  "events": [
    {
      "task": "Install k0s controller",
      "play": "Bootstrap the initial controller",
      "host": "server-a",
      "status": "ok",
      "changed": true,
      "startedAt": "2026-08-26T00:01:00Z",
      "endedAt": "2026-08-26T00:01:30Z"
    }
  ]
}
```

`events` is ordered as the runner emitted them. `host` is a `serverId`, matching the
inventory. A pending run with no events yet returns an empty `events` array. Credentials
must never appear; task results that could contain secrets are omitted rather than
surfaced.

## Retry

`POST /api/v1/operations/{operationId}/retry` creates a **new** operation that repeats a
finished one. The original is not modified and its logs and events are retained.

The new operation copies the original's kind, target servers, cluster, playbook, and
operator variables, and records `retryOfOperationId` pointing at the original. It is
accepted only when the original operation is in a terminal state and its targets are not
currently busy in another operation; the same creation checks as `POST /operations/`
apply. Success is `202 Accepted` returning the new operation.

Retry is always operator-initiated. swallow never retries automatically; a rerun is safe
only because the mapped playbook is idempotent.

## Errors

Target overlap and policy violations return `conflict`. Invalid target state or
unregistered playbook returns `validation_error`. Disabled/missing site automation or
credential returns `provider_unavailable`.

## Compatibility Notes

The response owns `execution` and contains no external-controller IDs. There is no
refresh endpoint or controller webhook. `retryOfOperationId` and the `events` endpoint are
additive: `retryOfOperationId` is a new optional field, and older callers that ignore it
and the events endpoint are unaffected.
