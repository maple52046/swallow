# Monitoring and Lab Verification

## 1. Purpose

Preserve the implemented Monitoring v2 architecture, its lab evidence, operational constraints, and the
remaining monitoring investigations. This plan is the long-term reference for Site Prometheus, Platform
Grafana, exporter ownership, health classification, and GPU monitoring validation.

## 2. Source Scope

Consolidated from:

- `docs/plans/manuscripts/20261103-monitoring-v2.md`
- `docs/plans/manuscripts/20261104-ram-platform-acceptance.md`
- `docs/plans/manuscripts/20261104-dashboard-health-and-review-fixes.md`
- `docs/plans/manuscripts/20261107-rocprof-omnistat-slurm-impact.md`
- Relevant monitoring and lab findings in `20261102-architecture-review-v2.md`

The architecture-review v3 manuscript was excluded and left untouched.

## 3. Consolidated Background

Monitoring v2 replaced product-specific and ownership-inferred monitoring with a Site-owned foundation.
Every Site has a metrics Integration pointing to its Prometheus; exporters are declared by Managed
Software or Platform/Accelerator contributions; `platform_id` tenancy comes from Platform Claim; and
Platform Grafana reads only its Platform's series through an authenticated gateway and label proxy.

The implementation was validated through repeated RAM deployments of Slurm and Kubernetes on the lab's
MI308X virtual functions. Both Platform scenarios, Node Exporter ensure behavior, per-Platform Grafana,
Omnistat, Device Metrics Exporter, tenancy isolation, and the revised health model were exercised.

## 4. Confirmed Decisions

- Each Site has its own Prometheus, installed and managed with the Site. A Site without a metrics
  Integration reports monitoring as unavailable rather than inventing data.
- Infrastructure exporters are collected once by Site Prometheus.
- Platform Grafana reads Site Prometheus through the api-server gateway and prom-label-proxy, constrained
  by `platform_id`.
- Slurm Grafana is Managed Software on the primary controller; Kubernetes Grafana is the
  grafana-community Helm chart inside the cluster.
- Platform Grafana is available for CPU-only and GPU Platforms and contains Node Exporter dashboards plus
  monitoring-scheme-specific dashboards.
- GPU monitoring depends on context: standalone DME, GPU Operator DME for Kubernetes, and Omnistat or DME
  for Slurm. RDC remains retired until reintroduced as an experimental feature.
- Exporter Ownership and Kubernetes exporter DaemonSet Workflow kinds are retired.
- Basic metrics API and dashboard monitoring are generally available; alerts remain the experimental
  feature.
- Server health is `up`, `down`, `unknown`, or `not_applicable`, based on Node Exporter policy and
  observations rather than a nullable default.
- Platform uninstall never removes Node Exporter from retained hosts.

## 5. Architecture and Design Principles

- Core does not know PromQL, exporter product names, or GPU-vendor series names.
- Metrics evaluation remains in Prometheus and alert evaluation in Alertmanager; Swallow stores neither.
- Discovery exposes installed exporters with `server_id`, `site`, `platform_id`, and `exporter`.
- `platform_id` comes from Swallow-owned Platform Claim, not runtime membership.
- The gateway validates the Platform datasource credential, then supplies the tenancy label to the
  label proxy. Invalid, old, or foreign tokens all return the same unauthorized response.
- Health distinguishes policy coverage from observation: no target on an uncovered Server is
  `not_applicable`; no target or no sample on a covered Server is `unknown`.
- Dashboard and API consumers display canonical health state; they do not rederive it.

## 6. Functional Scope

Implemented:

- Site Prometheus, prom-label-proxy, exporters discovery, Site-scoped queries, and one metrics Integration
  per Site.
- Platform monitoring token issuance, sealed storage, gateway routes, and tenant isolation.
- Slurm and Kubernetes Platform Grafana delivery using one shared datasource/dashboard definition.
- Node Exporter, DME, and Omnistat discovery and collection.
- Node Exporter desired policy and ensure controller with retry backoff.
- Removal of legacy GPU queries from the basic metrics API and dashboard.
- Four-state Server health across API, overview, dashboard, filtering, and stream refresh.
- Direct Platform Grafana links to stable dashboard UIDs and slugs.
- Lab acceptance for RAM Slurm and Kubernetes, GPU scheduling, exporter collection, and Grafana access.

Remaining:

- Alerts split and merged across Sites instead of reading an arbitrary metrics Integration.
- Generic dashboard GPU charts derived from exporter declarations.
- Firewalld rules for Site Prometheus access to exporter ports.
- Standalone DME conflict detection before Kubernetes claims a Server.
- Node Exporter controller opt-out, failed-target semantics, login-failure conditions, and immediate wakeup
  on Workflow changes.
- Platform Grafana disable automation and small token/data/idempotency fixes.
- k0s HA, standalone DME, Alertmanager, and physical-GPU metric validation.
- The Omnistat ROCProfiler impact experiment.

