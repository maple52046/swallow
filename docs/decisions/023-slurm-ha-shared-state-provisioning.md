# 023. Slurm HA: swallow provisions the shared StateSaveLocation

- Status: Accepted
- Date: 2026-09-09

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

- **State-server selection (in the deploy use case).** For a multi-controller deploy swallow picks
  a state server from the existing targets: a **compute-only** node (compute and not a controller)
  when one exists, otherwise the **primary controller**. Preferring an off-controller host keeps
  controller state off the controllers (a controller-host loss does not also lose state) and avoids
  a kernel NFS loopback mount on a controller. It emits `swallow_slurm_controller_state_mode=shared`,
  `swallow_slurm_state_server_id`, and `swallow_slurm_state_export`.
- **Managed NFS export (`slurm_state_server` role).** On the selected host, export a directory over
  NFS to the controller addresses only (`rw,sync,no_subtree_check,root_squash`). The `slurm` uid is
  uniform across nodes because they share one image, so `root_squash` is safe.
- **Fail-closed controller mount (`slurm_controller_state` role).** Each controller mounts the
  export at the `StateSaveLocation` and installs a `slurmctld` systemd drop-in
  (`RequiresMountsFor` + `ConditionPathIsMountPoint`) so the daemon refuses to start unless the
  path is a real mountpoint — state can never be written silently to a local disk. Single
  controllers use `mode=local` (a controller-local directory, no NFS).
- **`stateSaveLocation` becomes an optional override**, not a required field. The HA "required"
  validation is removed.

**Only `ansible.builtin` is used** for the mount (an fstab entry plus a guarded `mount`), not
`ansible.posix.mount`: the executor image installs no galaxy collections and playbooks must stay
air-gap-friendly.

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

## Controller start order (required for HA to form)

Shared state alone is not sufficient: the **primary must be configured and started before any
backup**. A backup that starts first comes up in standby and blocks waiting for a heartbeat the
not-yet-configured primary cannot write; its `scontrol ping` then fails and aborts the run
before the primary is even given `slurm.conf`, so the cluster never forms (this was the actual
failure behind the "every `slurmctld` DOWN" reports, independent of local vs shared state).
`group_by` does not preserve primary-first order, so `deploy-slurm.yml` runs the primary in its
own play (`hosts: slurm_primary`) and the backups in a following play
(`hosts: slurm_controller:!slurm_primary`, `serial: 1`), rather than relying on `serial`
ordering within a single controller play.

## Consequences

- An HA Slurm deploy now succeeds with no manual storage setup: controllers mount one shared
  `StateSaveLocation`, the primary becomes active and writes the heartbeat, backups settle into
  standby, and `slurmctld` stays up cluster-wide. Verified from 0 on a 3-controller / 4-compute
  lab cluster (all three `slurmctld` UP, partition up, nodes idle).
- The selected state server is a **single storage failure domain** — a documented lab-grade
  limitation, not storage HA. Production external/HA storage is future work.
- The mount guard makes a missing share a fast, explicit failure (with the enriched executor
  error) instead of silent local-state corruption.
- `slurm_config` no longer manages the state directory; `slurm_controller_state` owns it in both
  modes.

## Current status

Implemented. Roles `slurm_state_server` and `slurm_controller_state`, wired into `deploy-slurm.yml`
(state-server play, then controller-state play, then the controller-config play); state-server
selection and trusted vars in `deploy_platform.go` (`buildSlurmVars`, `slurmStateServerID`);
preflight assertion for HA shared-state inputs. Covered by `deploy_slurm_test.go` and
`slurm_ha_state_playbook_test.go`.

## Related

- [ADR 019](019-slurm-platform-deployment.md) — the Slurm platform deployment this completes.
- [platform-deployment.md](../development/platform-deployment.md) §6.3 — the Slurm reference,
  updated for swallow-provisioned HA shared state.
- `knowledge/Slurm/controller-ha-and-configless.md`, `knowledge/Slurm/shared-workload-storage.md`
  — the domain rationale for shared state and its failure domains.
