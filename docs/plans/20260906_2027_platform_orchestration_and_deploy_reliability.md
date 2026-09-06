# Platform Orchestration and Deployment Reliability — Consolidated Plan

## 1. Purpose

Provide one long-term plan for swallow's platform-lifecycle subsystem: the durable
Temporal-backed orchestration engine (Workflow → Job → Task → Runner), the
definition/ubiquitous-language reorganization that grounds it, and the sequence of
reliability fixes that together make **deploying a k0s platform from the dashboard
work end-to-end with no manual steps**. It consolidates ten dated manuscripts into a
single reference of confirmed decisions, constraints, boundaries, and open items —
not a concatenation.

## 2. Source Scope

Consolidated from `docs/plans/manuscripts/` (10 files, newest decision wins on
conflict):

- `20260904-platform-orchestration.md` — Cluster→Platform rename + durable Temporal
  orchestration re-land (Phases A–G).
- `20260904-uninstall-with-release.md` — platform uninstall with optional server
  release, as one durable Operation.
- `20260905-platform-definition-reorg.md` — `docs/` definition reorg; ADR 015/016/017
  (swallow vs Platform, Temporal, Workflow/Job/Task/Runner).
- `20260905-durable-provisioning-error-mapping.md` — map operation-layer errors on
  durable provisioning endpoints (no opaque 500s).
- `20260905-release-cancel-running-operations.md` — Release dialog cancels running
  Operations first (frontend-orchestrated).
- `20260905-delete-platform-cancels-operations.md` — deleting a platform cancels its
  in-flight Operations and frees leases.
- `20260905-recover-failed-deployment-on-ready.md` — reconcile self-heals a stale
  terminal deployment badge on a Ready server.
- `20260906-automatic-addressing-auto-assign.md` — `automatic` addressing via provider
  auto-assign (MAAS `AUTO`); ADR 018.
- `20260906-server-list-live-updates.md` — SSE per-Server change stream + 30s reconcile.
- `20260906-k0s-deploy-end-to-end.md` — host-key auto-scan, job-retry attempt bump,
  ephemeral guard; verified 7-node HA cluster.

## 3. Consolidated Background

swallow is a monorepo with two components (`api-server` Go backend, `dashboard`
React/TS frontend) sharing a root `docs/` model. Platform lifecycle work exposed two
intertwined problems:

1. **Language/definition drift.** "Platform" was overloaded (the whole system vs. the
   `Platform` aggregate), two glossary trees coexisted, constitutional docs used
   submodule-era language, and the Cluster→Platform rename was incomplete. This made
   the system "feel messy" and blocked consistent implementation.
2. **Deployment didn't actually work end-to-end.** A durable-orchestration checkpoint
   (`1487f1e`) bundled a rename with a Temporal subsystem and shipped audit-level
   safety defects. Even after re-landing it, real lab runs kept failing: stale
   deployment badges, orphaned operation leases, opaque errors, addresses that "never
   appeared," slow list updates, manual `known_hosts` maintenance, retries that
   replayed cached failures, and ephemeral OS images that can't host a cluster.

The through-line: prior "passing" claims were unit/mock e2e, not real end-to-end.
The standard now is to validate real flows against the dev lab and show evidence.

## 4. Confirmed Decisions

Owner-confirmed, newest-wins:

- **Naming (ADR 015).** Capital-P `Platform` = the domain aggregate (a k8s/slurm
  runtime). The system itself is always called `swallow`; no "monorepo/control
  plane/product" aliases as its name.
- **Single glossary tree.** `docs/development/glossaries/terms/` is canonical; the old
  `docs/glossaries/` tree was merged in and deleted.
- **`Allocation State`.** Canonical value set is `free | team | user` (API contract);
  dashboard's `free | assigned` is the deviation to align.
- **Temporal (ADR 016).** Accept Temporal as an external runtime dependency for durable
  orchestration with per-resource fencing leases; supersedes ADR 006's engine/site-lease.
- **Workflow model (ADR 017).** **Workflow → Job → Task**, executed by **Runners**.
  Execution is **route A — idempotent "ensure" convergence**: a deploy may mix
  already-provisioned and unprovisioned servers and converge each. Boundary rule:
  cross-Runner/reusable ⇒ a swallow-owned Task; single-platform host config ⇒ one
  idempotent playbook (ansible-owned). Inventory is swallow's published language to
  platform playbooks; a new platform = a playbook + trusted-vars contract + manifest.
