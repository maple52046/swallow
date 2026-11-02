# 069. Ownership boundaries: swallow owns desired state; providers converge to it

- Status: Accepted
- Date: unrecorded (predates the ADR template)
- 現行規格：[desired-state-model.md](../development/desired-state-model.md)

## Context

swallow manages Servers and Platforms whose real state lives in external systems: a provisioner per
Site, host automation, a Workflow executor, a store, and monitoring. Every stored field needs one
answer to "which side is the truth"; without it two systems claim the same state, a change made in a
provider silently becomes the new value, and features build repair loops to keep copies consistent.
The first swallow modelled those facts before deciding who owned them (hostname and IP as unique
keys, an agent on every server, its own alert lifecycle), and a later rule made ownership depend on
whether a Site's provisioner was capable. The answer must also survive replacing an external system,
since each is a provider adapter ([decision 070](070-core-providers-and-extensions.md) (core,
provider adapters, and extension modules)).

## Decision

### Who owns what

1. **swallow's store is the single source of truth for desired state and execution state.** The
   desired state (spec) of every managed resource and the state of every Workflow belong to swallow,
   as does what no external system can hold: Sites and their Integrations, policy, allocation, and
   identity mapping. Providers realize desired state and never write it.
2. **Integrate, do not rebuild.** Where a mature system already does a job well (OS provisioning, host
   automation, durable execution, metrics, alerting, workload scheduling), swallow drives it through a
   provider adapter and reads its facts back as observed state.
3. **Every stored field is desired, observed, or derived, and has one writer.** A new feature
   classifies each field before it is built.
   - *Desired*: written by the API or by the parent resource that owns it; authoritative, never stale.
   - *Observed*: written only by a provider adapter's observation path, with its source Integration
     and observation time; staleness is part of the API. It is never the basis of a write to an
     external system and becomes desired only when a user adopts it.
   - *Derived*: computed from desired and observed values; cacheable but always recomputable.
4. **Store what the fleet is queried by; read the rest live.** Observed facts the fleet is listed,
   filtered, sorted, or aggregated by (current hardware and GPU inventory, provisioning state, observed
   tags) are stored, so a listing never fans out to every Site. Detail read one object at a time (NIC,
   link, and subnet detail; the provider's image catalog) is a live read from its owner.
5. **Identity is swallow's.** A Server is identified by a swallow-issued opaque id; hostname, IP
   address, and provider ids are observed attributes or opaque references to the provider's object
   ([decision 002](002-server-identity.md) (server identity)). Joining a provider's machine, its metric
   labels, and a Platform's node name to that id is the identity mapping only swallow can hold.

### What swallow does not store or rebuild

These belong to the systems built for them; swallow reads them live or links to them, keeps no copy,
builds no replacement, and reports no drift on them:

- Time-series metrics (labels per [decision 003](003-metrics-label-contract.md) (metrics label
  contract)), alert rule evaluation and firing state, and dashboards. Acknowledging an alert creates
  an Alertmanager silence; swallow keeps no alert lifecycle of its own.
- Workload scheduling, and resources inside a managed Platform: Kubernetes objects and Slurm
  partitions, node states, and jobs ([decision 032](032-self-deployed-platform-management.md)
  (self-deployed Platform management)).
- Hardware detail and liveness: the provisioner's inspection and `node_exporter` provide them, so
  managed servers run no swallow agent, and swallow keeps no hardware history.
- Network services: DHCP, address management, subnets, and VLANs stay with the provisioner. swallow
  owns only deployment network intent, plus a subnet's DNS servers and the provisioner's HTTP Proxy as
  desired fields the provisioner realizes ([decision 071](071-provisioner-realized-desired-state.md)
  (provisioner-realized desired state)).
- Automation content: playbooks and roles ship with the release, not as database records, and only
  those in its allowlist (`api-server/modules/manifest.json`) run; raw run output is a retained
  artifact, not a store record.

### Providers converge to swallow

6. **Ownership never depends on the provider.** An adapter declares its capabilities. When it cannot
   realize a desired field, the value stays in the spec and the resource reports `realized=false` with
   reason `capability_missing`; swallow builds no same-shaped fallback.
7. **Convergence is hybrid.** Controllers converge non-destructive differences continuously. A
   destructive change (reinstalling an OS, releasing a Server, uninstalling a Platform, changing a Node
   Role) first produces a Change Plan that a user approves, is recomputed against current state, and
   runs as a Workflow ([decision 076](076-change-plans.md) (Change Plans)). Drift is reported, not
   corrected, unless the field's feature specification declares it auto-corrected (decision 071 does
   so for non-destructive provisioner-realized fields); a user adopts the observed value or reapplies
   the spec. Desired state is written only through the API; GitOps may later be an import and export
   channel, not a second write path.
