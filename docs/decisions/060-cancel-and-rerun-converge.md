# 060. Cancel keeps finished results and returns in-flight deployments; Rerun converges

- Status: Accepted
- Date: 2026-10-19
- 現行規格：[workflow-execution.md](../development/workflow-execution.md) §8 取消、§7 Rerun：以新 Workflow 收斂

## Context

On 2026-10-19 an operator canceled the `test-amd-operator` platform deployment while
`lab-afde-mi308-3` waited for a retry, inspected that Server again, and reran the deployment. The
rerun deployed `lab-afde-mi308-3` and k0s, but `lab-afde-mi308-1` and `-2` kept showing their OS
deployment as `canceled`, although both had been deployed for hours.

Two gaps combined:

- **Cancel rewrote finished results.** A jobbed Workflow finalizes cancellation from its start
  snapshot, in which every Task is still pending at attempt 1. The Workflow repository refused to
  regress the two succeeded `provision-os` Tasks, but reported success, so the Server deployment
  observer still received the stale `canceled` Task. Its own guard required the same attempt (1
  against 6, after Job retries) and let the rewrite through. The invariant stated in the Workflow
  contract and the platform-deployment guide was violated.
- **Rerun trusted history.** Rerun carried every succeeded Task over unchanged, so the new Workflow
  never ran those `provision-os` Tasks and nothing corrected the Server results.
  [Decision 017](017-workflow-job-task-runner-model.md) requires Tasks to be convergent ensure
  operations, but the choice of what to run was made once from records, not from the current
  state.

Separately, a cancel aborted an in-flight MAAS deployment and left the Machine at `Aborted
deployment` (Allocated). Every such Server needed a manual Release before anything could use it.

## Decision

- **A cancel never changes a finished OS deployment.** A Task transition the Workflow repository
  ignores as a regression reaches no projection observer and no timeline event; the Server
  deployment observer does not let a cancel from an attempt no newer than a recorded outcome
  overwrite it; and a background sweep restores a wrongly written `canceled` result once the
  Workflow has ended.
- **A cancel returns what it interrupted to Ready.** For every `provision-os` that was running when
  the cancel arrived, the canceled Workflow, still holding its leases, runs a best-effort, bounded
  `return-canceled-deployment` step: it aborts a Deploying Machine and returns an Allocated one to
  Ready with the Recover plan (a plain Release, static IP bindings kept, no disk erase). A Machine
  that is deployed, failed, broken, or in rescue holds the deployment's outcome and is left as it
  is. A parent Workflow waits for a Job child's cancellation, so the step finishes before the leases
  are released. `verify-os-image` keeps its own return-to-ready compensation.
- **Rerun converges to the desired state.** Rerun runs every ensure Task again, whatever its
  earlier outcome, at the attempt after its last one, and each executor observes before it acts on a
  later attempt, as an explicit retry does ([decision 059](059-retry-recovers-failed-deployment.md)).
  One-time actions keep the earlier carry-over rule, and so does a succeeded playbook once a
  `release-os` in the same Workflow succeeded, because a released host no longer runs the OS the
  playbook needs. A Server Default User is cleared only when a deployment is actually submitted, so
  a converged Server keeps it.

## Alternatives considered

- **Keep carrying succeeded Tasks over and only copy their results onto the Servers.** Rejected by
  the user: it trusts history instead of reality. A Server released or redeployed since then would
  be reported with a result that is no longer true.
- **On cancel, only abort and leave the provider state to the operator.** Rejected by the user:
  every canceled deployment then needs a manual Release, and the Allocated state reads as a stuck
  Server.
- **Release every Server of a canceled Workflow.** Rejected: a finished deployment, or a failure an
  operator may want to inspect, is a real result that a cancel must not erase.
- **Merge child Job outcomes into the parent before finalizing a cancel.** Not done: the projection
  guards make the stale snapshot harmless without another Temporal history change.

## Consequences

- A canceled platform deployment shows each Server's real OS result, and the Server a cancel
  interrupted returns to Ready without operator action, or the timeline says why it did not.
- A rerun repairs Server results and re-validates every node instead of assuming them. It costs the
  time of the idempotent playbook again, about 10 minutes for k0s.
- Every Task kind added to the convergent set must observe before it acts on a later attempt.
