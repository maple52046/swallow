# Consolidated AI Plans

## Purpose

Consolidate the October 2026 implementation manuscripts into one durable plan
for Swallow's operator experience, provisioning and orchestration reliability,
monitoring, GPU management, installation recovery, and documentation.

Most source manuscripts record work that is already implemented. This plan
therefore treats their latest explicit decisions as the current baseline,
preserves the remaining acceptance work and unresolved questions, and avoids
reopening superseded designs.

## Source Scope

This consolidation processed 30 draft manuscripts from
`docs/plans/manuscripts/`, excluding the consolidation specification
`README.md`:

- Server inventory and Dashboard presentation:
  `20261006-server-list-layout.md`, `20261006-server-list-actions.md`,
  `20261007-server-list-power-deployment.md`,
  `20261007-os-image-list-ui-refactor.md`,
  `20261007-dashboard-visual-review.md`,
  `20261007-platform-list-origin-label.md`,
  `20261008-boot-isos-ui-refactor.md`,
  `20261008-software-deployment-ui-refactor.md`,
  `20261009-workflow-execution-running-spinner.md`,
  `20261012-releasing-activity-ux.md`,
  `20261013-integration-settings-page.md`,
  `20261019-dashboard-ops-polish.md`, and
  `20261019-search-input-truncation.md`.
- Server enrollment and OS provisioning:
  `20261006-server-enrollment.md`,
  `20261006-gpu-inventory-classification.md`,
  `20261007-dashboard-os-provisioning-flow.md`,
  `20261009-provisioner-power-configuration.md`,
  `20261009-libvirt-virtual-machine-enrollment.md`,
  `20261013-maas-http-proxy.md`,
  `20261013-uefi-boot-iso.md`,
  `20261013-deploy-dhcp-dns.md`,
  `20261019-retry-failed-deployment.md`, and
  `20261019-uefi-layout-deploy-guard.md`.
- Workflow and installation reliability:
  `20261016-cancel-deploy-status.md`,
  `20261016-swallowctl-ensure.md`,
  `20261019-lost-parked-workflow.md`, and
  `20261019-cancel-rerun-convergence.md`.
- Monitoring, GPU platform management, and documentation:
  `20261012-monitoring-integration.md`,
  `20261020-amd-gpu-operator.md`, and
  `20261021-documentation-gaps.md`.

## Consolidated Background

Swallow evolved from basic inventory and deployment controls into an operator
console that owns durable intent across Servers, OS provisioning, Platforms,
Managed Software, Workflows, and monitoring integrations. The manuscripts
capture a concentrated period of field-driven refinement:

- Server inventory needed denser, more truthful lifecycle presentation and
  contextual actions without conflating provider and Swallow state.
- Provisioning needed guided enrollment, Boot Media, UEFI-safe Boot ISOs,
  provider-owned power and network settings, and reliable retry/recovery.
- Durable Workflows needed deterministic cancel, retry, rerun, lease renewal,
  and projection repair after worker or Temporal failure.
- The Dashboard needed contextual deployment flows, resource-oriented catalogs,
  shareable URL state, responsive parity, and disposable visual review.
- Monitoring needed bounded Prometheus-backed charts without turning Swallow
  into a TSDB or Grafana replacement.
- GPU support needed accurate inventory classification and an operational AMD
  GPU Operator lifecycle for Swallow-deployed Kubernetes Platforms.
- Public and developer documentation needed to catch up with the resulting
  operator surface and accepted decisions.

The newest manuscripts supersede earlier assumptions where they conflict. In
particular, DHCP is a first-class deployment mode; Site automation cannot
override the Deployment Key; UEFI Boot ISOs use embedded GRUB rather than
chaining MAAS GRUB from iPXE; AMD GPU Operator node selection is NFD-driven
rather than operator-selected; and cancel/rerun behavior converges external
state instead of merely rewriting projections.

## Confirmed Decisions

- A Server remains a reconciliation projection of provider inventory.
  Enrollment never creates a Swallow Server record directly.
- The normal Site operating model has one enabled provisioner Integration.
  The Dashboard treats missing, ambiguous, disabled, or source-mismatched
  provisioners as configuration errors, without adding a new persistence
  constraint.
- Provider-owned facts remain provider-owned:
  Power Configuration, Provisioner HTTP Proxy, and Subnet DNS are read live and
  written through. Swallow stores intent or projections only where the accepted
  domain model requires it.
