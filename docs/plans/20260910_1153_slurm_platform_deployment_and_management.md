# Slurm Platform Deployment and Management

## 1. Purpose

Consolidate three related AI plan manuscripts into one long-term reference covering
swallow's Slurm platform work: making HA Slurm deploy end-to-end without manual
storage setup, giving each platform type its own management view (with a live
Slurm-native read), and optimizing the uninstall+release lifecycle. It preserves the
architectural decisions, constraints, and open questions so later work does not
re-derive them. It is a planning record, not a re-implementation spec; several items
are already implemented (noted where relevant), but the reasoning is retained.

## 2. Source Scope

Consolidated from `docs/plans/manuscripts/` (all dated 2026-09-09; `README.md` is the
spec, not a source):

- `20260909-slurm-ha-shared-state.md` — swallow-provisioned HA shared
  `StateSaveLocation`; also carries the primary-first controller-start fix, the
  dedicated Stderr tab/endpoint, and the platform reader connection-leak fix.
  Implemented and verified from 0 on a 3-controller/4-compute lab cluster.
- `20260909-platform-specific-management.md` — platform-type-specific management UI
  plus a backend Slurm-native cluster read (controllers/partitions/nodes).
- `20260909-uninstall-release-shortcut.md` — uninstall+release releases servers
  directly and skips the redundant platform-software uninstall step.

Related decisions: ADR 019 (Slurm deployment), ADR 021 (platform-type-specific
management), ADR 022 (uninstall+release shortcut), ADR 023 (swallow-provisioned Slurm
HA shared state); `docs/development/platform-deployment.md`; the reference skill
`vanguard/skills/deploy-slurm` and `knowledge/Slurm/*`.

## 3. Consolidated Background

- **HA Slurm could not form.** A multi-controller deploy left every `slurmctld` DOWN
  with `get_last_heartbeat: heartbeat open attempt failed from
  <StateSaveLocation>/heartbeat`. HA requires all `SlurmctldHost` to share one
  `StateSaveLocation`, but swallow did not provision shared storage and only
  *required the operator to hand-supply* a path — a manual half-step, not a working
  mechanism.
- **A second, order-dependent failure.** Even with shared state, the `serial: 1`
  controller play started a backup before the primary (group_by does not preserve
  primary-first order). The backup came up in standby, waited forever for a heartbeat
  the not-yet-configured primary could not write, and its `scontrol ping` aborted the
  run before the primary was ever given `slurm.conf`. This same ordering bug also
  broke the earlier local-state deploy.
- **Diagnostics were opaque.** A failed step surfaced only `ansible-runner: exit
  status N` with no visible error/stderr, so operators could not tell why a deploy
  failed.
- **Membership sync stalled ~1h after a good deploy.** The uncached, per-read
  platform reader used a keep-alive `http.Transport` that was never closed, leaking
  one connection per 30s sync into slurmrestd's ~124 concurrent-connection cap; once
  full, slurmrestd deferred all new connections and reads/syncs timed out with
  "Could not reach the Slurm API".
- **One k0s-shaped detail page misrepresented Slurm.** A single `PlatformDetailPage`
  forced both types through the same summary + member table, and its `State` column
  conflated scheduler/membership state, host power, and monitoring health.
  `slurmctld` controllers are not scheduler nodes, compute `role` is really a
  partition, and a healthy controller with no exporter read as "down".
- **Uninstall+release did wasted, fragile work.** Uninstall always ran the
  `uninstall-platform` ansible step first; when releasing servers (which wipes the
  OS) that step is pointless and can fail on half-gone nodes.

## 4. Confirmed Decisions

- **swallow provisions the HA shared `StateSaveLocation` itself** (lab-grade): a
  managed single-server NFS export, mounted on every controller with a fail-closed
  mount guard before `slurmctld` starts. Mirrors the reference `ha-controller.yml`
  (`slurm_state_server` + `slurm_controller_state` in `shared` mode).
- **State-server placement is auto-selected**, no new topology role: prefer a
  compute-only node (`compute && !controller`) so controller state lives off the
  controllers and there is no NFS loopback on a controller; fall back to the primary
  controller when every node is also a controller. The state server is a single
  storage failure domain (documented lab limitation, not storage HA).
- **`stateSaveLocation` is no longer required for HA.** It stays an optional override
  of the state directory path. Because swallow always auto-provisions and the field
  cannot express external source/fstype, the deploy wizard does **not** surface it;
  it remains only as an optional API field.
- **Controllers start primary-first.** `deploy-slurm.yml` runs the primary in its own
  play (`hosts: slurm_primary`) and backups in a following play
  (`hosts: slurm_controller:!slurm_primary`, `serial: 1`), rather than relying on
  serial order within one controller play.
- **Meaningful failure diagnostics.** The runner enriches a failed run's error from
  its job events (failing task, host, message, rc, bounded stderr/stdout), and a
  dedicated error-only Stderr view/endpoint is exposed; the enriched detail is not
  put into the public task-event projection (kept output-free).
