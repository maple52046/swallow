# 022. Uninstall + release: release servers directly, skip the platform-software uninstall

- Status: Accepted
- Date: 2026-09-09
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §2.4 Uninstall 的 Job 分組（對稱於 deploy）

## Context

Platform uninstall ([ADR 010](010-cluster-lifecycle-actions.md)) can optionally release the
member servers back to the provider in the same durable Operation. The original flow ran the
`uninstall-platform` ansible step first and then a `release-os` step per server that depended on
it, so the platform software (k0s or Slurm) was removed before each host was released.

But releasing a host wipes its operating system. Running the platform-software uninstall first is
therefore redundant work, adds minutes to the Operation, and can even fail on a node that is
already partially gone — failing the whole uninstall for no benefit, since the host is about to
be erased anyway.

A wrinkle: platform completion (clearing membership, deleting the owned credential Integration,
clearing sync) was triggered by the observer's `AnsibleStepSucceeded` when the
`uninstall-platform` ansible step succeeded, and the observer's `OperationSucceeded` was dead
code. So simply dropping the ansible step would also drop the projection cleanup.

## Decision

- **When an uninstall also releases the servers, release them directly and skip the
  platform-software uninstall step.** The uninstall builds a `release-os` step per member (in
  parallel) followed by a single internal `complete-uninstall` finalize step that depends on them.
- **The finalize step performs the projection cleanup** that `AnsibleStepSucceeded` does on the
  keep-servers path. It needs only the platform id and carries no ansible result. Exporter
  restoration stays off, because a released host is wiped.
- **The keep-servers uninstall is unchanged**: it still runs the uninstall playbook and its
  existing completion hook.
- **Scope: whole-platform uninstall only.** A future scale-in that removes specific nodes while
  the platform keeps running must still uninstall the node from the cluster (drain/remove) before
  releasing it, so scale-in will not reuse this path.

## Alternatives considered

- **Keep the uninstall step but make it a fast no-op when releasing:** rejected — it still runs
  ansible against hosts about to be wiped, which is the waste and fragility we are removing.
- **Wire the dead `OperationSucceeded` observer to fire at operation success:** rejected for now —
  deploy completion legitimately needs the install step's result (the credential), so a generic
  operation-success hook would not serve deploy and would double-fire with the per-step hook. An
  internal finalize step is a smaller, targeted change for the uninstall case.
- **Do the projection cleanup at accept time:** rejected — the Operation can fail; cleanup must
  happen only on success.

## Consequences

- Uninstall + release is faster and no longer fails on a half-removed node, because it never runs
  the platform-software uninstall against a host that is about to be erased.
- Lifecycle is unchanged: the Operation kind is still the platform's uninstall kind, so the
  platform still moves `uninstalling -> uninstalled` and the same retry rules apply.
- The internal step executor gains a narrow finalizer dependency on the platform context.
- Scale-in remains a separate, future flow that keeps the uninstall step.
