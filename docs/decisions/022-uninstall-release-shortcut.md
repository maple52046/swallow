# 022. Uninstall + release: release servers directly, skip the platform-software uninstall

- Status: Accepted
- Date: 2026-09-09

## Context

Platform uninstall ([ADR 010](010-cluster-lifecycle-actions.md)) can optionally release the
member servers back to the provider in the same durable Operation. The original flow ran the
`uninstall-platform` ansible step first and then a `release-os` step per server that depended on
it, so the platform software (k0s or Slurm) was removed before each host was released.

But releasing a host wipes its operating system. Running the platform-software uninstall first is
therefore redundant work, adds minutes to the Operation, and can even fail on a node that is
already partially gone - failing the whole uninstall for no benefit, since the host is about to
be erased anyway.

A wrinkle: v3 platform completion (clearing membership, deleting the owned credential
Integration, clearing sync) is triggered by `platformDeploymentObserver.AnsibleStepSucceeded`
when the `uninstall-platform` ansible step succeeds. `OperationSucceeded` on the same observer is
dead code. So simply dropping the ansible step would also drop the projection cleanup.

## Decision

**When an uninstall also releases the servers, release them directly and skip the
platform-software uninstall step.** `LaunchUninstall` builds a `release-os` step per member (with
no cross-dependency, so they run in parallel) followed by a single internal `complete-uninstall`
finalize step that depends on them. There is no `uninstall-platform` ansible step on this path.

**The finalize step performs the projection cleanup** that `AnsibleStepSucceeded` does on the
keep-servers path. `CompleteUninstall` needs only the platform id (it clears memberships, deletes
the owned credential Integration, clears sync), so an internal step keyed on the platform is
sufficient; it carries no ansible result. Exporter restoration stays off, because a released host
is wiped.

**The keep-servers uninstall is unchanged**: it still runs the `uninstall-platform` playbook and
its existing completion hook.

**Scope: whole-platform uninstall only.** This shortcut lives in `LaunchUninstall`. A future
scale-in that removes specific nodes while the platform keeps running must still uninstall the
node from the cluster (drain/remove) before releasing it, so scale-in will not reuse this path.

## Alternatives considered

- **Keep the uninstall step but make it a fast no-op when releasing:** rejected - it still runs
  ansible against hosts about to be wiped, which is the waste and fragility we are removing.
- **Wire the dead `OperationSucceeded` observer to fire at operation success:** rejected for now -
  deploy completion legitimately needs the install step's result (the credential), so a generic
  operation-success hook would not serve deploy and would double-fire with the per-step hook. An
  internal finalize step is a smaller, targeted change for the uninstall case.
- **Do the projection cleanup at accept time:** rejected - the Operation can fail; cleanup must
  happen only on success.

## Consequences

- Uninstall + release is faster and no longer fails on a half-removed node, because it never runs
  the platform-software uninstall against a host that is about to be erased.
- Lifecycle is unchanged: the Operation is still `uninstall-kubernetes`/`uninstall-slurm`, so the
  platform still moves `uninstalling -> uninstalled` (derived from Operation status), and the same
  `retryOfOperationId` retry rules apply.
- The internal step executor gains a narrow `CompleteUninstall` finalizer dependency, wired in the
  worker process where the internal steps run.
- Scale-in remains a separate, future flow that keeps the uninstall step.

## Current status

Implemented. `LaunchUninstall` branches on `releaseServers`; the internal
`platformWorkflowStepExecutor` handles the `complete-uninstall` step via a `PlatformService`
finalizer wired in the worker.

## Related

- [ADR 010](010-cluster-lifecycle-actions.md) - Uninstall vs Delete lifecycle actions this refines.
- [ADR 016](016-temporal-operation-orchestration.md) / [ADR 017](017-workflow-job-task-runner-model.md)
  - the Workflow/Task model the uninstall Operation uses.