- **Per-reader transports disable keep-alives.** `platformapi.newReaderTransport`
  sets `DisableKeepAlives` for both `SlurmReader` and `KubernetesReader`; an uncached,
  short-lived reader has no reuse to protect and must not leak connections.
- **Platform management is a shared shell + per-type body views**
  (`KubernetesPlatformView` / `SlurmPlatformView`); shared functions stay in the
  shell, type-specific management lives in each view.
- **A live Slurm-native read is added** (on-demand, read-only) so the Slurm view
  shows real controllers (slurmrestd ping + order), partitions, and compute node
  states, kept separate from the membership-sync axis and deployment-intent
  projection.
- **Health is a distinct axis**, removed from the `State` column for now (monitoring
  not integrated), with a seam for a future dedicated Health column; scheduler state
  and controller ping status are never conflated with host power/health.
- **Uninstall+release releases servers directly** and skips the `uninstall-platform`
  ansible step; a small internal `complete-uninstall` step does the projection
  cleanup after the releases succeed. This shortcut is confined to whole-platform
  uninstall.

## 5. Architecture and Design Principles

- **Three data sources must stay separated** for a platform's read/management view:
  1. Deployment intent (operation-history projection) — requested topology / role
     assignments.
  2. Membership sync (`ListMembers` → `Server.membership`) — for Slurm, compute
     (`slurmd`) nodes only.
  3. Live platform read (Slurm: slurmrestd `/ping`, `/partitions`, `/nodes`) —
     controllers, partitions, node scheduler states.
  A fourth monitoring/health axis is separate and deferred; it never appears in the
  State column.
- **Clean layering** (mirror the on-demand provisioner-detail pattern): domain port +
  types → infra REST reader → application use case → delivery handler/route → contract
  first.
- **Deployment mechanism owns HA storage.** Making HA work is swallow's
  responsibility, not an operator prerequisite; the playbook provisions and mounts
  shared state and fails closed if the mount is missing.
- **Fail-closed mount guard.** A controller must never write state to a local
  directory under an unmounted path; the `slurmctld` drop-in requires the mount to be
  a real mountpoint before start.
- **Completion is tied to a real step.** v3 platform completion runs from a step
  observer; when the ansible uninstall step is dropped on the release path, an
  internal finalize step preserves the projection cleanup.

## 6. Functional Scope

- HA Slurm deploy provisions shared `StateSaveLocation` automatically and forms a
  working active/standby cluster from 0 (verified: all `slurmctld` UP, partition up,
  nodes idle).
- Single-controller deploy keeps `StateSaveLocation` local (mode `local`, no NFS,
  no mount guard).
- Failed operation steps surface a concise enriched error plus a dedicated Stderr tab
  (error-only report) alongside the existing Stdout log.
- Platform detail page renders a shared shell with a Kubernetes or Slurm body view by
  `platform.type`; the Slurm view shows controllers, partitions, and compute nodes
  from the live read, degrading to intent + membership when the live read is
  unavailable.
- Uninstall+release releases the member servers directly and finalizes via an internal
  step; keep-servers uninstall is unchanged.

## 7. Constraints and Rules

- **No new Ansible collection.** The executor image installs only pip deps and
  playbooks must stay air-gap-friendly; the NFS mount uses `ansible.builtin` (fstab
  line + guarded `mount`), not `ansible.posix.mount`.
- **Uniform `slurm` uid/gid** comes from the shared MAAS image, so NFS ownership is
  consistent without forcing a numeric uid; the export uses `root_squash`.
- **Uninstall shortcut is whole-platform only.** A future scale-in (removing specific
  nodes while the platform runs) must still uninstall the node first; the change is
  confined to `LaunchUninstall`.
- **Exporter restoration stays skipped on the release path** (meaningless on a wiped
  host), matching the uninstall use case's existing condition.
- **Contract-first.** Update the platforms/workflows API contracts before or with
  implementation.
- **Coding-style gate.** `gofmt`/`go build`/`go vet`/`go test ./internal/...` and the
  documentation gate for changed Go; dashboard `tsc`/`eslint`.
- **Do not conflate axes.** State column = scheduler/membership state (and controller
  ping) only; never host power or monitoring health.

## 8. Data Model and Format Notes

- **`SlurmClusterState`** (domain type behind the live read):
  - config: clusterName, stateSaveLocation, ordered controller hosts;
  - controllers: name/address/index/primary/`up|down|unknown`;
  - partitions: name/nodes/state;
  - nodes: name/state/cpus/realMemory/gres/partitions/address.
  `PlatformReader.ListMembers` is unchanged.
- **`SlurmDeploymentSpec.StateSaveLocation`** is optional: single controller uses a
  controller-local default; HA gets a swallow-provisioned shared directory; when set,
  it overrides the directory path in either mode.
