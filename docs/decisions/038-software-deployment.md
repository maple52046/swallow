# 038. Software deployment: single-software deploy target, distinct from platform, composed as a Job

- Status: Accepted
- Date: 2026-09-24

## Context

swallow deploys **platforms** (Kubernetes/Slurm) through the Workflow/Job/Task/Runner
model ([ADR 016](016-temporal-operation-orchestration.md),
[ADR 017](017-workflow-job-task-runner-model.md)): a platform is a runtime composed of
several components brought to a desired topology (roles, membership, credential). What was
missing was a way to install a **single** piece of host software — Docker CE, Podman, an
NFS server/client, and later PostgreSQL or an LDAP client — onto one or more existing
Servers, and to link such software into a platform deploy (Slurm choosing NFS today; a
future Kubernetes choosing Docker as a runtime).

Three things had to be settled and are easy to question later.

1. **What distinguishes software deployment from platform deployment.** It is tempting to
   define it by whether OS provisioning runs, whether the deploy claims a batch of Servers,
   or whether the result has cluster membership. Those are consequences, not the essence,
   and using them as the definition produces contradictions (a platform can reuse existing
   OS; software could one day provision an OS).
2. **Whether software becomes a new Platform-shaped aggregate.** Modelling software as a
   Platform (membership, credential, whole-batch claim) would create a fake platform for
   something that is a single software install.
3. **How a platform composes software** without duplicating the software's automation and
   without two operator-visible Workflows fighting over the same host.

## Decision

**The dividing line is the deploy target's granularity.** Software deployment targets *one
specific software and its variants* (roles, version, server/client, distro install path).
Platform deployment targets *one platform: a runtime composed of multiple components*.

- NFS `server` and `client` are two **variants** of one software, not two platforms.
- Docker CE and Podman are two **software kinds** (mutually exclusive), not two variants of
  one software.
- Kubernetes (k0s) and Slurm are platforms because the deploy target is the composed
  runtime, not any single package inside it.

The relationship to Server provisioning is real but is **not** the boundary. It is a
difference in default capability/orchestration: the standalone software entry expects
targets already `deployed` (like `install-exporters`), while a platform deploy commonly
composes `ensure-os`. Software Workflows may compose `ensure-os` later, and a platform may
run against existing OS; neither fact reclassifies the deployment.

**Software is not a Platform aggregate.** There is no cluster membership, no platform
credential, and no whole-batch `targetServerIds` claim. Instead a swallow-owned durable
**Software Assignment** keyed by `(serverId, kind)` records desired intent and last-applied
state so the fleet can be listed, duplicates prevented, and uninstall driven from a single
source of truth. It is swallow-owned intent, not a fourth Server status axis; the three
axes (`provisioning`/`membership`/`health`) are unchanged. When a Server leaves `deployed`
(release, recover, reinstall) its assignments are marked `absent`, mirroring the lifecycle
honesty of [ADR 036](036-provisioning-lifecycle-integrity.md).

**Platform ↔ software linkage composes the same Job, never nests a Workflow.** A platform
Workflow that needs a software component inserts the software's Job (for example
`configure-nfs`) into itself in cross-Job dependency order, on the same parent Workflow,
sharing the same Ansible roles. It must not `ExecuteChildWorkflow` an entire standalone
software Workflow (that would duplicate `wait-for-ssh`, contend for the same per-Server
lease, split retry/cancel ownership, and surface two Workflows), and it must not rely on
ansible `include_role` as the only linkage (that erases the Job boundary and the
operator-visible grouping). Roles are shared; orchestration is Jobs.

**First cut installs Docker CE, Podman, and NFS.** Docker CE and Podman are mutually
exclusive, and both are refused on a Server that is already a Kubernetes Platform member
because Docker's containerd coexisting with k0s's is undefined. NFS may coexist with a
platform, which is exactly what makes the linkage meaningful.

**First cut does not refactor Slurm's embedded NFS.** Slurm's HA state export and workload
storage remain inside the `slurm_*` roles. A generic `configure-nfs` Job that Slurm composes
is deferred to a second slice, so a single change cannot break HA. Until then the design
deliberately carries two NFS stories, and the docs flag that generic NFS is not the
Slurm-managed state NFS.

## Alternatives considered

- **Define the boundary by OS provisioning / claim / membership:** rejected — those are
  consequences and produce contradictions; the honest boundary is the deploy target.
- **Model software as a Platform:** rejected — it invents membership, credential, and
  whole-batch claim for a single package and blurs the Platform term.
- **Link platforms to software by nesting a whole software Workflow:** rejected — duplicated
  readiness, lease contention, ambiguous retry/cancel ownership, and two operator-visible
  Workflows for one intent. Compose the Job instead.
- **Link only via ansible `include_role`:** rejected as the sole mechanism — it hides the
  Job boundary and the standalone-vs-composed views diverge. Playbooks may still
  `include_role`; the launcher decides where the Job boundary is.
- **Extract Slurm's NFS immediately:** rejected for the first cut — high risk to HA state
  provisioning; slice it after the generic contract is proven.

## Consequences

- A new `software` feature in `api-server` owns the Managed Software catalog and Software
  Assignment record, and a launcher that composes `prepare-hosts` (wait-for-ssh),
  `configure-<kind>` (ansible), and `record-software` (internal) Jobs — reusing the existing
  Temporal engine, leases, retry, and `requires_attention` machinery rather than a new one.
- New Workflow kinds (`configure-docker-ce`/`uninstall-docker-ce`,
  `configure-podman`/`uninstall-podman`, `configure-nfs`/`uninstall-nfs`) with hardcoded
  playbook names (no site `playbookMappings` entry), registered in the release manifest.
- New glossary terms `Managed Software` and `Software Assignment`; `Platform` gains a
  disallowed meaning clarifying it never denotes a single software install.
- The dashboard gains a "install/uninstall software on deployed Servers" flow and an
  assignment list, consuming the new `software` HTTP contract owned by `api-server`.
- The shared `wait-for-ssh` readiness step became **authenticated**: it completes a real SSH
  login with the site automation key instead of only probing TCP, so a host whose SSH user does
  not authorize the automation key fails fast at readiness with a clear credential message rather
  than deep inside the Ansible step as a raw `Permission denied (publickey)`. This surfaced from
  software deployment (which always targets operator-picked, already-deployed hosts) but applies
  to the platform `ensure-os` Job too; see `docs/development/platform-deployment.md` §4.6.1.

## Current status

First cut in progress: catalog + assignment model, Docker CE / Podman / NFS playbooks,
the software launcher and internal record/clear steps, the OS-lifecycle assignment sweep,
the HTTP contract, and the dashboard install/uninstall flow. Slurm NFS extraction and
Kubernetes-Docker linkage are Planned (slice 2).

## Related

- [ADR 017](017-workflow-job-task-runner-model.md) — Workflow/Job/Task/Runner and the
  "reusable Job" layer this composes.
- [ADR 019](019-slurm-platform-deployment.md) — the Slurm platform whose embedded NFS a
  later slice extracts into a composable software Job.
- [`docs/development/software-deployment.md`](../development/software-deployment.md) — the
  design and integration contract this decision establishes.