- **Automatic addressing (ADR 018).** swallow's `automatic` intent maps to the
  provider's auto-assign (MAAS `AUTO`) — a stable, provider-recorded IP — not raw DHCP.
  `static` unchanged; `dhcp` is a one-release deprecated alias of `automatic`.
- **Live server list.** Reconcile cadence 60s → 30s; the dashboard uses **SSE push** of
  per-Server change events and patches only changed rows (no whole-list polling).
- **Uninstall with release.** One durable `uninstall-kubernetes` Operation removes k0s
  first, then optionally releases members; a failed release never precedes k0s removal.
- **Delete cancels work.** `PlatformService.Delete` cancels the platform's active
  durable Operations (freeing leases) before removing the record; if cancellation
  fails, delete is refused.
- **Release cancels running work (frontend).** The Release dialog detects active
  Operations and offers opt-in cancel-then-wait, reusing existing endpoints; the
  backend `409` remains the correctness backstop.
- **Self-healing stale deployment.** Reconcile clears a terminal deployment outcome
  (`succeeded|failed|canceled`) when the machine is observed back in the provider's
  `ready` pool; active/`requires_attention` deployments are left untouched.
- **k0s deploy reliability.** The workflow learns target host keys itself; job retries
  bump the task attempt; ephemeral OS is rejected for k0s deploys (see §7).

## 5. Architecture and Design Principles

- **Clean Architecture per component; Strategic DDD across contexts.** Domain owns
  ports; infra adapts; delivery maps transport; app wires. No cross-component reach-in;
  interaction is via the provider-owned API contract and shared `docs/`.
- **Ownership model (ADR 001, integrate-or-own).** Owned data (Sites, Integrations,
  Identity mapping, Policy, Operations/Workflows, Tenancy, Platform records); mirrored
  facts (Server projection, provisioning axis, carrying `source` + `observedAt`); live
  reads (health, membership, alerts — never persisted).
- **Server has no single status** — three independent axes (provisioning/membership/
  health) plus a swallow-owned `Deployment` result (the last OS-deploy verification,
  distinct from the provisioner axis).
- **Durable orchestration.** Parent Operation Workflow; Jobs run as Temporal **child
  workflows** (`swallow.job.v1`) in cross-Job dependency order; a retryable Job failure
  pauses for operator retry and re-runs the idempotent Job. Workflows without Jobs use
  the flat path (histories stay replay-safe). Per-resource fencing leases; Runners key
  idempotency on `operation/task/attempt`.
- **Anticorruption at the provider edge.** swallow intents (`automatic`) map to provider
  capabilities (MAAS `AUTO`); provider protocol names never leak into swallow intent.
- **Single source of truth for definitions.** Constitution = `architecture-spec.md`;
  structure = `codebase-structure.md`; language = glossary; contracts = `api-contracts.md`
  + provider-component contracts; rationale = ADRs. Each concept defined once, elsewhere
  referenced.
- **Host-key trust.** Verification stays enabled; the run learns keys of exactly its own
  targets immediately before connecting (bounded trust-on-first-use), so `provision_os`
  and `existing_os` configure identically.

## 6. Functional Scope

- Durable platform **deploy** (k0s), **uninstall** (± member release), and standalone OS
  **deploy/release** on the durable engine.
- **Convergent deploy:** mix `ready` and `deployed` targets; provision only those needing
  an OS; configure all identically.
- **Automatic/static addressing** with `automatic` = provider auto-assign.
- **Live server list** via SSE with fingerprint-deduplicated per-Server events.
- **Operator recovery ergonomics:** delete-cancels-operations, release-cancels-operations,
  self-healing stale deployment badge, actionable durable-provisioning errors, per-step
  retry.
- **Automatic host-key capture** and **job retry that actually re-runs**.
- **Guardrail:** reject ephemeral OS for k0s platform deploys.

## 7. Constraints and Rules

- **Host-key verification cannot be disabled.** `LocalRunner` still requires a non-empty
  final `known_hosts`; it is now satisfied by scanned target keys merged with any static
  entries. A run with targets but no learnable key and no static entry fails clearly.
