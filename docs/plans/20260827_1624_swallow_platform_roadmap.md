# Swallow Platform Roadmap

## Purpose

This plan consolidates the long-term delivery direction for the Swallow control
plane: environment installation and release management, the PatternFly operator
console, k0s Kubernetes cluster deployment, and Prometheus-based machine
monitoring. It preserves component boundaries, operational safety rules,
confirmed user experience decisions, and acceptance criteria.

## Source Scope

This consolidation covers four manuscripts:

- 20260825-deployment-environments.md: implemented installation baseline for
  development, testing, production, release, backup, and offline operation.
- 20260826-web-ui-kubernetes-deployment.md: in-progress k0s HA deployment,
  progress, retry, credential capture, and membership design.
- 20260827-dashboard-operator-console.md: completed PatternFly 6 console and
  interaction-test baseline.
- 20260827-prometheus-monitoring-integration.md: in-progress exporter
  ownership, per-site Prometheus, named metrics, and Kubernetes exporter path.

Newer, explicit decisions take precedence. The completed PatternFly console
replaces earlier statements that cluster, operation, or monitoring screens do
not exist. Visual-regression tests were explicitly removed; the maintained
browser suite is interaction-only.

## Consolidated Background

Swallow is a monorepo with provider-owned API contracts under docs/, a Go
control-plane API under api-server/, a React operator console under dashboard/,
and installation assets under deploy/. MAAS owns machine and operating-system
provisioning. Swallow owns durable automation execution, cluster intent and
observation, monitoring integration, and operator-facing coordination.

The installation baseline uses one control-plane node on Ubuntu 24.04 amd64.
Development runs source bind mounts with Go and Vite hot reload. Testing uses
immutable production images and isolated data. Production promotes the tested
OCI digest into either the Compose or native systemd installation path.

Automation is embedded rather than delegated to AWX. Operations are persisted
before dispatch, leased per site through MongoDB, executed through a pinned
ansible-runner, and retain logs and artifacts. AWX remains an information
architecture reference for job views, not a runtime dependency.

The dashboard has been rebuilt on PatternFly 6. It provides the operator shell,
fleet and machine workflows, multi-cluster views, deployment wizard, stdout-first
operations, and current monitoring. The remaining roadmap connects those
surfaces fully to verified k0s deployment and exporter lifecycle behavior.

## Confirmed Decisions

- Use installation for installing, running, upgrading, restoring, or
  uninstalling the Swallow control plane. Reserve deployment for managed-machine
  and cluster actions.
- Keep deploy/dev as the only development golden path. Branch names never
  identify environments.
- Promote a candidate by OCI digest. A SemVer release references the already
  tested digest and does not rebuild it.
- Use Nginx as the only production public edge.
- Use PatternFly 6 as the only dashboard design system. External consoles inform
  information architecture but contribute no CSS, assets, components, or brand.
- Keep ?site=<id> as the sole dashboard site-scope source. Saved views never
  persist site scope.
- Treat missing monitoring data as No data, never zero. Provider failures
  degrade only their panels.
- Deploy k0s HA with exactly three dedicated control-plane servers plus one or
  more workers. Translate control-plane to k0s controller only at the automation
  boundary.
- Treat the cluster API as authoritative for membership. Merge Kubernetes Nodes
  for workers with k0s-ctrl-<hostname> Leases for dedicated controllers.
- Enable the k0s lease convention per integration. Lease-only controllers match
  servers by name.
- Use pod CIDR 10.244.0.0/16 and service CIDR 10.96.0.0/12 as verified defaults,
  and reject ranges that contain a target node address.
- Build role assignments server-side. Do not trust operator-supplied swallow_*
  extra variables or derive new-cluster roles from existing membership.
- Capture generated credentials through private result files and encrypted
  storage. Never place credentials, join tokens, VRRP passwords, or private keys
  in logs, artifacts, or persisted plain-text extra variables.
- A retry creates a new linked operation. Automatic retry remains prohibited.
- Use one effective exporter owner per server: locked means unmanaged;
  Kubernetes members follow cluster policy; other managed servers use Ansible.
- Use fixed host ports 9100 for node-exporter and 5000 for rdc-exporter.
  Kubernetes DaemonSets use hostNetwork.
- Identify AMD GPU servers through the MAAS amd-gpu tag.
- Use maple52046/rdc-exporter at rocm-7.2.4 and image
  ghcr.io/maple52046/rdc-exporter:v1-rocm7.2.4-20260610 for the confirmed RDC
  path.

## Architecture and Design Principles

- Respect bounded contexts and provider-owned contracts. Cross-component
  interaction goes through /api/v1 rather than internal model imports.
- Use provider-owned aggregate read models for operator summaries. The Overview
  endpoint aggregates behind narrow application read ports.
- Persist operation intent before execution. Site leases guarantee one active
  run per site while allowing cross-site concurrency.
- Fail safely. Interrupted leases become indeterminate and are never replayed
  automatically. Upgrade refuses active operations unless force is explicit.
- Keep credentials write-only and encrypted at rest. Materialize temporary job
  credentials under /run/swallow/jobs with restrictive permissions.
