# 002 — Server Identity and the Three Status Axes

## Decision

**A server has three layers of identity, and its status is three independent axes.**

Identity:

1. `serverId` — a swallow-issued opaque identifier, stable for the physical machine's whole
   life in the platform. Every swallow reference (operation targets, allocations, cluster
   membership, metrics labels) uses this and only this.
2. `source` — the external key `(siteId, integrationId, providerMachineId)`. Unique.
   How the reconciler finds the record it is updating.
3. `hardware` — system UUID, serial number, and MAC addresses. Used to recognise the
   same physical machine when its `source` changes.

`hostname` and IP addresses are **observed attributes**: nullable, mutable, and not
unique anywhere.

Status is not one field. It is three axes, each with its own owner and its own
`observedAt`: `provisioning`, `membership`, and `health`.

## Terminology

`Server` stays swallow's word for a managed physical machine, as the API contract's naming
rules already require. This redesign does **not** rename it to `Node`, even though the
projection is now derived entirely from provider inventory.

The reason is stronger now than when the rule was written: Kubernetes and Slurm are in
scope, and both have their own `node` concept. A sentence like "resolve the cluster's node
name to a `server_id`" is unambiguous; the same sentence with `node` on both sides is not.

`node` therefore appears only for upstream tool names such as `node_exporter`, or when
explicitly qualified as a Kubernetes or Slurm node. `Machine` remains the provider-side
term for an entry in a provisioner's inventory.

## Context

The previous model made `hostname` and `ip` unique keys on the server record. That cannot
survive contact with the intended installation topology:

- Two sites can legitimately both have `gpu-node-01` at `10.0.1.10`. A fleet-wide unique
  constraint on either makes the second site unregistrable.
- During a reinstall a machine has no address at all, and afterwards may have a
  different one. A unique, required IP makes the reinstall window unrepresentable.
- A machine that is discovered but not yet commissioned has no address either. The first
  import implementation had to reject exactly this case, which was the symptom that
  exposed the modelling error.

Meanwhile a single `status` field was being asked to answer three unrelated questions
from three systems with three different refresh characteristics. "Is this machine ready
to be reimaged", "is it a worker in cluster A", and "is it responding right now" are not
points on one scale, and no single value can honestly represent them.

## Identity Model

### Why three layers and not one

Each layer answers a question the others cannot:

- `serverId` gives swallow a reference that survives everything. If a machine is
  re-enrolled in MAAS and gets a new `system_id`, allocation history and past operations
  must still point somewhere.
- `source` gives the reconciler a deterministic lookup. It is the only key that can be
  computed from a provider's response without heuristics.
- `hardware` gives continuity when `source` changes: the machine was re-enrolled, moved
  to a different provisioner, or the provider's database was rebuilt.

Without `hardware`, a re-enrollment silently becomes a second server and the fleet count
drifts. Without `serverId`, every historical reference breaks when a provider ID changes.

### Reconciler matching rules

For each machine a provider reports:

1. Match on `source`. Found means update in place.
2. No `source` match: attempt a hardware match, in order of trustworthiness — system
   UUID, then serial number, then MAC address intersection. A hit means the same physical
   machine re-appeared under a new provider identity: keep `serverId`, update `source`,
   and record that the source changed.
3. No match at all: a new server.
4. **Ambiguous hardware match** — more than one existing server matches: do not guess and
   do not merge. Create nothing, leave the existing records alone, and surface a conflict
   for an operator. Silent merging of two servers is unrecoverable; a visible conflict is
   merely annoying.

Rule 4 matters more than it looks. Hardware identifiers are not as unique in practice as
their specifications claim: virtual machines cloned from a template share a system UUID,
and some vendors ship batches with duplicate or empty DMI fields. The reconciler must
treat a duplicate as bad data rather than as a merge instruction.

Two refinements were forced by real data rather than anticipated:

**A placeholder is not an identifier.** Firmware usually reports an unfilled DMI field as
a string rather than as nothing: MAAS reports the literal `Unknown` as the serial of every
virtual machine, and bare metal ships with `To Be Filled By O.E.M.`, `Default string`, and
all-zero UUIDs. Compared literally, every machine in a batch matches every other. Such
values are stripped before matching and before storage, so an unfilled field is stored as
absent rather than as a string that happens to be shared.

**Rule 4 has to count claims within a pass, not existing records.** Ambiguity was
originally detected by the number of servers a machine's hardware matched. That never
fires for a batch sharing one identifier: the first machine relinks the single matching
server, which leaves the count at one, so the second machine also sees an unambiguous
match and steals the same record. Seven machines collapsed into one server, one relink at
a time — each step individually defensible. A server already claimed earlier in the same
pass is therefore a conflict, regardless of how many records matched.

A machine that disappears from a provider is **not** deleted. It is marked absent, with
the time it was last seen. Providers lose machines for uninteresting reasons — a
database restore, a rack being re-cabled — and deletion would take allocation history
with it.

