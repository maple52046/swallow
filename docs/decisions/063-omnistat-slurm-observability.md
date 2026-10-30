# 063. Slurm deployment configures AMD GPU GRES and optionally installs Omnistat

- Status: Accepted
- Date: 2026-10-21
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §6.3 Slurm reference（已實作現況）的 GPU GRES、amdgpu driver options、GPU 可用性驗收、container runtime 與 Omnistat；Prometheus／Grafana 的安裝方法見 [software-deployment.md](../development/software-deployment.md) §2 Managed Software 目錄（第一刀）

## Context

A Swallow-deployed Slurm Platform had no GPU scheduling: `slurm.conf` listed CPU and memory only,
so jobs could not request GPUs and had no device isolation. Operators who wanted AMD's Slurm-aware
monitoring, [Omnistat](https://github.com/ROCm/omnistat), overlaid it by hand after the deploy:
an exporter on every compute node, Prolog/Epilog job attribution on the controller, GPU GRES, and a
Docker Compose Prometheus and Grafana. Every Slurm redeploy rewrote `slurm.conf` and lost the
overlay. Prometheus and Grafana are generic single programs that other runtimes can also want.

## Decision

- **GPU GRES is part of every Slurm deployment.** Compute nodes whose GPU Inventory has AMD
  `compute` GPUs become GPU nodes. Their `amdgpu` driver is loaded by the same post-boot unit as
  the GPU Operator ([decision 062](062-swallow-managed-gpu-operator.md)), now a shared role, and
  `slurmd` is ordered after it. Each GPU's render node is derived from the KFD topology (never by
  counting `/dev/dri/renderD*`, because XCP partitions add extra nodes), GRES is configured
  explicitly (no autodetection), and `ConstrainDevices=yes` stays on. The GRES type defaults to the
  derived architecture name, and a deploy may name one type for every GPU node. A selected node
  whose KFD shows no GPU fails the deploy.
- **Driver options never name hardware.** The shared loader writes a Swallow-owned modprobe file
  before the driver loads, from the operator's options plus options a topology rule derives from
  what KFD reports. Today's only rule: a GPU node that reaches the same peer through both an XGMI
  `io_link` and a PCIe `p2p_link` overflows libhsakmt's peer array (heap corruption when HIP
  processes exit), so the rule adds `pcie_p2p=0`. A rule option is kept once applied, because
  applying it removes the condition; an operator option always wins; every option is verified
  against the loaded driver, and a name the installed driver does not have is refused before
  anything loads. A rule is known only after the driver loads, so a driver running with other
  parameters loads again: a driver nothing holds is unloaded and loaded in place; a driver still
  held is applied by one reboot on a disk root and refused on a memory-backed root. Units that need
  the GPUs (`slurmd`, `omnistat`) require the loader; k0s is only ordered after it because it also
  runs non-GPU work.
- **GPUs are proven with the HIP runtime, not an inventory listing.** A host-only HIP probe (no
  device code, so any architecture) must see as many GPUs as KFD lists and allocate, fill, and free
  memory on each, repeatedly and in separate processes, directly and again through Slurm; a
  failure drains the node and fails the deployment.
- **The device cgroup is the GPU access boundary.** A udev rule opens `/dev/kfd` and the render
  nodes to every user on GPU nodes, so a job's user needs no `render` group membership; Slurm's
  device cgroup still admits a job only to its allocated GPUs. The trade-off is that a process
  outside Slurm on a compute node (for example an SSH login without `pam_slurm_adopt`) could open
  the GPUs, the same exposure as adding every user to `render`.
