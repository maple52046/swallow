# 017. Workflow model: Workflow / Job / Task / Runner, convergent and playbook-pluggable

- Status: Accepted
- Date: 2026-09-05

## Context

[ADR 016](016-temporal-operation-orchestration.md) established that durable Operations run on
Temporal. Two things were still unsettled and were the source of real confusion:

- **Shape and vocabulary.** The model was "Operation → Step" with an `executor`
  (`internal`/`ansible`/`maas`). "Operation" collided with everyday English, "Step" undersold
  the DAG, and there was no name for a reusable, composable unit like "deploy the OS" — which
  an operator thinks of as one thing but which is several steps.
- **The ansible / workflow overlap.** In the k0s deploy, the entire install
  (prereq → config → bootstrap → controller-join `serial:1` → worker-join → verify →
  credential) lives inside **one** ansible playbook run as a single step. So the *orchestration*
  lives in ansible while swallow also runs a workflow engine — two things orchestrating, which
  is exactly where the confusion came from.

Separately, `deploy-kubernetes` required the whole target batch to be uniformly `deployed`
(existing OS) or uniformly `ready` (provision OS), which cannot express "7 Servers, 3 already
have an OS and 4 do not".

## Decision

### 1. Ubiquitous language: Workflow → Job → Task, executed by Runners

- **Workflow** — an operator's intent expressed as a **desired end-state** for a set of
  resources (e.g. "these 7 Servers are a k0s Platform with these roles"). Durable, observable,
  cancellable; orchestrated by Temporal ([ADR 016](016-temporal-operation-orchestration.md)).
- **Job** — a **reusable, convergent unit** that brings a set of resources to a sub-goal
  (e.g. `ensure-os`, `configure-k0s`). Composed into Workflows and runnable on its own.
