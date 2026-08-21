# 001 — System Ownership Boundaries

## Decision

**gdcm owns intent, policy, and identity mapping. It owns no facts about the physical or
runtime world.**

Every fact about hardware, operating systems, execution, metrics, or cluster state is
owned by an external system. gdcm references those facts; it does not become their
second home. The only fact gdcm caches is external inventory, and only because fleet
scale forces it — that cache is always marked with its own staleness so that no reader
can mistake it for the truth.

What gdcm does own is the part no external system can: which sites and integrations
exist, what an operator wants to happen, what policy applies where, and how an
identifier in one system maps to an identifier in another.

## Context

The first iteration of gdcm modelled the world directly: a `Server` record with
hostname, IP, hardware inventory, an operational status, and a bespoke agent reporting
into it. Every one of those fields already had an owner elsewhere — MAAS knew the
hardware in far more detail, Prometheus knew whether the host was up, and nobody had
decided which one gdcm should believe.

That is the actual defect. It was not bad code; it was modelling before deciding
ownership. The symptoms all followed from it:

- `hostname` and `ip` were unique keys, which cannot hold in a multi-site fleet and
  breaks during a reinstall when a machine briefly has no address.
- The agent collected inventory once at startup and never again, because there was no
  answer to "who asks it to look again".
- `AgentStatusStale` was defined and never set, because nothing owned the passage of time.
- Alerts had an `acknowledge`/`resolve` lifecycle in gdcm while Alertmanager already had
  silences, so two systems claimed the same state.

This document exists so that the next feature starts from ownership instead of from a
data model.

## The Boundaries

### Facts gdcm does not own

| Fact | Owner | gdcm access | gdcm stores |
|------|-------|-------------|-------------|
| Which machines exist; CPU, RAM, GPU, disk, NIC detail | Provisioner (MAAS, one per site) | Reconciler poll | Cached projection, with staleness |
| OS deployment state and deployed OS | Provisioner | Reconciler poll | Cached projection |
| Power state | Provisioner (via BMC) | Reconciler poll | Cached projection, short freshness window |
| IP assignment, DHCP, DNS, subnets, VLANs | Provisioner | Read-only, as machine attributes | Nothing modelled |
| Long-running execution: state, logs, retries, concurrency | AWX | REST API | Job reference plus mirrored status |
| Playbooks, roles, automation content | Git, consumed by AWX | Not accessed | Nothing |
| Time-series metrics | Central TSDB | PromQL at query time | Nothing, ever |
| Alert rule evaluation and firing state | Prometheus and Alertmanager | Read on demand, silences out | Nothing |
| Dashboards | Grafana | Deep link | Nothing |
| Kubernetes node state, membership, workloads | Kubernetes API | Live read | Nothing |
| Slurm partitions, Slurm node states, jobs | Slurm | Live read | Nothing |
| Job scheduling and queueing | Slurm and Kubernetes | Live read | Nothing |

### Facts gdcm owns

| Fact | Why gdcm must own it |
|------|----------------------|
| Sites | No external system knows the set of sites; it is the frame everything else hangs off |
| Integrations: endpoints and credentials per site | This is gdcm's own configuration. Credentials may be delegated to a secret store, but the registry of what exists is gdcm's |
| Identity mapping | The join between a provisioner machine ID, a metrics label set, and a cluster's own node name exists nowhere else. This is gdcm's central value |
| Clusters as records: which cluster exists, at which site, with which policy | The cluster's own API knows its members but not its intended shape or its governing policy |
| Policy, e.g. `gpuStackOwner` | Pure intent. Two subsystems both want to install GPU drivers; only an operator decision resolves it |
| Operations: intent, target set, and the reference to the AWX job that executes it | AWX knows the job ran. Only gdcm knows it was "drain and reimage these 12 servers to move them from cluster A to B" |
| Tenancy: teams, users, server allocation | Allocation is a platform-level policy question, not a fact any provisioner or cluster holds |

### The one cache, and its rules

Provider inventory is cached because a fleet cannot be served by fanning out to every
site on every request: the slowest provisioner would set page latency, one unreachable
site would break the whole listing, and cross-site sorting and pagination cannot be
computed correctly by merging per-provider pages.

The cache is therefore permitted, under three rules that make it honest:

1. Every cached record carries the source it came from and when that source was last
   observed.
2. Freshness is part of the API, not an implementation detail. A client can always ask
   how stale a view is and must be able to tell "this site last synced 14 minutes ago"
   from "this site is up to date".
