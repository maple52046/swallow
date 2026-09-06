# Runner

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: The mechanism that executes a Task. Each Task names exactly one Runner.
- Allowed meaning: The current Runners are `provisioner` (drives an OS provisioning provider through a vendor adapter — MAASProvider, IronicProvider), `ansible` (runs a bounded, single-purpose idempotent playbook on remote hosts), and `internal` (swallow's own in-process logic); future kinds such as `terraform` are added by purpose. Runners are named by **purpose** where several implementations exist (`provisioner`), and by the **tool** where the tool is the identity (`ansible`). Calls to a provider's API are the `provisioner` Runner, never the `ansible` Runner — ansible is only for remote-host work.
- Disallowed meaning: Not the OS Provisioning Provider itself (that is a vendor adapter behind the `provisioner` Runner); not necessarily a separately deployed service (a Runner is a role; the Temporal worker hosts them); and not an orchestrator — a Runner executes one Task, while orchestration is the Workflow's job.
- Synonyms: Executor (current code name).
- Deprecated terms: `executor` — current code name, retained as a migration alias until the [decision 017](../../../decisions/017-workflow-job-task-runner-model.md) rename; `maas` as a Runner name — superseded by `provisioner` with vendor adapters.
- Examples: "boot-and-wait-OS is a `provisioner` Runner Task; install-k0s is an `ansible` Runner Task; record-credential is an `internal` Runner Task."
- Related terms: Task, OS Provisioning Provider, Workflow.
- Change note: Added 2026-09-05 ([decision 017](../../../decisions/017-workflow-job-task-runner-model.md)), renaming executor to Runner, naming by purpose, and fixing the ansible = remote-host-only boundary.