- Keep historical ownership explicit. Swallow queries named current metrics;
  Prometheus owns time series and Grafana owns historical exploration.
- Preserve independent machine state axes. Provisioning, membership, and health
  are observed by different owners and must not become one synthetic status.
- Build dense, task-oriented workflows: NetBox/MAAS for inventory,
  Cockpit/OpenBMC/Ironic for machine detail, Rancher/Harvester/Headlamp for
  clusters, AWX for operations, and Grafana for monitoring hierarchy.
- Keep accessibility and responsive behavior in shared shell primitives:
  system/light/dark appearance, icon-only collapsed navigation with tooltips,
  focus-managed mobile drawer, predictable states, and keyboard workflows.

## Functional Scope

### Installation and release

- Development, testing, and production environment contracts.
- Compose and native lifecycle commands for preflight, install, upgrade, backup,
  restore, doctor, uninstall, and explicit data purge.
- Signed OCI digests, SBOMs, native binary, dashboard dist, playbooks, offline
  Python wheelhouse, checksums, and independent offline media manifests.
- Daily MongoDB backups with seven daily and four weekly recovery points.
- Disconnected, restart, failure-injection, isolation, and restore acceptance.

### Operator console

- PatternFly masthead, docked navigation, mobile drawer, site scope, account
  controls, and persistent system/light/dark appearance.
- Fleet overview with health, attention, cluster issues, alerts, integrations,
  and recent operations.
- Configurable machine inventory with URL filters, local saved views, pagination,
  density, columns, and selection-driven lifecycle actions.
- Machine detail for summary, power/provisioning, hardware, provider data,
  monitoring, network, storage, PCI, and recent activity using available data.
- Multi-cluster list/detail with type-aware Kubernetes language, neutral Slurm
  terminology, readiness, freshness, roles, issues, and related operations.
- Four-step cluster deployment wizard: Basics, Networking, Roles, Review.
- Operations list and stdout-first detail with URL filters, events, search,
  match navigation, copy, download, and retry.
- Monitoring with current fleet health, alert filters, acknowledgement, bounded
  metric batches, refresh time, and provider-returned Grafana links.
- Interaction-only Playwright coverage; no screenshot baseline workflow.

### k0s cluster deployment

- Select deployed servers, assign trusted roles, validate topology/networking,
  create cluster intent, and start a deploy-kubernetes operation.
- Bootstrap the initial controller, join controllers one at a time, then workers.
- Report per-phase, per-task, and per-host progress from runner events.
- Capture a read credential, register the cluster integration, attach it, and
  synchronize membership after success.
- Support explicit retry and idempotent scale-out using the k0s systemd unit as
  the idempotency boundary.

### Prometheus monitoring

- Mirror tags, expose filtered discovery, and query vendor-neutral named CPU,
  memory, and GPU metrics.
- Install/uninstall containerized exporters through embedded Ansible.
- Auto-install host-owned exporters when a managed server reaches deployed;
  manual reinstallation is default after Kubernetes ownership is removed.
- Scrape per-site targets through HTTP service discovery with server_id, site,
  and cluster labels.
- Apply/remove Kubernetes exporter DaemonSets through an operation backed by a
  write-capable kubeconfig.
- Validate CPU lab VMs and the tagged tainan-ci.maas GPU server while leaving
  locked physical servers untouched.

## Constraints and Rules

- Initial supported host: Ubuntu 24.04 amd64; initial control-plane topology:
  one node.
- Production images are digest-pinned. Secrets use Compose secrets or root-only
  files, and sensitive settings support the *_FILE convention.
- Back up the credential encryption key with MongoDB and job artifacts.
- Uninstall preserves data unless --purge-data is explicit. Destructive
  downgrade is unsupported.
- External MAAS and monitoring failures do not make the API unready when
  MongoDB and schema checks succeed.
- Cluster deployment targets must already be provisioned as deployed.
- Locked servers are unmanaged and must never receive automation.
- Dedicated k0s controllers do not appear as Kubernetes Nodes.
- Remove a controller join-token file only after the node has joined.
- Use k0s kubectl wait for readiness, not fragile command-quoted JSONPath.
- Swallow may create its own Kubernetes credential and exporter resources, but
  is not a general-purpose Kubernetes workload manager.
- Monitoring requests use at most 200 server IDs per request and two concurrent
  dashboard calls.
- Open Grafana in a new tab using the API-returned URL; do not iframe or assume
  a dashboard UID.
- Dashboard preferences remain browser-local.

## Data Model and Format Notes

- Server.id is the only stable machine identifier. Hostnames and addresses are
  observed, mutable, and not globally unique.
- Server state remains three independent nullable axes: provisioning,
  membership, and health, each with an observation time.
- MAAS tag_names map into observed.tags; amd-gpu selects the RDC path.
- Cluster roles use control-plane and worker. Slurm retains neutral
  manager/compute terminology.
- Kubernetes membership combines Nodes and optional k0s controller Leases.
- Cluster represents observed/registered state; deployment intent accompanies
  cluster creation and produces a durable operation.
