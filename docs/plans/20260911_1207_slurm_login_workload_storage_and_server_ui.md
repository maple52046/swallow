# Slurm login + workload storage, and dashboard server-list status UI

## 1. Purpose

Consolidate the current AI plan manuscripts into one long-term reference. They fall into two
coherent clusters: (a) a Slurm deployment feature adding a login-node role and a configurable
shared workload filesystem, and (b) a set of dashboard server-list presentation changes for
power state and deployment progress. This preserves the decisions, constraints, and boundaries
so later work does not re-derive them. It is a planning record; several items are already
implemented (noted inline).

## 2. Source Scope

From `docs/plans/manuscripts/` (dated 2026-09-10/11; `README.md` is the spec, not a source):

- `20260910-slurm-storage-config.md` — Slurm login role + workload shared storage
  (self-hosted / external NFS); two topologies. Status: implemented (ADR 024). Its earlier
  "/home overlay" and "workload external-only, no login" drafts are superseded by the final
  scope below.
- `20260910-server-list-power-icon.md` — render server power state as lucide icons.
- `20260910-server-list-power-actions-dialog.md` — make the power icon a button opening a
  power-actions dialog.
- `20260910-deployment-in-progress-stripes.md` — animated "in-progress" stripes on deployment
  status labels.

## 3. Consolidated Background

- Slurm previously modeled only controller/compute daemons and provisioned only the controller
  `StateSaveLocation`. The knowledge base separates two distinct storages that must not be
  conflated: controller `StateSaveLocation` (slurmctld-only, HA failover) and the **workload
  filesystem** (user/job data, consumed by login and compute). swallow lacked a login role and
  workload storage; it needed to support both a single-controller topology and an HA topology
  where a login host serves the cluster's NFS.
- The dashboard's `ServersPage` and `ServerDetailPage` show provider/deployment/power axes via
  shared badges (`DeploymentBadge`, `PowerBadge` in `AxisBadge.tsx`). Power state was raw text;
  the power column was non-interactive; and an in-progress deployment had no motion cue. These
  are presentation refinements over existing domain state, not new domain concepts.

## 4. Confirmed Decisions

Slurm (final scope, superseding the historical draft):
- Add a `login` (submission/client) role; a login-only node is valid; it runs no cluster daemon
  and may host the NFS exports.
- Add an optional workload shared filesystem (`slurm.workloadStorage`), distinct from controller
  state, mounted on every node at a non-overlapping path (default `/shared`, never `/home`).
  Modes: `self-hosted` (the login node exports NFS) or `external` (operator `nfs.url`); type
  `nfs`.
- In HA, the controller-state server is the login node when one is assigned (else an
  off-controller node). Controller-state storage stays auto-provisioned (not operator-configured).
- Supported topologies: single controller + N compute; 1 login + N controllers (HA) + N compute
  (login serves the cluster NFS).
- Superseded earlier draft decisions: mounting workload at `/home` (rejected — hides the SSH
  user's home; use a non-overlapping path); self-hosting on the primary controller (superseded
  by the login node).

Dashboard server-list UI:
- Power state renders as lucide `Power`/`PowerOff` icons via a shared `PowerBadge` in
  `AxisBadge.tsx`, reused by `ServersPage` and `DeployOSWizardPage`. `on` green, `error` red,
  `off`/`unknown` neutral, `null` a neutral dash. `SummaryCards` keeps text (different context).
- The server-list power cell becomes a plain icon button (decorative `PowerBadge` inside) that
  opens a `ServerPowerDialog` (Modal, mirroring `ServerLockDialog`) offering Power on / Power off
  as square grid tiles, gated by `serverActionAvailability`; selection routes through the
  existing `onAction`/`runAction` flow (toast + reload). `PowerBadge` gains a `decorative` mode
  and a shared `powerStateLabel(state|null)`.
- In-progress deployment/provider states get animated CSS stripes overlaid on the existing
  `Label` (`.sw-label-progress`), applied in the shared `DeploymentBadge` so both pages get it.
  PatternFly has no built-in equivalent; a custom CSS overlay was chosen over a `Spinner`.

## 5. Architecture and Design Principles

- Reuse shared components (DRY): power/deployment presentation lives in `AxisBadge.tsx`; action
  execution reuses the existing `ActionDropdown` → `runServerAction` → toast + reload path;
  changes flow to every consumer page rather than being copied.
- Two Slurm storages are separate data planes with separate lifecycles and validation;
  controller state is swallow-managed, workload storage is operator-configured.
- Ansible mounts are builtin-only (fstab + guarded `mount` + `mountpoint`/`findmnt` verify,
  fail-closed), consistent with `slurm_controller_state`, so no galaxy collection is needed.
- Login hosts are pure clients: configless discovery with a sackd-managed cache by default
  (`auth/munge`, so sackd is a cache choice, not an auth requirement).
- Presentation never conveys state by colour or motion alone: icons/labels/tooltips carry the
  meaning, and animation degrades under `prefers-reduced-motion`.

## 6. Functional Scope

- Deploy a single-controller Slurm cluster and an HA (multi-controller) cluster with a login
  node that self-hosts controller-state and workload NFS; workload storage may instead be an
  external NFS URL, or omitted entirely.
- Server list shows power state as icons, and the power cell opens a dialog to power a server
  on/off (gated by locks); the deployment status label animates while in progress.