- OS deployment has two independent axes: Swallow's durable deployment result
  and the provider-owned provisioning lifecycle. Canceling a Workflow must not
  fabricate provider state.
- Explicit retry of a failed OS deployment first observes current state. For
  `failed`, `allocated`, `broken`, or `rescue`, it recovers to Ready and
  redeploys the frozen intent; activity retry does not perform destructive
  recovery.
- Cancel preserves completed deployment results. An in-flight deployment is
  aborted and returned toward Ready on a best-effort basis. Rerun re-executes
  convergent ensure Tasks against current external state.
- Release and Recover may supersede blocking Workflows through
  `cancelActiveWorkflows`; the backend accepts the new Operation and waits for
  leases instead of keeping a Dashboard dialog open.
- Parked Workflows use long-lived leases with infrequent renewal. If their
  Temporal execution disappears, Cancel finalizes Mongo state and releases
  leases rather than permanently returning conflict.
- Boot ISO is a Swallow-built, per-provisioner artifact. BIOS uses iPXE; UEFI
  uses GRUB embedded in the ISO, performs DHCP itself, and loads MAAS's per-MAC
  GRUB configuration.
- Disk deployment is refused when a UEFI Server's observed storage layout needs
  a new inspection. RAM deployment remains allowed.
- Deployment networking has three modes: `automatic`, `dhcp`, and `static`.
  Subnet DNS applies to later Automatic and Static deployments; DHCP gets DNS
  from the network DHCP server.
- GPU inventory classifies workload accelerators as `compute` and console/BMC
  graphics as `display`. Only compute devices contribute capacity and
  monitoring applicability.
- AMD GPU Operator is an optional Kubernetes Platform feature, configurable
  during and after deployment. It uses the image's driver, installs no KMM,
  and lets NFD discover every supported physical or SR-IOV GPU node. Operators
  do not select nodes individually.
- Monitoring integrates a Site's Prometheus-compatible backend through fixed,
  bounded named queries. Swallow does not own a TSDB, arbitrary PromQL, or
  Grafana dashboards.
- `swallowctl doctor` observes the installation health contract;
  `swallowctl ensure` idempotently converges it and then runs doctor.
- Visual verification uses disposable screenshots that an agent opens and
  evaluates. Pixel baselines are not committed regression tests.
- Public documentation is mirrored in English and Traditional Chinese.
  Provider-owned contracts, glossary terms, ADRs, and developer documents
  remain the implementation source of truth.

## Architecture and Design Principles

- Preserve component boundaries. `api-server` owns the HTTP contract;
  Dashboard and CLI are conformist consumers and do not infer wire behavior
  from backend internals.
- Follow Clean Architecture dependency direction inside every component.
  Provider SDK types, React, Fiber, Mongo, Temporal, and shell details remain
  outside domain and application boundaries.
- Use ubiquitous language from the root glossary. Record cross-component or
  non-obvious choices in ADRs, and update context maps when integration
  ownership or translation changes.
- Keep provider vocabulary behind anticorruption adapters. MAAS values such as
  power drivers, `AUTO`, `dns_servers`, and boot methods are translated into
  Swallow's published language.
- Model long-running work as durable Workflow/Job/Task execution. Activities
  own I/O and external effects; Workflow code remains deterministic and uses
  Temporal version gates for replay-sensitive changes.
- Prefer idempotent ensure behavior over imperative repair sequences. Observe
  external state before acting, preserve completed effects, and never guess an
  outcome that cannot be proven.
- Reuse shared presentation and application boundaries. Server desktop/mobile
  views, deployment configuration, status rendering, search input, monitoring
  charts, and software installation flows must not fork equivalent behavior.
- Keep URL state for shareable filters and navigation. Keep secrets and
  cloud-init out of URLs, router state, browser persistence, logs, events, and
  artifacts.
- Treat accessibility, responsive parity, meaningful status text, and direct
  screenshot inspection as completion gates for user-visible Dashboard work.

## Functional Scope

The consolidated product baseline includes:

- Dense Server inventory with provider-neutral Deployment presentation,
  contextual next-step icons, labelled action menus, integrated power control,
  URL-owned filtering/grouping/sorting, bulk actions, and explicit compute vs
  display GPU capacity.
- Contextual OS deployment from Servers and OS Images, with the shared
  configuration flow embedded in Platform deployment. The obsolete global
  Deploy OS workspace is retired.
- OS Image, Boot ISO, and Managed Software resource-oriented catalogs with
  responsive desktop/mobile information architecture.
