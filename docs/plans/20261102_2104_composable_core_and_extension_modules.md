# Composable Core, Extension Modules, and Platform Desired State

## Purpose

Consolidate the 2026-10-31 to 2026-11-02 manuscripts that made swallow's core
composable: Kubernetes, Slurm, AMD and every Managed Software kind load as
extension modules through a manifest (decision 075); providers register their
own kinds; Platform became a desired-state resource whose destructive changes go
through Change Plans (decision 076); the dashboard composes its screens from
module UI; module authors have a guide; and host automation pauses the OS's
automatic package maintenance while it runs (decision 073).

The work ran in eight phases, P0 to P7, all completed and verified in the lab.
This plan keeps the decisions, the rules modules must follow, the deliberate
deferrals, and the known gaps. The live tracker of unfinished items is
`docs/plans/manuscripts/20261102-architecture-review-v2.md`.

## Source Scope

This consolidation is one of three produced together from 14 draft manuscripts
in `docs/plans/manuscripts/` (excluding the consolidation specification
`README.md` and the live tracker `20261102-architecture-review-v2.md`). This
plan covers 4 of them:

- Shared with the other two plans: `20261030-architecture-review.md` (AR-06,
  AR-07, AR-17 to AR-20, AR-22 to AR-28, DF-25, DF-38 to DF-44).
- `20261101-composable-core.md`: phases P0 to P7.
- `20261101-df22-module-authoring.md`: the module authoring guide, old-to-new
  path mapping and path checks (DF-22, part of P0).
- `20261031-package-maintenance-hold.md`: pausing OS package maintenance during
  automation (decision 073).

Companion plans: `20261102_2104_store_driven_execution_and_desired_state_migration.md`
and `20261102_2104_documentation_and_api_contract_governance.md`.

## Consolidated Background

After the store-driven execution work, core still enumerated providers and
plugins: Software kinds were constants and switches in core, the Docker Host
Explorer lived in core, Kubernetes and Slurm shared one API client package, Boot
ISO templates hard-coded MAAS, the Workflow model named runners after products,
and Platform deployment was an imperative launch with no spec. The goals of this
work were: core no longer enumerates any provider or plugin; Software,
Kubernetes, Slurm and AMD are plugins loaded through manifests; Platform is a
desired-state resource with Change Plans for destructive changes; the dashboard
composes screens by module. Server stage 2 resources (Default User, Boot Media,
Power, Lock, OS deployment intent, Release/Recover) were explicitly out of scope.

Separately, a Slurm deployment failed in `Install crun` because
`unattended-upgrades` held the dpkg lock: a RAM-deployed image re-ran a missed
`apt-daily-upgrade` on every boot (`Persistent=true`, up to 60 minutes of random
delay), the playbook checked the lock only once, and ansible-core's apt module
does not retry `apt-get install` or `dpkg -i` lock failures. Of 23 package tasks,
only 18 had retries.

## Confirmed Decisions

### P0 Preparation

- A core vendor-term ratchet (`api-server/tests/core_vendor_terms_test.go` with a
  per-file baseline in `tests/testdata/core_vendor_terms.json`; the initial
  baseline was 1,861 terms in 66 files) only lets counts fall.
- Result handlers read `Completion.Params` instead of Runner extra vars; the two
  Platform target checks merged.
- `ProvisioningTaskWorker` was removed: release cleanup runs inside the
  `release-os` Task, and Task retry retries the owning release Task.
- Inspect's direct provider fallback was removed.
- `core-and-extensions.md` §3.6 became the actual list of boundary violations.

### P1 Module contract and provider registration (decision 075)

- `module.json` names a root Go package (embedded with `go:embed`);
  `internal/core/module` holds the manifest model and registry;
  `compiledModules` in `internal/app/modules.go` builds the same registry for all
  three processes; the playbook allowlist stays the runtime source and a
  conformance test compares it with the manifests; a definition is recorded as
  `<kind>/<id>@<version>`.
