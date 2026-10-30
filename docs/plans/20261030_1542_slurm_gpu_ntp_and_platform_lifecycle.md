# Slurm GPU Stack, Time Synchronization, and Platform Lifecycle

## Purpose

Consolidate the late-October 2026 implementation manuscripts — the work that
shipped in `v0.1.0-rc.5` — into one durable plan for Swallow's Slurm GPU
stack (GRES, driver readiness, Omnistat, Device Metrics Exporter, native OCI,
PMIx), Control-Plane NTP and post-provision time synchronization, Platform
Claims, Platform removal controls, and the Platform deployment and Workflow
diagnostics UI.

Nearly every source manuscript records implemented and lab-accepted work. This
plan therefore treats their latest explicit decisions as the current baseline,
preserves remaining follow-up work and open decisions, and records which
earlier designs were superseded so they are not reopened.

## Source Scope

This consolidation processed 10 draft manuscripts from
`docs/plans/manuscripts/`, excluding the consolidation specification
`README.md`:

- Slurm GPU stack and observability:
  `20261021-omnistat-slurm-gpu.md`,
  `20261026-gpu-node-readiness.md`,
  `20261027-ram-deploy-amdgpu-reload.md`,
  `20261029-slurm-stabilize-dme-oci-mpi.md`, and
  `20261030-dme-managed-software.md`.
- Time synchronization: `20261028-control-plane-ntp.md`.
- Platform ownership and lifecycle:
  `20261029-platform-claim.md` and
  `20261029-platform-lifecycle-button.md`.
- Dashboard deployment and diagnostics:
  `20261024-platform-deploy-ui-refactor.md` and
  `20261026-workflow-step-event-diagnostics.md`.

## Consolidated Background

After the AMD GPU Operator work for Kubernetes, this period turned Slurm into a
GPU-capable Platform validated on `lab-afde-mi308-{1,2,3}` (MI308X SR-IOV VFs):

- GPU GRES became a core `deploy-slurm` capability, then Omnistat and the AMD
  Device Metrics Exporter (DME) were added as mutually exclusive GPU monitoring
  stacks composed with Prometheus and Grafana Managed Software.
- An external review of the GPU Slurm deployment found driver-option, workload
  readiness, Prometheus exposure, retention, and GRES naming gaps. They were
  fixed without binding deployment to GPU models, PCI IDs, or SR-IOV.
- RAM (ephemeral) deployments exposed that "reboot to apply driver options"
  destroys the in-memory OS, which led to in-place amdgpu reload.
- Slurm was stabilized (job hook dispatcher, shared-node memory defaults,
  idempotent reruns) before adding DME, native OCI containers, and PMIx.
- Time synchronization moved to the control plane: provisioned OSes are pointed
  at the Swallow host's NTP service, and Platforms only check clocks.
- Platforms gained an owned Platform Claim on their Servers so Release and
  Delete can no longer silently break a Platform, and the Dashboard merged
  Uninstall and Delete into one removal control.

Where manuscripts conflict, the newest explicit decision wins. In particular:
Site NTP Configuration and the generic `check-time-sync` Task were removed;
driver options are reloaded in place instead of always rebooting; Prometheus
listens on loopback by default; Omnistat's Prolog wrapper became a dispatcher
hook; DME moved from a Platform-only feature Job to composed Managed Software;
native OCI needs no Swallow crun wrapper and sets no `MountSpoolDir`; and the
three-way removal radio dialog was replaced by a split button.

## Confirmed Decisions

### Slurm GPU stack

- GPU GRES is part of `deploy-slurm`, enabled automatically on compute nodes
  whose GPU Inventory has AMD `compute` GPUs (`swallow_slurm_gpu_compute_ids`).
  A selected node without KFD GPUs fails with `[slurm_gpu_missing]`.
- GRES is derived from KFD topology (`drm_render_minor`, `gfx_target_version`),
  never by counting `/dev/dri/renderD*`. `AutoDetect=off`,
  `Flags=amd_gpu_env,explicit`, no RSMI autodetect or `gres/gpumem`
  accounting. `slurm.gpuGresType` may override the derived type name.
- amdgpu loading is deferred to the shared `amdgpu_loader` role
  (`swallow-amdgpu-load.service`), used by both the Kubernetes GPU Operator and
  Slurm. GPU-dependent units use `Requires=` plus `After=` on the loader.
- Driver options live in the Swallow-owned
  `/etc/modprobe.d/swallow-amdgpu.conf`: operator `gpuDriver.moduleOptions`
  plus topology-derived rules. The only rule today: a KFD GPU node with both
  `io_link` and `p2p_link` to the same peer gets `pcie_p2p=0`. Operator keys
  always win.
