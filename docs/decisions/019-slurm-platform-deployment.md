# 019. Slurm platform deployment: per-daemon roles, image-supplied packages, slurmrestd credential

- Status: Accepted
- Date: 2026-09-07

## Context

swallow already deploys Kubernetes (k0s) Platforms through the Workflow/Job/Task/Runner model
([ADR 016](016-temporal-operation-orchestration.md),
[ADR 017](017-workflow-job-task-runner-model.md)) and already registers Slurm Platforms for
membership reads (a `SlurmReader` over slurmrestd exists). What was missing was the ability to
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

**Reuse the ensure-os Job; add a `configure-slurm` Job.** A Slurm deployment provisions the OS
(the operator's Slurm image) or reuses already-deployed hosts via the same `ensure-os` Job as
k0s, then runs a hardcoded `deploy-slurm` playbook. The playbook name is fixed on the launcher
(like `uninstall-kubernetes`), so a Slurm deploy needs no site `playbookMappings` entry. The
workflow kind is `configure-slurm`.

**Model roles as per-daemon flags, not a mutually exclusive role.** The deploy request carries
`slurm.nodeAssignments[]` of `{ serverId, controller, compute }`. At least one controller and
one compute are required; a node with neither is rejected. Trusted vars are ordered id lists
(`swallow_slurm_controller_ids`, `swallow_slurm_compute_ids`, `swallow_slurm_primary_controller_id`),
with the first controller as the primary.

**Slurm packages come from the OS image; other software may still be installed.** SchedMD
recommends operator-built packages and swallow has no local repo, so the `slurm-smd` packages
are baked into the MAAS image and the playbook only verifies them (`dpkg-query`) and configures
the cluster — it does not build or install Slurm from a distribution repo. This is not a ban on
`apt`: supporting software the deployment needs (MUNGE, and later MariaDB for accounting) is
installed normally.

**The credential is a slurmrestd reader, and it is optional.** The primary controller runs
`slurmrestd` on a TCP port with JWT auth, mints a long-lived JWT for the SlurmUser, and writes
`{ slurmrestdEndpoint, token, apiVersion }` to the run's result file. swallow records this as a
`slurm` platform Integration, mirroring how k0s records its kubeconfig-equivalent credential.
Because `slurmrestd` (`slurm-smd-slurmrestd`) powers only membership reads and the cluster
(slurmctld + slurmd) is fully functional without it, its package is **not required**: when the
image omits it, the playbook skips slurmrestd and the credential, and the backend treats a
missing Slurm credential as non-fatal. The deploy still succeeds; the platform is `active` with
no reader integration and no members until the image includes `slurm-smd-slurmrestd`. (k0s, by
contrast, requires its kubeconfig-equivalent credential.)

**No internal validate step for Slurm (first cut).** The deployed integration only exists after
the install step's result is recorded, so a mid-workflow membership read is not available.
Cluster validation lives in the playbook (`scontrol ping`, `sinfo --Node`, `srun`), and swallow's
background membership sync fills in members after success.

**Single controller first; HA is a reserved hook.** One controller is the default and needs no
shared storage. More than one controller is high availability and requires an operator-supplied
shared `StateSaveLocation` (validated, not defaulted), because a backup `slurmctld` can only
recover state from shared storage that swallow does not provision.

**No ephemeral guard for Slurm.** Slurm accepts an ephemeral (run-from-RAM) OS. Kubernetes did
not support that mode when this decision was accepted; ADR 028 later adds it with a booted-host
compatibility gate.

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
  swallow does not provision; forcing it would block the common single-controller case.

## Consequences

- Deploying Slurm reuses the OS-provisioning, SSH-readiness, retry, secret-sealing, and
  lifecycle machinery already built for k0s; only the configure Job and credential differ.
- A Slurm Platform appears with `deploying`/`active`/`deploy_failed` lifecycle and claims its
  target Servers exactly like k0s, so target protection and the dashboard work unchanged. The
  deployment projection is derived from the recorded `swallow_slurm_controller_ids` /
  `swallow_slurm_compute_ids` (controller -> manager, controller+compute -> manager that also
  runs workloads, compute -> worker), so the manager (controller) nodes are counted and listed
  even though slurmrestd only reports the compute (slurmd) scheduler nodes as members. Compute
  membership still fills in via the background sync.
- Membership works only once slurmrestd + JWT are configured; the endpoint version is detected
  (or pinned) and stored, so a fleet running several Slurm releases each reads the right version.
- Uninstall is supported: `uninstall-slurm` removes the Slurm configuration, keys, controller
  state, and daemons Swallow deployed (leaving the OS and image packages), mirroring
  `uninstall-kubernetes`, and can optionally release the member Servers in the same Operation.
- HA remains partial: the vars and validation exist, but shared-storage provisioning,
  accounting (SlurmDBD), GRES/GPU scheduling, and login nodes are future work.

## Current status

Partial, validated in the dev lab. A single-controller + four-compute Slurm cluster deployed
end-to-end from the operator image: OS provisioning, MUNGE, slurm.conf/cgroup.conf, slurmctld,
configless slurmd, and an `srun` smoke test across all compute nodes all succeeded, and the
platform reached `active`. The lab image did not include `slurm-smd-slurmrestd`, so slurmrestd
and the reader credential were skipped and membership stayed empty — the designed graceful
degradation. Per-daemon deploy API, the Slurm-aware deploy wizard, and (when slurmrestd is
present) credential recording are Implemented, and `uninstall-slurm` is Implemented and
validated in the lab (a five-node Slurm platform uninstalled cleanly, rc=0 on every node, and
the platform reached `uninstalled`). Accounting, GRES scheduling, HA shared storage, and
login-node roles are Planned.

## Related

- [ADR 017](017-workflow-job-task-runner-model.md) — the Workflow/Job/Task/Runner model and the
  "new platform = a playbook + trusted-vars contract + manifest" extension point this follows.
- [ADR 007](007-cluster-deployment-ownership.md) — the k0s deployment-ownership decision this
  mirrors for a non-Kubernetes platform.
- [`docs/development/platform-deployment.md`](../development/platform-deployment.md) — the
  design and integration contract, whose Slurm section is updated for this implementation.