- Workflow kinds and completion types belong to modules (Kubernetes:
  deploy/uninstall and exporters, `kubernetes-credential`; Slurm: configure,
  uninstall, Omnistat, `slurm-credential`, Omnistat and DME completions; AMD:
  GPU Operator and GPU driver, `gpu-operator-state`; Software kinds:
  `configure-<kind>` and `uninstall-<kind>`). Core keeps OS deploy, release,
  recover, image verification, hardware inspection, VM enrollment, exporters,
  NTP and custom, with the server-deployment, software-assignment-failure and
  platform-uninstall completions.
- Provider kinds are owned by their adapters (MAAS, Prometheus, the platform
  readers) and collected by `integrationProviders()`.
- `RunnerKindAnsible` became `RunnerKindHostAutomation` (the stored value stays
  `ansible`); Task logs, stderr and events depend on whether the Task recorded an
  external run, not on the runner.
- Boot ISO rendering is a core `BootChainRenderer` port implemented by the MAAS
  adapter (templates moved unchanged; the output was byte-identical in the lab).

### P2 Software modules

- The Docker Host Explorer moved into the docker-ce module; Registry Credentials
  stayed in core.
- The catalog lives in each module's `software` hook (`Entry()`, with
  `ValidateSpec`, `NormalizeSpec`, `InstallVars`, and optional
  `ServerReferencer`), not in the manifest: decision 075 is frozen and the
  manifest has no catalog field, so adding one needs a new ADR.
- Naming: Workflow kinds `configure-<kind>`/`uninstall-<kind>`, playbooks
  `deploy-<kind>`/`uninstall-<kind>`, derived from the kind; startup checks the
  manifest declares them. Workflow kinds come from the registry; core has no
  Software kind constants or switches.
- Software Workflow definitions are recorded as `software/<kind>@<version>`.
- The contract's `SoftwareKind` lost its enum (kebab-case pattern); the dashboard
  repository keeps only kinds it has screens for until module UI registration
  replaces that list.

### P3 Platform and AMD modules (API unchanged)

