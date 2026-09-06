# Operations (deprecated alias of Workflows)

## Status

Deprecated (one-release compatibility alias)

## Owner Component

`api-server`

## Purpose

`Operation` is the former name of a [Workflow](workflows.md). The canonical contract is
[`workflows.md`](workflows.md). For one release the `/api/v1/operations` surface remains
available, served by the same handlers, and returns a `Deprecation: true` header plus a
`Link: </api/v1/workflows>; rel="successor-version"` header.

See [`workflows.md`](workflows.md) for endpoints, request and response shapes, status sets,
the deprecated-path mapping, the in-progress wire field rename (`steps`->`tasks`,
`executor`->`runner`, `operationId`->`workflowId`, `maas`->`provisioner`), and the
schema-v2 compatibility rules. Consumers should migrate to `/api/v1/workflows`.

Rationale and migration window: [ADR 017](../../../../../docs/decisions/017-workflow-job-task-runner-model.md).
