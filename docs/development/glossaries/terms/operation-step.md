# Operation Step

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: Retired name for a [Task](task.md). It survives only in names the rename of [decision 017](../../../decisions/017-workflow-job-task-runner-model.md) (workflow job task runner model) has not reached yet: the `steps[]` wire field, a timeline event's `stepId`, and code types such as `OperationStep`. The `.../steps/{stepId}/...` HTTP paths were retired with the `/operations` alias ([decision 074](../../../decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases)).
- Allowed meaning: Use only when naming one of those remaining wire or code names. All domain modeling, documentation, and design use Task.
- Disallowed meaning: Do not treat "Operation Step" / "Step" as canonical in new glossary or design work; it is a migration alias.
- Synonyms: Task (canonical).
- Deprecated terms: This whole term is the deprecated alias; the canonical definition lives in [`task.md`](task.md).
- Examples: "Each entry of a Workflow's `steps[]` is a Task." 
- Related terms: Task, Workflow.
- Change note: Demoted 2026-09-05 ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md)) from canonical to a deprecated alias for Task. Its former definition now lives in `task.md`. On 2026-10-31 the step HTTP paths were retired ([decision 074](../../../decisions/074-retire-schema-v2-and-deprecated-aliases.md)), so the term is retired except for the remaining wire and code names.