- Guided Server enrollment for network boot, existing OS, and libvirt virtual
  machines; automatic bounded hardware inspection; provider-owned Power
  Configuration; Redfish and libvirt Boot Media.
- UEFI-correct Boot ISO build/rebuild, DHCP and Static/Automatic networking,
  Subnet DNS, MAAS HTTP proxy, deployment failure diagnostics, inspection
  guards, and retry/recovery convergence.
- Workflow projection repair, parked-workflow lease management, cancel
  compensation, rerun convergence, release/recover supersession, and truthful
  activity/timeline presentation.
- Basic Prometheus-backed usage charts, monitoring scopes, optional Grafana and
  Platform Monitoring Links, and correct distinction between absent and failed
  Alertmanager configuration.
- Kubernetes Platform GPU Operator install/configure/uninstall through API,
  Dashboard, CLI, Workflow, and versioned Ansible content.
- Installation health diagnosis and convergence for production and development
  Compose profiles.
- Bilingual public guides for the active operator surface, including API Keys,
  CLI command discovery, recovery behavior, Integration settings, GPU
  inventory, Platform settings, and Kubernetes exploration.

## Constraints and Rules

- Never create manual Server records or treat enrollment as Server creation.
- Do not store provider-owned Power Configuration, HTTP proxy, Subnet DNS, or
  live Kubernetes/Slurm resources as competing Swallow truth.
- Passwords, tokens, private keys, become passwords, registry credentials, and
  provider credentials are write-only and must not enter logs or artifacts.
- The Deployment Key is installation-wide and is the only automation SSH
  credential. A Site cannot override it.
- Boot Media and UEFI changes must preserve Secure Boot limitations and must
  not rewrite provider storage layouts as a repair shortcut.
- A first deployment attempt and infrastructure-level activity retries do not
  silently perform destructive provider recovery.
- Completed Task and Server outcomes are monotonic. Older attempts, cancel
  snapshots, and stale asynchronous reads cannot overwrite newer terminal
  results.
- Dashboard experimental gates affect presentation only. They do not weaken an
  Active API or CLI contract.
- Managed Software and Platform deployment remain separate bounded concepts:
  one installs a single software target, the other composes a runtime.
- Monitoring queries remain allowlisted and bounded by time range, series
  count, and scope. Metrics are never persisted by Swallow.
- `swallowctl ensure` does not apply image/config upgrades and does not
  cancel/retry business Workflows.
- UI-only refactors do not create API fields or domain state. Preserve real
  data behavior, keyboard use, accessible names, and explicit empty/error
  states.
- Every public documentation change is mirrored under `docs/en/` and
  `docs/zh-TW/`. Developer documents must not depend on public guides.

## Data Model and Format Notes

- `ServerGPU.kind` is `compute` or `display`. Legacy documents derive the kind
  on read until refreshed inventory persists it.
- Server provisioning exposes `inspectionRequired` and
  `inspectionRequiredReason` as provider-derived observations used by disk
  deployment preflight.
- Deployment network intent is `automatic | dhcp | static`; provider link
  modes and observations remain separate.
- Provisioner subnet projection includes `provisionerDhcp`, `dnsServers`, and
  `attached`. `attached` excludes host-internal bridge/veth subnets while
  retaining real controller, boot, DHCP, and hardware-backed networks.
- Power Configuration uses adapter families (`bmc`, `virsh`) and write-only
  password semantics. Redfish and libvirt are optional Boot Media extensions,
  not competing power ownership.
- Boot ISO records retain stable identity and URL across rebuild and include
  generated iPXE/GRUB content, hashes, and rebuild attribution.
- Workflow timeline status events preserve error code, message, stage,
  retryability, Task attempt, and deterministic identity.
- Release and Recover requests may carry `cancelActiveWorkflows`.
- Monitoring range data supports one explicit scope, bounded windows, and
  aggregate or per-Server series. Platform `monitoringLinks` are bounded,
  validated external URLs.
- AMD GPU Operator Platform state stores desired settings, lifecycle state, and
  last Workflow identity. The final model has no operator-provided
  `serverIds`; NFD determines managed nodes.
- Cloud-init remains an encrypted or opaque execution input and is kept only in
  in-memory Dashboard draft state before submission.

## CLI / API / Config Notes

- Active enrollment surfaces include the enrollment bundle, host-side
  `swallow servers enroll`, libvirt VM listing/enrollment, Power Configuration,
  and Boot Media commands.
