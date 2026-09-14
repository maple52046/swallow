# 028. Kubernetes deployment supports disposable ephemeral hosts

- Status: Accepted
- Date: 2026-09-14

## Context

Swallow already carries provider-neutral ephemeral OS intent and MAAS can boot an Ubuntu root
filesystem from memory without installing it to disk. Kubernetes deployment rejected that
intent because the available in-memory image lacked netfilter modules and because Kubernetes
state is normally expected to persist. The lab needs intentionally disposable k0s Platforms,
so durability is not a valid reason to reject an otherwise compatible host.

Image metadata cannot prove compatibility with the kernel MAAS actually PXE-boots. Accepting
every ephemeral image without a runtime gate would move the old deterministic validation error
into a partial cluster installation.

## Decision

The existing `machinePreparation.settings.ephemeral` field is accepted for Kubernetes without
a schema change. Its contract is explicitly volatile: OS changes, etcd, container runtime data,
and workloads are lost when a Server reboots.

Kubernetes automation installs the requested k0s binary, then performs a fail-closed host gate
before starting any controller or worker. The gate verifies required commands, cgroup v2,
required networking/filesystem kernel modules, and the version-matched `k0s sysinfo` report.
For a MAAS memory-backed root, the gate also configures containerd's `native` snapshotter:
nesting the default overlayfs snapshotter inside `overlayroot` produces unreliable `EINVAL`
pod mounts. Custom `root.tgz` boots may expose `/` as overlayfs or directly as tmpfs; both are
accepted only under provider-confirmed ephemeral intent.
The retained Ansible stdout is the diagnostic artifact. Image construction remains outside
Swallow; the image must carry modules that match the provider's boot kernel.

## Alternatives considered

- **Keep rejecting ephemeral Kubernetes:** rejected because it prevents the confirmed disposable
  lab use case even when the image and boot kernel are compatible.
- **Trust image tags or names:** rejected because provider metadata does not prove the running
  kernel ABI or loadable module set.
- **Bake k0s into the image:** rejected because it would split version ownership between the
  image catalog and the deployment request.
- **Treat ephemeral Kubernetes as durable:** rejected because that would hide the intentional
  loss of control-plane and workload state after reboot.

## Consequences

- Existing API clients gain support for an already-defined flag; persistent deployment behavior
  is unchanged.
- Compatibility failures happen before cluster state is created and identify the missing host
  capability in retained automation output.
- Operators must recreate or redeploy a Platform after an ephemeral host reboot; automatic
  restoration is not part of this decision.

## Current status

Implemented in backend validation, k0s automation, the provider-owned API contract, and the
Dashboard. Lab validation uses an ABI-matched Ubuntu 24.04 MAAS image and a seven-node HA k0s
Platform.

## Related

- [ADR 007](007-cluster-deployment-ownership.md) — Swallow ownership of k0s deployment.
- [ADR 009](009-deployment-template-ownership.md) — reusable OS deployment intent.
- [ADR 018](018-automatic-addressing-provider-auto-assign.md) — provider automatic addressing.
- [`docs/development/platform-deployment.md`](../development/platform-deployment.md) — workflow
  and playbook integration contract.