3. The cache is never authoritative for a write. An action always goes to the owning
   system, and the cache converges afterwards. gdcm never writes to the cache to reflect
   what it hopes happened.

## Consequences

### What this makes possible

A single view across sites that no individual tool can produce, because gdcm holds the
identity mapping: this server, in this site, provisioned by that MAAS, currently a worker
in that Kubernetes cluster, emitting these metrics, last touched by that AWX job.

That correlation is the product. Everything else is someone else's job.

### What this forbids

- **No custom agent on managed servers.** Its two jobs are already covered: hardware
  detail by provisioner commissioning, liveness and host metrics by `node_exporter`.
  A bespoke agent on every server in a fleet is a maintenance cost with no unique output.
- **No metric storage.** gdcm queries a TSDB. It does not keep a `GPUMetrics` table.
  Storing metrics is rebuilding a time-series database badly.
- **No log storage.** Operation logs live in AWX and are proxied on demand.
- **No alert rule evaluation.** Rules live with Prometheus. gdcm receives what fires.
- **No alert lifecycle of its own.** Acknowledging is creating an Alertmanager silence,
  not setting a field in Mongo.
- **No automation content.** gdcm does not store playbooks, scripts, or an equivalent of
  a provisioning "profile" containing packages and scripts. Those belong in git.
- **No network management.** No DHCP, DNS, subnet, or VLAN modelling.
- **No scheduler.** Deciding which workload runs where is Slurm's and Kubernetes' job.
- **No hardware history or CMDB.** The provisioner keeps commissioning history.

### What must be deleted or changed

- Retire the custom agent entirely: `internal/agent`, `internal/app/agent.go`,
  `cmd/swallow/agent.go`, the gRPC service and its proto, the shared server auth token,
  and `AgentInfo`/`AgentStatus` on the server record.
- Replace the `Server` record with a server projection: new identity, three status axes,
  nullable hostname and IP, and no uniqueness constraint on either.
- Turn the provisioning provider from a single configured endpoint into provider
  instances stored as data, one or more per site.
- Change provider reads from pass-through to a reconciled cache with visible staleness.
- Drop the frontend's `ProvisioningProfile` and `ProvisioningJob` concepts, which
  assumed gdcm owned both automation content and execution state.
- Drop the frontend's alert acknowledge and resolve mutations in favour of silences.

## Rejected Alternatives

**gdcm owns orchestration and drives servers itself.** This is the most capable design and
gives the most consistent experience, and it is what the original agent plus a bidirectional
gRPC stream was drifting towards. Rejected because it means owning idempotency, retry,
concurrency limits, rollback, and log durability — that is Ansible's and AWX's entire
problem domain, solved, and reimplementing it is the largest possible detour from what
gdcm is for. The gRPC control channel was structurally already in place, which made this
tempting and is worth recording as the closest call in this document.

**gdcm as a pure read-only pane of glass, with every change made in the underlying tools.**
Cheapest to build and impossible to get wrong. Rejected because the cross-cutting
workflows are the reason the platform exists: moving servers between clusters, or reimaging
a rack and putting it back into service, spans provisioner, automation, and cluster. If
every action has to be performed by hand in three tools, gdcm is a dashboard.

**Store metrics in gdcm for a unified API.** Superficially attractive: one API for
clients, no PromQL knowledge needed in the frontend, no dependency on a TSDB being
reachable. Rejected because it is a time-series database with worse retention,
cardinality handling, and query language than the one already deployed, and because the
copy is guaranteed to disagree with the original.

**A single Prometheus scraping the whole fleet directly.** Simplest topology, one place
to query. Rejected because it does not survive multiple sites: cross-site scraping
depends on flat network reachability that a fleet does not have, and a single scraper
becomes both a bottleneck and a single point of failure. See
[003](003-metrics-label-contract.md).

**Keep the agent for liveness only.** The heartbeat is genuinely useful and already
built, and a case can be made that liveness independent of the metrics pipeline is worth
having. Rejected because "one small component on every server" is never small at fleet
scale: it needs packaging, upgrades, certificate handling, and a story for when it
disagrees with Prometheus. `up{}` from a scrape answers the same question with nothing
new to deploy.

## Related

- [002 — Server Identity](002-server-identity.md)
- [003 — Metrics Label Contract](003-metrics-label-contract.md)
- [004 — Automation via AWX](004-automation-via-awx.md)
- [`docs/glossaries/provisioning.md`](../glossaries/provisioning.md)
