# Store-driven Execution and Desired-state Migration

## Purpose

Consolidate the 2026-10-30 to 2026-10-31 architecture manuscripts that moved
swallow away from "two sources of execution truth" to a store-owned model:
swallow owns desired state and execution state (decisions 069, 070, 071), the
store became a MongoDB replica set with transactions and a change feed,
Workflow execution became store-driven with Ansible running as Temporal
activities (decision 072), the code moved to its target layout, the
installation lifecycle (backup, restore, upgrade) follows the store, and the
schema-v2 compatibility layer was removed (decision 074).

Almost all of this work is implemented and drilled on the development stack.
This plan records the decisions that later work must keep, the rules the work
established, and what is still unfinished. The live tracker of unfinished items
is `docs/plans/manuscripts/20261102-architecture-review-v2.md`, which this
consolidation keeps.

## Source Scope

This consolidation is one of three produced together from 14 draft manuscripts
in `docs/plans/manuscripts/` (excluding the consolidation specification
`README.md` and the live tracker `20261102-architecture-review-v2.md`). This
plan covers 7 of them:

- Shared with the other two plans: `20261030-architecture-review.md` (the
  architecture review: AR-01 to AR-05, AR-09, AR-10, AR-12, AR-15, AR-16,
  AR-21, AR-23 and the implementation defects DF-01 to DF-21).
- Ownership model and core boundary: `20261030-desired-state-model.md`.
- Store: `20261031-ar04-store-transactions.md`.
- Desired-state stage 1: `20261031-ar15-stage1-provisioner-desired-state.md`.
- Store-driven execution: `20261031-ar01-ar03-store-driven-execution.md`.
- Layout move: `20261031-ar21-layout-move.md`.
- Installation lifecycle and compatibility removal:
  `20261031-lifecycle-and-compat-removal.md`.

The companion plans are `20261102_2104_composable_core_and_extension_modules.md`
(modules, Platform desired state, Change Plan) and
`20261102_2104_documentation_and_api_contract_governance.md` (ADRs, API
contracts, documentation).

## Consolidated Background

The review (baseline `v0.1.0-rc.5`, commit `0dd9aa2`) found that the problem was
not "too many layers" but two things:

1. **Execution state had two sources of truth.** Temporal kept execution
   history, while MongoDB was also the Workflow start inbox
   (`startState=pending`, polled every second by a Starter running in both the
   api and the worker), the Ansible job queue (polled by the executor with its
   own lease) and the resource lease store. Keeping them consistent grew
   polling, compensation and repair loops (a lost-execution reconciler, a
   deployment projection repair sweep, three mechanisms guarding "a cancel must
   not undo a finished OS deployment").
2. **The cross-feature core flow had no owner.** Which Jobs and Tasks a
   deployment has and what happens on completion lived in the composition root
   and was dispatched by Task ID string conventions.

Related findings: MongoDB had no transactions (cross-document invariants such as
Platform Claims relied on hand-written compensation and startup repair); the api
process ran over ten background loops and an in-memory SSE broker that could not
see writes from other processes; backup excluded Temporal and restore ran while
the worker and executor were still writing; decision 031 required inert
"swallow-owned fallbacks" for providers that did not exist; and decision 016
promised permanent v2 compatibility before any stable release.

Production topology (decision 040): Docker Compose on a single VM with MongoDB,
PostgreSQL (shared by Temporal and MAAS), Temporal, `api-server`, `worker`,
`ansible-executor`, `dashboard`, and the MAAS snap. The three backend processes
use the same `swallow-api` image with different commands.

## Confirmed Decisions

### Ownership and convergence (decision 069)

- swallow's store is the single source of truth for every managed resource's
  desired state (spec) and for execution state. Provider adapters realize the
  spec; what is read back is observed state (status).
- Convergence is hybrid: non-destructive settings converge continuously;
  destructive actions (reinstall OS, release, tear down a Platform, change a node
  role) first produce a Change Plan that an operator approves, then run as a
  Workflow. Desired state is written through the API, dashboard and CLI; GitOps is
  a later import/export channel.
- Temporal stays, but only as the Workflow executor adapter. Workflow semantics
  (DAG, retry, cancel, wait) are defined by core and stored by swallow; Temporal
  history can be rebuilt from the store.
- The store is a single-node MongoDB replica set; transactions and change
  notifications are platform requirements. Store rules are expressed as ports so
  a later PostgreSQL migration (deferred, not rejected) only rewrites the adapter.
- Decision 069 refines 001, 016, 017 and 040 and supersedes 031.

### Core boundary and product positioning (decision 070)

- Core holds only the domain model (resources, desired and observed state,
  Change Plan), the Workflow model and semantics, controllers, and provider
  interfaces. External systems are swappable adapters: OS provisioning (MAAS),
  Workflow executor (Temporal), host automation (Ansible), store (MongoDB),
  metrics and alerts (Prometheus/Alertmanager), BMC and hypervisor
  (Redfish/libvirt).
- Architecturally a generic core; delivered to customers as a packaged GPU
  cluster distribution. Integrated platforms must be done correctly and deeply.
  Target users are datacenter operators who want it to work out of the box.
- Extension modules come in three kinds: Platform, Accelerator (GPU vendor, a
  dimension separate from Platform) and Software (Managed Software).
- Module evolution has three stages, re-allocated on 2026-10-31: stage 1
  (implemented) keeps modules in this repository, the module directory is the
  package format, and a release assembles modules from one configuration that
  also produces the support matrix; stage 2 manages module lifecycle on an
  installation (trigger: customers need a module mix or update cadence different
  from swallow's releases); stage 3 lets people outside the project own modules
  independently (signing, no Go hooks in external packages, out-of-process hooks).
- Adapter identifiers already persisted or on the API (for example the
  `RunnerKind` value `maas`) stay; only core's interpretation changes, with no
  data migration and no contract change.

### Store transactions (AR-04)

- Single-node replica set everywhere: production uses a keyFile secret copied by
  the entrypoint to tmpfs as 0400 (the container lacks `CAP_FOWNER`, so chmod
  before chown); an authenticated healthcheck runs `rs.initiate` because the
  official entrypoint drops `--replSet` and `--keyFile` during initdb; dev needs no
  keyFile; `swallowctl` waits for a primary before migrating and converts existing
  standalone installations; native installs merge the replication settings.
- Every process checks the topology at startup and refuses a standalone server.
- Transaction scope: infrastructure plus flows with no external I/O between
  writes, or flows split into read-then-write: Workflow create and rerun, Platform
  deploy acceptance (launchers split into Prepare and Persist), Platform delete,
  uninstall completion, credential recording, GPU Operator day-2, Software
  install and uninstall. Resource acquisition and migrations (with DDL) stay
  outside transactions.
- Events publish after commit (`AfterCommit`); unit tests use a fake Transactor
  to check events and transaction use, and atomicity is proven by integration
  tests on a replica set.
- "Insert, then find on duplicate key" aborts a transaction and makes the driver
  retry until timeout; use a single upsert instead.
- The target layout was decided before AR-04 so new code went straight there;
  modules live in `api-server/modules/<kind>/<id>/`.

### Desired-state stage 1 (decision 071)

- swallow corrects provider drift but does not watch in real time: observation
  reuses the provisioner reconcile (30 seconds) and marks `drifted`; a spec change
  converges immediately; drift is corrected at the controller's periodic resync
  (default 10 minutes). Power Configuration-like fields only report drift.
- swallow owns all manual tags; automatic tags with a definition stay
  provider-owned and observed. SSH Key management covers only keys swallow
  registered.
- A Server can arrive by discovery, enrollment or import (`origin` is
  `discovered`, `enrolled` or `imported`); discovered machines are adopted
  automatically and swallow decides from then on; a vanished machine is
  `realized=false` with reason `provider_missing`.
- Stage 1 resources: Server tags and Zone/Pool placement, the Zone/Pool catalog,
  HTTP Proxy, Subnet DNS, SSH Key, OS Image metadata. Power Configuration, Server
  Default User and Boot Media are stage 2.
- The API was reshaped to spec/status for the migrated resources only, with no
  aliases; contract, dashboard and CLI changed in the same commit.
- First observation adopts: migrations never contact the provider.
- Controller runtime: runs in the worker; one TTL singleton lease per controller
  (separate from Workflow resource ownership); triggered by the change feed,
  periodic resync and API requests; transient errors back off, permanent ones
  become `requires_attention`; corrections and drift go to a timeline. Hold
  reasons: Server Lock, rescue, a Workflow owning the Server.

### Store-driven execution (decision 072)

- Upgrades keep no old Temporal workflow code: the dispatcher treats old
  executions as lost, terminates them and resumes from the store with the new
  driver. `swallowctl upgrade` already refuses while Workflows are in progress
  unless `--force`.
- Scheduling is a pure function from the stored Workflow to the next step. There
  is one path: every Workflow has at least one Job (implicit when undeclared); a
  Job declares retry granularity (retry only the Task, or the whole Job); Jobs no
  longer use child workflows; concurrency is a sliding window bounded by
  `OperationMaxParallelism`.
- Resource ownership is tied to the Workflow lifecycle: acquired when the
  Workflow starts (`waiting_dependency` if held), released in the same
  transaction as the terminal state, with no TTL and no renewal. Each dispatch
  increments a generation; core rejects writes from stale generations, and Runners
  confirm their generation before and during side effects. The cost: ownership
  never expires on its own; a stuck Workflow is canceled by an operator.
- The Workflow service is the only writer of state transitions (Begin, Record,
  Park, Finish, Retry, Cancel, Adopt), each one transaction that also writes the
  timeline and projection-type completion behaviors.
- Completion behaviors are declared with the Task: projection behaviors write
  only the store inside Record's transaction; result behaviors need playbook
  output and run in the executing activity before Record, so runner result data
  never leaves the executor. A declaration without a handler fails startup. Task
  ID string dispatch was removed.
- A Workflow is a declarative execution plan and the engine knows no kind: the
  plan carries Jobs, Tasks, dependencies, `ContinueOn`, retry granularity,
  completion behaviors, cancel compensation (`provision-os` returns a canceled
  deployment, `verify-os-image` recovers the Server), targets and resources; Task
  kind properties (rerun behavior, adoption behavior, Runner and task queue) are
  declared; the whole plan is validated at acceptance.
- Operator commands (Cancel, Retry, Rerun) are written to the store first; the
  `workflow-dispatch` convergence controller (worker, one active instance, change
  feed plus a 30-second resync and an `orphans` key) starts, resumes, signals or
  terminates runs. The api no longer creates a Temporal client. Executions the
  store does not know, or that the store has finished, are terminated.
- The Temporal workflow ID stays `swallow-operation/<id>` (`ALLOW_DUPLICATE` with
  conflict policy `FAIL`); each dispatch is a new run whose run ID the store
  records.
- Adopt: provider and internal Runners observe first and continue with the same
  attempt; an Ansible Task becomes `requires_attention`
  (`executor_outcome_unknown`).
- The ansible-executor is a Temporal worker on the `swallow-ansible` task queue
  with only the Ansible activity (MaximumAttempts 1, heartbeat timeout).
  Cancellation arrives by heartbeat; the executor stops ansible-runner, and the
  driver waits for the activity before releasing ownership. Activity input
  carries only IDs; inventory, configuration, known_hosts and secrets are read in
  the executor.
- Per-host task results stay in `ansible_task_events`, `Task.Live` progress is
  written by the executor through core, and raw stdout and job events stay in the
  artifact directory; none of it passes through Temporal, so operator-visible data
  never depends on Temporal history retention or backup and stdout (which may
  contain secrets) never enters Temporal history.
- Rerun will eventually be replaced by reapply; until then Task retry is the main
  recovery and rerun's Task copy rule is frozen.

### Runtime topology (decided 2026-10-31)

swallow is standalone, without blocking multi-node: correctness never depends on
process memory; background work uses store leases for a single active instance;
the store holds no absolute host paths; external calls are idempotent. Prefer
standalone simplicity in trade-offs. Whole-machine migration (restore on a new
host) is a first-class scenario, ahead of multi-node. The rule lives in the
api-server architecture spec's Runtime Topology.

### Layout move (AR-21)

- `internal/operation` became `internal/core/workflow` during the move; type
  names (including `Operation*`), bson fields, collections and Temporal
  registration names did not change.
- Host automation content and module-specific Go code moved to
  `api-server/modules/`. At runtime nothing is assembled: `modules/` is the
  ansible-runner project directory; the manifest lists each module playbook's
  relative path; the runner sets `ANSIBLE_ROLES_PATH` to the playbook's module
  roles plus `shared/roles`, so a module cannot see another module's roles. The
  install path is `/opt/swallow/automation/modules`; configuration keys and
  `SWALLOW_API_PLAYBOOK_*` names stayed.
- Workflow records store playbook names (manifest names), resolved to paths at
  runtime, so reruns of old records survive path changes.

### Installation lifecycle and compatibility removal

- **Restore follows the store** (AR-02): Temporal is not backed up. Restore stops
  every backend, verifies `SHA256SUMS` and `backup.json`, restores, migrates to
  the current schema, terminates all `swallow-operation/*` runs with the Temporal
  CLI, then starts; `workflow-dispatch` re-dispatches unfinished Workflows with a
  new generation. Tasks finished after the backup run again under ensure
  semantics; OS package maintenance holds left by terminated runs expire on their
  TTL (decision 073). Resync releases ownership whose owner is finished or gone.
- **No downgrades** (DF-07): the manifest's schema versions come from
  `swallow-api version --json`; `install.sh` and `swallowctl upgrade` refuse
  downgrades with a jq SemVer comparison, and upgrade also refuses a database
  schema newer than the target; upgrade stops backends before migrating and keeps a
  `pre-upgrade-<version>-<stamp>` backup.
- **nginx re-resolves the API** (DF-19): Docker DNS resolver
  (`127.0.0.11 valid=10s`), an upstream `zone`, and `server api-server:30051
  resolve` (open-source nginx since 1.27.3), keeping upstream keepalive.
- **Compatibility removal** (AR-10, decision 074, which replaced decision 016's
  "v2 readable forever" and refined 017 and 014): migration v10 deleted the v2
  `operations` collection; deprecated routes and true field aliases were removed;
  single-name renames wait for desired-state stage 2.

## Architecture and Design Principles

- One source of truth: the store owns desired state and execution state;
  providers only report observations; the executor only executes.
- One writer per fact: Workflow state has a single writer (the Workflow service);
  each provider-realized resource has one controller.
- Invariants are domain rules inside transactions, not filters, compensations or
  repair sweeps.
- Store semantics stay behind ports; `bson` and Mongo behavior do not leave the
  store adapter.
- Core imports no adapter, module or vendor SDK (enforced by the import check);
  adapters do not import each other.
- Operator-visible state must survive losing Temporal history.
- Don't block multi-node, but prefer standalone simplicity; whole-machine
  migration comes first.

## Functional Scope

Delivered:

- Replica-set store with topology checks, a Transactor, after-commit events and
  transactional cross-document flows.
- Convergence framework: spec/status with generation and conditions, controller
  runtime, change feed port, convergence timeline, convergence requests.
- Stage 1 resources under swallow ownership (Server tags and placement, Zone/Pool
  catalog, HTTP Proxy, Subnet DNS, SSH Key, OS Image metadata), with drift
  reporting and correction.
- Store-driven Workflow engine, `workflow-dispatch`, declarative completion and
  cancel compensation, dispatch-generation fencing, Ansible as Temporal
  activities; the old driver, Starter, lost-execution reconciler, Ansible queue,
  lease renewal and projection repair are gone.
- Target code layout with an import check (`api-server/tests/layout_test.go`).
- Backup with checksums and metadata, store-first restore, downgrade refusal,
  nginx re-resolution, removal of v2 records, deprecated routes and aliases.

## Constraints and Rules

- Every commit builds and passes `gofmt`, `go vet` and `go test` (including Mongo
  integration tests against a replica set via `SWALLOW_TEST_MONGO_URI`); nothing is
  pushed without the user.
- Development stack safety: ask before acting on it; take a `mongodump` before
  migrations and record a snapshot; the dev api-server, worker and
  ansible-executor run under Air and apply saved Go code immediately, so stop the
  worker and executor while building controller or engine code; only `lab-*`
  machines are used for drills, and the machines are announced first.
- Migrations: v5 moved overlay tags into Server spec; v6 removed
  `providerRealized` and lets the catalog controller adopt Zone/Pool descriptions
  on first observation (giving existing Zone/Pool generation 1 directly wrote
  swallow's description to the provider's default zone); v7 reshaped SSH Key
  sync; v8 moved `os_image_overlays` to `os_image_metadata`; v9 removed old leases
  and `ansible_executions` and backfilled completion, Job and cancel declarations;
  v10 deleted the v2 `operations` collection. `migrate` refuses without a
  confirmed backup unless `--backup-confirmed`.
- Unadopted Servers reject spec edits (409); spec writes carry the expected
  generation.
- Ownership never expires; stale generations cannot write.

## Data Model and Format Notes

- Server: `{id, origin, generation, spec, status}`; tag edits write only spec in a
  transaction conditioned on generation (conflict 409); `spec.zoneId` and
  `spec.poolId` with `null` meaning the provider's default group.
- Zone/Pool: `{id, siteId, generation, spec, status}`; delete stays synchronous
  (provider first, then the record; 409 while referenced).
- HTTP Proxy and Subnet DNS: stored resources (`provisioner_http_proxies`,
  `provisioner_subnet_dns`); reads with no stored setting adopt from a live read
  without writing.
- SSH Key: `status.provisioners[]` conditions plus aggregate conditions;
  generation increments only when the Deployment Key is replaced;
  `POST /ssh-keys/sync` writes a convergence request (`convergence_requests`).
- OS Image: `{id, architecture, spec, status}`; `spec` is swallow metadata;
  `status` carries effective values, provider originals, size and verification.
  Provider Data Overlay was retired.
- Controller leases live in `controller_leases`; Workflow resource ownership
  records have no TTL.
- `GET /workflows/{id}` `leases`: no `expiresAt`; `owner` is the Workflow ID;
  `fencingToken` is the dispatch generation. `startState` and `temporal` kept
  their shape; the store records the current run ID.
- Backup contents: `backup.json` (release version, schema version, creation time),
  `mongo.archive.gz`, `job-artifacts.tar.gz`, `credential-key`, `SHA256SUMS`,
  plus the co-located MAAS database.
- `ansible_task_events` follow artifact retention (events are deleted before the
  artifact directory; a failed event delete keeps the directory for the next
  pass).

## CLI / API / Config Notes

- Removed routes: `/api/v1/operations*`, `/api/v1/clusters*`, the synchronous
  `POST /servers/{id}/deploy`, `POST /servers/{id}/release` and
  `POST /provisioning/deployments`; `POST /workflows` returns a `Workflow`.
- `PATCH`/`DELETE /images/overlay` became `PUT`/`DELETE /images/metadata`; the
  CLI's `images overlay` became `images metadata`; `--clear-zone` and
  `--clear-pool` now work.
- Configuration: `controllerResyncInterval` replaced `sshKeySyncInterval`;
  `TemporalStartInterval` became the dispatcher's resync; `OperationDispatchInterval`
  and `OperationLeaseDuration` were removed; `ReconcileInterval` serves only the
  provisioner reconcile.
- `swallowctl doctor` checks the replica set and that the executor has an activity
  poller on `swallow-ansible`; `swallowctl` backup, restore and upgrade behave as
  described above; `swallow-api version --json` feeds the release manifest.

## Implementation Plan

Implemented, with these commits:

- Ownership documents: `34c4ea6`; target layout `199182a`.
- Store: `be632e2` (replica set), `18e77ac` (store foundation), `7d10811`
  (transactions).
- Stage 1: `dcf9f63` (documents and decision 071), `3cbdcf9` (foundation), then
  one commit per resource; fixes `5107bda`, `2a148c3`.
- Store-driven execution: `42e2d85` (decision 072), `ca61c7e` (engine), `6d5743f`
  (driver and dispatcher), `4ca71e4` (declared completions), `45b84aa` (Ansible as
  activities), `4f8ef39` (cut-over, migration v9); `5d34b14` stops the playbook on
  cancel.
- Layout move: `b5cdbf9`, `dce2e79`, `e00b840`, `6027074`, `a22e8ee`.
- Lifecycle: `70efccb` (nginx), `851d1d3` (downgrade), `bae1c26` (restore),
  `1d688ac` and `1aa9d4b` (compatibility removal).

Verified on the development stack: replica-set conversion, migrations v4 to v10,
adoption leaving the provider unchanged, drift detection and correction,
executor kill and Retry, cancel stopping the playbook within seconds.

Remaining (tracked in the v2 tracker):

- Testing-stack drills that were skipped: restore (AR-02), downgrade refusal
  (DF-07), API-only image replacement (DF-19), upgrading with `--force` and
  resuming parked Workflows, terminating an execution and re-dispatching,
  terminating orphan executions, and stage 1 adoption on upgrade.
- AR-05: move the api's remaining background loops into worker controllers and
  drive SSE from the change feed.
- AR-15 stage 2 and AR-23 (power on/off through Workflows).

## Non-goals

- Keeping old Temporal workflow code across upgrades.
- Backing up Temporal.
- Multi-node operation (not blocked, not built).
- GitOps now; pure continuous convergence; pure plan/apply.
- Replacing Temporal or building a custom durable execution engine now.
- Migrating to PostgreSQL now (AR-16, deferred).
- A second provider adapter or inert fallbacks for providers that do not exist.
- Keeping schema-v2 records or deprecated aliases.

## Open Questions

Tracked in detail in the v2 tracker:

- AR-15 stage 2: whether power on/off becomes desired power state or stays an
  action (recommended: stays an action, moved into a Workflow); whether Server
  Lock becomes a swallow-owned hold (recommended) or stays provider-owned; the API
  form of reapply, which no contract defines yet.
- Power Configuration: decision 054 still forbids swallow storing power
  credentials, so a superseding ADR is required; parked Workflows hold the Server
  and block applying a corrected value; report-only drift is not supported by the
  Server controller; adoption rules for a field added to an existing spec; sealing
  write-only secrets; whether VM enrollment remains a second writer.
- Unregistered paths return 500 instead of 404 (DF-46); batch deployments answer
  409 for unsupported RAM targets (DF-47); the `boot-media` volume is not in the
  backup (DF-48).

## Future Work

- AR-05: background loops as worker controllers; SSE from the change feed.
- AR-15 stage 2: Power Configuration, Server Default User, Boot Media, OS
  deployment intent with Change Plans, Release and Recover; reapply replacing
  rerun; operator-facing declarative apply.
- AR-23: power on/off as Workflows.
- AR-16: PostgreSQL with its own ADR, after removing `bson` tags from the
  workflow domain.
- Declarative Workflow templates (AR-22) are covered by the composable-core plan.