8. **Workflow semantics belong to swallow; the executor only executes.** swallow defines the Workflow
   model (Task DAG, attempts, retry, cancellation, waiting for a user) and stores its state; Temporal
   executes it, and a lost execution is re-dispatched from the store
   ([decision 072](072-store-driven-workflow-execution.md) (store-driven Workflow execution)).
9. **Transactions and change notification are platform requirements.** A cross-document invariant is
   kept in one transaction, never by compensation or a repair sweep. Store rules are ports (a
   transaction port and a change feed port); database-specific types stay in the store adapter.
   MongoDB runs as a single-node replica set; no process starts without transactions and a change feed.

## Alternatives considered

- **Integrate and mirror, keeping no intent where a provider can act** (the original rule, later with
  capability-first ownership and a swallow-owned fallback for providers that cannot). Rejected: who
  owns a fact varied with the provider, every such feature carried two paths (one inert while MAAS is
  the only provisioner), and a change made in the provider silently became the new value.
- **A read-only pane of glass, with every change made in the underlying tools.** Rejected: cross-system
  work (reimaging a rack and returning it to service, moving Servers between Platforms) spans
  provisioner, automation, and Platform; without it swallow is a dashboard.
- **Rebuild what a mature system owns**: an agent on every server (even for liveness only), a metric
  store or alert lifecycle of swallow's own, or swallow's own durable execution. Rejected: each repeats
  a solved problem worse (inspection and `up{}` from a scrape; a TSDB's retention, cardinality
  handling, and query language; Alertmanager silences; Temporal's durable timers, replay, and fencing),
  and the copy is bound to disagree with the original.
- **Pure continuous reconciliation, or pure plan and apply.** Rejected: correcting all drift
  automatically can reinstall a machine or power off a node a user changed on purpose; requiring a
  plan for every change means nothing heals itself and even a tag change needs an approval.
- **GitOps as the write path now.** Rejected for now: two write sources, secrets in a repo, offline sites.
- **Temporal as the single source of execution state.** Rejected: what a user sees would depend on
  Temporal's history retention and on restoring it with swallow's store, and the Workflow semantics
  would be tied to one executor.
- **Move the store to PostgreSQL now.** Deferred, not rejected: one database engine with Temporal and
  MAAS, but a rewrite of the store layer for no change in behavior. Store rules as ports keep the move
  open; it needs its own ADR.

## Consequences

- Every managed resource separates spec from status and reports conditions; every field states its
  class and its writer.
- A missing provider capability is a condition, not a second code path; read-time overlays and
  same-shaped fallbacks retire as their facts become desired fields (decision 071).
- A change made directly in a provider or a Platform is drift; an auto-corrected field is restored at
  the next resync, so users change those fields in swallow.
- Idempotent ensure Tasks remain; Change Plans add an approval step for destructive changes only.
- One view can show a Server across its Site, provisioner, Platform, metrics, and Workflows without
  fanning out to every Site, at the cost of observed data that can be stale and says how stale it is.
- Cost: a controller framework, a Change Plan model, and a staged migration of existing features.

## Revision history

- unrecorded: decision 069 decided which system owns which facts: swallow owns intent, policy, and identity mapping, integrates a mature system for everything else, and keeps external facts only as mirrored facts with their source and observation time.
- 2026-09-15: decision 069 made a fact a provisioner may own capability-first: owned by a capable provisioner, and by a same-shaped swallow-owned fallback otherwise.
- 2026-10-30: decision 069 made swallow's store the single source of truth for desired and execution state, with providers converging to it, hybrid convergence, Temporal only executing, and a transactional store, and retired the capability-first fallback.
- 2026-11-02: merged decision 069 into this ADR; the body states the current decision.
