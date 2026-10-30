# 019. Slurm platform deployment: per-daemon roles, image-supplied packages, slurmrestd credential

- Status: Accepted
- Date: 2026-09-07
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §6.3 Slurm reference（已實作現況）

## Context

swallow already deploys Kubernetes (k0s) Platforms through the Workflow/Job/Task/Runner model
([ADR 016](016-temporal-operation-orchestration.md),
[ADR 017](017-workflow-job-task-runner-model.md)) and already registered Slurm Platforms for
membership reads (a `SlurmReader` over slurmrestd existed). What was missing was the ability to
**deploy** a Slurm Platform. Adding it raised four decisions that differ from the k0s path and
are easy to question later.

1. **Node roles.** k0s uses a single mutually exclusive Node Role (`control-plane`/`worker`)
   with an opt-in workload flag. Slurm does not work that way: a node runs the controller
   daemon (`slurmctld`), the compute daemon (`slurmd`), or both, and a controller commonly
   also contributes compute. There are also login/submit and accounting daemons.
2. **Where the Slurm packages come from.** SchedMD recommends operator-built `slurm-smd`
   deb/rpm and swallow has no local package repository yet.
3. **How swallow reads a deployed Slurm Platform.** Slurm has no kubeconfig; membership is read
   from slurmrestd, which needs an endpoint, a JWT, and an endpoint version.
4. **Where cluster validation happens.** k0s adds an internal `validate-platform-health` Task
   that reads membership through the platform integration mid-workflow.

## Decision

- **Reuse the ensure-os Job; add a `configure-slurm` Job.** A Slurm deployment provisions the OS
  (the operator's Slurm image) or reuses already-deployed hosts via the same `ensure-os` Job as
  k0s, then runs a `deploy-slurm` playbook whose name is fixed on the launcher (like
  `uninstall-kubernetes`), so no site `playbookMappings` entry is needed. The workflow kind is
  `configure-slurm`.
- **Model roles as per-daemon flags, not a mutually exclusive role.** The deploy request carries
  `slurm.nodeAssignments[]` of per-daemon flags. At least one controller and one compute are
  required; a node with no role is rejected. Trusted vars are ordered id lists, with the first
  controller as the primary. ([ADR 024](024-slurm-login-and-workload-storage.md) later added the
  `login` flag.)
- **Slurm packages come from the OS image; other software may still be installed.** The
  `slurm-smd` packages are baked into the MAAS image and the playbook only verifies and configures
  them — it does not build or install Slurm from a distribution repo. Supporting software the
  deployment needs (MUNGE, and later MariaDB for accounting) is installed normally.
- **The credential is a slurmrestd reader, and it is optional.** The primary controller runs
  `slurmrestd` with JWT auth and returns `{ slurmrestdEndpoint, token, apiVersion }` through the
  run's result file; swallow records it as a `slurm` platform Integration, mirroring how k0s records
  its credential. Because `slurmrestd` powers only membership reads and the cluster is fully
  functional without it, its package is not required: without it the playbook skips the
  credential, the backend treats the missing credential as non-fatal, and the platform is `active`
  with no reader integration. (k0s, by contrast, requires its credential.)
- **No internal validate step for Slurm.** The deployed integration only exists after the install
  step's result is recorded, so cluster validation lives in the playbook, and background
  membership sync fills in members after success.
- **Single controller first.** One controller is the default and needs no shared storage. More
  than one controller is high availability; this decision required an operator-supplied shared
  `StateSaveLocation`, which [ADR 023](023-slurm-ha-shared-state-provisioning.md) replaced with
  swallow-provisioned shared state.
- **No ephemeral guard for Slurm.** Slurm accepts an ephemeral (run-from-RAM) OS. Kubernetes did
  not support that mode when this decision was accepted; [ADR 028](028-ephemeral-kubernetes-deployment.md)
  later adds it with a booted-host compatibility gate.

## Alternatives considered

- **Overload the k0s `control-plane`/`worker` Node Role for Slurm:** rejected — it cannot
  express a node that is both controller and compute, which is normal for Slurm, and would leak
  Kubernetes vocabulary into a different scheduler.
- **Install Slurm from a distribution repo or build it in the playbook:** rejected for this
  cut — SchedMD recommends operator-built packages, swallow has no local repo, and building per
  host causes version drift. Baking packages into the image is the reliable path until a local
  repo exists.
- **Add a `validate-platform-health` Task for Slurm like k0s:** rejected for the first cut — the
  reader integration is not recorded until the install step completes, so the playbook's own
  verification plus background membership sync is simpler and avoids a credential-timing race.
- **Require HA (multiple controllers) up front:** rejected — HA depends on a shared filesystem
  swallow did not provision; forcing it would block the common single-controller case.

## Consequences

- Deploying Slurm reuses the OS-provisioning, SSH-readiness, retry, secret-sealing, and
  lifecycle machinery already built for k0s; only the configure Job and credential differ.
- A Slurm Platform has the same lifecycle and claims its target Servers exactly like k0s. Its
  deployment projection is derived from the recorded controller and compute id lists, so the
  controller nodes are counted and listed even though slurmrestd reports only the compute
  (slurmd) nodes as members.
- Membership works only once slurmrestd and JWT are configured; the endpoint version is detected
  (or pinned) and stored, so Platforms running different Slurm releases each read the right
  version.
- Uninstall mirrors `uninstall-kubernetes` and can optionally release the member Servers.
- Accounting (SlurmDBD) remains future work. HA shared storage, login nodes, and GPU GRES were
  added later by ADRs 023, 024, and [063](063-omnistat-slurm-observability.md).