- The shared Platform API client split into `kubernetes/kubeapi` and
  `slurm/slurmapi`; common JSON-over-HTTP transport is
  `internal/shared/httpjson`; each module maps its own errors (modules never
  import each other, and core's domain does not depend on HTTP status codes).
- Core's `ReaderFactory` only resolves an Integration to connection details and
  hands off to the builder each module registers with its `platform-reader` hook.
- Kubernetes TLS (DF-01, part one): the k0s credential role returns the cluster
  CA, the Integration records it, and the transport trusts only that CA.
- An `Accelerator` port in the platform domain; the AMD module's `accelerator`
  hook decides which GPUs are AMD compute capacity.
- A `platform-credential` hook; core has one `Record`.
- Moving deployment Job composition into hooks was folded into P5 (user decision,
  2026-11-01).

### P4 Change Plan (decision 076)

- Convergence gained a Change Plan model, repository and `Planner` port;
  `ChangePlanService.Propose` runs inside the spec-write transaction and
  supersedes the previous open plan; Approve and Reject; `ApplyApproved`
  recomputes, waits while held, and writes the Workflow and marks the plan applied
  in one transaction; plans expire (`change-plan-expiry` controller, TTL
  `SWALLOW_API_CHANGE_PLAN_TTL`, default 24 hours).
- Storage: `change_plans` with a unique partial index on `open`.
- API: four routes under `/api/v1/change-plans`; CLI: `swallow plans
  list|get|approve|reject`; dashboard: a `/change-plans` approval screen that
  disables Approve with a reason when a plan has blockers, has expired or is not
  pending.

### P5 Platform desired state

- Change classification (user decision): adding members, toggling monitoring
  features, enabling the GPU Operator, and enabling storage or a runtime for the
  first time are non-destructive; version upgrades, disabling or reconfiguring
  the GPU Operator, and changing or disabling storage or a runtime are
  destructive; network identity (`podCidr`, `serviceCidr`, `apiVip`,
  `clusterName`) is immutable (409). Recorded in `desired-state-model.md` §5.2.
- Model: Platform has a spec (core fields `members` with module-defined roles and
  `desired` present/absent; module `settings`) and a generation; status carries
  `observedGeneration`, conditions, observed membership, and a lifecycle derived
  from Workflows and the spec.
- A `PlatformModule` port: validate the spec (including immutable fields),
  classify a spec change into planned actions, and choose the realizing Workflow.
- Writes carry the expected generation; non-destructive changes are written and
  converged by the controller; destructive ones write the spec and propose a plan
  in the same transaction and return 202 with the plan. Platform Claims derive from
  spec members in the same transaction; `ReconcilePlatformClaims` was removed.
- The worker's platform controller adopts existing Platforms from their latest
  deployment Workflow (generation 1, no provider contact), claims, records the
  realizing generation and Workflow, converges non-destructive changes, applies
  approved plans, and reports membership drift. A failed generation is not
  restarted.
- Scope (user decision): the model is complete while automation is reused.
  Creation and uninstall go through Change Plans; adding members converges
  through deploy; GPU Operator changes reuse the existing Workflow; removing
  members, changing node roles, version upgrades and the other destructive changes
  can be written and planned but carry the blocker "swallow cannot execute this
  change yet" (their automation is AR-24).
- Implementation trade-off: the generic API's spec is translated by the module
  hook into the existing `DeployPlatformInput`, reusing validation and Workflow
  composition to reduce rewrite risk; moving composition into modules continues
  after that boundary (AR-25).
- Cloud-init user data embedded in the spec is sealed with the credential key; a
  spec write that omits it keeps the stored value.

### P6 Dashboard module UI

- Module UI lives in `dashboard/src/modules/<kind>/<id>/`, each exporting a UI
  manifest registered statically in `src/modules/registry.ts`; core reaches modules
  only through the registry (lint rule). Module-specific API adapters also live in
  the module folder, bound by `src/di`.
- The deploy wizard is a shell (Site, name, candidate Servers, OS preparation,
  clock check, submit); type-specific parts come from each module's form hook; the
  AMD module contributes GPU Operator features to Kubernetes.
- Schema-driven forms reuse the manifest's `spec` with a small JSON Schema subset
  (root `type: object` with `additionalProperties: false`; string, integer,
  boolean and string-list properties, each with a `title`; description, examples,
  enum, pattern, minimum, maximum; unknown keywords rejected at load; empty string
  means unset). For Software modules with a schema, the composition root checks
  the properties against the catalog's `specFields` and validates against the
  schema before the module's `ValidateSpec`. Read-only `GET /api/v1/modules`
  returns kind, ID, version and the schema. NFS was the first user.
- Job labels are registered in the plan catalog (no migration: old declarations
  without a label read the catalog, unknown names use title case); the Workflow
  API provides `jobs`, and the dashboard stopped deriving Jobs from Task IDs.

### P7 Close-out

- Documentation: the module authoring guide gained Platform hooks, Job labels and
  dashboard screens; architecture and deployment specs were synchronized.
- `.github/CODEOWNERS`: every module has one line in api-server and one in the
  dashboard (all assigned to `@maple52046` for now, user decision); a test checks
  every compiled module has an owner.

### Module authoring guide (DF-22)

- `docs/development/module-authoring.md` (Traditional Chinese) covers module
  layout, `ANSIBLE_ROLES_PATH` limited to the module and `shared/`, unique role
  names and playbook file names across modules (a new test enforces file names),
  manifest registration, package hold and retries, content tests and helpers;
  later phases added the manifest, Go hooks, CODEOWNERS and dashboard screens.
- The old-to-new path mapping lives in the api-server architecture spec's Layout
  section.
- `scripts/check-docs.py` checks backtick repository paths in current specs (ADRs
  and plans excluded; deliberate historical paths are explicit exceptions with a
  reason).

### OS package maintenance hold (decision 073)

- User choice: while swallow automation runs, pause the OS's automatic package
  maintenance and resume it afterwards; never disable it permanently and never
  rely only on waiting for the lock.
- Shared role `modules/shared/roles/package_maintenance_hold/` with a script
  (`acquire <id> <ttl> <wait>`, `release <id> <grace>`, `expire`) serialized with
  `flock`; state in tmpfs (`/run/swallow/package-hold/`) records which timers
  swallow stopped and each lease's expiry.
- Only runtime `systemctl stop/start` (no disable or mask, never interrupting a
  running upgrade); a `systemd-run` transient timer expires the latest lease as a
  safety net; waiting for in-flight maintenance (apt-daily, apt-daily-upgrade,
  dnf-automatic, dpkg lock holders) times out with `[package_maintenance_busy]`;
  `DPkg::Lock::Timeout "600"`; `dnf-makecache.timer` is also held on Rocky.
- Release waits a 10-minute grace to bridge consecutive Tasks of one Workflow;
  multiple leases support concurrent Tasks.
- Every shipped playbook that changes packages acquires in its first play and
  releases in its last; package tasks retry (`until`/`retries`); a content test
  enforces both.

## Architecture and Design Principles

- Core lists no provider or plugin; it asks registries built from manifests and
  adapter registrations.
- Provider adapters answer "how core talks to an external system"; extension
  modules answer "what swallow can manage".
- Modules depend only on core contracts and shared packages, never on each other.
- Destructive changes are previewed and approved as Change Plans; non-destructive
  ones converge on their own.
- Module UI is composed through a registry, not imported by core screens.
- Host automation never fights the OS for the package manager: it holds
  maintenance for the duration and retries.

## Functional Scope

Delivered and lab-verified:

- Module registry, manifests and conformance tests for all Software kinds,
  Kubernetes, Slurm and AMD.
- Provider kind registration, host-automation runner naming, Boot ISO rendering
  through a port.
- Software modules with hook-provided catalogs, the Docker Host Explorer in its
  module, registry-driven Workflow kinds.
- Kubernetes and Slurm readers and credentials through hooks; Kubernetes TLS with
  a recorded CA; AMD accelerator classification.
- Change Plans in API, CLI and dashboard.
- Platform desired state with adoption of existing Platforms, spec-derived claims,
  convergence, drift reporting, and the generic Platform API.
- Dashboard module registry, wizard shell, schema-driven settings, Job labels.
- Package maintenance hold in every package-changing playbook.

The P7 lab regression covered Software install and removal across kinds, k0s
created and uninstalled through Change Plans with TLS-verified exploration, k0s
with the AMD GPU Operator (device-plugin, then DRA through a destructive plan),
and Slurm with DME.

## Constraints and Rules

- Already-persisted identifiers (Workflow kind, completion type, Task ID, trusted
  var, Platform document fields) do not change; changing one needs a schema
  migration.
- Module hooks do no I/O inside a transaction.
- All three processes build the registry with the same constructor.
- Core vendor-term counts per file may only fall; a new platform or vendor name in
  core raises the baseline only in the commit that adds it, listing the files.
- Transitional placement until composition moves into modules is defined in
  `core-and-extensions.md` §3.6.
- Every commit passes Go (gofmt, vet, tests with Mongo), contracts (OpenAPI bundle,
  `contract:check`), dashboard (lint, build, e2e), CLI tests and `check-docs.py`;
  nothing is pushed without the user.
- Lab: only `lab-*` machines; announce which machines a drill uses before acting.

## Data Model and Format Notes

- Platform API shape: `{id, siteId, name, type, generation, spec, status}`; spec
  `members` (Server ID and roles), `desired`, module `settings`; lifecycle state
  gained `planned`.
- Change Plan: stored in `change_plans`, one open plan per resource; actions,
  blockers, expiry; applied plans record the Workflow.
- Module manifest: root Go package, Workflow kinds, completions, playbooks,
  optional `spec` schema (the subset above); definitions recorded as
  `<kind>/<id>@<version>`.
- Job declarations carry a `label` (no bson tag; the driver default key, guarded by
  a Mongo round-trip test).
- Package hold state: tmpfs files for stopped timers and leases.

## CLI / API / Config Notes

- `POST /api/v1/platforms` (spec, returns 202 with an install plan),
  `PUT /api/v1/platforms/{id}/spec` (generation; 200, or 202 with a plan; 409 for
  immutable fields, unsupported changes or generation conflicts),
  `DELETE /api/v1/platforms/{id}` (absent plus removal choices, returns 202 with an
  uninstall plan; 204 for a Platform never approved for install;
  `?recordOnly=true` deletes only the record). Removed: `/deploy`,
  `/{id}/uninstall`, `PUT /{id}/gpu-operator`. Module routes (Kubernetes explorer,
  Slurm cluster, GPU Operator releases) stayed.
- `/api/v1/change-plans` routes; `GET /api/v1/modules`; the Workflow API's `jobs`.
- CLI: `platforms create`, `set-spec`, `delete` flags, GPU Operator through the
  spec; `plans list|get|approve|reject`.
- Config: `SWALLOW_API_CHANGE_PLAN_TTL` (default 24h).

## Implementation Plan

Implemented, with these commits:

- P0: `7896392`, `00d11a0` (guide and path checks), `bc40f13` (ratchet),
  `f27a8d7`, `96ec37a`, `389cfce`.
- P1: `91306ba`, `9fff132`, `6b15e02`, `0b09b3f`.
- P2: `624b70a`, `8a3b8ae`; `d0693df` fixed Docker CE replacing docker.io.
- P3: `88589ee`, `3b50cb1`, `c27c7af`, `6be6e17`; fixes `02d9020`, `195ed29`,
  `ee91362`.
- P4: `2743238`, `0a99fe4`, `1b41315`, `05e5154`.
- P5: `c3ea216`, the generic Platform API (`4e9c466`), lab fixes `3481717`,
  `c0c60ef`, `11d587f`.
- P6: `eff511e`, `bd42773`, `d00ff5e`, `0d1ca7f`, `68e9622`, `7775d94`.
- P7: `5d7f0f9`, `314870c`, `c515040`, `9521749`.
- Package hold: decision 073 and commit `504db84`.

Remaining (tracked in the v2 tracker): AR-24 (automation of the blocked Platform
changes), AR-25 (Platform and AMD composition and trusted vars into modules),
AR-26 (Platform settings schemas, classification and catalog as data, lifecycle
from conditions), AR-27 (module API adapters and Software screens in modules, the
fixed `SoftwareKind` list), AR-28 (distribution assembly and support matrix),
AR-07 (`Attach*` and nil-skipped claims), AR-20 (remaining vendor concepts in
core), AR-22 (declarative Workflow templates), DF-01 (reader permissions), DF-25,
DF-38, DF-39, DF-40, DF-41, DF-43, DF-44, DF-51.

## Non-goals

- Server stage 2 resources (owned by the desired-state migration).
- A Software catalog field in the manifest (needs a new ADR).
- Rewriting Platform deployment in one step; composition moves after the
  `DeployPlatformInput` boundary.
- Out-of-process hooks or externally owned modules (stages 2 and 3).
- Permanently disabling OS package maintenance.

## Open Questions

- Whether destructive classification and the Software catalog become manifest
  data (needs an ADR; decision 075 is frozen).
- Reader ServiceAccount permissions for swallow-deployed Kubernetes (still
  `cluster-admin`), to be reviewed with what the explorer needs.
- Target OS images need Python 3.8 or later for ansible-core 2.18 (a Rocky 8.10
  image with only Python 3.6 cannot run any playbook); not yet documented or
  checked (DF-51).

## Future Work

- AR-22 declarative Workflow templates, then AR-25 moving Platform and AMD
  composition into module hooks (with AR-06 and AR-07).
- AR-24 Platform change automation (drain and remove k0s nodes, Slurm role
  changes, k0s upgrades).
- AR-26, AR-27, AR-28 module completeness.
- AR-18 and AR-19 when their triggers occur.
- Lab coverage still missing: k0s HA with three disk-deployed control planes,
  standalone DME, Omnistat (DF-43).
