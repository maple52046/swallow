# Operation

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: Deprecated name for a [Workflow](workflow.md). Retained only because the current code (`OperationV3`) and HTTP API (`/operations`) still use "Operation" until the rename in [decision 017](../../../decisions/017-workflow-job-task-runner-model.md) completes.
- Allowed meaning: Use only when referring to the current `/operations` API surface, the `OperationV3` code type, or historical schema-v2 records. All new domain modeling, documentation, and design use Workflow.
- Disallowed meaning: Do not treat "Operation" as canonical in new glossary or design work; it is a migration alias, not the model's word.
- Synonyms: Workflow (canonical).
- Deprecated terms: This whole term is the deprecated alias; the canonical definition lives in [`workflow.md`](workflow.md).
- Examples: "The `/operations` endpoint is the current wire name for a Workflow." / "`OperationV3` in code is a Workflow."
- Related terms: Workflow, Task.
- Change note: Demoted 2026-09-05 ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md)) from canonical to a deprecated alias for Workflow, so the active operations API contract link keeps resolving during the rename window. Its former v3 definition now lives in `workflow.md`.
