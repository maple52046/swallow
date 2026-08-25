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
operation per site may run; different sites may run concurrently.

## List Query

Optional filters are `siteId`, `clusterId`, `serverId`, `kind`, `status`,
`active=true`, and the common pagination query. The common pagination envelope applies.

## Logs

Logs are UTF-8 `text/plain` read from the persistent local artifact directory. Pending
runs and runs without output return an empty body. Credentials must never appear.

## Errors

Target overlap and policy violations return `conflict`. Invalid target state or
unregistered playbook returns `validation_error`. Disabled/missing site automation or
credential returns `provider_unavailable`.

## Compatibility Notes

The response owns `execution` and contains no external-controller IDs. There is no
refresh endpoint or controller webhook.
