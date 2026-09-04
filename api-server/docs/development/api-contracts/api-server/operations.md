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
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)

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
  "platformId": null,
  "clusterId": null,
  "playbookName": "diagnostic-ping",
  "extraVars": {}
}
```

`playbookName` is required for `custom`; built-in kinds use their Site mapping unless
an explicit registered playbook is supplied. `uninstall-kubernetes` is a built-in kind;
the Platform use case supplies its release-owned playbook explicitly, so existing Site
mappings need no migration. Target IDs are frozen at acceptance and must belong to one
Site. Every target must also be unlocked in a live provider read before the pending
Operation is persisted. An unavailable lock read fails closed. Success is `202 Accepted`
after the pending Operation is persisted.

## Operation Response

```json
{
  "id": "operation-id",
  "kind": "custom",
  "intent": "verify SSH connectivity",
  "siteId": "site-id",
  "platformId": null,
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
operation per site may run; different sites may run concurrently. After claiming a
pending
Operation, the dispatcher reads every target lock again before runner setup. A newly
locked target fails that Operation with an actionable status reason and the runner does
not start. A lock appearing after the runner starts does not cancel the active work.

`retryOfOperationId`
is the operation this one was created to retry, or `null` when it was requested directly.

## List Query

Optional filters are `siteId`, `platformId`, `serverId`, `kind`, `status`,
`active=true`, and the common pagination query. The common pagination envelope applies.
`clusterId` is accepted as a deprecated one-release alias for the `platformId` filter.

## Logs

Logs are UTF-8 `text/plain` read from the persistent local artifact directory. Pending
runs and runs without output return an empty body. Credentials must never appear.

## Events

`GET /api/v1/operations/{operationId}/events` returns the run's task-level progress,
derived from the runner's own event stream, so that a long multi-phase run such as a
platform deployment can be followed beyond a single status word:

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

The new operation copies the original's kind, target servers, platform, playbook, and
operator variables, and records `retryOfOperationId` pointing at the original. It is
accepted only when the original operation is in a terminal state and its targets are not
currently busy in another operation; the same creation checks as `POST /operations/`
apply. This includes the live unlocked-target check. A finished `deploy-kubernetes` or `uninstall-kubernetes` Operation cannot be
retried after its referenced Platform has been deleted. Success is `202 Accepted`
returning the new Operation.

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

Following the Cluster to Platform rename, `clusterId` is a deprecated one-release alias
for `platformId` in the create request body, the list query, and the Operation response.
When both are supplied on create, `platformId` wins; the alias is removed after the
deprecation window (see
[ADR-014](../../../../../docs/decisions/014-platform-resource-language.md)).
