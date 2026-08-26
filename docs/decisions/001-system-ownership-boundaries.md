# 001 — System Ownership Boundaries

> Automation ownership in this decision is amended by [ADR 006](006-embedded-ansible-execution.md).

## Decision

**swallow owns intent, policy, and identity mapping. For everything else, it integrates a
system that already owns the capability rather than rebuilding it — and where no such
system exists, it may own the data itself.**

This is a test applied per capability, not a blanket ban on holding facts. An earlier
wording — "swallow owns no facts about the physical or runtime world" — was too rigid: it
could not tell the difference between "do not rebuild a time-series database" (always
right, because that is Prometheus's whole problem domain) and "swallow may never store a
hardware attribute" (not obviously right, and an obstacle to features that have no
external owner).

The test for any new capability:

1. **Does a mature system already do this well, with an API to integrate?** Then
   integrate it. Do not copy its data model into swallow; reference it and mirror only what
   fleet-scale querying forces you to cache. OS provisioning is the archetype: MAAS and
   Ironic already do it completely, so swallow drives them rather than reimplementing them.
2. **Is there no such system, or is the value in correlating several of them?** Then swallow
   may own it outright, including its own tables. Identity mapping, policy, sites, and
   tenancy are here because nothing else can hold them.

### The two kinds of data swallow holds

Every field swallow stores is exactly one of these, and the distinction is the point:

- **Owned data** — swallow is the source of truth. There is no external owner to disagree
  with, so there is no `observedAt` and no staleness: sites, integrations, identity
  mapping, policy, operations, tenancy. Reading it back is authoritative.
- **Mirrored facts** — a copy of something an external system owns, held only so the
  fleet can be queried without fanning out to every site on every request. Every
  mirrored field carries its source and when that source was last observed, and it is
  never authoritative for a write.

A new feature must say which kind each of its fields is before it is built. That single
question is what this document exists to force; the earlier version answered it by
forbidding the second category, which is why it read as a prohibition rather than a
guide.

> **Out of scope here:** whether swallow's owned data lives in MongoDB or something lighter
> like SQLite is a storage decision, independent of this ownership boundary, and is not
> settled by this document.

## Context

The first iteration of swallow modelled the world directly: a `Server` record with
hostname, IP, hardware inventory, an operational status, and a bespoke agent reporting
into it. Every one of those fields already had an owner elsewhere — MAAS knew the
hardware in far more detail, Prometheus knew whether the host was up, and nobody had
decided which one swallow should believe.

That is the actual defect. It was not bad code; it was modelling before deciding
ownership. The symptoms all followed from it:

- `hostname` and `ip` were unique keys, which cannot hold in a multi-site fleet and
  breaks during a reinstall when a machine briefly has no address.
- The agent collected inventory once at startup and never again, because there was no
  answer to "who asks it to look again".
- `AgentStatusStale` was defined and never set, because nothing owned the passage of time.
- Alerts had an `acknowledge`/`resolve` lifecycle in swallow while Alertmanager already had
  silences, so two systems claimed the same state.

This document exists so that the next feature starts from ownership instead of from a
data model.

## The Boundaries

### Facts swallow does not own

| Fact | Owner | swallow access | swallow stores |
|------|-------|-------------|-------------|
| Which machines exist; CPU, RAM, GPU, disk, NIC detail | Provisioner (MAAS, one per site) | Reconciler poll | Cached projection, with staleness |
| OS deployment state and deployed OS | Provisioner | Reconciler poll | Cached projection |
| Power state | Provisioner (via BMC) | Reconciler poll | Cached projection, short freshness window |
| IP assignment, DHCP, DNS, subnets, VLANs | Provisioner | Read-only, as machine attributes | Nothing modelled |
| Long-running execution: state, logs, leases, concurrency | Swallow embedded Ansible runtime | Internal application port | Owned operation metadata and retained local artifacts |
| Playbooks, roles, automation content | Git and the signed Swallow release | Manifest allowlist | Immutable release bundle, not mutable database content |
| Time-series metrics | Central TSDB | PromQL at query time | Nothing, ever |
| Alert rule evaluation and firing state | Prometheus and Alertmanager | Read on demand, silences out | Nothing |
| Dashboards | Grafana | Deep link | Nothing |
| Kubernetes node state, membership, workloads | Kubernetes API | Live read | Nothing |
| Slurm partitions, Slurm node states, jobs | Slurm | Live read | Nothing |
| Job scheduling and queueing | Slurm and Kubernetes | Live read | Nothing |

### Facts swallow owns