- **Trusted vars** (`swallow_slurm_*`): existing `controller_ids` /
  `primary_controller_id` / `high_availability`; new
  `controller_state_mode` (`local`|`shared`), and for HA `state_server_id` and
  `state_export`; `state_save_location` remains optional.
- **Slurm state flags** collapse to one word; drain/down/fail flags take precedence
  over a base IDLE state.

## 9. CLI / API / Config Notes

- `GET /api/v1/platforms/:id/slurm` — live Slurm cluster read (controllers,
  partitions, nodes); typed error when the platform is not Slurm or has no
  integration; reuse reader-error mapping.
- `GET /api/v1/workflows/:id/tasks/:taskId/stderr` (+ `/operations` alias) — error-only
  report (failed/unreachable tasks with message/rc/stderr/stdout); empty body for a
  non-Ansible task or no failure.
- Deploy request `slurm.stateSaveLocation` is optional and not surfaced in the wizard;
  the wizard shows an info note that HA shared state is auto-provisioned.
- Reader HTTP client: keep-alives disabled per reader (`newReaderTransport`);
  integration settings still include `timeout`, `insecureSkipVerify`,
  `slurmApiVersion`.
- `deploy-slurm.yml` `pre_tasks` set `slurm_controller_state_mode`,
  `slurm_state_export`, `slurm_state_source`, `slurm_state_fstype/options`, and
  `slurm_state_save_location` default per mode.

## 10. Implementation Plan

Slurm HA shared state (ADR 023):
- Ansible: add `slurm_state_server` (NFS export restricted to controllers) and
  `slurm_controller_state` (`local`/`shared`; builtin mount + systemd mount guard);
  wire `deploy-slurm.yml` with a state-server play then a controller-state play before
  the controller-config play; `slurm_config` stops managing the state dir;
  `slurm_preflight` asserts HA shared-state inputs.
- Controller start: split the controller-config play into primary-first then backups.
- Go: `deploy_platform.go` drops the HA "stateSaveLocation required" validation, adds
  the state-server selector and the new trusted vars; tests updated + a Slurm topology
  playbook test.

Diagnostics:
- Runner builds an enriched failure summary from job events; add a `Stderr` runner
  method + `StderrForRun` service + `StepStderr` handler/route + contract; dashboard
  Stderr tab (failed step auto-opens on it) with the Alert reduced to a headline and
  the Reason cell truncated.

Reader connection leak:
- `platformapi.newReaderTransport` with `DisableKeepAlives`; used by both readers;
  regression test asserts keep-alives disabled; remediation is a slurmrestd restart to
  drop leaked connections.

Platform-type-specific management (ADR 021):
- Backend: domain `SlurmClusterReader` port + `SlurmClusterState`; infra
  `platformapi/slurm.go` `GetClusterState()`; expose via `factory.go`; application
  `GetSlurmClusterUseCase`; delivery route; contract-first.
- Frontend: thin `PlatformDetailPage` into a shell; `KubernetesPlatformView`
  (unchanged behavior) and `SlurmPlatformView` (controllers/partitions/nodes/config,
  degrade path); `getSlurmCluster` adapter + `useSlurmCluster` hook; decouple health
  from the State column.

Uninstall+release (ADR 022):
- `LaunchUninstall` branches on `ReleaseServers`: parallel `release-<serverID>` steps
  + one internal `complete-uninstall` finalize step; no ansible uninstall step on the
  release path. `platform_workflow_step_executor.go` handles `complete-uninstall` via
  a narrow finalizer satisfied by `*PlatformService`, wired in `worker.go`.

## 11. Non-goals

- Production-grade / external / redundant HA shared storage (current is lab-grade
  managed NFS with a single failure domain).
- SlurmDBD/accounting, GRES/GPU scheduling views (QOS/Reservations/Accounts),
  login/submit roles.
- Type-specific write actions (drain/cancel), a live jobs list, monitoring/health
  integration, multi-cluster.
- Scale-in/out: still runs the full uninstall flow; the release shortcut does not
  apply. No dashboard change and no change to the keep-servers uninstall path.

## 12. Open Questions

- How should an operator supply **external / pre-mounted** shared storage? A bare path
  cannot express it; it needs source (`host:/export`) + fstype + options + a mode
  (managed / external / pre-mounted), plus an `external` mode in
  `slurm_controller_state`. Not designed yet.
- When and how to integrate the **monitoring/health axis** into the platform views
  (the reserved Health column).
- Whether the live jobs list (`/jobs`) is added to the Slurm view (noted as an easy
  follow-up, currently deferred).

## 13. Future Work

- External/production shared-storage support for HA `StateSaveLocation` (the role keeps
  a mode hook for it).
- A dedicated Health column and monitoring integration in the platform detail views.
- Slurm jobs list and, later, SlurmDBD-dependent views and type-specific write actions.
- Scale-in/out flow that uninstalls a node before releasing it.