- **Omnistat is a Slurm Platform feature, not Managed Software.** The Platform records Omnistat
  and its Omnistat State, projected from its install Task like the GPU Operator State. The
  Slurm-side overlay (job attribution, `PrologFlags=Alloc,Contain`, an exclusive partition because
  Omnistat's job file is node-wide) is rendered by `deploy-slurm`, the single owner of
  `slurm.conf`, so a redeploy re-applies it; attribution succeeds as a no-op until the exporter
  exists, so the order of Jobs never drains a node. A separate playbook installs the pinned exporter
  (with the patches Swallow maintains under [decision 064](064-upstream-patches-are-the-exception.md))
  and returns the Grafana URL, which Swallow records as a Monitoring Link.
  ([ADR 066](066-slurm-job-hooks-and-device-metrics-exporter.md) later moved the attribution into a
  Prolog/Epilog dispatcher hook.)
- **Prometheus and Grafana are new Managed Software kinds, composed as Jobs.** With Omnistat
  enabled, the `configure-slurm` Workflow composes their Jobs onto the primary controller after the
  Slurm install and before the Omnistat Job, so the primary controller gets ordinary Software
  Assignments. This is the first implementation of [decision 038](038-software-deployment.md)'s
  composition rule: no nested software Workflow and no `include_role` as the only link.
- **Containers are the default install method, not the only one.** Prometheus and Grafana install
  by `container` (on an already installed Docker CE or Podman) or `native` (each project's official
  release or package repository at a pinned version). Method and runtime are spec variants of the
  same kind; switching either replaces the previous instance on the Server.
- **A container runtime is a Slurm Platform feature, composed as Managed Software.** Slurm needs no
  container runtime, but its workloads (AI ones especially) usually run containers, so a deploy may
  compose `docker-ce` or `podman` onto every compute and login node where it is not already
  installed. The Platform records only the choice; each Server's runtime is an ordinary Software
  Assignment that outlives the Platform.
- **Prometheus listens on the loopback address by default.** Its API has no authentication.
  Omnistat composes `loopback`: only Grafana on the same Server reads it, and `omnistat-query` on
  the compute nodes reads it through Grafana's read-only datasource proxy, which already serves the
  same data to anonymous viewers, so Prometheus and its lifecycle API stay off the network.
- **Swallow still never queries Omnistat's Prometheus** (decision 003, 2026-10-12 amendment).

Revision history: 2026-10-22 added the `native` install method and the container-runtime feature
(containers had been the only method, with a hard Docker CE requirement); 2026-10-26 replaced
module options from cloud-init with operator options plus KFD topology rules, added the HIP probe
and the per-deploy GRES type, and made Prometheus listen on loopback with a documented retention;
2026-10-27 replaced "never unload, reboot once" with an in-place reload of an unheld driver and
moved `omnistat-query` to Grafana's datasource proxy (original text in git history).

## Alternatives considered

- **Omnistat as a Managed Software kind.** Rejected by the operator: its target is the Slurm
  Platform's job attribution and every compute node, and it only works together with Slurm.
- **Letting `configure-omnistat` edit `slurm.conf`.** Rejected: two writers of one file, and the
  next `deploy-slurm` run would silently remove the overlay — the exact failure of the manual setup.
- **RSMI autodetection (`AutoDetect=rsmi`).** Not used: the SchedMD packages in the image ship no
  `gpu_rsmi.so`. Explicit KFD-derived files schedule, count, and isolate GPUs; `gres/gpumem` and
  `gres/gpuutil` accounting need an RSMI build and stay out of scope.
- **A fixed GRES type such as `mi308x_vf`.** Rejected for a generic name derived from the hardware
  (`gfx942`), so other Instinct models need no new setting; jobs may still request untyped
  `--gres=gpu:N`. A site whose job scripts depend on a product name sets `slurm.gpuGresType`.
- **Matching GPU models or "SR-IOV" to decide driver options** (for example a PCI device ID list
  for `pcie_p2p=0`). Rejected: it binds the deployment to hardware it must keep listing, misses new
  models, and applies the workaround where the condition does not hold. The rule tests the KFD
  topology condition the userspace bug needs instead.
- **Unloading and reloading amdgpu to apply new options.** Rejected on 2026-10-26 because
  unloading a GPU driver that a daemon or a VF guest still holds is not reliably safe; adopted on
  2026-10-27 for a driver nothing holds, after a lab test on eight MI308X virtual functions
  unloaded and reloaded it three times (about 1 s and 6 s) with every GPU passing the HIP probe.
  A held driver still gets a reboot on a disk root.
- **Rebooting a RAM-deployed node to apply options.** Rejected: the reboot erases the operating
  system and the deployment with it.
- **Requiring RAM deployments to declare driver options.** Rejected: it pushes hardware knowledge
  back to the operator; the option remains available for anyone who wants no second load.
- **Remembering each Server's derived options in the backend for the next deploy.** Not done: the
  in-place reload makes it unnecessary; kept as the fallback had the reload proved unreliable.
- **A management-address Prometheus behind a firewall allowlist.** Rejected for now: Swallow does
  not manage host firewalls, and only the local Grafana needs Omnistat's Prometheus.
- **Reopening Prometheus on the management address for `omnistat-query`, or adding basic auth whose
  credentials every cluster user must be able to read** (2026-10-27). Rejected in favor of the
  Grafana datasource proxy.
- **The reviewer's 7-day retention.** Not adopted as the default: it came from an unrelated
  exporter guide; the default stays Prometheus's own `15d`, now written into the contract and
  overridable per install.
- **A dedicated observability Server.** Deferred: the primary controller hosts Prometheus and
  Grafana, which fits the lab topology without a new role.
- **Requiring job users in the `render` group.** Rejected: user identity is site-managed (LDAP,
  local accounts), Swallow does not provision users, and a fresh image's users are not in the
  group, so GRES would hand out GPUs that jobs cannot open.
- **Freezing Prometheus scrape targets as IP addresses at acceptance.** Rejected: a `provision_os`
  deployment learns its addresses only after the OS install. Prometheus takes Server ids
  (`scrapeServerIds`) and resolves them from the run's inventory snapshot.
- **Containers as the only Prometheus and Grafana install method** (the first implementation,
  with a hard Docker CE requirement). Rejected by the operator: some sites prefer distribution
  packages over a container runtime on their monitoring host.
- **Grafana's native install from the distribution repository, or Prometheus from Ubuntu's
  `prometheus` package.** Rejected: the distribution versions lag and differ per OS, so the pinned
  version could not hold across image families. Both come from the upstream projects' official
  release and package repository instead.
- **The container runtime as a Slurm platform component installed by `deploy-slurm`.** Rejected:
  the runtime is one host program with its own lifecycle and an existing Managed Software kind;
  embedding it would give Slurm a second installer for Docker CE and Podman.

## Consequences

- A Slurm deployment with Omnistat succeeds only when its composed runtime, Prometheus, Grafana,
  and the exporter all install; a failure leaves the Workflow retryable at the failed Task.
- Uninstalling the Slurm Platform removes the exporter, the Slurm overlay, the Slurm amdgpu
  ordering, and the link; Prometheus, Grafana, and the container runtimes keep their Software
  Assignments until they are uninstalled through the software API.
- Omnistat and the Python packages it needs are downloaded at pinned versions at run time; the
  container images are pulled at pinned tags, and the native method downloads the pinned
  Prometheus release and Grafana package from upstream.
- Omnistat 1.15.0's report locates a job's nodes by GPU utilization, which SR-IOV virtual
  functions do not report, so on them it cannot build a report; that is reported to users rather
  than patched.
- Grafana's `native` method owns the Server's `grafana` package: uninstalling it removes the
  package Swallow installed, never one installed outside Swallow.
