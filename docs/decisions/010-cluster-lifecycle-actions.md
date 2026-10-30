# 010. Cluster uninstall and delete are separate lifecycle actions

- Status: Accepted
- Date: 2026-08-29
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §2.5 Uninstall 與 Delete：分開的 lifecycle 動作（decision 010）

> **Terminology note:** This ADR predates [ADR 014](014-platform-resource-language.md). Where it
> names the swallow aggregate *Cluster*, the canonical term is now **Platform**; *cluster* stays
> valid only for the external technology (see [ADR 015](015-platform-term-disambiguation.md)).

## Context

A Cluster record previously supported deployment and record deletion, but no host-side
removal. Treating deletion as teardown would be unsafe because registered clusters and
operator-owned Integrations are not Swallow's infrastructure to destroy. Treating teardown
as deletion would also erase the Operation history needed to diagnose a partial k0s reset.

Membership cannot identify the deployment target set reliably: members can be stale,
unmatched, or already unreachable after a failed deployment. Durable deployment Operations
already preserve the accepted targets and are the provenance that proves Swallow built the
cluster.

Later, operators who wanted both host cleanup and record removal had to uninstall, wait for
`uninstalled`, and come back for Delete, and the dashboard showed Uninstall and Delete as two
separate destructive actions, which read as one harmless action and one dangerous one.

## Decision

Swallow exposes two explicit commands:

- Uninstall creates an uninstall Operation (`uninstall-kubernetes`, and `uninstall-slurm` since
  [ADR 019](019-slurm-platform-deployment.md)) against the complete target snapshot from the latest
  deployment provenance. It removes Swallow-installed platform state and retains the record and
  Operation history. It may also be asked to delete the record (`deleteRecord`): the record and the
  projections Delete removes go only once the uninstall succeeds, without canceling Operations,
  and a failed, canceled, or indeterminate uninstall keeps the record.
- Delete removes the record, membership projection, and only a credential Integration proven to
  be Swallow-owned. It never executes automation, changes hosts, or restores exporters. It first
  cancels the Platform's in-flight durable Operations so their leases are released, and is refused
  when that cancellation cannot be performed.

The lifecycle read model is derived in one backend batch from durable deployment and uninstall
Operations. The API owns `origin`, `lifecycleState`, and `lifecycleOperationId`; clients do not
infer lifecycle from membership or `integrationId`.

New deployments persist the exact owned Integration identifier. Legacy integrations are removed
only when deployment provenance and the complete auto-generated Integration signature both match.
A successful uninstall can queue a separate exporter restoration Operation when Kubernetes
previously owned exporters; its failure does not roll back or change the completed uninstall.

The dashboard presents removal as one red control whose primary action is Uninstall; record-only
Delete and "Uninstall and delete record" are secondary menu entries, so the action that leaves the
runtime running on the hosts is never the obvious one.

Revision history: 2026-09-05 Delete began canceling the Platform's in-flight Operations before
removing the record (originally it never canceled them; changed by a plan, not an ADR);
2026-10-29 amendment added `deleteRecord` and the single dashboard removal control (original text
in git history).

## Alternatives considered

- Make Delete run k0s reset: rejected because deletion must remain valid for external and
  Slurm clusters, and a record command must not hide destructive host work.
- Derive targets from current membership: rejected because membership is observed,
  incomplete, and may disappear precisely when cleanup is needed.
- Delete every linked Integration: rejected because `integrationId` can point to an
  operator-owned registration.
- Remove the Cluster after uninstall: rejected because partial failures, retries, and audit
  history need a durable cluster context. (The 2026-10-29 `deleteRecord` choice keeps this: the
  record is removed only after a successful uninstall, and only when the operator asks.)
- Add a Force or Forget command: rejected because the two intended meanings are already
  expressed precisely by Uninstall and Delete; still rejected in the 2026-10-29 amendment, since
  record-only removal remains Delete.
- Have the dashboard send Delete after the uninstall (2026-10-29): rejected because Delete cancels
  the Platform's in-flight Operations, so a Delete sent during the uninstall stops it, and a
  browser cannot be relied on to wait for the uninstall to finish.

## Consequences

- Only Swallow-deployed Platforms are eligible for uninstall. Failed, canceled, or indeterminate
  uninstalls may be retried over the full frozen target set because the playbook is idempotent.
  Missing, absent, locked, or busy targets refuse the whole request before dispatch.
- A Delete sent while an uninstall runs cancels that uninstall; `deleteRecord` is the way to get
  both. Completion cleanup treats a missing record as success, so a retried completion is safe.
  Finished deploy or uninstall Operations cannot be retried after their Platform is deleted.
