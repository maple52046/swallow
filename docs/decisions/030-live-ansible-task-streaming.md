# 030. Live Ansible task-event streaming and progress projection

- Status: Accepted; the executor writes `Task.Live` through the core instead of a worker mirroring it, and events follow the artifact retention ([ADR 072](072-store-driven-workflow-execution.md))
- Date: 2026-09-14
- 現行規格：[workflow-execution.md](../development/workflow-execution.md) §11 Ansible Task 的執行、即時事件串流與進度投影（`Task.Live`）

## Context

A platform deployment's remote host configuration runs as one `ansible` Runner Task (for k0s,
`install-platform`). `ansible-runner` writes a per-task event file into its artifact directory as it
runs, but swallow read those events only after the subprocess exited, and the Task's
`externalExecution` reference was projected onto the Operation only when the Temporal activity
returned. The API gated its task-event and log endpoints on that reference, so an operator saw one
opaque step that revealed nothing until the whole run finished — a long, silent wait, and no view of
the many underlying tasks the run performed.

## Decision

Stream a run's per-task results into MongoDB as they are produced, expose the run reference on the
Task as soon as the run starts, and surface a live progress summary — keeping the existing polling
delivery and not inventing a percentage.

- The process that owns the `ansible-runner` subprocess streams compact, output-free task results
  into a durable store and keeps a coarse run progress (current play/task and cumulative result
  counts). Raw stdout is never streamed into MongoDB; it remains the on-disk artifact.
- The Temporal ansible activity publishes the Task's run reference as soon as the run starts (the key
  unblock) and mirrors the run progress onto an advisory `Task.Live` without clobbering the Task's
  intent.
- The API reads task events from the durable store, so a Task's task list is available live during
  the run, independent of the artifact filesystem.
- Progress is expressed as counts and the current play/task, never a percentage: Ansible's total
  task count is not known up front.

## Alternatives considered

- **Read job events live from the shared artifact volume.** The API, worker, and executor already
  share the artifact volume, so the API could read the accumulating `job_events` directly once the
  run reference was exposed. Rejected as the primary path because it couples the live UI to a shared
  filesystem across processes; streaming to MongoDB decouples the event surface and is a natural fit
  for a future push (SSE) delivery. (Setting the run reference early is still required and is done.)
- **Stream raw stdout into MongoDB.** Rejected: stdout is unbounded and can carry secrets. Only the
  bounded, output-free task-result projection is stored; stdout stays on disk.
- **Model each Ansible task as its own Workflow Task.** Rejected per ADR 017: intra-playbook
  ordering belongs inside the playbook, not the orchestrator. Tasks remain the unit of orchestration;
  this decision only makes one Task's internal progress observable.

## Consequences

- Operators see task-by-task progress and the current task while a deployment's Ansible step runs,
  and the single step no longer hides the many tasks it performs. No change to how deployments run.
- A new durable collection grows with play size. Events are compact and output-free; aligning their
  retention with workflow/artifact retention is a follow-up.
- `Task.Live` and the run progress are advisory: the authoritative outcome remains the
  Task/Operation status. Live projection is best-effort and never affects a run's result.
- Refines [ADR 016](016-temporal-operation-orchestration.md) (Temporal orchestration) and
  [ADR 017](017-workflow-job-task-runner-model.md) (Workflow/Job/Task/Runner) by adding live
  intra-Task observability; it does not change the execution model or the Runner boundary.
