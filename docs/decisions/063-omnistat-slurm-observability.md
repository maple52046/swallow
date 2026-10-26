# 063. Slurm deployment configures AMD GPU GRES and optionally installs Omnistat

- Status: Accepted
- Date: 2026-10-21

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
  the GPU Operator (`swallow-amdgpu-load.service`, decision 062), now a shared role, and `slurmd`
  is ordered after it. The playbook derives each GPU's render node from the KFD topology (never by
  counting `/dev/dri/renderD*`, because XCP partitions add extra nodes), writes `GresTypes=gpu`,
  `Gres=gpu:<gfx target>:<n>`, and a `gres.conf` with `AutoDetect=off`, explicit `File=` lists, and
  `Flags=amd_gpu_env,explicit`; `ConstrainDevices=yes` stays on. A selected node whose KFD shows no
  GPU fails the deploy. (Amended 2026-10-26: the GRES type defaults to that derived architecture
  name but a deploy may name one for every GPU node, `slurm.gpuGresType`, for example `mi308x_vf`.)
- **Driver options never name hardware** (amended 2026-10-26, replacing "module options come from
  cloud-init"). The shared loader writes a Swallow-owned `/etc/modprobe.d/swallow-amdgpu.conf`
  before the driver loads, from the operator's `gpuDriver.moduleOptions` plus options a topology
  rule derives from what KFD reports. Today's only rule: a GPU node that reaches the same peer
  through both an XGMI `io_link` and a PCIe `p2p_link` overflows libhsakmt's peer array (heap
  corruption when HIP processes exit), so the rule adds `pcie_p2p=0`. A rule option is recorded
  and kept once applied, because applying it removes the condition; an operator option always
  wins. A driver loaded with other parameters is never unloaded: the node reboots once, and every
  option is verified in `/sys/module/amdgpu/parameters`. Units that need the GPUs (`slurmd`,
  `omnistat`) require the loader; k0s is only ordered after it because it also runs non-GPU work.
- **GPUs are proven with the HIP runtime, not an inventory listing** (amended 2026-10-26). A
  host-only HIP probe (ctypes against `libamdhip64`, no device code, so any architecture) must see
  as many GPUs as KFD lists and allocate, fill, and free memory on each, three times, each in its own
  process so a crash at exit fails it. It runs on every GPU node and again through Slurm (no GPU
  request sees none, one GPU request sees one as device 0, a whole-node request passes the probe);
  a failure drains the node and fails the deployment.
- **The device cgroup is the GPU access boundary.** A udev rule opens `/dev/kfd` and the render
  nodes (mode 0666) on GPU nodes, so a job's user needs no membership in the node's `render`
  group; Slurm's device cgroup still admits a job only to its allocated GPUs (verified: a job
  without `--gres` cannot open a render node). The trade-off is that a process outside Slurm on a
  compute node (for example an SSH login without `pam_slurm_adopt`) could open the GPUs, the same
  exposure as adding every user to `render`.
- **Omnistat is a Slurm Platform feature, not Managed Software.** The deploy request carries
  `omnistat.enabled`; the Platform records Omnistat and its Omnistat State, projected from the
  `install-omnistat` Task like the GPU Operator State. The Slurm-side overlay (Prolog/Epilog,
  `PrologFlags=Alloc,Contain`, an exclusive partition because Omnistat's job file is node-wide) is
  rendered by `deploy-slurm`, the single owner of `slurm.conf`, so a redeploy re-applies it. The
  Prolog succeeds without attribution until the exporter exists, so the order of Jobs never drains
  a node. A separate `configure-omnistat` playbook installs the pinned exporter (with a
  compatibility patch for SR-IOV guests that reject the bulk GPU metrics call) and returns the
  Grafana URL, which Swallow records as the Monitoring Link `Omnistat Grafana`.
- **Prometheus and Grafana are new Managed Software kinds, composed as Jobs.** With Omnistat
  enabled, the `configure-slurm` Workflow composes `configure-prometheus` and `configure-grafana`
  Jobs onto the primary controller after the Slurm install and before `configure-omnistat`, each
  with its ansible Task and record Task, so the primary controller gets ordinary Software
  Assignments. This is the first implementation of decision 038's composition rule: no nested
  software Workflow and no `include_role` as the only link.
- **Containers are the default install method, not the only one** (amended 2026-10-22). Prometheus
  and Grafana install by `container` (the official image under Docker Compose on Docker CE or a
  Podman Quadlet unit on Podman; the runtime must already be installed) or `native` (Prometheus's
  checksum-verified release binary under a Swallow systemd unit, Grafana's pinned package from
  Grafana's apt/yum repository with a Swallow drop-in). The method and the runtime are spec
  variants of the same kind; switching either replaces the previous instance on the Server. The
  deploy request's `omnistat.installMethod` picks the method for both; the `container` method also
  composes the primary controller's runtime (the deploy's container runtime, else the runtime
  already assigned there, else Docker CE).
- **A container runtime is a Slurm Platform feature, composed as Managed Software** (amended
  2026-10-22). Slurm needs no container runtime, but its workloads (AI ones especially) usually
  run containers, so `containerRuntime.kind` (`docker-ce` or `podman`) composes that runtime's Job
  onto every compute and login node where it is not already installed. The Platform records only
  the choice; each Server's runtime is an ordinary Software Assignment that outlives the Platform.
- **Prometheus listens on the loopback address by default** (amended 2026-10-26). Its API has no
  authentication, so the Prometheus spec's `listen` is `loopback` unless an install asks for
  `management`, and its `retention` (default `15d`) is a documented, overridable spec field.
  Omnistat composes `loopback`: only Grafana on the same Server reads it, which every install
  method reaches through the host network.
- **Swallow still never queries Omnistat's Prometheus** (decision 003, 2026-10-12 amendment).

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
- **Unloading and reloading amdgpu to apply new options.** Rejected: unloading a GPU driver that a
  daemon or a VF guest still holds is not reliably safe; one reboot is.
- **A management-address Prometheus behind a firewall allowlist.** Rejected for now: Swallow does
  not manage host firewalls, and only the local Grafana needs Omnistat's Prometheus.
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
- Grafana's `native` method owns the Server's `grafana` package: uninstalling it removes the
  package Swallow installed (marked by Swallow's systemd drop-in), never one installed outside
  Swallow.

## Current status

Implemented in `api-server`, `dashboard`, and `cli`.
