# 024. Slurm: login node role and workload shared storage

- Status: Accepted
- Date: 2026-09-11
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §6.3 Slurm reference（已實作現況）的「login 角色 + workload 共享儲存」

## Context

The updated Slurm knowledge base and reference `deploy-slurm` skill added a login (submission)
node role and a shared **workload** filesystem, and defined a lab HA topology in which one
login host provides NFS to the whole cluster. swallow previously modeled only
controller/compute daemons and provisioned only the controller `StateSaveLocation`
([ADR 023](023-slurm-ha-shared-state-provisioning.md)); it had no login role and no workload
storage.

Two distinct storages must not be conflated (`knowledge/Slurm/shared-workload-storage.md`): the
controller `StateSaveLocation` (slurmctld-only, HA failover) and the **workload filesystem**
(user/job data — `/home`-like — consumed by login and compute). ADR 023 provisioned the former;
this ADR adds the latter and the login role, and supports the two operator-requested topologies:

1. single controller + N compute;
2. 1 login + 3 controllers (HA) + 3 compute, with the login node hosting the cluster NFS.

## Decision

- **Add a `login` role.** A Slurm node assignment gains a login flag; a login-only Server is valid
  (it runs no cluster daemon). A login host is a configless client using a sackd-managed cache by
  default (`auth/munge`, so sackd is a cache choice, not an authentication requirement).
- **In HA, the login node is the controller-state server.** State-server selection prefers a
  login node, then an off-controller compute node, then the primary controller. Controller state
  stays swallow-provisioned and is not operator-configured.
- **Add an optional workload shared filesystem** (`slurm.workloadStorage`), distinct from
  controller state, mounted on every node at a non-overlapping path (never `/home`, which would
  hide the automation user's `~/.ssh` and break SSH). It is either `self-hosted` (swallow exports
  NFS from the login node, so a login node is required) or `external` (swallow mounts an
  operator-supplied NFS source and runs no server). The type is an enum (`nfs`) so another backend
  is additive; omitting it means no shared filesystem.
- **Ansible mounts are builtin-only** and fail-closed, consistent with the controller-state
  mount, so no galaxy collection is required.

## Alternatives considered

- **Self-hosted workload storage on the primary controller** (as an earlier iteration did for
  the abandoned no-login design): rejected — the natural NFS host is a login node, and putting
  user data on a controller couples the submission path to the control plane.
- **Mount the workload filesystem at `/home` on all nodes**: rejected — an initially empty NFS
  export over `/home` hides `~ubuntu/.ssh/authorized_keys` and breaks SSH/Ansible mid-deploy. A
  non-overlapping path avoids it; true shared home would require seeding, which is future work.
- **Making controller state operator-configurable (external) too**: deferred — controller state
  stays auto; only workload storage is operator-configured here.
- **Adding the `ansible.posix` collection for mounts**: rejected — an image/build dependency for
  something expressible with builtins.

## Consequences

- swallow deploys both target topologies: a single-controller cluster, and an HA cluster whose
  login node serves controller-state and workload NFS.
- The workload NFS host (login node) is a single storage failure domain — a documented lab
  limitation, not storage HA.
- swallow provides the workload mount, not cluster identity: consistent workload-user UID/GID
  across clients remains the operator's responsibility.
- The deploy request and the trusted-vars contract grow login and workload-storage fields.
