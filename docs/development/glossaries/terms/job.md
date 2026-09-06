# Job

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: A reusable, convergent unit of Tasks that brings a set of resources to a sub-goal, composable into Workflows and runnable on its own.
- Allowed meaning: A named capability such as `ensure-os` or `configure-k0s`. A Job is convergent as a whole: re-running it leaves resources already at the sub-goal unchanged. The same Job is reused across Workflows — `ensure-os` runs standalone and inside `deploy-kubernetes` — rather than being duplicated. A Job groups Tasks that may use different Runners.
- Disallowed meaning: Not a synonym for a whole Workflow (a Job is a composable part), not a single Task, and not an OS Deployment or a Platform.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "`ensure-os` is a Job reused by both the standalone OS deployment Workflow and the deploy-kubernetes Workflow." / "`configure-k0s` is a Job whose remote host work is one idempotent playbook."
- Related terms: Workflow, Task, Runner.
- Change note: Added 2026-09-05 ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md)) to name the reusable convergent middle layer between a Workflow and its Tasks.
