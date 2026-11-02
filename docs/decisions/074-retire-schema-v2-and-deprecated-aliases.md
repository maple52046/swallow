# 074. Retire the schema-v2 Operation records and the one-release compatibility aliases

- Status: Accepted
- Date: 2026-10-31
- 現行規格：[workflow-execution.md](../development/workflow-execution.md) §1；API 契約
  `api-server/docs/development/api-contracts/api-server/workflows.md`（Compatibility Notes）

## Context

Three earlier decisions left compatibility surfaces behind:

- [ADR 072](072-store-driven-workflow-execution.md) kept the schema-v2 single-run Operations
  readable through a *permanent* compatibility projection. They lived on in the `operations`
  collection, read by every Workflow list, get, and history projection (overview, Platform
  lifecycle, the Server lock's active-work check), plus three v2-only
  routes (`/operations/{id}/logs`, `/events`, `/retry`). No release has created or run one since the
  v2 dispatcher was removed; the records only age.
- [ADR 072](072-store-driven-workflow-execution.md) kept the `/operations` HTTP alias "for one
  release", with `stepId` and `operationId` duplicates in the Task-retry response.
- [ADR 015](015-platform-term-disambiguation.md) kept the `/clusters` routes, the `clusterId` fields
  and query parameter, `kind=cluster`, and the `cluster` monitoring label "for one release".

Several deprecated request aliases of the same kind accumulated elsewhere: the `ephemeral` request
field next to `deployTarget`, and the synchronous `POST /servers/{id}/deploy`, `POST
/servers/{id}/release`, and `POST /provisioning/deployments` next to their durable Workflow
endpoints. The synchronous single-Server deploy was the only deploy path that did not clear the
Server Default User.

swallow has no external installations yet, so every consumer of these surfaces is in this
repository (the dashboard, the CLI, tests) and moves in the same change.

## Decision

- The schema-v2 Operation records are retired. No API or projection reads them, the v2-only routes
  are removed, and schema v10 drops the `operations` collection, behind the same backup
  confirmation as the v3 rename. `POST /workflows` answers with the Workflow projection instead of
  the v2-shaped compatibility body.
- The one-release aliases end now: the `/operations` alias with its `steps/{stepId}` paths, the
  `/clusters` routes, `clusterId` (fields and query), `clusters` and `inventory.clustered` in the
  overview, `kind=cluster`, the `cluster` monitoring label and its read fallback, the duplicate
  `stepId` and `operationId` in the Task-retry response, and the request `ephemeral` alias.
- The deprecated synchronous `POST /servers/{id}/deploy`, `POST /servers/{id}/release`, and `POST
  /provisioning/deployments` are removed; deploy and release go through their Workflow endpoints.
  `POST /provisioning/deployments/preflight` stays.
- Out of scope: wire names that have a single name and no alias yet (`operationId`, `steps`,
  `executor`, `lifecycleOperationId`, the `execution` summary). Their rename is a separate
  migration.

## Alternatives considered

- **Keep v2 readable forever, as ADR 072 said.** Rejected: it keeps a second read model in every
  history projection and a collection no code can write, for records no installation needs.
- **Keep the aliases one more release.** Rejected: there is no external consumer to give the time
  to, and each alias is a second name a reader must learn.
- **Rename the single-name wire fields in the same change.** Rejected: that is a wire and store
  migration of its own, independent of removing duplicates.

## Consequences

- One read model for Workflows, and one name per concept on the wire for everything listed above.
- Breaking for any client of the removed routes and fields; the contracts carry compatibility notes.
- A database upgraded to v10 loses the v2 records; the pre-upgrade backup keeps them.

## Revision history

- 2026-11-03: dropped the automatic exporter installation from the readers and consequences, because that mechanism was retired when OS deployment began composing the Node Exporter.