## 7. Constraints and Rules

- Slurm: workload `mountPath` must be absolute and must not be `/` or `/home`; `type` must be
  `nfs`; external requires an `nfs.url` of the form `host:/path`; self-hosted requires a login
  node. NFS exports use `root_squash`. UID/GID consistency for workload users across clients is
  the operator's responsibility (swallow provides the mount, not identity).
- Dashboard: coding style per `dashboard/docs/development/coding-style.md` (no semicolons, single
  quotes, 2-space indent, JSX double-quoted attrs; reuse `sw-` classes and PatternFly tokens;
  avoid hard-coded hex except a documented rgba highlight overlay). Accessibility: status via
  shape/label/tooltip, not colour or motion alone; a reduced-motion fallback; the power cell
  `stopPropagation`s so a button click does not trigger row navigation.
- Validation gates: `npm run lint` + `npm run build` for the dashboard;
  `gofmt`/`go build`/`go vet`/`go test ./internal/...` for the api-server; playbook YAML parse.

## 8. Data Model and Format Notes

- `SlurmNodeAssignment` gains `Login bool`. `SlurmDeploymentSpec` gains
  `WorkloadStorage SlurmWorkloadStorageSpec{ Enabled, Mode, Type, MountPath, NFSURL,
  NFSMountOptions }`. `SlurmStorageType` = `nfs`; `SlurmWorkloadStorageMode` = `self-hosted` |
  `external`.
- Deployment intent projection carries Slurm `loginServerIds` (login hosts are neither role
  assignments nor members).
- In-progress state sets: provider `releasing|commissioning|testing|deploying`; deployment
  `deploying|verifying`. Terminal states are not animated.
- Power state → presentation: `on` green `Power`, `off` neutral `PowerOff`, `error` red `Power`,
  `unknown` neutral `Power`, `null` neutral dash. Power actions: `power-on`, `power-off`
  (destructive).

## 9. CLI / API / Config Notes

- Deploy request `slurm.nodeAssignments[].login` and optional
  `slurm.workloadStorage { mode, type, mountPath, nfs { url, mountOptions } }` (omit = none).
- Trusted vars: `swallow_slurm_login_ids`, `swallow_slurm_login_config_mode`,
  `swallow_slurm_login_use_sackd`; `swallow_slurm_workload_enabled|mode|mount_path|fstype|
  mount_options|server_id|export|source`. Self-hosted export path `/srv/slurm-workspace`;
  default workload mount options `rw,_netdev,hard,timeo=600,retrans=2`.
- Platform read `deployment.loginServerIds`.

## 10. Implementation Plan

Slurm (per `20260910-slurm-storage-config.md` final scope, implemented under ADR 024):
- Domain: add `Login` and `SlurmWorkloadStorageSpec` (+ helpers) in
  `platform/domain/deployment.go`.
- Validation + trusted vars in `deploy_platform.go` (`validateSlurm`, `buildSlurmVars`, consts;
  state-server prefers the login node; workload self-hosted/external).
- New Ansible roles `slurm_login`, `slurm_workload_storage_server`,
  `slurm_workload_storage_client` (builtin-only); wire login/workload groups and plays into
  `deploy-slurm.yml`, keeping primary-first controller start.
- Delivery DTO (`handler.go`): `nodeAssignments[].login` + `workloadStorage`.
- Dashboard: wizard Login role option + Workload storage section; `types.ts`; review + validation.
- Docs: platforms contract, `platform-deployment.md` §6.3, ADR 024; Go tests + topology test.

Dashboard server-list UI:
- `AxisBadge.tsx`: `PowerBadge` (+ `decorative` mode, `powerStateLabel`), in-progress detection
  for `DeploymentBadge`.
- `ServersPage.tsx`: power cell as icon button opening the dialog; `DeployOSWizardPage.tsx` power
  cells reuse `PowerBadge` (non-interactive).
- New `ServerPowerDialog.tsx` (Modal + square action tiles, availability-gated).
- `index.css`: `.sw-power-indicator`/`.sw-power-on`/`.sw-power-error`, `.sw-power-button`,
  `.sw-label-progress` (+ reduced-motion).

## 11. Non-goals

- Slurm: SlurmDBD/accounting, non-NFS storage types, multiple workload filesystems, explicit
  pre-mounted mode, read-write marker validation, a systemd `slurmd` mount dependency, user
  account / UID-GID provisioning, `auth/slurm`. Controller-state stays auto (not operator
  configured).
- Dashboard: no domain-type, API-contract, sorting/grouping, or state-semantics changes; no
  reboot action (not a domain `ServerAction`); do not icon-/button-ify `SummaryCards` or the
  `DeployOSWizardPage` power cells; the Spinner alternative was rejected in favor of stripes.

## 12. Open Questions

None outstanding. Prior forks are resolved: single-controller + external workload storage is
allowed; the `/home` overlay risk is avoided via a non-overlapping mount path; a login node is
the self-hosted NFS server.

## 13. Future Work

- Slurm: production/external HA controller-state storage; multiple workload filesystems and
  non-NFS backends; login submission over `auth/slurm`; SlurmDBD/accounting and GRES/GPU
  scheduling views.
- Dashboard: a reboot power action if it becomes a domain `ServerAction`; consider icon-izing the
  `SummaryCards` power detail if a shared `Fields` treatment emerges.