- Option names are checked with `modinfo` before loading
  (`[amdgpu_option_unknown]` with a nearest-name hint). When the loaded driver
  has different parameters, the loader and strict dependents stop, and the
  driver is reloaded in place if `refcnt` is 0. A busy driver reboots a disk
  root; a memory-backed root fails with `[amdgpu_reload_blocked]` and lists the
  holders. Applied values are verified (`[amdgpu_option_not_applied]`).
- GPU workload acceptance uses a hardware-agnostic HIP probe (ctypes over
  `libamdhip64`) three times per node, plus Slurm gates (no GPU, one GPU, whole
  node). A failure drains the node and fails the Task.
- `/dev/kfd` and render nodes are opened to job users by udev; the access
  boundary is the `ConstrainDevices=yes` device cgroup.
- `slurm.conf` has a single owner (`deploy-slurm`). Prolog and Epilog always
  point at Swallow dispatchers (`PrologFlags=Alloc,Contain`) that run ordered
  hooks with timeouts, log failures, and always exit 0. Omnistat is hook
  `10-omnistat`; DME is `20-device-metrics-exporter`.
- Without Omnistat, partitions use `OverSubscribe=NO` and `DefMemPerCPU` is
  derived from each node's schedulable memory after an OS reserve, so jobs
  without `--mem` can share a node. Omnistat keeps `OverSubscribe=EXCLUSIVE`.
- Omnistat and DME are Slurm Platform features, mutually exclusive in the
  first version. Both compose Prometheus and Grafana Managed Software onto the
  primary controller (runtime, then Prometheus, then Grafana, then the
  feature), with `installMethod` `container` (Docker CE or Podman) or
  `native`.
- Prometheus listens on loopback by default (`management` must be chosen
  explicitly) with `15d` retention by default. `omnistat-query` reaches it
  through Grafana's read-only datasource proxy.
- DME is also a standalone Managed Software kind `device-metrics-exporter`
  (native `amdgpu-exporter` 1.5.3 deb, port 5001, Ubuntu 24.04 only, no spec
  fields). Slurm composes this software Job and records Software Assignments
  for GPU compute nodes. Standalone install refuses Kubernetes members and
  Servers with a Platform Claim.
- DME's identity gate reports `collapsed` instead of claiming per-GPU
  attribution when distinct `gpu_id` values are fewer than KFD GPUs (as on
  MI308X VFs). The deployment still succeeds.
- Native OCI (`slurm.ociContainers`) installs rootless `crun` with a
  controller-owned `oci.conf`, keeps
  `kernel.apparmor_restrict_unprivileged_userns=1`, relies on Ubuntu's crun
  AppArmor profile, excludes `NOTIFY_SOCKET`, and sets no `MountSpoolDir`.
- PMIx (`slurm.pmix`) installs Ubuntu OpenMPI 4.1.6 with PMIx 5.0.1, keeps
  `MpiDefault=none`, and verifies host and native-OCI MPI launches. Containers
  use host networking; same-node multi-rank containers need
  `OMPI_MCA_btl=self,tcp`.
- Container runtime selection for Slurm: Docker CE or Podman (or neither),
  with native OCI as an independent choice. The deployment page describes what
  is installed and does not advise how jobs should run.
- Swallow maintains upstream patches only as an exception (ADR 064). Each
  Omnistat patch is documented with its symptom, check, and removal condition,
  guarded by `TestOmnistatPatchesAreDocumented`.

### Time synchronization

- The Control-Plane NTP Server is a host service. With co-located MAAS, MAAS's
  chronyd is the server and Swallow only sets its upstream; without MAAS,
  `local-ntp.sh` installs chrony. Installing the server and setting the
  upstream are separate steps; `swallowctl ntp upstream` controls the upstream.
- Every provisioned OS gets `configure-ntp-<serverId>` after
  `provision-<serverId>`, pointing at `SWALLOW_API_NTP_SERVER` (production:
  `SWALLOW_ADDRESS`). If it is unset, accepting an OS deployment returns `409`.
- `configure-ntp` configures the client already in the OS (`systemd-timesyncd`,
  then `chrony`, then `ntpd`) and installs chrony only when none exists.
- Platforms do not configure time. Kubernetes and Slurm install playbooks run
  `time_sync_check` first; the per-deploy `timeSyncCheck` is `enforce`
  (default) or `warn`.

### Platform ownership and removal

- A Server carries a Swallow-owned `PlatformClaim { platformId, claimedAt }`,
  separate from observed membership. It is written atomically for all targets
  when a deployment is accepted and released on successful uninstall, Platform
  record deletion, or acceptance rollback. Failed deploys keep their claims.
