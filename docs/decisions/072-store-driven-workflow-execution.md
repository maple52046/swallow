# 072. Workflow execution: the Workflow model, store-driven execution on Temporal, and swallow-run Ansible

- Status: Accepted
- Date: 2026-08-25
- 現行規格：[workflow-execution.md](../development/workflow-execution.md) §2–§12；[desired-state-model.md](../development/desired-state-model.md) §6.4, §7.6, §9；[platform-deployment.md](../development/platform-deployment.md) §1, §4

## Context

Deploying a Platform mixes provisioner actions, host automation (Ansible), and swallow's own steps
across Servers that may start in different states. Doing it durably needs Task-level state,
dependencies, retry, cancellation mid-run, resumption after a lost execution, and serialization per
resource. Under [decision 069](069-swallow-owns-desired-state.md) (swallow owns desired state) the
store is the single source of truth for execution state; under
[decision 070](070-core-providers-and-extensions.md) (core, provider adapters, and extension modules)
the executor and host automation are adapters and what gets deployed comes from modules. The
installation runs on one host ([decision 040](040-single-vm-production-installation.md) (single-VM
production installation)).

## Decision

### Model

- **Workflow → Job → Task, executed by Runners.** A Workflow is a user's intent as a desired end
  state for a set of resources: durable, observable, cancelable. A Job is a reusable, convergent unit
  that reaches a sub-goal (`ensure-os`); a Task is the atomic unit, run by exactly one Runner. Runners
  are named by the port they drive (decision 070): `provisioner`, host automation, and `internal`.
- **Execution is convergent.** Every Task is an idempotent "ensure", a no-op when its resource is
  already in the desired state, so each resource converges from whatever state it is in. Destructive
  changes are previewed as Change Plans first (decision 069, [decision 076](076-change-plans.md)
  (Change Plans)).
- **Orchestration up, host configuration down.** Work that crosses Runners or is reused across
  platform types is a Workflow Task. One platform's remote host configuration is one idempotent
  playbook, and ordering inside it (`serial:1` joins, handlers) stays in the playbook; a playbook is
  split into two Tasks only when a non-Ansible action must run in between.
- **The inventory is swallow's published language to playbooks**: `serverId`-keyed hosts made ready
  for OS, network, and SSH, plus reserved trusted variables a client cannot forge. Adding a platform
  is a module ([decision 075](075-module-contract-v1.md) (module contract v1)) whose host automation
  content is a playbook against this contract.

### Engine

- **The store is the only record of execution state** (status, attempts, results, owned resources,
  commands, dispatch generation). The core Workflow service is its only writer; each transition is
  one transaction that also writes the timeline and the Task's declared projections. Acceptance is
  one transaction with the launcher's records and does not need the executor.
- **Temporal is the Workflow executor adapter and only executes.** A stateless driver asks the core
  what to do next each round and runs Tasks as activities whose inputs carry only identifiers;
  Runners resolve parameters and secrets from the store, so secrets never enter Temporal history and
  losing that history loses no state. A driver change ships as a new version; old code is not kept.
- **Users write the store; a controller dispatches.** The API writes intent and commands only. The
  `workflow-dispatch` controller ([decision 071](071-provisioner-realized-desired-state.md)
  (provisioner-realized desired state) runtime) starts runs and delivers commands from the change
  feed, and on resync resumes a Workflow whose run is gone and terminates runs it does not recognize.
- **Dispatch generations fence executions.** Each dispatch increments the generation; the core
  rejects older writes, and Runners check it before and during side effects. On resume, a provider
  or internal Task observes first at the same attempt; an Ansible Task, whose outcome cannot be
  proven, becomes `requires_attention` (`executor_outcome_unknown`), never success and never retried
  automatically.
- **A Workflow owns its resources for its lifetime.** It acquires all its `server:<id>` and
  `platform:<id>` in one transaction before any Task runs and releases them in its terminal
  transaction, with no TTL or renewal, also while waiting for the user. Controllers do not converge
  an owned resource; cancel always works because a lost run is re-dispatched to cancel.
- **One scheduling path, no automatic retry.** Every Task belongs to a Job (implicit when unnamed),
  and Tasks run in a bounded sliding window. A retryable failure waits as `requires_attention`; a Job
  declares whether a retry reruns the failed Task or the whole Job. Only a user's retry increments
  the attempt and may recover destructively. Starting over is a rerun, a new linked Workflow
  ([decision 061](061-recovery-supersedes-blocking-work.md) (cancel, rerun, and superseding blocking work)).
