# Slurm Platform Deployment and Durable Operation Recovery

Consolidated long-term plan. Synthesized from the manuscript drafts listed in
Source Scope; it is a planning record, not the authoritative design. The
authoritative design lives in
[`docs/development/platform-deployment.md`](../development/platform-deployment.md),
[ADR 016/017](../decisions/017-workflow-job-task-runner-model.md) (orchestration),
[ADR 019](../decisions/019-slurm-platform-deployment.md) (Slurm), and
[ADR 020](../decisions/020-durable-operation-recovery.md) (recovery).

## 1. Purpose

Define two tightly related bodies of work: (a) end-to-end deployment of a **Slurm
platform** on swallow's existing Workflow/Job/Task/Runner orchestration, and (b) a
**durable-operation failure recovery** redesign so a stuck or execution-lost
deployment can be recovered **without deleting the platform**. They belong
together because the recovery gap was discovered while deploying a multi-node HA
Slurm cluster: the same orchestration that deploys k0s and Slurm must also survive
host restarts and remain recoverable.

## 2. Source Scope

- `docs/plans/manuscripts/20260907-slurm-deployment.md` — full-stack Slurm platform
  deployment (backend + ansible + dashboard), first version.
- `docs/plans/manuscripts/20260907-operation-recovery.md` — durable operation
  recovery redesign (three layers: rerun primitive, wait hardening, lost-execution
  reconciler).

## 3. Consolidated Background

- swallow deploys platforms as Temporal-orchestrated Workflows composed of Jobs of
  Tasks run by Runners (ADR 016/017). k0s is the reference implementation. The
  reusable `ensure-os` Job brings targets to a booted, SSH-reachable OS; a
  type-specific configure Job installs the platform on top.
