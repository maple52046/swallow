# 030. Live Ansible task-event streaming and progress projection

- Status: Accepted
- Date: 2026-09-14

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

- The `ansible-executor` process, which owns the `ansible-runner` subprocess, tails the run's
  events on a short interval and appends compact, output-free task results to a durable store
  (`ansible_task_events`, idempotent on `(runId, seq)`). It also records a coarse
  `AnsibleExecution.Progress` (current play/task and cumulative ok/changed/failed/unreachable/skipped
  counts). Raw stdout is never streamed into MongoDB; it remains the on-disk artifact served by the
  logs endpoint.
- The Temporal ansible activity publishes the Task's `externalExecution` and `startedAt` on its
  first observation (the key unblock), and mirrors `AnsibleExecution.Progress` onto a new advisory
  `Task.Live` on each poll via a targeted step update that never clobbers the Task's intent.
- The API reads task events from the durable store, so a Step's task list is available live during
  the run, independent of the artifact filesystem. Counts are derived from the events.
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
- `Task.Live` and `AnsibleExecution.Progress` are advisory: the authoritative outcome remains the
  Task/Operation status. Live projection is best-effort and never affects a run's result.
- Refines ADR 016 (Temporal orchestration) and ADR 017 (Workflow/Job/Task/Runner) by adding live
  intra-Task observability; it does not change the execution model or the Runner boundary.

## Current status

Implemented: durable event stream + progress model, executor streaming, early reference and live
projection in the ansible activity, API read path, the `workflows` contract, and the dashboard
operation detail. A push (SSE) delivery and event retention alignment remain follow-ups.
