# 061. Recovering and stopping a Workflow: rerun, cancel, and superseding blocking work

- Status: Accepted
- Date: 2026-09-07
- 現行規格：[workflow-execution.md](../development/workflow-execution.md) §6 `requires_attention` 停等、§7 Rerun：以新 Workflow 收斂、§8 取消、§9 取代佔用中的工作（`cancelActiveWorkflows`）、§10 派發與接續

## Context

A Workflow owns its Servers until it ends. It can fail, park in `requires_attention`, lose its
Temporal execution, or keep running while the user needs its Servers elsewhere; each case needs a
way out that leaves the Servers and the Platform usable and the recorded results true. A parked
platform deployment once ended as `canceled` when its host rebooted, leaving deletion of the
Platform as the only way out. A cancel rewrote finished OS deployments as `canceled`, the rerun
that followed trusted those records, and the cancel left an interrupted MAAS deployment Allocated.
And Release, Recover, and Platform uninstall were refused while a Workflow held their Servers, so
the dashboard canceled that work and polled before submitting, which read as a frozen dashboard.

## Decision

### Recover with a new Workflow, never by deleting the Platform

- **Rerun** creates a new Workflow (a new id, so a new Temporal Workflow ID) for the same frozen
  intent: kind, definition, Jobs, intent snapshot, Site, Platform, and targets. It records lineage
  to the original (`retryOfOperationId`) and gets its own copy of the sealed secrets; the original
  stays as a diagnostic record. A Platform's lifecycle follows its newest deploy or uninstall
  Workflow, so a rerun takes over the Platform's state and no recovery deletes a Platform. Rerun is
  one primitive for every kind. A parked original is superseded: its cancel is written in the same
  transaction, and the new Workflow waits for its resources.
- **Rerun converges from the current state, not from history.** Every convergent (ensure) Task
  ([decision 072](072-store-driven-workflow-execution.md) (store-driven Workflow execution)) runs
  again whatever its earlier outcome, at the attempt after its last, and observes before it acts.
  One-time actions keep a succeeded result, and so does a succeeded playbook once a `release-os` in
  the same Workflow succeeded, because the released host no longer runs the OS the playbook needs.
  These copy rules are frozen until the reapply of
  [decision 069](069-swallow-owns-desired-state.md) (swallow owns desired state) replaces rerun.

### Parked and lost Workflows wait for the user, not for a timer

- A `requires_attention` Workflow keeps its resources and runs nothing. Parking depends on no timer
  and no renewal, so a host reboot or a brief database outage never ends it; only the user's Task
  retry or cancel does.
- A lost execution is re-dispatched from the store by the `workflow-dispatch` controller
  (decision 072 (store-driven Workflow execution)), not
  only marked for attention. A resumed Task continues at the same attempt and only observes; an
  Ansible Task whose outcome cannot be proven becomes `requires_attention`. Task retry and cancel
  therefore work after an execution is lost, and rerun is for ended Workflows or for converging
  from the start. Nothing automatic recovers destructively: only a user's explicit retry or rerun,
  which starts a new attempt, may redo side-effecting work.

### Cancel stops unfinished work and keeps real results

- **A cancel never rewrites a finished result.** A Task that already succeeded, failed, or was
  skipped keeps its result, and so does a Server's finished OS deployment. Decision 072's single
  writer guarantees this: the cancel transaction writes only unfinished Tasks, and a Task's
  projections run only on a real transition.
- **A cancel returns what it interrupted to Ready.** For each `provision-os` running when the cancel
  arrived, the canceled Workflow, still owning its resources, runs the Task's declared cancel
  compensation, bounded and best-effort. It aborts a Deploying Machine and returns an Allocated one
  to Ready with the Recover plan of [decision 033](033-provider-recovery-policy.md) (provider
  recovery policy): a plain Release that keeps static IP bindings and erases no disk. A Machine that
  is deployed, failed, broken, or in rescue holds the deployment's outcome and is left as it is.
- A cancel does not undo playbook changes or release the Workflow's other Servers, and it always
  completes, because a Workflow whose execution is gone is re-dispatched to run its cancel path.

### Release, Recover, and Platform uninstall supersede the work holding their Servers