- Slurm is the second platform type. It reuses `ensure-os` (provisioning the
  operator's Slurm image or reusing already-deployed hosts) and adds a
  `configure-slurm` Job that runs an idempotent `deploy-slurm` playbook. Slurm has
  no kubeconfig; membership is read from slurmrestd through the existing
  `SlurmReader`.
- Recovery problem: an Operation parked at `requires_attention` (awaiting an
  operator Step retry) is killed by a host reboot — lease renewal fails, the
  workflow self-cancels to a terminal state. Because the Temporal Workflow ID is
  stable, reuse policy is reject-duplicate, and the starter only launches
  `startState=pending` records, the terminal Operation can never be restarted, so
  the only escape was deleting and redeploying the platform. This is unacceptable,
  especially once post-deploy validation Steps exist.

## 4. Confirmed Decisions

Slurm deployment:

- Full-stack scope (backend, ansible, Slurm-aware dashboard wizard).
- Single-controller first version, HA left as a reserved hook: ordered
  `swallow_slurm_controller_ids`, `swallow_slurm_high_availability` derived from
  count; HA requires an operator-supplied shared `StateSaveLocation`.
- Slurm packages are baked into the MAAS image (`slurm-smd`) because SchedMD
  recommends operator-built packages and swallow has no local repo yet. The
  playbook verifies image packages and does not compile or distro-install Slurm;
  other support software (MUNGE, later MariaDB) may still be `apt install`ed.
- Per-daemon role flags (`slurmctld` and/or `slurmd` per node; a controller may
  also be compute) — not k0s mutually-exclusive roles.
- No ephemeral-OS guard for Slurm v1 (the k0s ephemeral question is deferred).
- The recorded credential is a slurmrestd endpoint + JWT + api version written to
  `result.json`, recorded as a `ProviderKindSlurm` integration when the
  `install-platform` Step succeeds; membership is filled by the background sync.
- No internal `validate-platform-health` Step; validation lives in the playbook
  (`scontrol ping`, `sinfo --Node`, `srun`).
- No cluster-DNS dependency: `SlurmctldHost=<host>(<ip>)`, `NodeName/NodeAddr`, and
  compute `--conf-server <controller-ip>:6817` all use inventory addresses.

Durable operation recovery:

- Recovery is a **new Operation with `RetryOfOperationID` lineage and a fresh
  WorkflowID**, reusing the uninstall-retry pattern rather than fighting Temporal's
  stable id + reject-duplicate.
- The rerun clones the failed Operation's Steps, **preserving succeeded/skipped
  Steps** (side effects not repeated) and resetting the rest, and clones the sealed
  secrets (a new secret-repo `CloneForOperation`) with remapped `SecretRefs`.
- The `requires_attention` wait tolerates a transient lease-renewal failure: it
  keeps waiting instead of self-canceling, so a reboot no longer terminates a
  parked Operation.
- A lost-execution reconciler only **marks** an Operation `requires_attention`
  (surfacing it as repairable); it never auto-reruns. The rerun itself drives a
  non-terminal superseded Operation to canceled to free its target claims.

## 5. Architecture and Design Principles

- Reuse `ensure-os` across platform types; keep the configure Job type-specific.
- Recovery is a platform-type-agnostic primitive in the operation kernel, so it
  serves k0s and Slurm equally.
- Clean layering: domain (Slurm spec + per-daemon assignments), application
  (`DeployService` type dispatch, `WorkflowService.Rerun`), delivery (deploy and
  rerun endpoints), infra (`SlurmReader`, Mongo repos, Temporal Starter and
  Reconciler), and app adapters (launcher, lifecycle projection, observer).
- Idempotent ("ensure") playbooks and Tasks so a rerun re-drives only unfinished
  work.
- The platform lifecycle read model projects `platform.deployment` from recorded
  operation intent (k0s role vars; Slurm controller/compute id lists), never from a
  live cluster read.

## 6. Functional Scope

- Slurm deploy flow: `POST /platforms/deploy (type=slurm)` → `DeployService` Slurm
  branch → `platformDeploymentLauncher` builds `ensure-os` + `configure-slurm`
  (hardcoded `deploy-slurm` playbook) → on `install-platform` success record the
  slurmrestd credential → background membership sync reads slurmrestd.
- `deploy-slurm` roles: preflight, packages_verify, munge, config, controller,
  compute, slurmrestd, verify, cluster_credential.
- Recovery flow: `POST /workflows/{id}/rerun` launches a fresh execution;
  `awaitRetryStep` hardening keeps parked Operations alive; a periodic
  `Reconciler.SweepLostExecutions` marks lost executions repairable; the dashboard
  Repair action falls back from per-Step retry to rerun on a 409/lost execution.

## 7. Constraints and Rules

- Platform-playbook hard rules: idempotent; consume only the inventory + trusted
  vars contract; identity is `serverId`; return results only via
  `swallow_result_path`; secrets only through sealed refs; register in
  `manifest.json`; pin Python/collection deps.
- Secrets never enter Temporal history or API responses; only opaque references
  cross boundaries.
- Lab safety: do not disturb the `tainan-` real machines or MAAS data.
- Completion gates: `go build/vet/test` and dashboard `tsc/lint/build`, plus lab
  end-to-end verification.
- Workflow logic changes must stay replay-safe for open histories.

## 8. Data Model and Format Notes

- `SlurmDeploymentSpec` and `SlurmNodeAssignment{Controller,Compute}` with ordered
  helpers (`ControllerServerIDs`, `ComputeServerIDs`, `PrimaryControllerID`,
  `HighlyAvailable`). k0s `DeploymentSpec`/`NodeRole` untouched.
- Slurm trusted vars: `swallow_slurm_cluster_name`, `swallow_slurm_platform_name`,
  `swallow_slurm_controller_ids`, `swallow_slurm_compute_ids`,
  `swallow_slurm_primary_controller_id`, `swallow_slurm_high_availability`,
  `swallow_slurm_state_save_location`, `swallow_slurm_api_version`.
- `result.json`: `slurmrestdEndpoint`, `token`, `apiVersion`.
- Operation intent snapshot: `machinePreparation`, `resolvedProvisioning`,
  `extraVars` (the lifecycle projection reads the Slurm id lists from here).
- Secrets: `workflow_secrets` documents keyed by `(workflowId, name)` with a sealed
  value; `CloneForOperation` copies them to the rerun Operation and returns an
  old→new reference map.
- Mongo indexes: `operation_v3_starter` and new `operation_v3_recovery`
  `(schemaVersion, startState, status)`.
- `RetryOfOperationID` links a rerun (and an uninstall retry) to its origin.

## 9. CLI / API / Config Notes

- `POST /api/v1/platforms/deploy` accepts `type` and a `slurm` block
  (`nodeAssignments[]` of `{serverId, controller, compute}`, optional
  `clusterName`, `apiVersion`, `stateSaveLocation`), reusing `machinePreparation`.
- `POST /api/v1/workflows/{id}/rerun` (with the deprecated `/operations/{id}/rerun`
  alias) returns `202` with the new Operation; a succeeded or still-advancing
  Operation is rejected as a conflict.
- The lost-execution reconciler reuses `SWALLOW_API_RECONCILE_INTERVAL`.
- `SlurmReader` defaults to api version `v0.0.40`, overridable per integration from
  the recorded credential.

## 10. Implementation Plan

Slurm deployment (in order):

1. Domain: `SlurmDeploymentSpec` + `SlurmNodeAssignment` and helpers.
2. Application: `DeployPlatformInput.Type`/`SlurmSpec`; `Deploy` dispatch;
   `validateSlurm`; `buildSlurmVars`; MUNGE key via sealed secret.
3. `deployment_credential.go`: `RecordSlurm` (`ProviderKindSlurm`).
4. Delivery: deploy request `type` + `slurm` fields.
5. Adapter: `Launch` dispatch; shared `ensure-os` helper; `configure-slurm` Job;
   observer handles `configure-slurm`.
6. Lifecycle: include `configure-slurm`; Slurm deployment-intent projection.
7. Ansible: `deploy-slurm.yml` + roles; register in `manifest.json`.
8. Dashboard: platform-type selector + Slurm wizard branch; API client; detail
   copy; e2e fixtures/tests.
9. Docs: `platform-deployment.md`, ADR 019, glossary.
10. Gates + lab end-to-end.

Durable operation recovery (in order):

1. Secret repo `CloneForOperation` (+ domain interface).
2. `WorkflowService.Rerun` (clone/preserve/reset/re-seal/lineage) and make `Create`
   preserve succeeded/skipped Steps.
3. `POST /workflows/{id}/rerun` delivery + route (+ alias).
4. Dashboard `rerunOperation` + Repair fallback.
5. Harden `awaitRetryStep` (continue on renewal failure).
6. `ListNonTerminalStarted` + `operation_v3_recovery` index.
7. `Reconciler.SweepLostExecutions` + wire into api and worker.
8. ADR 020 + `platform-deployment.md` §4.7 + workflows contract.
9. Tests (rerun, wait hardening, reconciler, dashboard fallback).
10. Lab: recover a stuck HA Slurm platform via rerun without deletion.

## 11. Non-goals

- Slurm v1 excludes SlurmDBD/accounting, GRES/GPU scheduling, HA shared-filesystem
  provisioning, dedicated login/submit node roles, and (per the source manuscript)
  `uninstall-slurm`, whose v1 fallback was delete-platform + release-servers.
- Recovery does not auto-rerun from the reconciler, and does not change the stable
  Temporal Workflow ID or the reject-duplicate reuse policy.

## 12. Open Questions

- slurmrestd endpoint version string per Slurm release (e.g. 26.05 → `v0.0.4x`):
  the playbook detects it and writes `apiVersion`; the integration uses that value.
- Time synchronization: whether the image ships chrony/systemd-timesyncd; preflight
  asserts rather than installs.
- The lease-weakening trade-off during the parked wait (documented in ADR 020):
  tolerating a lost lease while parked is safe because a Step retry re-validates and
  re-acquires before any side effect.

## 13. Future Work

- Slurm: accounting (SlurmDBD), GRES/GPU scheduling, HA shared storage, login nodes.
- Recovery: consider safe auto-reconciliation where a rerun has no destructive
  effect; improve manager/controller visibility in read models (slurmrestd only
  reports compute scheduler nodes).
- Revisit the k0s ephemeral-OS restriction.
