# 066. Slurm job hooks, shared nodes, the Device Metrics Exporter, native OCI, and PMIx

- Status: Accepted
- Date: 2026-10-29
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §6.3 Slurm reference（已實作現況）的 Job hook dispatcher 與共用節點、Device Metrics Exporter、native OCI 與 PMIx

## Context

A Swallow-deployed Slurm Platform could attribute jobs to monitoring only through Omnistat, whose
Prolog and Epilog were `slurm.conf`'s `Prolog=` and `Epilog=` themselves, so a second integration
would have to overwrite them. Operators wanted AMD's Device Metrics Exporter (DME) instead: per-GPU
metrics with Slurm job attribution on nodes that several jobs share. Two other gaps showed up on
the way. A job that named no memory took the whole node (`DefMemPerNode=UNLIMITED`), so a second
job waited although CPUs and GPUs were free. And AI workloads wanted MPI and containers launched by
Slurm itself, which the cluster could not do (`MpiDefault=none`, no `oci.conf`).

Lab work on MI308X SR-IOV virtual functions showed that DME 1.5.3 reports every GPU of such a node
as `gpu_id="0"`, so its Slurm join attributes a one-GPU job to all eight GPUs and a job on another
GPU to none, while Slurm's own GRES allocation and device cgroup are correct.

## Decision

- **One Prolog/Epilog dispatcher.** `slurm.conf` always names Swallow's Prolog and Epilog
  dispatchers. They run every executable in `prolog.d`/`epilog.d` in name order, each with a
  30-second limit; a failing hook is logged and never fails the job or drains the node, because the
  hooks attribute jobs to monitoring. Swallow installs its own enabled hooks on every compute node
  (configless distributes no scripts) and removes the rest; executables an operator adds under
  other names run too.
- **Jobs share nodes by default.** Without Omnistat the partition is `OverSubscribe=NO`, each
  node's `RealMemory` keeps a share for the operating system, and `DefMemPerCPU` is the smallest
  per-CPU share over the compute nodes, so a job that names no memory gets memory in proportion to
  its CPUs. The deployment proves two such jobs run on one node together. Omnistat keeps
  `EXCLUSIVE`.
- **The Device Metrics Exporter is a Slurm Platform feature like Omnistat, never together with
  it.** It composes Prometheus and Grafana onto the primary controller exactly as Omnistat does
  (one composition path for both features), then installs AMD's pinned native package on every GPU
  compute node: no privileged container, `gpuagent` after the amdgpu loader, the exporter at port
  5001 (5000 is the RDC exporter Swallow scrapes, decision 003), Slurm attribution only, profiler
  counters off. The hook writes one file per allocated GPU atomically and removes a file only while
  it still names the ending job. The two features are mutually exclusive: Omnistat needs whole
  nodes and both would take the single hardware profiler; whether they may coexist is left to the
  team, and the dispatcher already allows it. ([ADR 068](068-device-metrics-exporter-managed-software.md)
  later made the exporter a Managed Software kind that this feature composes.)
- **Swallow reports whether the exporters tell GPUs apart.** The install compares each node's KFD
  GPU count with the exporter's distinct GPU identities and records the Platform's GPU identity,
  `distinct` or `collapsed`. A collapsed identity does not fail the deployment (the endpoint works)
  but is shown to the operator instead of letting job dashboards look right.
- **Native OCI containers and PMIx are Slurm options.** Native OCI installs crun and a
  configless-distributed `oci.conf`: rootless crun as the job user, a `ContainerPath` identical on
  every node so PMIx paths resolve across nodes, the step spool at that same path inside the
  container (a separate `MountSpoolDir` broke `MPI_Init`), and slurmd's `NOTIFY_SOCKET` kept from
  crun. The host keeps `kernel.apparmor_restrict_unprivileged_userns=1`; the distribution's
  path-scoped crun profile grants the user namespace, so no Swallow wrapper or profile is needed.
  PMIx installs the distribution's OpenMPI with PMIx on compute and login nodes; `MpiDefault` stays
  `none`. Each is proven by a probe job. Podman through `scrun` is a later slice.

## Alternatives considered

- **Slurm's own `Prolog=/etc/slurm/prolog.d/*` glob.** Rejected: any failing script drains the node
  and requeues the job, the opposite of what attribution hooks need.
- **Each integration owning `Prolog=`.** Rejected: the second one silently removes the first's
  attribution.
- **DME's official Slurm scripts.** Rejected: the Epilog removes a GPU's file unconditionally, so a
  late Epilog detaches the next job, and a file names every GPU of the job.
- **DME as the privileged container image.** Rejected: the native package needs no container
  runtime on compute nodes and no privileged mode.
- **Failing the deployment on a collapsed GPU identity.** Rejected: the exporter is still the only
  per-GPU telemetry on those nodes; the operator decides with the identity shown.
- **Hiding the collapse by aggregating dashboards.** Rejected: it would present wrong attribution
  as working.
- **Lowering only `RealMemory`, or setting `DefMemPerNode`.** Not enough alone: a job without
  `--mem` would still take every schedulable MiB; a per-CPU default shares memory as CPUs are shared.
- **A cluster-wide `MpiDefault=pmix_v5`.** Rejected: it changes every existing job's launch path.
- **A Swallow crun wrapper entering the profile with `aa-exec`.** Not needed: on the lab's Ubuntu
  24.04 nodes the packaged profile let rootless crun 1.14.1 start containers for a user without a
  login session; the deployment checks the profile is loaded instead.
- **Relaxing `kernel.apparmor_restrict_unprivileged_userns`.** Rejected: it opens user namespaces to
  every program on the node.

## Consequences

- Every Slurm deployment now has the dispatchers, even without hooks; a redeploy of an older
  Platform replaces Omnistat's former Prolog/Epilog files.
- Existing clusters get `OverSubscribe=NO` and `DefMemPerCPU` on their next redeploy, so
  `--exclusive` is how a job asks for a whole node.
- On MI308X virtual functions the exporter installs and is scraped, but the Platform reports a
  `collapsed` GPU identity until AMD fixes the per-VF identity.