- They accept `cancelActiveWorkflows`. After every other check passes, swallow writes a cancel for
  each unfinished Workflow holding a target and accepts the new Workflow at once, leaving those
  Workflows out of its busy check. The new Workflow needs the same resources, so it waits in
  `waiting_dependency` and starts by itself once they end. Superseding skips only the busy check,
  never ownership or the Platform Claim
  ([decision 067](067-platform-claim-on-server.md) (Platform Claim on Server)).
- A Platform is uninstalled by deleting it: the flag is stored with the removal and takes effect
  when the approved Change Plan ([decision 076](076-change-plans.md) (Change Plans)) starts the
  uninstall, and with it a `deploying` Platform may be uninstalled. A refused cancel fails a Release
  or Recover with a conflict naming the Workflow; the Platform's `realized` Condition reports it for
  an uninstall.
- **A Release observes before acting on every attempt**: it succeeds on a Server that a canceled
  deployment already returned to Ready and follows one that is Releasing. When static IP cleanup was
  requested but this Task sent no Release, it fails rather than claim a cleanup that never ran.
- **Clients only send the flag**; the cancel-then-act sequence lives in one backend place. The
  dashboard's Release and Recover dialogs close within the request, Platform deletion lists the
  holding Workflows for the user to confirm, and the CLI offers `--cancel-active-workflows`.
  Without the flag, the busy conflict names the holding Workflow.
- **A preemptible Workflow is superseded by any Workflow that needs its Servers**, without a flag:
  the Workflow that declared itself preemptible (a report-mode Validation Run,
  [decision 081](081-validation-suites-and-runs.md) (validation suites and runs)) gets its cancel in
  the accepting transaction, exactly as above, so validation never blocks a deployment. A preemptible
  Workflow never preempts another one.

## Alternatives considered

- **Restart the same Workflow record instead of creating a new one.** Rejected: it would not
  cleanly free the targets or keep lineage, while a new Workflow with lineage reuses the pattern a
  failed uninstall's retry already used.
- **Rerun automatically when an execution is lost.** Rejected: a rerun can redo side-effecting
  provisioning, which the user should decide. Decision 072 resumes a lost execution instead, at the
  same attempt and observing only.
- **Keep carrying succeeded Tasks over and only copy their results onto the Servers.** Rejected by
  the user: it trusts history; a Server released or redeployed since would show a stale result.
- **On cancel, only abort and leave the provider state to the user.** Rejected by the user: every
  canceled deployment would need a manual Release, and Allocated reads as a stuck Server.
- **Release every Server of a canceled Workflow.** Rejected: a finished deployment, or a failure
  the user may want to inspect, is a real result a cancel must not erase.
- **Let the client cancel the blocking work and wait before submitting.** Rejected (the uninstall
  variant by the user): the intent lives in a browser tab and is lost when it closes, the wait is
  unbounded once a canceled deployment returns its Server, and a waiting dialog leaves the user
  unsure what is happening. Asking the user to act again later was rejected by the user too: it
  moves the waiting to them and loses the intent if they forget.

## Consequences

- A failed, canceled, or interrupted Workflow recovers on the same Platform without deleting
  anything; a canceled deployment shows each Server's real OS result, and the Server it interrupted
  returns to Ready without user action, or its timeline says why not.
- A rerun re-validates every node, at the cost of running the idempotent playbook again (about 10
  minutes for k0s). Every Task kind added to the convergent set must observe before it acts on a
  later attempt.
- Release, Recover, and uninstall behind running work are durable intents that start by
  themselves, possibly after the canceled Workflow's cancel path (up to about 30 minutes when it
  returns an interrupted deployment). An abandoned parked Workflow blocks its Servers until a user
  cancels or supersedes it.

## Revision history

- 2026-09-07: decision 061 made a failed or lost Workflow recoverable by a rerun as a new Workflow with lineage instead of deleting the Platform, and had lost executions only marked for the user, never rerun automatically.
- 2026-10-19: decision 061 made a cancel keep finished results and return interrupted deployments to Ready, and made rerun converge from the current state.
- 2026-10-19: decision 061 moved superseding the Workflows that hold a Release's or Recover's Servers from the dashboard into the backend, and on 2026-10-27 extended it to Platform uninstall.
- 2026-11-02: merged decision 061 into this ADR; the body states the current decision.
- 2026-11-12: a preemptible Workflow (a report-mode Validation Run, decision 081) is superseded by any Workflow that needs its Servers, without a flag.