- **Ephemeral is invalid for k0s.** `provision_os` + `ephemeral=true` is rejected up
  front (runs from RAM, disks untouched, missing `nf_tables`, no durable etcd/containerd).
- **Job retry must change the idempotency key.** Retried Jobs offset each Task's attempt
  by the Job run number, or the executor returns the cached prior outcome.
- **k0s removal precedes release** in uninstall; a failed release never wipes a server
  before k0s is gone.
- **Delete never orphans work:** cancel active Operations (free leases) before removing a
  platform; refuse delete if cancellation fails. Delete stays non-destructive to hosts.
- **Reconcile ownership:** `Upsert` writes only `source/hardware/observed/provisioning`;
  `membership`/`deployment`/`gpus` are written by their own methods so reconcile can't
  clobber platform/swallow-owned state.
- **`defaultGateway` is static-only.** `automatic` consumes a static IP from the subnet's
  non-dynamic space (which must exist).
- **Lab-only validation.** Validate on `lab-*` VMs; never touch `tainan-*` physical
  machines; never delete MAAS data.
- **Quality gate per change.** api-server: `go build/vet`, `gofmt`, `go test` green +
  coding-style review; dashboard: `tsc`, `lint`, `build`, Playwright + JSDoc/a11y/DRY/
  layering review; delivery: `docker compose config`. Do not claim tests that were not run.

## 8. Data Model and Format Notes

- **Server (Mongo `servers`).** `health` never persisted; `gpus` at document top level;
  `membership`/`deployment` written independently of `Upsert`. Unique key `source_key`;
  sparse `hardware.*`; non-unique `observed.hostname`; `absent` marked, not deleted
  (provider-first delete).
  - `provisioning.State` ∈ `new|commissioning|ready|allocated|deploying|deployed|
    releasing|testing|rescue|broken|failed|retired|unknown`.
  - `Deployment.State` ∈ `deploying|verifying|succeeded|failed|requires_attention|canceled`;
    reconcile clears terminal states when the machine is `ready`.
- **Workflow model rename (schema v4).** Mongo `operations→workflows`, `steps→tasks`,
  `executor→runner`, `operation_events/secrets→workflow_*`, `operationId→workflowId`.
  `Task.Job` groups tasks into Jobs; `JobWorkflowInput.Attempt` is the 1-based Job run
  number used to offset task attempts.
- **Network modes.** Deployment intent = `automatic|static` (`dhcp` deprecated alias→
  `automatic`); observed NIC state enum keeps `dhcp`/`provider_managed`; link mode
  `NetworkLinkAuto` → MAAS `AUTO`.
- **Frozen Ansible inventory** must set `bson:",omitempty"` (nil `children` as BSON
  `null` breaks the script inventory plugin). `known_hosts` for a run = scanned target
  keys + static entries.
- **Migration safety.** `renameCollection` with copy/verify/drop fallback for non-admin
  envs; count verification on re-run; no hardcoded CLI timeout; documented rollback.

## 9. CLI / API / Config Notes

- **Canonical routes:** `/api/v1/workflows` (+ `/tasks`), with `/operations` a deprecated
  alias; `/api/v1/platforms` canonical, `/api/v1/clusters` deprecated with deprecation
  headers; `clusterId`/`platformId` and overview `clusters`/`clustered` aliases documented.
- **SSE:** `GET /api/v1/servers/stream` (`text/event-stream`, admin-only, optional
  `siteId`), `ready` event then one `message` per `ServerEvent`, heartbeat comments;
  EventSource auth via short-lived `access_token` query param (`BearerTokenFromQuery`).
  Contract `servers-stream.md`.
- **Durable provisioning errors:** map operation-layer errors — busy/locked/invalid-state
  → `409`, validation → `400`, durable unavailable → `503`; only genuine
  repository/persistence failures keep `500`.
- **Uninstall body:** optional `{releaseServers, releaseOptions:{erase, secureErase,
  quickErase, unbindStaticIps}}`.
- **Deploy guard:** `provision_os` + `settings.ephemeral=true` for a Kubernetes platform
  → `400` with a clear message.
- **Config:** `SWALLOW_API_RECONCILE_INTERVAL` default 30s across `config/defaults.go`,
  `deploy/dev/compose.yaml`, `deploy/dev/.env.example`, `api-server/docs/config-example.yaml`.
