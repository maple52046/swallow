# 023. Slurm HA: swallow provisions the shared StateSaveLocation

- Status: Accepted
- Date: 2026-09-09
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §6.3 Slurm reference（已實作現況）的「HA shared state 由 swallow 供給」

## Context

A highly available Slurm cluster runs more than one `slurmctld` in ordered active/standby. Every
`SlurmctldHost` must read and write **one shared `StateSaveLocation`**; a backup can only take
over from state the primary persisted there. Controller redundancy is not storage redundancy
(see `knowledge/Slurm/controller-ha-and-configless.md`).

The first Slurm implementation ([ADR 019](019-slurm-platform-deployment.md)) shipped single
controller fully but left HA half-done: the deploy use case *required the operator to hand-supply*
`stateSaveLocation`, and nothing mounted a real shared filesystem. In practice an HA deploy then
failed with every `slurmctld` DOWN and `get_last_heartbeat: heartbeat open attempt failed from
<StateSaveLocation>/heartbeat`, because the path was a plain local directory, not a shared mount.
That is a manual, error-prone half-step, not a complete deployment mechanism.

The reference deployment project models the complete mechanism as two roles — a managed NFS state
server and a controller-state mount with a fail-closed guard — wired by an `ha-controller`
playbook. swallow was missing both.

## Decision

**swallow provisions the HA shared `StateSaveLocation` itself; the operator supplies nothing.**

- **State-server selection in the deploy use case.** For a multi-controller deploy swallow picks
  a state server from the existing targets: a compute-only node when one exists, otherwise the
  primary controller. Preferring an off-controller host keeps controller state off the
  controllers (a controller-host loss does not also lose state) and avoids a kernel NFS loopback
  mount on a controller. ([ADR 024](024-slurm-login-and-workload-storage.md) later put a login
  node first.)
- **Managed NFS export.** The selected host exports a directory over NFS to the controller
  addresses only, with `root_squash`; the `slurm` uid is uniform across nodes because they share
  one image, so `root_squash` is safe.
- **Fail-closed controller mount.** Each controller mounts the export at the `StateSaveLocation`
  and `slurmctld` refuses to start unless the path is a real mountpoint, so state can never be
  written silently to a local disk. Single controllers keep a controller-local directory.
- **`stateSaveLocation` becomes an optional override**, not a required field.
- **Only `ansible.builtin` is used** for the mount, not `ansible.posix.mount`: the executor image
  installs no galaxy collections and playbooks must stay air-gap-friendly.
- **The primary starts before any backup.** Shared state alone is not sufficient: a backup that
  starts first comes up in standby, blocks waiting for a heartbeat the not-yet-configured primary
  cannot write, and its failing `scontrol ping` aborts the run before the primary is configured.
  `group_by` does not preserve primary-first order, so the primary is configured in its own play
  before the backups.

## Alternatives considered

- **Keep requiring an operator-supplied `stateSaveLocation`:** rejected — it is exactly the manual
  half-step that produced an all-DOWN HA cluster; the goal is a complete deployment mechanism.
- **A dedicated storage-host role in `SlurmDeploymentSpec`:** rejected for now — it expands the
  domain model, deploy API, and dashboard wizard. Auto-selecting an existing node deploys a working
  HA cluster today; a dedicated/external storage endpoint is a clean future extension (the role
  keeps a mode hook for external/pre-mounted storage).
- **Always co-locate the NFS server on the primary controller:** rejected as the default — it makes
  the primary host a storage SPOF and uses NFS loopback; preferring a compute-only node is better
  within the same model. It remains the fallback when every node is a controller.
- **Add the `ansible.posix` collection for `mount`:** rejected — it adds an image/build dependency
  and works against air-gap packaging for one task expressible with builtins.
- **Rely on `serial` ordering within a single controller play:** not used — `group_by` does not
  preserve primary-first order.

## Consequences

- An HA Slurm deploy succeeds with no manual storage setup: controllers mount one shared
  `StateSaveLocation`, the primary becomes active, backups settle into standby, and `slurmctld`
  stays up cluster-wide.
- The selected state server is a **single storage failure domain** — a documented lab-grade
  limitation, not storage HA. Production external/HA storage is future work.
- The mount guard makes a missing share a fast, explicit failure instead of silent local-state
  corruption.
- The controller-state role, not the Slurm configuration role, owns the state directory in both
  modes.
