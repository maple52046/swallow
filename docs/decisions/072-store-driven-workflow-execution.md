# 072. Store-driven Workflow execution

- Status: Accepted
- Date: 2026-10-31
- 現行規格：[workflow-execution.md](../development/workflow-execution.md) §2–§12；[desired-state-model.md](../development/desired-state-model.md) §6.4, §7.6, §9

## Context

[ADR 069](069-swallow-owns-desired-state.md) decided that a Workflow's state belongs to swallow's
store and that Temporal only executes. The implementation still followed
[ADR 016](016-temporal-operation-orchestration.md): the Temporal workflow kept the Task list in its
own memory and projected every change back to Mongo, so the two copies needed guards and sweeps to
agree.

- An API Starter and a worker Starter both polled for unstarted Workflows. A lost-execution
  reconciler, also run in both processes, could only mark a Workflow whose execution vanished; the
  operator then had to cancel or rerun it.
- A parked Workflow renewed its resource leases on a timer for as long as it waited, and a jobbed
  Workflow ran each Job as a child workflow that canceled from a stale snapshot. Three mechanisms
  kept a cancel from rewriting a finished deployment (DF-11).
- Ansible ran through a hand-rolled Mongo job table: the worker enqueued an execution and polled it
  every two seconds, and the executor polled to claim it and held its own lease
  ([ADR 016](016-temporal-operation-orchestration.md) had rejected such a table).
- What a Task does when it completes was decided by matching Workflow kinds and Task IDs in five
  observers and one completion switch, so the engine had to know every kind.

[ADR 071](071-provisioner-realized-desired-state.md) then added a controller runtime with a
cross-process change feed, singleton leases, and resyncs, which is what dispatching needs.

## Decision

1. **The store is the only record of execution state.** A Workflow's status, its Tasks' status,
   attempt, and result, the resources it owns, the commands operators sent, and its dispatch
   generation exist only in the store. The core Workflow service is their only writer; each
   transition is one store transaction that also writes the timeline and the Task's declared
   projections. The driver workflow keeps no Task state: each round asks the core what to do next.
2. **Operators and launchers write the store; a controller dispatches.** The API process writes
   intent and commands (cancel, Task retry) and never contacts the executor. The `workflow-dispatch`
   controller runs in the worker on the ADR 071 runtime. On the change feed it starts a Workflow's
   first run and delivers commands. On its resync, every 30 seconds, it re-dispatches a Workflow
   whose run is gone and terminates runs the store does not recognize. A lost execution is resumed,
   not just marked.
3. **Dispatch generations fence executions.** Each dispatch increments the Workflow's generation,
   and every activity carries the generation it was started with. The core rejects a write from an
   older generation, and a Runner checks it before side effects and while running. On resume, a
   Task whose Runner can observe (provider, internal) continues at the same attempt and observes
   before acting. An Ansible Task, whose outcome cannot be proven, becomes `requires_attention`
   with `executor_outcome_unknown`. Only an operator retry, which increments the attempt, may
   recover destructively (DF-03).
4. **A Workflow owns its resources for its lifetime.** It acquires its `server:<id>` and
   `platform:<id>` resources in one transaction when it starts. It releases them in the
   transaction that makes it terminal. Ownership has no TTL and is never renewed, also while
   parked. A stuck Workflow is canceled by an operator; cancel always works because the dispatcher
   re-dispatches a Workflow whose run is gone so it can run its cancel path.
5. **One scheduling path.** Every Task belongs to a Job; a Task without one belongs to an implicit
   Job. Cross-Job dependencies order Jobs, `ContinueOn` and skipping work inside a Job, and Tasks
   run in a sliding window of `OperationMaxParallelism`. A Job declares its retry granularity: retry
   only the failed Task, or every Task of the Job at the next attempt. Jobs no longer run as child
   workflows.
6. **A Workflow is a declarative execution plan, and the engine knows no kind.** A Task declares:
   - its completion behaviors: projections that only write the store, run in the transition's
     transaction; results that need the Runner's data, run in the executing activity before the
     outcome is recorded;
   - its cancel compensation: return an interrupted deployment to Ready, or run this Task on
     cancel;
   - through its kind, whether a rerun runs it again and how a resume treats it.

   Acceptance validates the whole plan. A declared behavior without a handler fails process start.
   The engine's packages may not reference a kind.
7. **The Ansible executor is a Temporal worker.** It registers the Ansible Task activity on its own
   `swallow-ansible` task queue, with one attempt and a heartbeat timeout; a cancel reaches it
   through the heartbeat. The activity input carries only identifiers; the executor reads the Task,
   inventory, connection policy, and secrets itself. It writes per-task events and `Task.Live`
   through the core, never through Temporal. The Mongo execution queue is removed, and the events
   follow the artifact retention (DF-10).
8. **Existing work is adopted at upgrade.** The upgrade migration declares completion behaviors and
   retry granularity on existing Workflows and turns leases into ownership records. The dispatcher
   then terminates each old execution and resumes its Workflow with the new driver. Old workflow
   code is not kept.

## Consequences

- One writer and one scheduling path replace the repository guard, the observer's attempt
  comparison, the deployment repair sweep, and the reconciler. Canceling a parked Workflow cancels
  its `requires_attention` Tasks too (DF-11).
- Task retry and cancel work after an execution is lost, so neither answers `409` for that reason.
  Rerun stays as a recovery primitive; its Task copy rules are frozen until the reapply of
  [ADR 069](069-swallow-owns-desired-state.md) replaces it.
- The API process no longer needs Temporal; only the worker and the Ansible executor connect to it.
  Temporal history may be lost without losing state, which simplifies backup and restore (AR-02).
- A resource stays owned until its Workflow ends, so an abandoned Workflow blocks its Servers until
  it is canceled; the lease TTL no longer frees them after an outage.
- `GET /workflows/{id}` reports ownership instead of leases: no `expiresAt`, the owner is the
  Workflow id, and `fencingToken` is the dispatch generation.

## Alternatives considered

- **Keep Temporal as the Task state's owner and make Mongo a pure projection** (the original
  AR-01 direction). Rejected by ADR 069: what operators see would depend on Temporal's history
  retention and backup.
- **Keep timed leases and renew them from the store.** A renewal loop is the part that ended
  parked Workflows on transient outages; dispatch generations give the same exclusion without it.
- **Keep the Ansible queue and only move polling to the change feed.** It keeps a second lease,
  a second idempotency table, and a cancel path through Mongo; a Temporal activity already
  provides delivery, heartbeat, and cancellation.

## Related

[ADR 006](006-embedded-ansible-execution.md), [ADR 016](016-temporal-operation-orchestration.md),
[ADR 017](017-workflow-job-task-runner-model.md), [ADR 020](020-durable-operation-recovery.md),
[ADR 030](030-live-ansible-task-streaming.md), [ADR 060](060-cancel-and-rerun-converge.md),
[ADR 061](061-recovery-supersedes-blocking-work.md), [ADR 069](069-swallow-owns-desired-state.md),
[ADR 071](071-provisioner-realized-desired-state.md).
