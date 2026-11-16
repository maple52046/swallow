# 083. Platform member scaling: node groups installed from Deployment Templates, member removal as a planned scale-in, and GPU validation by driver and containers

- Status: Accepted
- Date: 2026-11-14
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §2.4、§6.3（節點群組、partition 與擴縮；GPU 可用性驗收）；
  [desired-state-model.md](../development/desired-state-model.md) §5.2；[os-provisioning.md](../development/os-provisioning.md) §3；
  [validation.md](../development/validation.md) §5、§7

## Context

A deployed Slurm Platform could only grow by redeploying every member, and removing a member or changing
roles was a destructive change swallow could not carry out (its Change Plan carried a blocker). Teams add
compute nodes as hardware arrives, take failed or borrowed machines out again, and mix GPU generations in one
cluster; each new node had to be installed exactly like its peers, without anyone repeating the machine
preparation by hand.

What existed before:

- A Platform had one machine preparation (image, Deploy Target, network, cloud-init) for every member.
  Deployment Templates ([decision 009](009-deployment-template-ownership.md) (deployment template ownership))
  existed behind an experimental flag, carried no hardware facts, and nothing referenced them.
- `slurm.conf` had one partition, `main`, over every node, with one cluster-wide `DefMemPerCPU` (the smallest
  per-CPU share of any node), so one small node lowered the default of every job.
- The GPU gate was a host HIP probe that failed the deployment and drained the node. It needs a host ROCm that
  covers the GPU's gfx target, which the teams' images do not promise (their jobs run in containers that
  bring their own ROCm), and the container check bind-mounted the host's `/usr` and `/opt`.
- The acceptance gate ran on every node of every deployment, so adding one node exercised (and could drain)
  the busy ones.

## Decision

**Scaling is per node group, and each group installs from one Deployment Template.** A Platform type that
scales declares node groups in its spec: a name, one role, a Deployment Template, an optional expected GPU
count, and members. Core knows only the generic part — which template each member installs from (a
member-level template reference) and that a module can list its groups — so the same mechanism serves the
next Platform type. A new member installs from its group's template and keeps only its own network
assignment from the machine preparation. A group without a template gets one: the Platform's machine
preparation template when it names one, otherwise a template the Platform generates from the machine
preparation, named `<platform>-<group>` in the members' provisioner Integration. A generated template is
created when its realization is accepted; on an applied Platform it waits for a Change Plan, because it
creates provisioner-scoped data nobody asked for by hand. Members record the template and a digest of it,
so a member installed from an older template is visible.

**A template may carry a minimum hardware floor, checked when a member joins.** The floor (CPU cores,
memory, storage, GPU model, GPU count; each optional) is an eligibility check, not a specification: a spec
write that adds a Server below it is refused, naming what the Server lacks, and missing inventory counts as
not met. A template a node group references cannot be deleted.

**Removing members is a planned, bounded scale-in.** Removing a member is a destructive change with its own
realization: approve the Change Plan, drain the leaving nodes (bounded per node, printing what it waits for,
failing or force-ending the work at the timeout as the removal says), reconfigure the members that stay,
clean the leaving nodes, release them when asked, and drop them from the Platform. The plan is the Platform
module's declarative workflow template; only the per-Server release Tasks and the internal finalize Task,
which need per-Server parameters a template cannot carry, are built by the composition root. A type module
blocks, with a reason, what it does not scale: for Slurm the control plane (controller and login), adding
and removing in one change, and removing the last compute node. A member whose machine failed is the
main reason to remove one, so the plan never depends on the leaving member answering: it is drained
from the controller by the name swallow stores for it, and with force a member that cannot be reached
is left uncleaned (with a warning) rather than blocking the removal.

**Slurm partitions belong to the Slurm spec.** Partitions name compute groups, have exactly one default,
and carry their own limits and default memory per CPU. A Platform from before groups reads as one group per
role and the one partition `main` it always had, and is stored in that form, rendering the same
`slurm.conf` apart from node Features. Removing a partition that still has jobs is blocked with the count,
and a count that cannot be read blocks too.