- Retry creates a new operation linked to the original with trusted targets and
  variables.
- Generated credentials and VRRP material use encrypted storage. Artifacts and
  stdout remain credential-free.
- Prometheus uses stable server_id, site, and cluster labels. RDC metrics are
  gpu_util, gpu_temp, power_usage, gpu_memory_usage, and gpu_memory_total,
  keyed by gpu_index.
- Missing samples remain absent and render as No data.

## CLI / API / Config Notes

- Start development from deploy/dev with docker compose up -d. Dashboard and API
  bind to 0.0.0.0 and Vite normally uses a same-origin API proxy.
- Keep separate /livez and /healthz or /readyz semantics. External integrations
  do not determine readiness.
- GET /api/v1/overview?siteId=<optional> is the provider-owned console aggregate
  and represents partial monitoring failure in its response.
- Existing server, site/integration, action, alert, metrics, operation, and
  cluster contracts remain /api/v1 compatible.
- POST /api/v1/clusters accepts optional deployment intent after topology, CIDR,
  role, address, and target-state validation.
- Operation APIs expose durable status/logs and normalized task/host events.
- Monitoring discovery supports tags and fixed ports. Dashboard loading chunks
  scoped IDs by 200 with no more than two concurrent calls.
- Production inputs include DNS, TLS CA, MAAS networking, target machines, and
  external integration endpoints.
- The release bundle permits only manifest-approved playbooks and verifies SSH
  host keys.

## Implementation Plan

### Completed foundations

1. Maintain the environment contract, Nginx edge, immutable release identity,
   lifecycle commands, backup rules, and offline bundle model.
2. Maintain the PatternFly console, provider-owned Overview slice, scoped
   navigation, domain workflows, partial states, and interaction E2E suite.
3. Keep active API contracts synchronized with provider behavior and record
   cross-component decisions through ADRs.

### Complete k0s deployment

1. Finalize active cluster/deployment contracts and distinguish registration
   state from deployment intent.
2. Release the manifest-approved HA playbook with trusted role groups and safe
   join-token handling.
3. Validate topology, target state, CIDR overlap, VIP, and server-side roles.
4. Add private result capture for the read credential and encrypted VRRP data.
5. Normalize runner events and retain explicit linked retry semantics.
6. Register the integration after success and merge Node/Lease membership.
7. Run seven-VM lab acceptance, including controller visibility, induced
   failure, retry, and scale-out.

### Complete monitoring integration

1. Finish metrics-label and exporter-ownership documentation.
2. Complete tag-aware discovery, inventory groups, named metrics, and contracts.
3. Deliver manifest-approved exporter install/uninstall playbooks and mappings.
4. Enforce owner policy, locked-server guards, and deployed-state auto-install.
5. Maintain per-site Prometheus config, dev Compose service, seed flow, and
   third-party guidance.
6. Complete machine metrics and exporter-owner states without fake values.
7. Validate CPU lab VMs and tainan-ci.maas RDC metrics end to end.
8. Add operation-backed Kubernetes DaemonSet apply/remove with a write-capable
   kubeconfig and kubernetes.core.

### Release acceptance

1. Verify fresh dev, digest parity, Compose/native restarts, and disconnected
   installation.
2. Exercise MAAS through automation, logs, Prometheus, and dashboard observation.
3. Inject provider, process, and interrupted-operation failures.
4. Verify isolation, backup/restore, credential recovery, and active-operation
   upgrade refusal.

## Non-goals

- Scheduling workloads, managing Pods/Deployments, draining nodes, or providing
  a general Kubernetes resource explorer.
- Harvester VM, volume, or migration lifecycle.
- OpenBMC firmware, KVM, sensor, or console controls without new contracts.
- k0s upgrades or post-bootstrap cluster-wide settings in current scope.
- OS provisioning; MAAS remains the owner.
- Automatic operation replay or retry.
- Embedded Grafana, arbitrary PromQL passthrough, fake history/sparklines, or a
  new Swallow time-series store.
- Server-side user preference persistence.
- Destructive production downgrade.

## Open Questions

- Which production installation path should become the primary recommendation
  after Compose and native systemd satisfy the same acceptance gates?
- When should managed Alertmanager and Grafana packaging move from external
  integration/later milestone into the supported third-party bundle?
- What rotation/revocation workflow should govern the read cluster credential
  and future write-capable exporter kubeconfig?
- What exact operator/API workflow should expose supported k0s scale-out while
  post-bootstrap cluster-wide setting changes stay out of scope?
- Which monitoring phases and lab cases remain incomplete should be confirmed
  against current implementation before scheduling release work.

## Future Work

- API high availability and MongoDB replica sets.
- Central TSDB or remote write after per-site 30-day Prometheus is proven.
- Managed Alertmanager and Grafana installation with explicit ownership.
- k0s version upgrades and controlled post-bootstrap reconfiguration.
- Richer hardware management after OpenBMC/Ironic contracts support real actions.
- Additional deployment types and monitoring providers while preserving
  manifest execution, stable server identity, and one exporter owner.
