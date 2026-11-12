# 081. Validation: module-declared suites compiled into Workflows, composed into deployments as gates, and user scripts as data

- Status: Accepted
- Date: 2026-11-12
- 現行規格：[validation.md](../development/validation.md)；[workflow-execution.md](../development/workflow-execution.md) §4、§5.6、§6、§9、§12；
  [core-and-extensions.md](../development/core-and-extensions.md) §4、§5；[module-authoring.md](../development/module-authoring.md) §7

## Context

swallow offers three capabilities: Deployment (OS provisioning, Platform and Managed Software
deployment), Monitoring (with alerts), and Validation. The first two exist; Validation is the third.
A user (usually an admin) picks a test plan in the dashboard or the CLI and runs it against deployed
Servers (one or many) or a Platform (entered through its login or access node); plans range from a
single check on a Server to hardware checks, multi-host networking, Platform acceptance, HA tests,
and benchmarks. Runs are started by a user, on a schedule, or by a deployment (validate after
deploying), and the dashboard and the CLI show their progress, status, and results.

What existed before:

- The heavy functional tests live inside the Slurm deployment's `install-platform` Task (the
  `slurm_verify` role): a cross-node `srun`, two jobs sharing a node, a rootless OCI container step,
  MPI over PMIx across nodes, and three HIP probe jobs per GPU node. Except for the `srun` smoke test
  they are deployment gates ([decision 063](063-omnistat-slurm-observability.md) (omnistat slurm
  observability), [decision 066](066-slurm-job-hooks-and-device-metrics-exporter.md) (slurm job hooks
  and device metrics exporter)): a Slurm Platform that cannot hand out working GPUs, containers, or
  MPI is not deployed, and a node whose GPUs fail is drained. They cannot be run again on their own.
- Every deployment's Job and Task composition is Go code in the composition root; the module
  contract ([decision 075](075-module-contract-v1.md) (module contract v1)) has no Job or Task
  templates, and modules can be only Platform, Accelerator, or Software modules
  ([decision 070](070-core-providers-and-extensions.md) (core providers and extensions)).
- Every Workflow owns its resources exclusively and parks in `requires_attention` on a retryable
  failure, holding them until the user acts; nothing bounds a Workflow's total time, and an Ansible
  Task has no swallow-side timeout ([decision 077](077-task-diagnostics-and-wait-policy.md) (Task
  diagnostics and wait policy)). A test run that behaved that way would block deployments for as
  long as nobody looked at it.
- Host automation content comes only from the release's allowlist, never from mutable database
  content (core-and-extensions §5.2), yet teams want to run a script of their own as a check.

## Decision

1. **A Validation Suite is declared by a module and runs as a Workflow.** A module declares its
   suites in its manifest (`validations`): the target kind (`servers` or `platform`), the impact
   (`observe`, `workload`, `disruptive`), the checks, parameters as a spec schema, a budget, and the
   Workflow template that runs them (`templates`). Templates are data: Jobs and Tasks, their
   dependencies, retry granularity, timeouts, and host automation content as a content type plus a
   reference; the core compiles a template into a Workflow plan and accepts it like any other. Suites
   that belong to no Platform, Accelerator, or Software module live in modules of a fourth kind,
   `validation`. The contract version stays 1.
2. **A Validation Run is one execution, recorded by swallow.** It is an action, not desired state:
   its request (suite, targets, parameters, mode, trigger) is written once at acceptance; each
   check's verdict on each target is an observation the run's Task returned; its status and verdict
   are derived. A check that cannot be judged is `error`, never a guessed `failed` or `passed`.
3. **Two modes.**
   - In **report** mode (a user, a schedule, or after a deployment) the run is its own Workflow,
     declared unattended — a failure ends it instead of parking it, and its resources are released —
     with a wall-clock budget, a timeout per Task, and, unless its impact is `disruptive`, preemptible:
     a later Workflow that needs the same Servers cancels it at acceptance. Validation never blocks a
     deployment, and a failed check is a result, not a Workflow failure.
   - In **gate** mode a deployment composes the suite's Jobs into its own Workflow, never a nested
     Workflow; a final Task fails the deployment when a gating check fails, and the deployment parks
     as it always did. The Slurm acceptance checks move out of `install-platform` into a Slurm suite
     composed this way, so decisions 063 and 066 still hold: what makes a Slurm Platform deployed is
     unchanged. Cheap postconditions (readiness, node registration, membership) stay in the
     deployment's own Tasks.
4. **A user-defined suite is a script, run as data by one allowlisted playbook.** An admin writes a
   script; every edit is a new immutable revision; a run records the revision, its sha256, and the
   script it ran. One playbook from the release's allowlist copies the script to each target, runs it
   with a timeout as the automation user or root, and records exit code 0 as `passed`, any other code
   as `failed`, and a timeout as `error`. The allowlist rule stays for host automation content; a
   user script is never composed into a deployment gate, supports no secret, and is admin-only.
5. **Schedules are desired state.** A Validation Schedule names a suite, its targets, and a trigger:
   a cron expression in a time zone, or a deployment of the target succeeding. A controller starts the
   runs; a missed time runs once, and a busy target skips that occurrence.

## Alternatives considered

- **Keep the heavy checks only in the deployment.** Rejected: they could not be run again on a
  deployed Platform, nor on a schedule, and the same checks would be written twice.
- **Run deployment validation as a separate Workflow after the deployment succeeds.** Rejected for
  gating checks: the Platform would be recorded deployed before its GPUs were proven, which changes
  decisions 063 and 066; a separate run is kept for report-mode checks a user adds after deploying.
- **A shared (read) resource ownership for validation runs.** Deferred: preemption gives deployments
  priority without a second ownership mode, and many checks (GPU probes, benchmarks) compete with
  workloads anyway.
- **Store user scripts as host automation content.** Rejected: content would come from the database,
  bypassing the allowlist and the content tests.
- **Expected-output matching for user scripts.** Deferred: the exit code is the script's own verdict
  and needs nothing extra from the user.

## Consequences

- The module contract gains `templates`, `validations`, and `failureCodes`, and a fourth module kind;
  the first template users are validation suites, and the existing deployment launchers can move to
  templates later.
- The Workflow model gains three plan declarations (unattended, a Workflow budget, preemptible) and a
  Task timeout, usable by any Workflow.
- Results become swallow-owned evidence with their own retention; the Workflow still keeps no Task
  output.
- A user script runs arbitrary commands on the targets with the automation user's or root's rights:
  the API is admin-only, and the run records what ran.

## Revision history

- 2026-11-12: accepted.
