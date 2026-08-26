# Cluster

## Definition

A Kubernetes or Slurm cluster swallow knows about.

swallow owns three things about a cluster: its **registration**, its **policy**, and the
**intent to build it**. It does not own the cluster's membership, its configuration, or its
behaviour — those belong to the cluster, and changing any of them is an
[Operation](operation.md) that swallow executes through its embedded Ansible runner.

A cluster therefore arrives one of two ways: registered because it already exists, or
declared with a deployment specification and then built. Both end at the same record.

## Key Fields

- `clusterId` — swallow-issued, opaque
- `siteId` — the [Site](site.md) it lives at
- `name` — unique within its site
- `type` — `kubernetes` or `slurm`
- `integrationId` — the cluster-kind [Integration](site.md#integration) swallow reads live
  state through. **Nullable**: a cluster that is registered but not yet reachable is the
  normal state between deciding to build one and having built it
- `gpuStackOwner` — see below
- `sync` — freshness of the last membership read, plus `memberCount` and `matchedCount`

## GPU Stack Owner

Which subsystem installs GPU drivers and the DCGM exporter. Values: `provisioning` or
`gpu-operator`.

This exists because two subsystems both want to own the GPU stack and **cannot coexist on
one host**:

| Policy | Installs drivers and DCGM | Trade-off |
|--------|---------------------------|-----------|
| `provisioning` | An embedded Ansible playbook after OS deployment, on the host | One driver version per site under change control, and it works for servers in no cluster — but changing a driver means reprovisioning |
| `gpu-operator` | The GPU operator inside the cluster | Per-cluster versions and in-cluster upgrades — but only for cluster members, and the driver's lifecycle is coupled to the cluster's |

**There is no default.** Guessing would silently pick a side in a conflict that leaves two
owners on one host, which is how a GPU node ends up with a driver that neither subsystem
believes it installed. swallow refuses operations that contradict the policy: a driver
installation targeting servers in a `gpu-operator` cluster is rejected, not executed.

Under either policy the host `node_exporter` stays: it exists before the cluster does, and
survives the cluster being rebuilt. Only the driver and DCGM are contested.

## Membership

Which servers are in a cluster is **read from the cluster's own API**, never written by
swallow. It appears as the `membership` axis on a [Server](server.md#the-three-status-axes).

swallow holds the *intent* — an operation that asked a server to join — and the cluster holds
the *fact*. When they disagree the cluster is right, and the disagreement is worth
surfacing rather than reconciling away.

### Matching members to servers

A cluster reports its own node names. swallow matches them to servers by hostname first, then
by address. Two rules matter:

- **An ambiguous match is no match.** If two servers share a hostname, neither is given
  the membership: attributing it to the wrong server would misdirect every operation aimed
  at it.
- **Unmatched members are reported, not ignored.** A cluster containing machines swallow does
  not manage is normal, and the count gap between `memberCount` and `matchedCount` is the
  honest way to show it.

### Known gap: leaving a cluster is indistinguishable from never being in one

When a sync no longer sees a server among a cluster's members, the axis is cleared to
null — the same value it holds for a server no cluster ever reported on. Both mean "no
cluster currently claims this server", but the second also means "and we did look".

The consequence is that the axis cannot say *when* non-membership was observed, so a
server that was drained an hour ago reads the same as one that was never checked. Readers
must not present a null membership axis as a positive statement that a server is
standalone. Closing the gap needs the axis to hold an observed non-membership with its own
timestamp, which is deferred until something actually depends on the distinction.

A member's cluster node name is kept on the axis because it is the join key for metrics
produced inside the cluster, which carry `cluster` and the node name rather than a
`server_id`. See
[decision 003](../decisions/003-metrics-label-contract.md#two-join-paths-on-purpose).

## Relationships

- A cluster belongs to exactly one [Site](site.md).
- A cluster reads its live state through one cluster-kind [Integration](site.md#integration).
- A cluster has zero or more member [Servers](server.md), read from its API.
- Deleting a cluster clears the membership axis it produced, so that no server is left
  claiming to belong to something that no longer exists.

## Out of Scope

- **Cluster configuration and lifecycle.** Deploying, upgrading, draining, or cordoning
  are operations, not fields.
- **Workload scheduling.** That is what the cluster is for.
- **Cluster-internal objects** — pods, jobs, partitions. swallow reads membership, not
  workloads.

## Related Concepts

- [Server](server.md) — what a cluster's members are, from swallow's side.
- [Operation](operation.md) — how a cluster is built and changed.
- [decision 003](../decisions/003-metrics-label-contract.md) — the `gpuStackOwner`
  reasoning and the monitoring components each policy disables.
