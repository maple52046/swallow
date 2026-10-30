# 068. The Device Metrics Exporter is Managed Software that a Slurm deployment composes

- Status: Accepted
- Date: 2026-10-30
- 現行規格：[software-deployment.md](../development/software-deployment.md) §2 Managed Software 目錄（AMD Device Metrics Exporter）與 §5 與 Platform deployment 的連動

## Context

Decision 066 installs AMD's Device Metrics Exporter (DME) only as a feature of a Swallow-deployed
Slurm Platform: a `configure-device-metrics-exporter` Job of the Slurm Workflow, recorded on the
Platform and nowhere else. Operators also want per-GPU metrics on a single Server that runs no
Platform, and later on its AI NICs. The software deployment rule (decision 038, §5) is that a
Platform needing a piece of host software composes that software's Job instead of carrying its own
copy, so a standalone install and the Slurm feature should be one implementation.

The exporter itself is always installed natively on the GPU node, from AMD's pinned
`amdgpu-exporter` package; decision 066 rejected the privileged container image. The Slurm
feature's `installMethod` only chooses how Prometheus and Grafana run on the primary controller.

## Decision

- **A new Managed Software kind, `device-metrics-exporter`.** It is role-less and has one built-in
  install path, the same pinned native package as the Slurm feature. A standalone install provides
  the exporter only, with Slurm job attribution off; Prometheus and Grafana stay separate kinds the
  operator installs to scrape and show it. The request takes no spec fields; the assignment records
  the applied version.
- **Standalone targets.** Besides the usual `deployed` requirement, a target must list AMD compute
  GPUs in its GPU Inventory. A Kubernetes member is refused (the GPU Operator runs its own
  exporter), and so is a Server with a Platform Claim, for install and uninstall alike: the
  exporter on a Platform's node belongs to that Platform. Only Ubuntu 24.04 is supported; because
  the Server projection names a custom image rather than its distribution, the playbook checks the
  host.
- **The amdgpu driver.** `gpuagent` needs the shared post-boot amdgpu loader (decisions 062 and
  063). A standalone install installs it; its uninstall removes it only when no other unit still
  depends on it. Inside a Slurm deployment the Slurm GPU role owns the loader.
- **Slurm composes the kind.** With `deviceMetricsExporter` enabled, the Slurm Workflow composes
  `device-metrics-exporter` onto its GPU compute nodes like Prometheus and Grafana. The install
  Task carries the software identity (so the assignments turn `failed` with it) and the Platform's
  owner and record ids; Slurm job attribution and the scrape check are the Slurm run's extra
  trusted variables. The Platform's Device Metrics Exporter State, GPU identity, and Monitoring
  Link are recorded exactly as before.
- **Slurm uninstall.** It keeps removing the exporter from every target, and a successful Slurm
  uninstall marks the `device-metrics-exporter` assignments of the Platform's Servers `absent`, so
  the record never outlives the package.

## Alternatives considered

- **Keep the Slurm feature's own Job and only share the role.** Rejected: two install paths for one
  software, and the Slurm nodes would carry no Software Assignment.
- **A container method for the exporter.** Not now: the native package needs no runtime and no
  privileged mode, and the Slurm feature already runs it natively. A container method would also
  have to compose a container runtime.
- **Compose Prometheus and Grafana into the standalone install.** Rejected for now: they are
  already Managed Software an operator can point at the exporter, and one Prometheus usually
  scrapes many Servers.
- **Check the operating system from the Server projection.** Rejected: custom images report
  `osSystem` `custom`, so the projection cannot tell Ubuntu 24.04 apart.

## Consequences

- One playbook and role install the exporter for both a single Server and a Slurm Platform, and
  every exporter Swallow manages has a Software Assignment.
- The glossary's Device Metrics Exporter is now Managed Software as well as a Slurm feature.
- A Kubernetes deployment onto a Server that already runs a standalone exporter is not handled yet.