- **Task** — the **atomic executed unit**, carried out by exactly one Runner (e.g. "ensure OS
  on Server X", "run the k0s playbook", "verify membership").
- **Runner** — the **mechanism** that executes a Task: `provisioner` (drives an OS provisioning
  provider through a vendor adapter — MAASProvider, IronicProvider), `ansible` (runs a bounded
  remote-host playbook), `internal` (swallow's own logic), and future kinds such as `terraform`.
  Runners are named by **purpose** where several implementations exist (`provisioner`), and by
  the **tool** where the tool is the identity (`ansible`).

This replaces the ADR 016 vocabulary — Operation → **Workflow**, Step → **Task**, executor →
**Runner** — and adds the **Job** layer; the `maas` executor becomes the `provisioner` Runner.
ADR 016's Temporal-engine decision is unchanged.

### 2. Convergent (desired-state) execution — "ensure" Tasks

Tasks are **idempotent "ensure" operations**: each brings a resource to a desired state and is a
no-op when already there. A Workflow therefore declares a desired end-state and **converges each
resource independently**, so its targets may start in different states. A deploy may mix targets:
`ensure-os` installs an OS only on the Servers that lack one and skips those already `deployed`.
The uniform-batch requirement is retired.

Rejected: a plan/apply phase that diffs desired vs observed and emits only the needed Tasks.
Idempotent Tasks give the same convergence with far less machinery; an explicit plan/preview can
be added later without changing this vocabulary.

### 3. The Workflow / ansible boundary — orchestration up, host-config down

Orchestration lives in the **Workflow**, not in ansible. The boundary is drawn by **who owns the
capability**:

- **Cross-Runner, or reusable across platform types → a Workflow Task** owned by swallow: OS
  provisioning and network (`provisioner`), SSH readiness (`internal`/`provisioner`), credential
  and membership recording (`internal`).
- **Single-platform remote host configuration → one idempotent playbook per platform kind**, run
  by the `ansible` Runner (the k0s playbook today; a Slurm or storage playbook tomorrow).
  Within-playbook sequencing that is genuinely ansible's job — `serial:1` controller join,
  handlers, host loops — stays inside the playbook deliberately.

Rejected: extracting individual ansible tasks and driving them one-by-one from the Workflow. It
reimplements ansible's own orchestration (dependencies, handlers, `serial`, fact caching) — the
reinvention [ADR 001](001-system-ownership-boundaries.md) warns against — and it destroys the
playbook-level extensibility below. A playbook is split into two Tasks only when a non-ansible
action must run in the middle.

### 4. Inventory is swallow's published language to platform playbooks

swallow prepares a `serverId`-keyed inventory with each host's OS, network, and SSH made ready,
plus a documented set of trusted variables. Every platform playbook consumes this inventory.
Adding a new platform type is therefore **contributing an idempotent playbook + a trusted-vars
contract + a manifest entry** — not writing swallow Go code. This is the extension point for
Slurm, storage, and future platform kinds, and it is why the ansible boundary is drawn at the
**playbook**, not below it.

## Consequences

- Domain model, glossary, and eventually code and API speak one language: Workflow / Job / Task /
  Runner. Renaming the current `Operation` / `Step` / `executor` / `maas` names in code, the HTTP
  API (`/operations` → `/workflows`, …), Mongo, and the Dashboard is a **bounded migration with a
  one-release compatibility window**, mirroring [ADR 014](014-platform-resource-language.md), and
  is tracked as a follow-up implementation plan. Until then, glossary terms carry the current code
  names as deprecated synonyms, and `terms/operation.md` / `terms/operation-step.md` remain as
  migration aliases so the active operations API contract keeps resolving.
- `deploy-kubernetes` (and future deploys) accept mixed target states and converge each Server;
  the uniform `existing_os` / `provision_os` batch gate is replaced by per-Server `ensure-os`.
- New platform deployments are added as playbooks against the published inventory contract, keeping
  swallow's Go surface stable.
- Cost: idempotency becomes a hard requirement for every Task and playbook (already true for the
  ansible content; now explicit for `provisioner` and `internal` Tasks too), and the code rename is
  real work — accepted for one coherent vocabulary and a pluggable deployment surface.

## Current status

- Model, vocabulary, and principles: Accepted (this ADR).
- Implemented (2026-09-05): Temporal engine and per-resource lease fencing
  ([ADR 016](016-temporal-operation-orchestration.md)); the Go domain/application/delivery rename
  to `Workflow` / `Task` / `RunnerKind` / `WorkflowKind`; the canonical `/api/v1/workflows`
  (+ `/tasks/{taskId}`) HTTP surface with a deprecated `/operations` alias (Deprecation header);
  the glossary terms Workflow / Job / Task / Runner; convergent per-Server `ensure-os` so a
  `provision_os` deploy may mix `ready` and `deployed` servers; and removal of the v2 embedded
  dispatcher and Mongo site lease (Temporal is now mandatory, verified live); the Mongo persistence
  rename (collection `operations`->`workflows`, bson `steps`->`tasks`, `executor`->`runner`,
  `operation_events`/`operation_secrets`->`workflow_events`/`workflow_secrets` with `operationId`->`workflowId`,
  schemaVersion 4) via the `migrateV3ToV4` step, with schema-v2 records retained in `operations`
  for read compatibility (verified live: migrated 15 records, stack recovered healthy); the
  dashboard consuming the canonical `/workflows` (+ `/tasks`) routes; and Jobs as reusable Temporal
  **child workflows** — the `Task.Job` grouping field, the registered `swallow.job.v1` child
  workflow (`JobWorkflowV1`), a parent that runs each Job in cross-Job dependency order via
  `ExecuteChildWorkflow` and pauses the Workflow for operator retry on a retryable Job failure
  (re-running the idempotent Job), verified by a Temporal test-suite replay test. The proven flat
  single-workflow path is unchanged for Workflows whose Tasks declare no Job, so existing histories
  replay identically. The platform deploy now **emits Jobs**: `platformDeploymentLauncher` groups its
  Tasks into `ensure-os` (per-Server `provision-os` + SSH-readiness gate) and `configure-k0s`
  (k0s playbook + health validation), so a real deploy runs as two child workflows in cross-Job order.
- Verified live on `lab-` hardware (2026-09-05): a `deploy-kubernetes` run drove a real MAAS OS install
  and a k0s cluster to `active` on two lab Servers (one `control-plane`, one `worker`, both
  `membership: ready`). Temporal recorded the parent `swallow.operation.v1` plus both child
  `swallow.job.v1` workflows (`.../job/ensure-os/1`, `.../job/configure-k0s/1`) completing in order; the
  convergent `ensure-os` also handled a mixed batch (re-provisioning one `ready` Server while reusing an
  already-`deployed` one). Fixing one latent bug this exposed: the frozen Ansible inventory
  (`discovery.AnsibleGroup`) lacked `bson:",omitempty"`, so a nil `children` slice was stored as BSON
  `null` and broke Ansible's script inventory plugin ("`'NoneType' object is not iterable`") — now
  tagged so the durable inventory omits empties.
- Planned: the JSON wire field rename (`steps`->`tasks`, `executor`->`runner`, wire `schemaVersion`)
  and the matching dashboard field/DTO update — currently the wire stays v3-shaped so the dashboard is
  unaffected; the `RunnerKind` wire/stored value `maas`->`provisioner` (deferred until a Temporal drain
  so replay is unaffected); and native/air-gap Temporal systemd packaging.
- The deprecated `/operations` HTTP alias, the v2 read-compatibility projection, and the `operation`
  / `operation-step` glossary aliases remain until the compat window closes.

## Related

- [ADR 016](016-temporal-operation-orchestration.md) — Temporal orchestration (engine; vocabulary refined here).
- [ADR 006](006-embedded-ansible-execution.md) — playbook provenance and credential rules still in force.
- [ADR 001](001-system-ownership-boundaries.md) — integrate, don't reinvent.
- [ADR 014](014-platform-resource-language.md) — rename-with-compatibility-window precedent.
