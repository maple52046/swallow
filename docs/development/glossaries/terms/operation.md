# Operation

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: Retired name for a [Workflow](workflow.md). It survives only in names that have not been renamed yet: code types, and wire fields such as `operationId` and `retryOfOperationId`. The `/operations` HTTP alias and the schema-v2 Operation records were retired ([decision 074](../../../decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases)); the HTTP surface is `/workflows` ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md) (workflow job task runner model)).
- Allowed meaning: Use only when naming one of those remaining code or wire names. All domain modeling, documentation, and design use Workflow.
- Disallowed meaning: Do not treat "Operation" as canonical in new glossary or design work; it is a migration alias, not the model's word.
- Synonyms: Workflow (canonical).
- Deprecated terms: This whole term is the deprecated alias; the canonical definition lives in [`workflow.md`](workflow.md).
- Examples: "`operationId` in a response is a Workflow ID." / "`OperationV3` in code is a Workflow."
- Related terms: Workflow, Task.
- Change note: Demoted 2026-09-05 ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md)) from canonical to a deprecated alias for Workflow, so the deprecated operations compatibility contract keeps resolving during the rename window. Its former v3 definition now lives in `workflow.md`. On 2026-10-31 the `/operations` alias and the schema-v2 records were retired ([decision 074](../../../decisions/074-retire-schema-v2-and-deprecated-aliases.md)), so the term is retired except for the remaining code and wire names.
