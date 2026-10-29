# 067. A Platform's hold on its Servers is a stored Platform Claim

- Status: Accepted
- Date: 2026-10-29

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

- When a Platform deployment is accepted, Swallow claims every target of that deployment for the
  new Platform. Each Server is claimed with a conditional write that only matches an unclaimed
  Server, so of two concurrent deployments only one can take it. The other is refused as
  "already claimed", and its Platform record and any claims it wrote are removed. A refused
  launch removes them the same way.
- The claim stays while the Platform is `deploying`, `deploy_failed`, `active`, `uninstalling`, or
  `uninstall_failed`: the hosts may still carry platform state. A successful uninstall (with or
  without `deleteRecord`) and Delete release it.
- Release of a claimed Server is refused with `409 conflict`, naming the Server and the Platform,
  for the Release Operation, the deprecated single-Server release, and a Recover of a deployed
  Server. `cancelActiveWorkflows` does not get around it. A Platform's Servers are released
  through its uninstall with `releaseServers`. The Workflows that act for the Platform, such as
  that uninstall's `release-os` Steps, are not subject to the rule.
- Delete of a claimed Server is refused the same way, before the provisioner is called and even
  when its Machine is already gone. Deleting removes the Machine and the Server record, so it is
  at least as destructive as a release; the operator uninstalls the Platform or deletes its
  record first, which releases the claim.
- Recover of a claimed Server in a not-usable state (`failed`, `broken`, `rescue`, `allocated`)
  stays allowed, because returning it to Ready is how a broken deployment is repaired.
- The deployment preflight reads the claim instead of the lifecycle history, and the dashboard
  and CLI read it from the Server projection (`platformClaim`).
- The claim does not store the Platform's name, because a Platform can be renamed. Provider
  reconciliation preserves the claim, like the Server's `Deployment` result.
- When the API starts, Swallow reconciles the claims against the durable lifecycle once: every
  target of a Platform that is not uninstalled gets that Platform's claim if it has none, which is
  how Platforms deployed before claims existed get theirs, and a claim naming a Platform that is
  gone or uninstalled is released.

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

## Current status

Implemented.