| Fact | Why swallow must own it |
|------|----------------------|
| Sites | No external system knows the set of sites; it is the frame everything else hangs off |
| Integrations: endpoints and credentials per site | This is swallow's own configuration. Credentials may be delegated to a secret store, but the registry of what exists is swallow's |
| Identity mapping | The join between a provisioner machine ID, a metrics label set, and a cluster's own node name exists nowhere else. This is swallow's central value |
| Clusters as records: which cluster exists, at which site, with which policy | The cluster's own API knows its members but not its intended shape or its governing policy |
| Policy, e.g. `gpuStackOwner` | Pure intent. Two subsystems both want to install GPU drivers; only an operator decision resolves it |
| Operations: intent, target set, lease, execution state, and artifact reference | No external controller owns this cross-system intent or its execution record |
| Tenancy: teams, users, server allocation | Allocation is a platform-level policy question, not a fact any provisioner or cluster holds |

### Rules for mirrored facts

Mirroring exists because a fleet cannot be served by fanning out to every site on every
request: the slowest provisioner would set page latency, one unreachable site would
break the whole listing, and cross-site sorting and pagination cannot be computed
correctly by merging per-provider pages. So swallow keeps a local copy of external facts it
needs to query across sites — provisioner inventory today, and any future integration's
facts under the same rules.

A mirrored fact is honest only under three rules:

1. Every mirrored record carries the source it came from and when that source was last
   observed.
2. Freshness is part of the API, not an implementation detail. A client can always ask
   how stale a view is and must be able to tell "this site last synced 14 minutes ago"
   from "this site is up to date".
3. A mirror is never authoritative for a write. An action always goes to the owning
   system, and the mirror converges afterwards. swallow never writes to a mirror to reflect
   what it hopes happened.

How much to mirror is itself the integrate-or-own test applied field by field. Mirror
what the fleet is queried, filtered, sorted, or aggregated by — a machine's GPU
inventory, its coarse lifecycle state, the tags it is grouped under. Do not mirror what
is only ever read one machine at a time: a machine's full firmware detail, its per-disk
layout, its PCI bus map. Those stay a live read against the owner, fetched when a single
machine is opened, so that swallow carries no schema for them and no staleness to explain.

## Consequences

### What this makes possible

A single view across sites that no individual tool can produce, because swallow holds the
identity mapping: this server, in this site, provisioned by that MAAS, currently a worker
in that Kubernetes cluster, emitting these metrics, last touched by that Swallow-owned Ansible run.

That correlation is the product. Everything else is someone else's job.

### What the test still rules out

These are not banned by category; each is a case where a mature system already owns the
capability, so the integrate-or-own test lands on "integrate, do not rebuild":

- **No custom agent on managed servers.** Its two jobs are already covered: hardware
  detail by provisioner commissioning, liveness and host metrics by `node_exporter`.
  A bespoke agent on every server in a fleet is a maintenance cost with no unique output.
- **No metric storage.** swallow queries a TSDB. It does not keep a `GPUMetrics` table.
  Storing metrics is rebuilding a time-series database badly.
- **Bounded operation log storage.** Runner artifacts live locally for 30 days; durable operation metadata remains in MongoDB.
- **No alert rule evaluation.** Rules live with Prometheus. swallow receives what fires.
- **No alert lifecycle of its own.** Acknowledging is creating an Alertmanager silence,
  not setting a field in Mongo.
- **No mutable automation content in MongoDB.** Versioned playbooks belong in git and the signed release bundle; site configuration may select only manifest-listed names.
- **No network management.** No DHCP, DNS, subnet, or VLAN modelling.
- **No scheduler.** Deciding which workload runs where is Slurm's and Kubernetes' job.
- **No hardware history or CMDB.** swallow may mirror a machine's *current* hardware as the
  provisioner reports it — enough to query the fleet by GPU model or vendor — but it does
  not keep the history of how that hardware changed. Commissioning history stays with the
  provisioner, which is the system built to own it.

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
  assumed swallow owned both automation content and execution state.
- Drop the frontend's alert acknowledge and resolve mutations in favour of silences.

## Rejected Alternatives

**swallow owns orchestration and drives servers itself.** This is the most capable design and
gives the most consistent experience, and it is what the original agent plus a bidirectional
gRPC stream was drifting towards. Rejected because it means owning idempotency, retry,
concurrency limits, rollback, and log durability — that is Ansible's entire
problem domain, solved, and reimplementing it is the largest possible detour from what
swallow is for. The gRPC control channel was structurally already in place, which made this
tempting and is worth recording as the closest call in this document.

**swallow as a pure read-only pane of glass, with every change made in the underlying tools.**
Cheapest to build and impossible to get wrong. Rejected because the cross-cutting
workflows are the reason the platform exists: moving servers between clusters, or reimaging
a rack and putting it back into service, spans provisioner, automation, and cluster. If
every action has to be performed by hand in three tools, swallow is a dashboard.

**Store metrics in swallow for a unified API.** Superficially attractive: one API for
clients, no PromQL knowledge needed in the frontend, no dependency on a TSDB being
reachable. Rejected because it is a time-series database with worse retention,
cardinality handling, and query language than the one already in operation, and because the
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
- [006 — Embedded Ansible execution](006-embedded-ansible-execution.md)
- [`docs/glossaries/provisioning.md`](../glossaries/provisioning.md)
