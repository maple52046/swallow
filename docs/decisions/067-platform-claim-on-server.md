# 067. A Platform's hold on its Servers is a stored Platform Claim

- Status: Accepted
- Date: 2026-10-29
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §2.6 Platform Claim：Platform 對其 Server 的佔用（decision 067）

## Context

An operator could release a Server that a Platform was deployed onto, through the Release
Operation, the deprecated single-Server release, or a Recover of a deployed Server, or delete it
together with its provisioner Machine. Nothing checked whether the Server belonged to a Platform,
so the release or deletion took a node out from under a running Kubernetes or Slurm Platform and
left it broken.

"Which Platform holds this Server" already existed, but only as something recomputed on demand:
the deployment preflight read every Platform's durable lifecycle history and took the latest
deployment's target snapshot for each one that was not uninstalled, and the Deploy Platform
wizard read each such deployment Operation from the browser. Recomputing it on every Release
would be expensive, and the deployment-time check also had a race: two deployments accepted at
the same time could both take the same Server, because nothing was written until each
deployment's Operation existed.

Observed membership cannot stand in for it. Membership is the platform runtime's own answer and
is missing exactly when the hold matters most: a deployment that failed before the node joined, a
Slurm cluster without slurmrestd, or a failing membership sync.

## Decision

The Server carries a swallow-owned **Platform Claim**: the claiming Platform's id and when its
deployment was accepted.

- An accepted Platform deployment claims every target with a conditional write that only matches
  an unclaimed Server, so of two concurrent deployments only one can take it; the other is refused
  and leaves no Platform record or claim behind.
- The claim stays while the hosts may still carry platform state (from `deploying` through
  `uninstall_failed`). A successful uninstall and Delete release it.
- Release and Delete of a claimed Server are refused with `409 conflict` naming the Server and the
  Platform, and `cancelActiveWorkflows` does not get around it. A Platform's Servers are released
  through its uninstall with `releaseServers`; the Workflows that act for the Platform are not
  subject to the rule. Deleting removes the Machine and the Server record, so it is at least as
  destructive as a release.
- Recover of a claimed Server in a not-usable state stays allowed, because returning it to Ready is
  how a broken deployment is repaired.
- The deployment preflight, the dashboard, and the CLI read the claim instead of reconstructing the
  hold. The claim does not store the Platform's name, because a Platform can be renamed, and
  provider reconciliation preserves it.
- When the API starts, Swallow reconciles the claims against the durable lifecycle once, which is
  how Platforms deployed before claims existed get theirs and how a stale claim is released.

## Alternatives considered

- Recompute the hold from the lifecycle history on every request: rejected because it reads
  every Platform's deployment history for each Release, and it cannot close the concurrent
  deployment race without a write.
- Use observed membership: rejected because it is absent after a failed deployment or a failing
  sync, which is when the Server most needs protecting.
- Warn in the Release dialog but still allow the release: rejected because releasing a
  Platform's node is never the intended way to remove it; the Platform's uninstall already
  offers releasing every Server.
- A new schema migration to backfill the claims: rejected because the claim is a new optional
  field and the startup reconciliation fills it from the same lifecycle history without a schema
  version change. The reconciliation also repairs any claim a crash left behind.

## Consequences

Release, Delete, and the deployment preflight read a field on the Server instead of
reconstructing the hold. Concurrent deployments can no longer share a Server. The dashboard can
disable Release and Delete on a claimed Server with the Platform's name, and the CLI can point to
`platforms uninstall` (and, for Delete, `platforms delete`).

A Server whose Platform record was lost outside these paths stays protected until the startup
reconciliation releases the stale claim on the next API start.
