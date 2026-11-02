# 010. Platform removal: uninstall changes the deployed hosts, record-only delete changes nothing outside swallow

- Status: Accepted
- Date: 2026-08-29
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §2.4 Uninstall 的 Job 分組（對稱於 deploy）、§2.5 Uninstall 與 Delete：分開的 lifecycle 動作（decision 010）；platforms 契約 [Delete A Platform](../../api-server/docs/development/api-contracts/api-server/platforms.md#delete-a-platform)

## Context

Removing a Platform means two different things: removing the runtime swallow installed from the
deployed hosts, and removing swallow's record of the Platform. Treating record removal as teardown is
unsafe, because a record command must not hide destructive host work and a linked Integration may
belong to a user. Treating teardown as record removal would erase the history needed to diagnose a
partial k0s reset.

Membership cannot say which hosts to clean: it can be stale, unmatched, or gone after a failed
deployment. The durable deployment Workflow keeps the accepted target set and proves that swallow
built the Platform. When the uninstall also returns the Servers to the provider, the release wipes
their operating system, so uninstalling the platform software first is redundant, slower, and can
fail on a node that is already half removed.

A Platform is a desired-state resource, and uninstalling it is a destructive change
([decision 069](069-swallow-owns-desired-state.md) (swallow owns desired state),
[decision 076](076-change-plans.md) (Change Plans)).

## Decision

- **Uninstall** is deleting the Platform: its spec is marked absent and the uninstall is proposed as a
  Change Plan. Once a user approves it, the platform controller starts the type's uninstall Workflow,
  which removes swallow-installed platform state from the hosts. The record and swallow's projections
  are removed only when the uninstall succeeds; a failed or canceled uninstall keeps the record and is
  rerun. A Platform whose install was never approved touched no machine and is deleted at once.
- **Delete only the record** (`recordOnly`) removes the record and swallow's own projections:
  membership, the Platform Claims, and a credential Integration proven to be swallow-owned. It never
  runs automation, changes hosts, waits for an uninstall, or restores exporters, and it stays
  available while targets are locked. It first cancels the Platform's unfinished Workflows so they
  release its Servers, and is refused when that cancellation cannot be recorded.
- **Uninstall targets come from deployment provenance**: the complete target snapshot of the latest
  deployment Workflow, never membership. Only swallow-deployed Platforms can be uninstalled. A
  missing, absent, locked, or busy target keeps the approved plan from starting; busy targets can be
  superseded on request ([decision 061](061-recovery-supersedes-blocking-work.md) (recovery
  supersedes blocking work)).
- **An uninstall that also releases the Servers releases them directly**: one release per member in
  parallel, then an internal finalize Task that performs the cleanup a successful uninstall performs
  on kept hosts. The platform-software uninstall is skipped and exporters are not restored. This
  applies only to a whole-Platform uninstall; a future scale-in must remove the node from the runtime
  before releasing it. It is the only way a Platform's claimed Servers are released
  ([decision 067](067-platform-claim-on-server.md) (Platform Claim on Server)).
- **Only proven swallow-owned Integrations are deleted.** New deployments record the Integration they
  own; a legacy one is deleted only when deployment provenance and the complete auto-generated
  Integration signature both match.
- **Exporter restoration is its own Job.** When the hosts are kept and Kubernetes owned the exporters,
  the uninstall Workflow installs the Node Exporter on the kept Servers in a Job after the
  platform-software uninstall. The cleanup is done when that uninstall succeeds, so the Job's failure
  does not change the uninstall.
- **Lifecycle is a backend read model.** The API derives `origin`, `lifecycleState`, and
  `lifecycleOperationId` from the durable deployment and uninstall Workflows; clients do not infer
  lifecycle from membership or `integrationId`.
- **The Dashboard shows removal as one red control** whose primary action is Uninstall, with
  record-only Delete as a secondary entry, so the action that leaves the runtime on the hosts is never
  the obvious one.

## Alternatives considered

- **Make record deletion reset the runtime:** rejected; a record command must not hide destructive
  host work, and it must stay usable when an uninstall cannot run.
- **Derive targets from current membership:** rejected; membership is observed, incomplete, and may
  disappear exactly when cleanup is needed.
- **Delete every linked Integration:** rejected; `integrationId` can point to a user-owned
  registration.
- **Remove the record when the uninstall is requested, whatever its outcome:** rejected; failures,
  reruns, and diagnosis need the record, so it goes only after a successful uninstall.
- **Add a Force or Forget command:** rejected; Uninstall and record-only Delete already express the two
  meanings.
- **Have the Dashboard send a record-only Delete after the uninstall:** rejected; that Delete cancels
  the Platform's unfinished Workflows, so sent during the uninstall it stops it, and a browser cannot
  be relied on to wait for the uninstall to finish.
- **When releasing, keep the platform-software uninstall as a fast no-op:** rejected; it still runs
  Ansible against hosts about to be wiped.
- **Clean up the projections when the uninstall is accepted:** rejected; the uninstall can fail, and
  cleanup must happen only on success.

## Consequences

- Every uninstall waits for a user to approve its Change Plan, and a rerun of a failed or canceled
  uninstall covers the full frozen target set, which is safe because the playbook is idempotent.
- A record-only Delete sent while an uninstall runs cancels that uninstall; deleting the Platform is
  the way to get both host cleanup and record removal.
- Cleanup treats a missing record as success, so a retried completion is safe; finished deployment or
  uninstall Workflows cannot be rerun after their Platform is deleted.
- An uninstall with release is faster and no longer fails on a half-removed node.

## Revision history

- 2026-08-29: decision 010 separated Uninstall (changes the deployed hosts) from Delete (removes only swallow's record); revised 2026-09-05 (Delete first cancels in-flight work) and 2026-10-29 (an uninstall may delete the record; one removal control).
- 2026-09-09: decision 010 made an uninstall that also releases the Servers release them directly, skipping the platform-software uninstall.
- 2026-11-01: decision 076 made Platform removal a spec change: deleting a Platform marks it absent and its approved Change Plan runs the uninstall, and record-only removal became the `recordOnly` option.
- 2026-11-02: merged decision 010 into this ADR; the body states the current decision.
- 2026-11-03: exporter restoration became a Node Exporter Job in the uninstall Workflow instead of a separate exporter Workflow queued after it.
