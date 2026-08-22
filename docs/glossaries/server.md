# Server

## Definition

A physical machine swallow manages, projected from a provisioner's inventory.

A server is not a record an operator creates. It comes into being when a reconciler sees
a [Machine](provisioning.md#machine) in a provisioner's inventory and there is no server
for it yet. swallow assigns it an identity and keeps a cached projection of what the
provisioner reports.

`Server` is deliberately not called `Node`: Kubernetes and Slurm both own a `node`
concept and both are in scope. See
[decision 002](../decisions/002-server-identity.md#terminology).

## Identity

Three layers, each answering a question the others cannot.

| Layer | Fields | Purpose |
|-------|--------|---------|
| Platform identity | `serverId` | swallow-issued, opaque, stable for the machine's whole life in the platform. **Every swallow reference uses this and only this** |
| Source | `siteId`, `integrationId`, `providerMachineId` | The external key. Unique. How the reconciler finds the record to update |
| Hardware | `systemUUID`, `serialNumber`, `macAddresses[]` | Recognises the same physical machine when its source changes, e.g. after re-enrollment |

`serverId` is what operations target, what tenancy allocates, and what the
[metrics label contract](../decisions/003-metrics-label-contract.md) joins on.

## Observed Attributes

Everything the provisioner reports about the machine, cached and never authoritative:

- `hostname` — **nullable, mutable, not unique**. Two sites may both have `gpu-node-01`
- `addresses[]` — **nullable, not unique**. Empty before commissioning and during a reinstall
- `architecture`, `cpuCores`, `cpuModel`, `memoryMiB`, `storageGB`
- `gpus[]` — vendor, model, and count as the provisioner detected them. Refreshed on a
  slower cadence than the rest of the projection, because attached devices cost a call per
  machine and change only at commissioning; see
  [provisioning](provisioning.md#deepened-provider-integration)
- `systemVendor`, `systemProduct` — the machine's make, so the fleet can be grouped by
  hardware generation
- `tags[]` — the provisioner's own labels, mirrored because a fleet is routinely filtered
  by them
- `providerZone`, `providerResourcePool`, `providerPod` — the provisioner's own grouping
  labels (the last being the VM host, for a virtual machine), carried through opaquely and
  **not** mapped onto any swallow hierarchy
- `absent`, `lastSeenAt` — set when the machine stops appearing in its provisioner's
  inventory. An absent server is never deleted: absence is usually transient, deletion is not

Uniqueness is enforced on the source key, never on `hostname` or an address. A required,
unique IP would make a reinstalling machine unrepresentable.

Everything here is a **mirrored fact**, not owned data: it rides an `observedAt` freshness
and the provisioner stays the source of truth. What is mirrored is limited to what the
fleet is queried by; a machine's full firmware, disk, and PCI detail is read live one
machine at a time instead of cached. See
[decision 001](../decisions/001-system-ownership-boundaries.md#rules-for-mirrored-facts).

## The Three Status Axes

A server has no single `status`. Three axes, three owners, three refresh rates:

| Axis | Owner | Answers |
|------|-------|---------|
| `provisioning` | Provisioner | Can it be deployed, is a deployment running, did it fail, does the OS survive a reboot |
| `membership` | Kubernetes API, Slurm | Is it in a cluster, in what role, is it draining |
| `health` | Central TSDB | Is it up, is it hot, are there hardware errors |

Each axis carries `observedAt` and the integration it came from. **An axis that has never
been observed is absent, not defaulted.**

The rule that makes this work: *"we do not know" must never be presentable as "we know it
is bad"*. A site whose provisioner is unreachable does not make its servers unhealthy, and
a server with no metrics is not down — it may simply not be scraped yet. Staleness
therefore lives on each axis, so that "MAAS is stale but Prometheus is current" is
expressible.

The `provisioning` axis also carries whether the deployment is
[ephemeral](provisioning.md#ephemeral-deployment) — the OS running from memory, with
nothing on the root filesystem surviving a reboot. It sits on the axis rather than being
remembered from the deploy request, because the provisioner is what knows: a machine can
be redeployed the other way round without swallow being involved. Alongside it the axis
mirrors the provisioner's `locked` flag and its commissioning and testing status, so a
rejected action or a failed inspection is visible without opening the machine.

The provisioner can also be driven beyond deploy and release — power, hardware
validation, and operator state actions — and asked for a full live detail of one machine.
Those are the provider's own operations, triggered through swallow and mirrored back, never
reimplemented; see [provisioning](provisioning.md#deepened-provider-integration).

`membership` is read from the cluster, never written by swallow. swallow holds the *intent* that
a server should join a cluster; the cluster API holds the fact. When they disagree the
cluster is right, and the disagreement is worth surfacing.

## Relationships

- A server belongs to exactly one [Site](site.md), through its source.
- A server is projected from exactly one [Machine](provisioning.md#machine) at a time.
  The pair is re-linked, not duplicated, if the machine is re-enrolled.
- A server may be a member of a cluster. That is a fact read from the cluster.
- A server may be allocated to a tenant. That is swallow-owned policy.
- A server may be the target of an [Operation](../decisions/004-automation-via-awx.md).

## Out of Scope

- **Hardware history.** The provisioner keeps commissioning history in more detail than
  swallow could.
- **BMC and SSH credentials.** Host access credentials belong to the provisioner and to
  AWX, which need them to do their jobs. swallow never holds them.
- **Physical placement hierarchy.** swallow models `Site` and carries the provisioner's zone
  and pool labels. A swallow-owned datacenter, room, and rack hierarchy is deferred: it is
  not needed by any current feature, and inventing it now would mean maintaining
  placement data with no consumer.
- **Operational metrics.** Never stored. See
  [decision 003](../decisions/003-metrics-label-contract.md).

## Related Concepts

- [Site](site.md) — the frame a server exists in.
- [Provisioning](provisioning.md) — where servers come from.
- [decision 002](../decisions/002-server-identity.md) — the full reasoning, including
  reconciler matching rules and rejected alternatives.