- **A Workflow is a declarative plan, and the engine knows no kind.** A Task declares completion
  behaviors (store-only projections in the transition's transaction; results needing Runner data in
  the executing activity), cancel compensation (return an interrupted deployment to Ready, or run on
  cancel), and, through its kind, rerun and resume handling. A behavior without a handler fails
  process start, engine packages may not reference a kind, and modules declare the kinds and
  completion types they plan (decision 075).

### Ansible

- **swallow runs Ansible itself** through a pinned `ansible-runner`, not AWX. The Ansible executor is
  the only process that owns the subprocess: a Temporal worker with the Ansible activity on its own
  `swallow-ansible` task queue, one attempt, and a heartbeat that also carries cancellation. It reads
  its inputs from the store and writes through the core.
- **Only release content runs.** The release owns the playbook allowlist
  (`api-server/modules/manifest.json`) and the pinned Python dependencies; arbitrary playbook paths
  are rejected. Automation credentials are stored encrypted and materialized only in a private per-run
  directory removed afterwards. SSH host-key verification cannot be disabled.
- **Live task events.** The run reference is published when the run starts; compact, output-free
  per-host task results stream into the store, and an advisory `Task.Live` carries the current task
  and cumulative counts, never a percentage (the total is unknown in advance). Raw stdout, unbounded
  and possibly secret, never enters the store; it stays in the on-disk artifacts, whose retention the
  events follow.

## Alternatives considered

- **Keep AWX.** Rejected: its production installation is built on a Kubernetes operator, larger than
  the control plane it serves, against the one-host installation and the air-gap goal (deferred, not
  dropped, by decision 040). Temporal reopened that trade-off on purpose: it is self-hostable, runs in
  the same Compose stack, and its durability, replay, and cancellation are worth one more component.
- **swallow's own durable engine, or a job table or execution queue in the store.** Rejected: they
  rebuild timers, replay, fencing, and exactly-once side effects that Temporal provides, and an
  Ansible queue adds a second lease and cancel path that an activity already covers.
- **Temporal owns Task state and the store projects it.** Rejected by decision 069: what users see
  would depend on Temporal's history retention, and two copies need repair sweeps to agree.
- **One run per site, or timed leases renewed while a Workflow waits.** Rejected: deploys need
  per-resource serialization, and renewal ended waiting Workflows on transient outages.
- **Driving individual Ansible tasks from the Workflow.** Rejected: it reimplements Ansible's own
  orchestration and breaks playbook-level extensibility; live events give the visibility instead.
- **A plan/apply phase instead of ensure Tasks.** Rejected: idempotent Tasks converge with far less
  machinery; Change Plans cover destructive changes only.
- **Job containers through the Docker socket.** Rejected: it grants host-level control.
- **Reading live events from the shared artifact volume.** Rejected: it couples the live view to a
  filesystem shared across processes.

## Consequences

- Temporal is required but reached only by the worker and the Ansible executor, and it is not backed
  up: a restore terminates its runs and the dispatcher resumes unfinished Workflows from the store.
- The single writer keeps a cancel from rewriting finished results without repair sweeps; Task
  retry and cancel work after an execution is lost. Rerun's Task copy rules are frozen until
  desired-state reapply (decision 069) replaces it.
- An abandoned Workflow blocks its Servers until the user cancels it, and an Ansible Task cut off by
  an executor loss waits for the user's retry. Idempotency is mandatory for every Task and playbook,
  and an upgrade refuses while a Workflow is unfinished unless forced.

## Revision history

- 2026-08-25: decision 072 made swallow run Ansible itself through a pinned ansible-runner with a release-owned playbook allowlist, instead of delegating execution to AWX.
- 2026-09-05: decision 072 made Workflows durable Task DAGs run by Temporal with typed executors and per-resource lease fencing, replacing the embedded dispatcher, and kept schema-v2 records readable.
- 2026-09-05: decision 072 introduced the Workflow / Job / Task / Runner vocabulary, convergent "ensure" Tasks, and the boundary that puts orchestration in Workflows and single-platform host configuration in playbooks.
- 2026-09-14: decision 072 streamed per-task Ansible results and a live progress projection while a run executes, keeping raw stdout out of MongoDB.
- 2026-10-30: decisions 069 and 070 made the store own Workflow state with Temporal only executing, made Temporal and Ansible replaceable adapters, and named Runners by the port they drive.
- 2026-10-31: decision 072 made the store the only record of execution state, with a dispatching controller, generation fencing, lifetime resource ownership, declared Task behaviors, and the Ansible executor as a Temporal worker.
- 2026-10-31: [decision 074](074-retire-schema-v2-and-deprecated-aliases.md) (retired schema-v2 records and deprecated aliases) retired the schema-v2 Operation records and with them decision 072's permanent read compatibility.
- 2026-11-02: merged decision 072 into this ADR; the body states the current decision.