- Operator Release, Recover of a `deployed` Server, and Server Delete are
  refused with `409` and the Platform name while a claim exists. Workflow-
  internal `release-os` steps are not gated.
- `POST /platforms/{id}/uninstall` accepts `cancelActiveWorkflows` (same
  semantics as decision 061) and `deleteRecord` (delete the Platform record
  only after a successful uninstall).
- The Dashboard shows one red removal control per Platform: a split button
  (`Uninstall platform` plus `Delete only…`) while uninstall is possible, and a
  single `Delete platform` otherwise. The Uninstall dialog's confirm button is
  also a split button with the "and delete record" variant.

### Dashboard

- `/platforms/deploy` is a Kubernetes/Slurm selector; each Platform type has
  its own deployment flow sharing only layout, Server picker, OS provisioning,
  and accessibility primitives. No API, DTO, or deployment policy changed.
- Workflow event groups show the retained Step failure diagnostic beside the
  affected host and offer copying the Ansible task name, reusing the existing
  stderr endpoint rather than adding output to the event API.

## Architecture and Design Principles

- Never encode hardware names in deployment. Decide only from operator intent,
  facts read from the node at deploy time, and functional acceptance. Express
  quirks as observable conditions.
- Every default is derived or written in the contract, and can be overridden.
- One owner per host configuration file (`slurm.conf`, the amdgpu options
  file); other features contribute through that owner or through hooks.
- Platforms compose Managed Software Jobs into their parent Workflow instead of
  nesting child Workflows or embedding software roles.
- Report the truth instead of masking it: identity collapse, unverifiable
  driver parameters, and known upstream limitations are surfaced, not hidden
  by aggregation.
- Long-blocking dialogs are avoided: superseding blocking work happens in the
  backend, and the request returns immediately.
- Swallow-owned protection facts (Platform Claim) are owned data and are
  checked on operator paths, not inferred from projections.

## Functional Scope

The consolidated baseline includes:

- Slurm deployment with GPU GRES, driver option management with in-place
  reload, HIP readiness gates, job hook dispatcher, shared-node memory
  defaults, Omnistat or DME monitoring, optional Docker CE/Podman runtime,
  native OCI, and PMIx, all verified with idempotent reruns and clean
  uninstall.
- Prometheus and Grafana Managed Software with container (Docker CE, Podman
  Quadlet) and native install methods, loopback-by-default Prometheus, and
  configurable retention.
- Standalone DME Managed Software for single GPU Servers.
- Control-Plane NTP installation and upstream management through
  `swallowctl`, post-provision client configuration, and Platform clock checks.
- Platform Claims protecting Release, Recover, and Delete; uninstall with
  `cancelActiveWorkflows` and `deleteRecord`; merged removal controls in the
  Platform detail page and bulk toolbar.
- Type-specific Platform deployment flows and host-level Workflow failure
  diagnostics.

## Constraints and Rules

- Do not match GPU model, PCI device ID, or SR-IOV in deployment logic.
- Never reboot a memory-backed root (`/` on overlay or tmpfs); a static test
  guards the single reboot in the amdgpu role.
- Prolog/Epilog hooks must never drain nodes or requeue jobs; the dispatcher
  always exits 0.
- Prometheus must not be reachable from the network by default; its lifecycle
  APIs stay on loopback.
- Omnistat and DME stay mutually exclusive until the team decides otherwise.
- `configure-ntp` belongs to OS provisioning only; Platforms only check time.
  Already-deployed and `existing_os` Servers are not reconfigured.
- Platform Claims are released only by successful uninstall, record deletion,
  or acceptance rollback; failure paths keep them.
- Standalone DME is refused on claimed Servers and Kubernetes members.
- Deployment UI refactors must not change API payloads, domain types, or
  deployment policy.
- Upstream patches are an exception; each must be documented with how to tell
  whether a newer upstream still needs it.

## Data Model and Format Notes

- `Platform.omnistat`: `enabled`, `version`, `installMethod`,
  `observabilityServerId`, `state` (`installing | ready | failed | absent`),
  `lastWorkflowId`, `updatedAt`; projected from `install-omnistat` fenced by
  `lastWorkflowId`.
- `Platform.deviceMetricsExporter`: enablement, state, GPU identity
  (`collapsed` when attribution is not per-GPU), fenced by the record's
  Workflow id; Monitoring Link `Device Metrics Exporter Grafana`.
- `Platform.containerRuntime`, `slurm.ociContainers`, `slurm.pmix`,
  `slurm.gpuGresType`, `gpuDriver.moduleOptions` are recorded on the Platform.
- Prometheus spec adds `listen` (`loopback` | `management`), `retention`
  (default `15d`), `scrapeServerIds`, and `scrapePort`; addresses are resolved
  from the run's inventory snapshot, never frozen IPs.
