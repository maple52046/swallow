# Operation Step

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: Deprecated name for a [Task](task.md). Retained only because the current code (`OperationStep`) and HTTP API (`.../steps/...`) still use "Step" until the rename in [decision 017](../../../decisions/017-workflow-job-task-runner-model.md) completes.
- Allowed meaning: Use only when referring to the current step endpoints or the `OperationStep` code type. All new domain modeling, documentation, and design use Task.
- Disallowed meaning: Do not treat "Operation Step" / "Step" as canonical in new glossary or design work; it is a migration alias.
- Synonyms: Task (canonical).
- Deprecated terms: This whole term is the deprecated alias; the canonical definition lives in [`task.md`](task.md).
- Examples: "The `.../steps/{stepId}/retry` endpoint retries a Task." 
- Related terms: Task, Workflow.
- Change note: Demoted 2026-09-05 ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md)) from canonical to a deprecated alias for Task. Its former definition now lives in `task.md`.