- Provisioning exposes durable deploy/release/recover/verify operations,
  network inspection, Boot ISO build/read/delete/rebuild through API, and
  Task/Workflow control. CLI intentionally has no Boot ISO rebuild verb yet.
- Integration live settings use dedicated HTTP proxy and Subnet DNS endpoints;
  the CLI is a declared consumer but currently has no commands for them.
- Platform CLI covers deployment, lifecycle, Slurm requirements, live runtime
  views, Kubernetes exploration, and `platforms gpu-operator`.
- Monitoring CLI includes current values, metric names, bounded range queries,
  and alert operations.
- Managed Software remains a Dashboard/HTTP API surface; no software/docker CLI
  group is committed by these plans.
- Production and development installation profiles support
  `swallowctl doctor|ensure`. Worker startup retries Temporal with bounded
  exponential backoff.
- Integration settings are split into General, Connection, Credential, HTTP
  proxy, and Subnet DNS routes. The last two are live MAAS settings and are not
  part of the stored Integration settings map.
- The provisioner Integration may carry the MAAS rack's public
  `virshSshPublicKey`; Swallow authorizes it on managed hypervisors without
  storing another private key.

## Implementation Plan

1. Treat all manuscripts marked Implemented or Done, plus later manuscripts
   that depend on them, as the established baseline. Do not reimplement them
   from this consolidation.
2. Maintain contract/glossary/ADR/public-document synchronization whenever the
   baseline behavior changes. Keep bilingual public paths and semantics
   mirrored.
3. Close remaining environment acceptance items:
   - verify `swallowctl ensure` on a production installation;
   - verify the stuck deployment projection repair after deployment;
   - verify explicit failed-deployment retry and the UEFI inspection guard on
     the affected lab Server;
   - verify cancel of the lost parked release Workflow and subsequent Recover;
   - exercise real lab Release/Recover supersession and cancel/rerun
     convergence where state-changing approval is available.
4. Record each acceptance result in the durable development source of truth or
   a new narrowly scoped plan; do not restore deleted manuscripts.
5. Address unresolved questions only through a new contract/ADR-backed plan,
   with tests and field acceptance proportional to external side effects.
6. Continue enforcing component test gates, deterministic Temporal replay,
   Dashboard functional E2E, and direct visual inspection for affected routes.

## Non-goals

- Manual Server creation or foreign Platform registration.
- A Swallow-owned TSDB, arbitrary PromQL proxy, embedded Grafana, or Omnistat
  ingestion into Site monitoring.
- Replacing MAAS power control with direct Swallow power drivers.
- Automatic storage-layout mutation or automatic re-inspection during deploy
  retry.
- Site-specific automation private keys.
- Node-by-node positive selection for AMD GPU Operator.
- Managed exporters as Managed Software.
- Software/docker CLI implementation.
- Terraform/Pulumi-style installation state or Native preview `ensure`.
- Restoring committed screenshot baselines or introducing pixel-diff CI.
- Large new OS provisioning, enrollment, or monitoring architecture chapters
  solely to duplicate contracts and ADRs.

## Open Questions

- Should Provisioner HTTP Proxy support authenticated proxy URLs? This requires
  a new write-only secret model; the current contract rejects credentials.
- Should the 25-minute provider-stage stall threshold change for known slow
  proxies or firmware downloads?
- Does external DHCP consistently publish the post-install address and DNS to
  the provisioner across production networks, not only the validated lab path?
- Should the Monitoring alerts endpoint distinguish "no Alertmanager
  configured" from "configured but unavailable", as Overview already does?
- Which remaining environment-only acceptance checks above are still relevant
  after the next deployed release, and who authorizes state-changing lab tests?
- Does AMD GPU Operator eventually need per-Server exclusions? The accepted
  first version deliberately has no node picker.

## Future Work

- Add per-Server AMD GPU Operator exclusions only if field operation proves
  NFD-driven all-node management insufficient.
- Consider installation support beyond Compose only after packaging and health
  contracts are defined for that runtime.
- Improve handling of external-DHCP address changes if field evidence shows
  post-install SSH readiness can use stale observed leases.
- Add CLI coverage for live provisioner settings or Managed Software only when
  there is a concrete operator workflow that justifies the surface.
- Revisit monitoring alert-source error semantics and optional installation of
  Prometheus/Grafana as separate, contract-backed increments.
- Continue consolidating future manuscript batches so implemented history,
  remaining acceptance work, and open decisions do not diverge.
