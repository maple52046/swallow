# Operations (deprecated alias of Workflows)

## Status

Deprecated (one-release compatibility alias)

## Owner Component

`api-server`

## Purpose

`Operation` is the former name of a [Workflow](workflows.md). The canonical contract is
[`workflows.md`](workflows.md). For one release the `/api/v1/operations` surface remains
available, served by the same handlers, and returns a `Deprecation: true` header plus a
`Link: </api/v1/workflows>; rel="successor-version"` header (see below for which responses carry
them).

See [`workflows.md`](workflows.md) for endpoints, request and response shapes, status sets,
the deprecated-path mapping, the in-progress wire field rename (`steps`->`tasks`,
`executor`->`runner`, `operationId`->`workflowId`, `maas`->`provisioner`). Consumers should
migrate to `/api/v1/workflows`.

The alias's wire shape is in the OpenAPI document
[`../openapi/openapi.yaml`](../openapi/openapi.yaml), tag `workflows`: every alias operation is
marked `deprecated`, its `operationId` ends in `ViaDeprecatedAlias`, and it reuses the canonical
schemas. The schema-v2 routes `GET /api/v1/operations/{id}/logs`, `GET
/api/v1/operations/{id}/events`, and `POST /api/v1/operations/{id}/retry` were removed on
2026-10-31 together with the schema-v2 records ([`workflows.md`](workflows.md), Compatibility
Notes). The `Deprecation` and `Link` headers are set on every response the handlers
produce, success or error; a request rejected by authentication or authorization before it
reaches them carries neither.

Rationale and migration window: [ADR 017](../../../../../docs/decisions/017-workflow-job-task-runner-model.md).