- **Delivery topology:** Temporal + PostgreSQL Compose services and `temporal-db-password`
  secret across testing/production/CI; native/air-gap Temporal packaging still pending.

## 10. Implementation Plan

Status: the bulk is **implemented and lab-verified**; remaining items are packaging and
optional hardening.

1. **Definition reorg (done).** ADR 015/016/017; single glossary tree; constitutional
   language aligned to monorepo/component; aggregate naming; dead-link sweep.
2. **Rename + safe migration (done, Phase A).** Cluster→Platform across backend/frontend/
   docs; schema-v3 rename migration with verification and rollback.
3. **Durable orchestration re-land (done, Phases B–G).** Temporal foundation, durable
   Ansible Runner (queue/idempotency/observe/cancel; renew-failure → requires_attention),
   provider Runner (re-check lock + revalidate lease fencing before each host mutation;
   classify provider errors), end-to-end deploy with per-target validate, v3 uninstall +
   auto-exporter, cancellation completeness, active-operation unique index, redacted
   public API, dashboard durable UI.
4. **Workflow/Job/Task/Runner adoption (done).** Rename to schema v4; per-server
   `ensure-os` convergence; Jobs as child workflows; platform deploy emits `ensure-os` +
   `configure-k0s`; v2 dispatcher/site-lease removed.
5. **Operator ergonomics (done).** Delete-cancels-operations; release-cancels-operations;
   self-healing stale deployment on ready; durable-provisioning error mapping;
   uninstall-with-release.
6. **Addressing + live list (done).** `automatic`→MAAS `AUTO` (ADR 018); reconcile 30s +
   SSE per-Server stream with fingerprint dedup.
7. **Deploy reliability (done, this cycle).** Host-key auto-scan in the Ansible Runner
   (`HostKeyScanner`/`resolveKnownHosts`); Job-retry attempt offset in `JobWorkflowV1`;
   ephemeral guard in `DeployService.validate`; tests for all three.
8. **Verification (done).** Fresh `provision_os` + `automatic`, HA (3 CP + 4 workers),
   VIP `.249/24`, `ubuntu/noble`, `ephemeral=false`, with a **dummy** site `known_hosts`:
   workflow `succeeded`, platform `active`, 3-member HA etcd, `/readyz: ok`, VIP up, 4
   workers `Ready`, 0 unreachable — zero manual steps.

## 11. Non-goals

- Renaming component code/API routes and Mongo beyond what ADR 015/017 already covered;
  general code-comment/`platform`-string cleanup; monitoring `cluster`/`clusterId` alias
  removal (one-release compatibility).
- Multi-host Temporal/PostgreSQL/executor HA, DR drills, object-storage artifacts,
  approval steps, and quota subsystems.
- A server-enforced "cancel active work then release" API flag (frontend orchestration +
  `409` backstop suffices).
- WebSockets or server-side per-keyword stream filtering (client-side patch).
- A repo-wide audit of every generic `Internal error.` fallback.

## 12. Open Questions

- **Long-lived EventSource token refresh.** Today a long-lived stream whose token expires
  resyncs only on manual reload/navigation; a refresh path is undecided.
- **Server-side stream filtering.** Keyword/provisioning-state filtering is client-side;
  whether to push it server-side is open.
- **`PutAutomation` known_hosts requirement.** With auto-scan it could become optional;
  left required for now (scope), to be decided.
- **Native/air-gap Temporal packaging.** systemd/bundle topology for air-gapped installs
  is recorded but unresolved.
- **Component-doc/code alignment to ADR 015.** `api-server` contract wording and code
  comments still say "platform" for swallow in places; alignment deferred to component
  maintenance.

## 13. Future Work

- Dashboard mirror of the backend ephemeral guard (disable/warn "Ephemeral deployment"
  for Kubernetes platforms).
- Relax `PutAutomation` to accept an empty `known_hosts` once auto-scan is the norm.
- Remove the `dhcp`/`cluster`/`clusterId`/`/operations`/`/clusters` deprecated aliases
  after their one-release windows.
- Package Temporal for native/air-gapped delivery; broaden HA/DR beyond single-host dev.
- Add server-side stream filtering and an EventSource token-refresh path if operational
  need appears.
- Onboard additional platform types (slurm/storage) via the playbook + trusted-vars +
  manifest contract established by ADR 017.
