# 070. A generic core with replaceable provider adapters and extension modules

- Status: Accepted
- Date: 2026-10-30
- 現行規格：[core-and-extensions.md](../development/core-and-extensions.md)

## Context

swallow was meant to be a generic core that grows outward: Kubernetes, Slurm, and Managed Software
were added on top of it, and more Platforms (Docker Swarm, later possibly OpenStack or a lightweight
VM platform) and a second GPU vendor (NVIDIA, beside AMD) are expected. Within two years the
Platforms are expected to stay at Kubernetes and Slurm, possibly with Docker Swarm. Developers from
outside the team are expected to join the project and build modules in this repository alongside
the team; external teams developing and owning modules independently, against a published
protocol, is not a near-term goal. The target user is a data center operator who
wants a working GPU cluster out of the box, so every integrated Platform has to be done correctly
and in depth.

The code and the documents did not reflect that intent:

- **Platform and GPU-vendor knowledge grew into the core.** `internal/platform/application/deploy_platform.go`
  is about 1,430 lines; `platform/domain` holds the AMD GPU Operator, Omnistat, and Device Metrics
  Exporter models; completion handling is routed by Task ID strings; the Slurm composition lives in
  the composition root.
- **External systems' vocabulary crosses the adapter boundary.** The Boot ISO domain code hard-codes
  the MAAS rack (its HTTP boot port, chain URLs, and iPXE template); the Workflow domain has an
  `ansible_execution.go` and `RunnerKind` values `ansible` and `maas`; the Site domain lists a
  `ProviderKindMAAS`.
- **The documents disagree with each other.** [ADR 017](017-workflow-job-task-runner-model.md) says
  adding a platform is contributing a playbook, while in practice it needs a Go launcher, trusted
  variables assembly, and a composition adapter. [ADR 001](001-system-ownership-boundaries.md)
  positions swallow as an integration and correlation layer whose product is the correlation.
- **The boundary is achievable.** Temporal appears only in `operation/infra/temporalworkflow` and
  the composition root; no domain or use case imports it.

[ADR 069](069-swallow-owns-desired-state.md), decided together with this ADR, makes swallow the
owner of desired state. That only holds if no external system is part of the core.

## Decision

1. **The core is ports and adapters.** The core contains only the domain model (resources, desired
   and observed state, Change Plans), the Workflow model and its semantics, the controllers, and one
   provider interface (port) per kind of external system. It contains no vendor, product, Platform,
   or GPU-vendor names, types, or knowledge.
2. **Every external system is a replaceable provider adapter.** Today: OS provisioning (MAAS),
   Workflow executor (Temporal), host automation (Ansible), store (MongoDB), metrics and alerts
   (Prometheus and Alertmanager), BMC and hypervisor (Redfish and libvirt). An adapter translates its
   system's vocabulary, states, identifiers, and errors into the core's language; none of them cross
   the adapter. MAAS is the OS provisioning adapter shipped first; Temporal is the executor and can be
   replaced the same way.
3. **What swallow manages is an extension module.** There are three kinds: Platform modules
   (Kubernetes, Slurm), Accelerator modules (AMD; a separate axis from Platforms, so a GPU vendor is
   supported once rather than once per Platform), and Software modules (the Managed Software
   catalog). Platform-specific and GPU-vendor-specific logic lives only in modules and reaches the
   core through the module contract.
4. **The architecture is the generic core; the product is a distribution.** What a customer receives
   is the core plus a validated set of adapters and modules, recorded in a support matrix per
   release.
5. **Modules evolve in three stages, and only the first is built now.**
   - Stage 1: modules live in the repository and are developed by the team and outside
     contributors together. The core loads them from a manifest that carries a contract version
     from the start. Each module is one directory with a named owner, and that directory is the
     module's package format. A module authoring guide tells a contributor what a module may and
     may not do. Conformance tests and an import check keep the shape pluggable and keep
     contributions inside module boundaries. A release is assembled from one configuration that
     selects its modules, and the support matrix is produced from the same configuration.
   - Stage 2: module lifecycle on an installation: enabling, disabling, or updating one module
     without upgrading swallow, contract compatibility checks between the core and a module
     version, and offline module update bundles. An update covers only data and host automation
     content; a Go hook change still needs a swallow upgrade. Started when customers need a module
     combination or update cadence that differs from swallow's releases.
   - Stage 3: modules owned outside the project: signing and trust, packages that carry no Go
     hooks, and out-of-process hooks (webhook or gRPC). Started when someone outside the project
     needs to develop and release a module independently of swallow's releases.

   Adding a third Platform is not a stage trigger; it is the point to review whether the module
   contract is general enough.

## Alternatives considered

- **MAAS and Temporal as part of the core.** Rejected: replacing the provisioner (for example with
  Ironic) or the executor would mean changing the core, and the core would learn their lifecycle
  semantics (MAAS status codes, Temporal history), which conflicts with ADR 069's ownership model.
- **Add a second adapter now to prove each port is replaceable.** Rejected: there is no demand for
  one. Per-port conformance tests and an import check keep the shape instead.
- **A pure distribution, with Platform code in the core.** Rejected: moving to another Platform would
  need a redesign, which is what the generic core exists to avoid.
- **Installable, signed module packages now.** Rejected: outside developers are expected to
  contribute in this repository, not to ship modules on their own, and every module ships inside a
  swallow release, so per-module installation and signing would solve a problem nobody has and
  would likely be designed wrong. Per-installation module lifecycle is recorded as stage 2 and
  signing as stage 3, each with its trigger.
- **GPU-vendor differences inside each Platform module.** Rejected: adding NVIDIA would mean
  rewriting every Platform module.
- **Return to an integration and correlation layer.** Rejected: it conflicts with ADR 069, where
  swallow owns desired state.

## Consequences

- New code has a decided home: what swallow manages goes in a module, how it talks to an external
  system goes in an adapter, and only what is neither goes in the core.
- The existing leaks become migration work: Boot ISO's MAAS knowledge moves into the MAAS adapter;
  Runner kinds are named by port (host automation, OS provisioning) rather than product; Kubernetes,
  Slurm, and AMD code moves out of `internal/platform` and `internal/app` into modules. Identifier
  values already stored or on the API (such as the `RunnerKind` value `maas`) stay as opaque adapter
  identifiers, so no data migration or contract change is needed for them.
- ADR 017's "adding a platform is contributing a playbook" becomes "adding a platform is contributing
  a module", whose host automation content is still a playbook today.
- The dashboard needs a way to show modules without bespoke screens (a form generated from the
  module's spec schema), alongside first-party module screens.
- An outside contributor can add or change a module by working in its directory and following the
  authoring guide, and review can be routed to the module's owner, without the contributor having to
  learn the whole core first.
- Cost: a module contract, a manifest loader, conformance tests, an import check, module ownership,
  and the authoring guide now; the migration of existing Platform and AMD code; and a support matrix
  to maintain per release.
