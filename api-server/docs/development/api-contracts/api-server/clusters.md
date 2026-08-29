# Clusters

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Register Kubernetes and Slurm clusters, read observed membership, deploy k0s, uninstall
Swallow-deployed k0s, and delete Swallow records. Swallow owns registration, policy, and
durable lifecycle intent. It does not own externally registered hosts or cluster
membership.

## Related Glossary Terms

- [Cluster](../../../../../docs/development/glossaries/terms/cluster.md)
- [Cluster Lifecycle State](../../../../../docs/development/glossaries/terms/cluster-lifecycle-state.md)
- [Operation](../../../../../docs/development/glossaries/terms/operation.md)
- [Node Role](../../../../../docs/development/glossaries/terms/node-role.md)

## Endpoints

```text
POST   /api/v1/clusters/
GET    /api/v1/clusters/
POST   /api/v1/clusters/deploy
GET    /api/v1/clusters/{clusterId}
PATCH  /api/v1/clusters/{clusterId}
DELETE /api/v1/clusters/{clusterId}
POST   /api/v1/clusters/{clusterId}/uninstall
POST   /api/v1/clusters/{clusterId}/sync
POST   /api/v1/clusters/sync
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Cluster Resource

```json
{
  "id": "cluster-id",
  "siteId": "site-id",
  "name": "lab-k0s",
  "type": "kubernetes",
  "integrationId": null,
  "origin": "deployed",
  "lifecycleState": "deploying",
  "lifecycleOperationId": "operation-id",
  "gpuStackOwner": "provisioning",
  "exporterOwner": "ansible",
  "sync": {
    "lastStartedAt": null,
    "lastSucceededAt": null,
    "lastError": null,
    "memberCount": 0,
    "matchedCount": 0
  },
  "createdAt": "2026-08-26T00:00:00Z",
  "updatedAt": "2026-08-26T00:00:00Z"
}
```

`type` is `kubernetes` or `slurm`. `gpuStackOwner` is `provisioning` or
`gpu-operator` and has no default. `exporterOwner` is `ansible` (default) or `k8s`.

`origin` is `registered | deployed`. `lifecycleState` is exactly
`registered | deploying | deploy_failed | active | uninstalling | uninstall_failed |
uninstalled`. `lifecycleOperationId` is the newest deployment or uninstall Operation ID,
or `null`. These fields are derived by the API in a batch from durable Operation history;
clients must not infer them from `integrationId`, membership, or sync freshness.

`integrationId` is `null` before a deployment produces a credential and again after a
successful uninstall removes the Swallow-owned credential Integration. A registered
cluster may also have no Integration. `sync.matchedCount` is how many reported members
matched a Server.

## Register An Existing Cluster

`POST /api/v1/clusters/` registers a cluster that already exists.

```json
{
  "siteId": "site-id",
  "name": "lab-k0s",
  "type": "kubernetes",
  "integrationId": "cluster-integration-id",
  "gpuStackOwner": "provisioning",
  "exporterOwner": "ansible"
}
```

`siteId`, `name`, `type`, and `gpuStackOwner` are required. `integrationId` and
`exporterOwner` are optional; `exporterOwner` defaults to `ansible`. Success is
`201 Created` returning the resource with `origin=registered`.

## Deploy A New Cluster

`POST /api/v1/clusters/deploy` creates a cluster and starts a
`deploy-kubernetes` Operation that builds it with k0s.

```json
{
  "siteId": "site-id",
  "name": "lab-k0s",
  "gpuStackOwner": "provisioning",
  "k0sVersion": "v1.36.3+k0s.2",
  "podCidr": "10.244.0.0/16",
  "serviceCidr": "10.96.0.0/12",
  "apiVip": "192.168.100.200",
  "apiVipPrefix": 24,
  "roleAssignments": [
    { "serverId": "server-a", "role": "control-plane" },
    { "serverId": "server-b", "role": "control-plane" },
    { "serverId": "server-c", "role": "control-plane" },
    { "serverId": "server-d", "role": "worker" }
  ]
}
```

`type` is always `kubernetes`. The target Servers must exist, be `deployed`, belong to
one Site, include an odd number of at least three control-plane assignments, and include at
least one worker. Pod and service ranges may not cover target addresses.

Success is `202 Accepted` after both records are persisted:

```json
{
  "clusterId": "cluster-id",
  "operationId": "operation-id"
}
```

Progress is read through [operations](operations.md). On success Swallow creates and marks
ownership of a credential Integration, attaches it to the Cluster, and begins membership
reads.

## Uninstall A Deployed Cluster

`POST /api/v1/clusters/{clusterId}/uninstall` starts an
`uninstall-kubernetes` Operation and retains the Cluster record.

```json
{
  "clusterId": "cluster-id",
  "operationId": "operation-id"
}
```

Eligibility requires a Kubernetes Cluster with durable `deploy-kubernetes` provenance.
The target set is always the complete `targetServerIds` snapshot from its latest
deployment Operation, never current membership. All targets must still exist, be present,
be unlocked, and have no overlapping active Operation. No single provisioning state is
required because a failed deployment can leave a mixed target set.

A pending or running deployment/uninstall, an already successful uninstall, a missing,
absent, locked, or busy target, a registered Cluster, or a Slurm Cluster returns
`409 conflict`. Failed, canceled, or indeterminate uninstalls may be submitted again; the
new Operation records `retryOfOperationId` pointing to the latest uninstall.

The release-owned playbook stops k0s services, runs `k0s reset`, removes Swallow-created
k0s state, units, join token, installer, and binary, and reloads systemd. It preserves the
OS, user data, `conntrack`, unrelated packages, and power state, and does not reboot.

On success Swallow clears membership, deletes only a credential Integration proven to be
Swallow-owned, and clears the Cluster integration/sync projection. If Kubernetes owned
exporters, Swallow separately queues `install-exporters` for targets that remain present,
deployed, and unlocked. That Operation has its own failure lifecycle and does not roll back
the uninstall.

## List, Get, Update, And Delete

`GET /api/v1/clusters/` returns a plain array, optionally filtered by `siteId`.
`GET /api/v1/clusters/{clusterId}` returns one. Lifecycle history is fetched in one batch
for list requests; the API does not fan out one Operation query per Cluster.

`PATCH /api/v1/clusters/{clusterId}` updates `name`, `integrationId`,
`gpuStackOwner`, or `exporterOwner`; omitted fields do not change.

`DELETE /api/v1/clusters/{clusterId}` is record-only and always returns
`{"success":true}` on success. It clears membership and a Swallow-owned credential
Integration, then removes the Cluster record. It never runs Ansible, changes hosts, waits
for Uninstall, restores exporters, or cancels accepted Operations. Pending/running
Operations may finish after Delete; retrying a finished deploy/uninstall Operation whose
Cluster no longer exists is refused.

Legacy records delete their linked Integration only when deployment provenance and the
complete auto-generated Kubernetes Integration signature both match. Operator-owned
Integrations are never deleted.

## Sync Membership

`POST /api/v1/clusters/{clusterId}/sync` reads membership now and returns:

```json
{
  "clusterId": "cluster-id",
  "clusterName": "lab-k0s",
  "members": 7,
  "matched": 7,
  "cleared": 0,
  "unmatched": [],
  "error": null
}
```

`POST /api/v1/clusters/sync` syncs every registered cluster. One failing cluster does
not stop the rest. For Kubernetes, nodes are read from `/api/v1/nodes`; enabled k0s
control-plane lease discovery also reads dedicated controllers from `k0s-ctrl-*` leases.

## Errors

Unknown Cluster, Site, or Integration returns `not_found`. Duplicate names and uninstall
eligibility/target conflicts return `conflict`. Invalid resource values or deployment
topology return `validation_error`. Missing/disabled automation, missing automation
credential, or unavailable release playbook returns `provider_unavailable`. Cluster API
transport/auth failures return `provider_unavailable`; provider rejection returns
`validation_error`.

## Compatibility Notes

- Uninstall and lifecycle fields are additive. Existing paths and Delete response remain
  unchanged.
- Delete remains non-destructive to hosts.
- `roleAssignments` uses `control-plane | worker`; k0s's `controller` term is an
  implementation detail.
- Control-plane lease discovery remains per Integration because its naming is k0s-specific.