**A scheduling change reconfigures in place.** A change of groups and partitions alone, the reconfigure
of a scale-in, and a rescan render the controllers' configuration again and apply it with `scontrol
reconfigure`; they redeploy no member and reinstall no feature. Each compute node's detection records what
it found on the node, so the others are read from that record; a rescan detects only its node again and
the gate validates only that node.

**Adding compute nodes installs only them.** A scale-out provisions and installs only the new members,
reads the others' hardware records, applies the controllers' configuration in place, exports the shared
workload filesystem to the new members, installs the Platform's features on them alone, and validates
them; it does not reinstall the Platform's Grafana, so its monitoring token stays valid, and a fault on
a member already in the Platform does not fail the members being added. Like a scale-in it keeps the
Platform active. An uninstall therefore targets the Platform's members rather than its latest deploy's.

**GPU validation relies on the driver and containers.** The hard gate on a GPU node is what the driver
reports: the KFD device count and an AMD SMI health read. The host HIP probe is advisory. The acceptance gate
proves the GPUs inside a real container mounting only the devices the job may open: it builds a HIP
probe with `hipcc` for `gfx90a`, `gfx942`, and `gfx950` inside AMD's official ROCm development image at a
pinned tag, the kind of image user workloads run. Each GPU node pulls that image straight from its
registry, not through the Local Repository; a node that cannot pull it records the container check as
skipped with a warning, so the driver checks stay the hard gate, while a pulled image whose probe does not
build or whose kernel fails is a failure. A node with fewer GPUs than its group expects is marked degraded and scheduled
last instead of failing the deployment. A deployment that adds members validates only those members, and
the gate runs after the composed software so the container runtime is there.

## Alternatives considered

- **Keep redeploying every member to add one**: rejected — the existing members' configuration is applied
  in place with `scontrol reconfigure`, and only the new members need installing and validating.
- **Store hardware specifications in the template**: rejected — the same image and cloud-init install
  machines with different CPUs and memory, and Slurm reads each node's resources itself; the floor keeps
  only the eligibility question.
- **A node group per hardware class, derived from the inventory**: rejected — the inventory suggests the
  first grouping, but the group is the identity afterwards; a node that loses a GPU stays in its group.
- **Model node groups and partitions in core**: rejected — partitions are Slurm's language; core keeps the
  generic member-template reference and the module owns the rest.
- **Create generated templates silently during the upgrade of an existing Platform**: rejected — the first
  Change Plan after the upgrade lists them, so nothing appears in the provisioner without being seen.
- **Build and publish a swallow GPU probe image**: rejected — it adds a release artifact and a Local
  Repository dependency for a test that AMD's own image, the one workloads use, can run after a short
  build; a Site without upstream access loses only that one check.
- **Keep the host HIP probe as a gate**: rejected — it fails on images whose host ROCm does not cover the
  GPU, while the workloads, which bring their own ROCm in containers, would run.
- **Rerun the convergent deploy for a scheduling change or a rescan**: rejected — the result is the same,
  but every member's installation, the monitoring features, and the Platform's lifecycle are touched for a
  change that only the controllers' configuration needs, and other changes are refused meanwhile.
- **Plan the whole scale-in in Go**: rejected — module-declared templates are the path for Platform
  workflows; Go builds only what a template cannot express.

## Consequences

- Compute nodes of a Slurm Platform can be added and removed through the Platform spec, the dashboard, and
  the CLI, each change visible as a Change Plan when it is destructive. The control plane stays fixed until
  a later decision covers moving the controller state and workload servers.
- Deployment Templates leave the experimental flag and become load-bearing: a referenced template cannot be
  deleted, and editing one changes how later members install, not the existing ones.
- Mixed GPU generations are a group and a partition each; a group mixing models is a warning unless its
  template names a model.
- GPU nodes need access to AMD's image registry for the container GPU check; without it the gate passes
  with a warning on that check. The first check on a node spends the pull and the build, so the check's
  timeout allows for a multi-gigabyte image.
- Draining is bounded and can end jobs only when the removal says so; a removal that does not force waits
  for running work and fails visibly at the timeout.

## Revision history

- 2026-11-14: accepted.
- 2026-11-14: the container GPU check builds its probe in AMD's ROCm image instead of a swallow-built probe image.
- 2026-11-16: scheduling changes, a scale-in's reconfigure, and a rescan reconfigure in place instead of redeploying.
- 2026-11-16: a scale-out installs and validates only the members it adds instead of redeploying every member.