- Managed Software catalog uses `installMethods` and `containerRuntimes`
  instead of `requires`. DME has `installMethods: []` and no spec fields.
- `Server.platformClaim` (`platformId`, `claimedAt`) is indexed by
  `platformClaim.platformId`; a startup reconcile backfills missing claims and
  releases stale ones.
- Applied amdgpu options and their sources are recorded on the node in
  `/var/lib/swallow/amdgpu/options.json`.
- Playbooks were renamed to `deploy-device-metrics-exporter` and
  `uninstall-device-metrics-exporter`; `configure-device-metrics-exporter`
  remains a manifest alias so existing Workflow retries resolve.

## CLI / API / Config Notes

- Slurm deploy request fields: `omnistat`, `deviceMetricsExporter`,
  `containerRuntime`, `slurm.ociContainers`, `slurm.pmix`,
  `slurm.gpuGresType`, `gpuDriver.moduleOptions`, and `timeSyncCheck`.
- `POST /platforms/{id}/uninstall` accepts `cancelActiveWorkflows` and
  `deleteRecord`. CLI: `platforms uninstall`, `provisioning release`, and
  `provisioning recover` accept `--cancel-active-workflows`; `deleteRecord`
  passes through the existing `--file` body.
- `409` refusals caused by a Platform Claim name the Platform; the CLI hints at
  `platforms uninstall` or `platforms delete`.
- Site NTP Configuration (`/sites/{id}/ntp`, `swallow sites ntp`) was removed.
- Installation config: `SWALLOW_API_NTP_SERVER` (api-server),
  `SWALLOW_NTP_UPSTREAM` and `swallowctl ntp upstream [show|set|default]`.
- Managed Software still has no CLI group.

## Implementation Plan

1. Treat all source manuscripts as the implemented baseline shipped in
   `v0.1.0-rc.5`. Do not reimplement them from this consolidation. This
   includes native OCI and PMIx, which are complete and documented in the
   platforms contract, ADR 066, `platform-deployment.md`, and the bilingual CLI
   usage guide.
2. Fix or re-baseline the pre-existing failing Dashboard e2e test
   "Workflow list polling preserves last-good data", which also fails on
   unmodified HEAD.
3. Make the development host's firewalld NTP allowance permanent where lab
   machines depend on it, or document it as a lab prerequisite; the runtime
   rule is lost on reboot.
4. Take remaining open decisions below through new contract/ADR-backed plans,
   with lab acceptance proportional to external side effects.

## Non-goals

- Omnistat as a Managed Software kind; Swallow querying Omnistat's Prometheus;
  a separate observability node; toggling Omnistat after deployment.
- Splitting Slurm's embedded NFS into generic NFS software in this batch.
- RSMI autodetect and `gres/gpumem` accounting.
- Remembering derived driver options per Server in the backend (fallback only
  if in-place reload proves unreliable).
- Host firewall management or allowlists.
- Temporal-driven automatic rollback of Slurm deployments; host cgroup
  `MemoryMin`/`MemoryMax`.
- A Managed Software `chrony` kind; cloud-init `ntp:` synthesis (kept only as
  an alternative if clocks must be correct before SSH).
- Container DME, DME AI NIC monitoring, wiring DME into Swallow Monitoring, and
  a Managed Software CLI.
- MAAS-side release/delete protection, and restricting Deploy OS on claimed
  Servers.
- Reverting the public removal verb to Forget, or deleting records on every
  uninstall.

## Open Questions

- May Omnistat and DME coexist? The dispatcher allows it; the team has not
  decided.
- MI308X VF identity collapse in DME needs an AMD fix; until then Swallow only
  reports it.
- `omnistat-query` cannot produce reports on GPUs without utilization metrics
  (SR-IOV VFs). Version testing and how to tell users is tracked by the user.
- How should already-deployed Servers re-converge time settings? No entry
  point exists yet.
- If MAAS is not co-located (not supported by today's installation), no NTP
  server runs at `SWALLOW_ADDRESS`.
- What happens when a Kubernetes deployment targets a Server that already has
  standalone DME?

## Future Work

- Podman and rootless Docker with `scrun` (slice 3b) and their MPI (4b):
  release-matched `scrun.lua`, `SrunArgs="--mpi=pmix_v5"`, and the rootless
  Docker daemon settings from the SchedMD containers guide.
- Extract Slurm's managed NFS into a composable `configure-nfs` Job.
- Production-grade or external HA controller-state storage for Slurm.
- A "re-apply time synchronization" action for deployed Servers if field use
  needs it.
- Continue consolidating future manuscript batches so implemented history,
  remaining work, and open decisions do not diverge.
