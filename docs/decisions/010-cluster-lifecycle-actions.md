# 010. Cluster uninstall and delete are separate lifecycle actions

- Status: Accepted
- Date: 2026-08-29

> **Terminology note:** This ADR predates [ADR 014](014-platform-resource-language.md).
> Where it names the swallow aggregate or public resource *Cluster*, the canonical term is
> now **Platform**; *cluster* stays valid only for the external technology (Kubernetes/k0s).
> See [ADR 015](015-platform-term-disambiguation.md).

## Context

A Cluster record previously supported deployment and record deletion, but no host-side
removal. Treating deletion as teardown would be unsafe because registered clusters and
operator-owned Integrations are not Swallow's infrastructure to destroy. Treating teardown
as deletion would also erase the Operation history needed to diagnose a partial k0s reset.

Membership cannot identify the deployment target set reliably: members can be stale,
unmatched, or already unreachable after a failed deployment. Durable deployment Operations
already preserve the accepted targets and are the provenance that proves Swallow built the
cluster.

## Decision

Swallow exposes two explicit commands:

- Uninstall creates an `uninstall-kubernetes` Operation against the complete target
  snapshot from the latest `deploy-kubernetes` provenance. It removes Swallow-installed
  k0s state and retains the Cluster record and Operation history.
- Delete removes the Cluster record, membership projection, and only a credential
  Integration proven to be Swallow-owned. It never executes automation, changes hosts,
  cancels Operations, or restores exporters.

The Cluster lifecycle read model is derived in one backend batch from durable deployment
and uninstall Operations. The API owns `origin`, `lifecycleState`, and
`lifecycleOperationId`; clients do not infer lifecycle from membership or
`integrationId`.

New deployments persist the exact owned Integration identifier. Legacy integrations are
removed only when deployment provenance and the complete auto-generated Integration
signature both match. A successful uninstall can queue a separate exporter restoration
Operation when Kubernetes previously owned exporters; its failure does not roll back or
change the completed uninstall.

## Alternatives considered

- Make Delete run k0s reset: rejected because deletion must remain valid for external and
  Slurm clusters, and a record command must not hide destructive host work.
- Derive targets from current membership: rejected because membership is observed,
  incomplete, and may disappear precisely when cleanup is needed.
- Delete every linked Integration: rejected because `integrationId` can point to an
  operator-owned registration.
- Remove the Cluster after uninstall: rejected because partial failures, retries, and audit
  history need a durable cluster context.
- Add a Force or Forget command: rejected because the two intended meanings are already
  expressed precisely by Uninstall and Delete.

## Consequences

Only Swallow-deployed Kubernetes clusters are eligible for uninstall. Failed, canceled, or
indeterminate uninstalls may be retried over the full frozen target set because the release
playbook is idempotent. Missing, absent, locked, or busy targets refuse the whole request
before dispatch.

Delete can occur while an uninstall is running. The accepted Operation continues, and its
completion cleanup treats a missing Cluster record as success. Finished deploy or uninstall
Operations cannot be retried after their referenced Cluster is deleted.

## Current status

Implemented by the Cluster Uninstall and Delete workflow.

## Amendment (2026-10-29): Uninstall can also delete the record once it succeeds

Operators who wanted both host cleanup and record removal had to uninstall, wait for
`uninstalled`, and come back for Delete. The dashboard also showed Uninstall and Delete as two
separate destructive actions, which read as one harmless action and one dangerous one.

Uninstall now accepts `deleteRecord`. Swallow records the choice on the uninstall Operation
and, only after the uninstall succeeds, removes the Platform record together with the same
projections Delete removes. It does not cancel Operations, because the completing uninstall
is the Workflow holding the targets. A failed, canceled, or indeterminate uninstall still keeps
the record, so the reason for keeping the record after a partial teardown still holds.
Without `deleteRecord`, a successful uninstall keeps the record as before. Delete itself is
unchanged: it removes only the record and never changes hosts.

The dashboard presents removal as one red control: while the Platform can still be uninstalled
it is a split button whose primary part opens Uninstall and whose menu holds "Delete only"
(record-only Delete); once the Platform is `uninstalled`, or can never be uninstalled, it is a
single Delete. The Uninstall dialog's submit is the same kind of split button: its primary part
keeps the record, and "Uninstall and delete record" in its menu sends `deleteRecord`. Each
variant is deliberately a secondary menu entry rather than a peer of the primary action, so the
action that leaves the runtime running on the hosts is never the obvious one.

Alternatives considered:

- Have the dashboard send Delete after the uninstall: rejected because Delete cancels the
  Platform's in-flight Operations, so a Delete sent during the uninstall stops it, and a browser
  cannot be relied on to wait for the uninstall to finish.
- Add a Forget command: still rejected; record-only removal remains Delete.