## 7. Constraints and Rules

- GPU is optional: CPU-only Servers and Platforms receive complete base monitoring.
- No monitoring state may be inferred from a missing sample alone.
- A Platform datasource token is stored only as a hash or sealed secret and is never placed in dashboard
  JSON.
- Prometheus configuration uses one exporters discovery job; new exporters must not require central
  scrape-configuration edits.
- Existing Platform series retain their original Platform label after membership changes.
- Lab actions use only explicitly approved `lab-*` machines, never `tainan-*`; credentials remain outside
  the repository and are removed afterward.
- Do not change api-server Go code while Ansible Tasks are executing in the development stack.
- Visual review uses synthetic fixtures and includes desktop/mobile and required themes.
- SR-IOV VF limitations are not automatically classified as product defects; compare with physical or PF
  hardware first.

## 8. Data Model and Format Notes

- Exporter declarations identify exporter name and port in Software catalog entries or module
  contributions.
- Health state always exists. `observedAt` is meaningful for `up` and `down`; `unknown` and
  `not_applicable` have no observation timestamp.
- Monitoring links are named links and may point directly to stable Grafana dashboard paths.
- Platform metrics gateway credentials are distinct from admin sessions and Platform runtime credentials.
- Future generic GPU mapping belongs in exporter declarations as common metric name to series and
  conversion rules.
- The pending `Alert` glossary term and legacy `platform` label fallback remain to be resolved with alerts.

## 9. CLI / API / Config Notes

- Exporter discovery is available from the api-server and is the Prometheus HTTP service-discovery source.
- Basic metrics expose five host metrics; removed legacy GPU names return `validation_error`.
- Platform metrics gateway paths expose only query/read operations and `/federate`.
- Installation config includes Prometheus, label proxy, retention, public API URL, and monitoring scrape
  addresses.
- `SWALLOW_WORKER_NODE_EXPORTER_ENSURE` controls the ensure controller; the production default is enabled.
- Alert list requests without a Site still select an arbitrary metrics Integration and require redesign.
- Platform Grafana links should use `/d/<uid>/<slug>`, not the Grafana root.

## 10. Implementation Plan

1. Design Site-aware alert listing and acknowledgment, including cross-Site aggregation, Platform
   filtering semantics, the `Alert` glossary term, and removal or explicit retention of the legacy
   `platform` label fallback.
2. Decide whether generic GPU charts precede validation work. If selected, add exporter-declared metric
   mappings and keep product series names in adapters/modules.
3. Add firewalld rules scoped to Site Prometheus scrape addresses for Node Exporter, DME, and Omnistat,
   or reject unsupported OS combinations through the support matrix.
4. Reject or plan removal of standalone DME before Kubernetes deployment claims a Server.
5. Complete Node Exporter controller behavior: explicit uninstall/opt-out policy, preserve failed installed
   targets for health, represent automation-login failures as conditions, and react to Workflow changes.
6. Automate Platform Grafana disable through Platform Change Plans and Workflow uninstall.
7. Run remaining lab coverage: k0s HA, standalone DME, Alertmanager, and physical GPU counters.
8. Execute the controlled Omnistat experiment: compare stopped, deployed with ROCProfiler disabled, and
   isolated ROCProfiler `VALUBusy` collection using 12 balanced trials per state.

## 11. Non-goals

- Platform-owned Prometheus in the current monitoring generation.
- Multi-Site installation topology beyond the metrics Integration abstraction.
- Swallow-owned Grafana.
- AI NIC monitoring before inventory and metrics exist.
- Automatic removal of pre-existing Platform Prometheus instances.
- Treating VM/VF absence of temperature, power, or utilization as proof that exporters are defective.

## 12. Open Questions

- Should generic GPU charts be delivered before or after validation?
- Should alert Platform filtering use the alert's `platform_id` snapshot or current Platform membership?
- Should a Platform optionally run its own workload Prometheus and federate infrastructure series?
- How should Platform Grafana token rotation avoid unnecessary Pod/service restarts?
- Is incomplete GPU telemetry on MI308X SR-IOV VF a hardware exposure limit or exporter configuration
  issue?
- Which Server policy should let users intentionally suppress Node Exporter ensure behavior?
- Should Platform Grafana disable be considered destructive or non-destructive when it removes only the
  visualization service?

## 13. Future Work

- Platform-owned workload Prometheus with Site infrastructure federation.
- Built-in Alertmanager and richer alert workflows.
- Ingress or LoadBalancer access for Kubernetes Grafana.
- Experimental RDC support.
- AI NIC exporter discovery and dashboards.
- Support-matrix-driven acceptance of OS, deployment target, Platform, Accelerator, Managed Software, and
  monitoring combinations.
- Physical-GPU performance and telemetry baselines, including the ROCProfiler impact threshold: material
  regression requires median throughput loss above 1% and a bootstrap 95% confidence lower bound above
  1%.