### What is not part of identity

No operator-assigned display name. The provider's `hostname` is the display label, with
`serverId` as the fallback when there is none. Adding a swallow-owned name invites two names
for the same machine that disagree, and nothing currently needs it. If a stable label is
wanted later, it is additive.

## The Three Status Axes

| Axis | Owner | Answers | Refresh |
|------|-------|---------|---------|
| `provisioning` | Provisioner | Can this be deployed, is a deployment running, did it fail | Reconciler poll, tens of seconds |
| `membership` | Kubernetes API, Slurm | Is this in a cluster, in what role, is it being drained | Live read, or short-lived cache |
| `health` | Central TSDB | Is it up, is it hot, are there ECC errors | Query time |

Each axis carries `observedAt` and the integration it came from. An axis that has never
been observed is absent, not defaulted.

### Unknown is not a status value

The critical property: **"we do not know" must be distinguishable from "we know it is
bad"**. A site whose provisioner is unreachable does not make its servers unhealthy, and a
server with no metrics is not down — it might just not be scraped yet.

This is why staleness lives on each axis rather than on the server. When MAAS is
unreachable but Prometheus is fine, `provisioning` is stale while `health` is current,
and the API must be able to say exactly that. A single record-level "last updated" cannot
express it, and a single `status` field cannot express it at all.

The previous model's `unknown` server status conflated three different situations:
newly registered and never probed, monitoring not reporting, and data source unavailable.
Splitting the axes dissolves that ambiguity — each axis is simply absent or stale, and
the reason is the state of its own source.

### Membership is a projection, never a write target

swallow does not record that a server "belongs to" a cluster by setting a field. Membership
is read from the cluster's own API. swallow's server record holds the *intent* — an operation
that requested this server join that cluster — and the cluster API holds the fact. When
they disagree, the cluster is right and the disagreement is worth showing.

## Consequences

- Drop the unique indexes on `hostname` and `ip`. Add a unique index on
  `(siteId, integrationId, providerMachineId)`, plus non-unique indexes on `hostname` and
  addresses for search, and sparse indexes on the hardware identifiers for the
  re-enrollment lookup.
- `hostname` and addresses become nullable and non-unique.
- The single `status` field is replaced by three optional sub-documents.
- Server records are created by the reconciler, not by an operator. The manual
  "register a server" endpoint loses its reason to exist: a machine swallow has not
  discovered through a provider is a machine swallow cannot act on anyway. Manual
  registration is removed rather than kept as a second, weaker creation path.
- The import action from [001](001-system-ownership-boundaries.md) disappears with it.
  There is nothing to import: every provider machine is already a server projection. What
  used to be "import" is now just "allocate this server to a tenant", which is a tenancy
  operation on something that already exists.
- `server_id` is the metrics join label. See [003](003-metrics-label-contract.md).

The removal of manual registration and import is a direct consequence of the reconciler
existing, and it is a simplification: one way for a server to come into being, one key to
find it by.

## Rejected Alternatives

**Keep hostname as the primary key, scoped per site.** Would preserve human-readable
references and needs no reconciler matching rules. Rejected because hostname is not
stable across a reinstall — the whole point of a provisioning platform is that it changes
what is installed, including the name — and because the provider, not swallow, decides it.

**Use the provider's machine ID as swallow's server ID directly.** Removes a layer and makes
every reference traceable by eye. Rejected because it makes swallow's identifiers hostage to
a provider's database: a re-enrollment or a MAAS rebuild would orphan every operation
record and allocation that referenced the old ID.

**Trust the system UUID as the only identity.** It is exactly what a hardware identity
should be. Rejected on evidence: cloned virtual machines share it, and physical hardware
ships with duplicate or empty DMI fields often enough that a fleet will hit it. It is a
strong hint for re-enrollment matching, not a key.

**Keep one status field and compute it from the three sources.** Much simpler for the
frontend: one badge, one colour. Rejected because the computation has no correct
definition. A server that is `deployed`, not in any cluster, and not reporting metrics is
either a spare awaiting allocation or a broken host, and no rule can tell which. Any
collapsing function silently picks one interpretation and hides the other two axes'
staleness.

**Delete servers that vanish from their provider.** Keeps the projection exactly mirroring
the source, which is conceptually clean. Rejected because absence is usually transient
and deletion is not: a provider database restore would erase allocation history for
machines that never left the rack.

**Rename `Server` to `Node`.** It is the vernacular in GPU cluster management, and
`node_id` reads more naturally than `server_id`. Rejected because Kubernetes and Slurm
both own a `node` concept and both are in scope, so the collision is worse after this
redesign than before it. The existing naming rule was right.

## Related

- [001 — System Ownership Boundaries](001-system-ownership-boundaries.md)
- [003 — Metrics Label Contract](003-metrics-label-contract.md)
- [`docs/glossaries/provisioning.md`](../glossaries/provisioning.md)
