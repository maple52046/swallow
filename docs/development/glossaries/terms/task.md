# Task

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: The atomic unit of executed work in a Workflow, carried out by exactly one Runner, and idempotent ("ensure") so that it is a no-op when its target is already in the desired state.
- Allowed meaning: A Task has a kind, targets, a Runner, dependencies on other Tasks (forming an acyclic graph within its Job or Workflow), and its own lifecycle (`pending | running | waiting_dependency | waiting_external | succeeded | failed | canceled | skipped | requires_attention`) with attempt, progress, a normalized error, and artifact metadata. Examples: "ensure OS on Server X" (`provisioner` Runner), "run the k0s playbook" (`ansible` Runner), "verify membership" (`internal` Runner). A retryable failed Task is retried individually; retry is operator-driven, never automatic.
- Disallowed meaning: Not a Job or a Workflow; not raw Runner output — a Task's public projection carries normalized status and metadata, never secret parameters or raw logs. Not a single ansible task inside a playbook: that granularity stays inside the playbook; a Task is what the Workflow schedules.
- Synonyms: None.
- Deprecated terms: `Operation Step` / `Step` — the current code name, retained as a migration alias until the [decision 017](../../../decisions/017-workflow-job-task-runner-model.md) rename completes.
- Examples: "The `configure-k0s` Task runs one idempotent playbook; its internal `serial:1` controller-join is ansible's concern, not a separate Task."
- Related terms: Workflow, Job, Runner.
- Change note: Added 2026-09-05 ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md)), renaming Operation Step to Task and making idempotent ("ensure") semantics explicit.
