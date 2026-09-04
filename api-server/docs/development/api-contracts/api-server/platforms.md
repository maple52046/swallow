# Platforms

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Register Kubernetes and Slurm platforms, read observed membership, deploy k0s, uninstall
Swallow-deployed k0s, and delete Swallow records. Swallow owns registration, policy, and
durable lifecycle intent. It does not own externally registered hosts or platform
membership.

## Related Glossary Terms

- [Platform](../../../../../docs/development/glossaries/terms/platform.md)
- [Platform Lifecycle State](../../../../../docs/development/glossaries/terms/platform-lifecycle-state.md)
- [Operation](../../../../../docs/development/glossaries/terms/operation.md)
- [Node Role](../../../../../docs/development/glossaries/terms/node-role.md)
- [Platform Topology](../../../../../docs/development/glossaries/terms/platform-topology.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)

## Endpoints

```text
POST   /api/v1/platforms/
GET    /api/v1/platforms/
POST   /api/v1/platforms/deploy
GET    /api/v1/platforms/{platformId}
PATCH  /api/v1/platforms/{platformId}
DELETE /api/v1/platforms/{platformId}
POST   /api/v1/platforms/{platformId}/uninstall
POST   /api/v1/platforms/{platformId}/sync
POST   /api/v1/platforms/sync
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Platform Resource

```json
{
  "id": "platform-id",
  "siteId": "site-id",
  "name": "lab-k0s",
  "type": "kubernetes",
  "integrationId": null,
  "origin": "deployed",
  "lifecycleState": "deploying",
  "lifecycleOperationId": "operation-id",
  "deployment": {
    "topology": "standalone",
    "roleAssignments": [
      { "serverId": "server-a", "role": "control-plane", "runWorkloads": true }
    ]
  },
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

`deployment` is the non-secret topology intent recovered from the latest durable
`deploy-kubernetes` Operation. It contains `topology` as `standalone`, `multi-node`, or `high-availability` and the
original `roleAssignments`, including `runWorkloads`.
It is `null` for registered Platforms and legacy deployment history that cannot be
projected completely. A control-plane assignment with `runWorkloads=true` retains the
`control-plane` Node Role while also being workload-capable; clients must not treat the
number of worker-only assignments as total workload capacity.

`integrationId` is `null` before a deployment produces a credential and again after a
successful uninstall removes the Swallow-owned credential Integration. A registered
platform may also have no Integration. `sync.matchedCount` is how many reported members
matched a Server.

## Register An Existing Platform

`POST /api/v1/platforms/` registers a platform that already exists.

```json
{
  "siteId": "site-id",
  "name": "lab-k0s",
  "type": "kubernetes",
  "integrationId": "platform-integration-id",
  "gpuStackOwner": "provisioning",
  "exporterOwner": "ansible"
}
```

`siteId`, `name`, `type`, and `gpuStackOwner` are required. `integrationId` and
`exporterOwner` are optional; `exporterOwner` defaults to `ansible`. Success is
`201 Created` returning the resource with `origin=registered`.

## Deploy A New Platform

`POST /api/v1/platforms/deploy` creates a platform and starts a
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
    { "serverId": "server-a", "role": "control-plane", "runWorkloads": false },
    { "serverId": "server-b", "role": "control-plane" },
    { "serverId": "server-c", "role": "control-plane" },
    { "serverId": "server-d", "role": "worker" }
  ]
}
```

`type` is always `kubernetes`. The target Servers must exist, be `deployed`, and belong to one Site. They must also be unlocked in a live provider read. The complete lock preflight finishes before Swallow creates either the
Platform or Operation; one locked target rejects the batch with `409 conflict`.

A role assignment uses `control-plane | worker`. `runWorkloads` is optional,
defaults to `false`, and is valid only on a `control-plane` assignment; a `worker` always
runs workloads.

A target is rejected while it is claimed by another Swallow-deployed Platform whose
lifecycle is `deploying`, `deploy_failed`, `active`, `uninstalling`, or
`uninstall_failed`. Claims use the durable deployment Operation's complete
`targetServerIds` snapshot, so a partial failed deployment remains protected even before
membership can be observed. A successful Uninstall transitions the Platform to
`uninstalled` and releases that claim. A target carrying any observed Platform membership
is also rejected. The current Server projection has one membership axis; allowing
cross-platform co-residency requires a future platform-scoped multi-membership contract
rather than overwriting the existing observation.

The role assignments infer one of these supported topologies:

- Standalone: one control-plane assignment with `runWorkloads=true` and no worker.
- Non-HA multi-node: one control-plane assignment and one or more worker assignments; the
  control-plane may also set `runWorkloads=true`.
- High availability: an odd number of at least three control-plane assignments and at
  least one workload-capable assignment.

Two control-plane assignments and even control-plane counts are rejected. Every topology
must supply workload capacity. Pod and service ranges may not cover target addresses.
`apiVip` and `apiVipPrefix` are required only for high availability; standalone and
non-HA multi-node deployments omit them and use the initial control-plane Server address
as the API endpoint. An HA virtual IP may not equal a selected Server address.

Success is `202 Accepted` after both records are persisted:

```json
{
  "platformId": "platform-id",
  "operationId": "operation-id"
}
```

Progress is read through [operations](operations.md). On success Swallow creates and marks
ownership of a credential Integration, attaches it to the Platform, and begins membership
reads.

## Uninstall A Deployed Platform

`POST /api/v1/platforms/{platformId}/uninstall` starts an
`uninstall-kubernetes` Operation and retains the Platform record.

The request body is optional. An absent or empty body removes k0s only. To also return
the member servers to the provider in the same Operation, send:

```json
{
  "releaseServers": true,
  "releaseOptions": {
    "erase": false,
    "secureErase": false,
    "quickErase": false,
    "unbindStaticIps": false
  }
}
```

When `releaseServers` is true the Operation runs the k0s uninstall step first and then a
`release-os` step per member server that depends on it, so a failed uninstall never
releases a host. `releaseOptions` mirrors the standalone Release action (disk erase and
static-IP unbinding) and is ignored when `releaseServers` is false. Releasing wipes the
operating system, so Swallow does not restore host exporters for released servers. A
release requires the durable orchestration topology; if it is unavailable the request is
rejected. The accepted response is unchanged:

```json
{
  "platformId": "platform-id",
  "operationId": "operation-id"
}
```

Eligibility requires a Kubernetes Platform with durable `deploy-kubernetes` provenance.
The target set is always the complete `targetServerIds` snapshot from its latest
deployment Operation, never current membership. All targets must still exist, be present,
be unlocked, and have no overlapping active Operation. Lock is checked live before
the uninstall Operation is created. No single provisioning state is
required because a failed deployment can leave a mixed target set.

A pending or running deployment/uninstall, an already successful uninstall, a missing,
absent, locked, or busy target, a registered Platform, or a Slurm Platform returns
`409 conflict`. Failed, canceled, or indeterminate uninstalls may be submitted again; the
new Operation records `retryOfOperationId` pointing to the latest uninstall.

The release-owned playbook stops k0s services, runs `k0s reset`, removes Swallow-created
k0s state, units, join token, installer, and binary, and reloads systemd. It preserves the
OS, user data, `conntrack`, unrelated packages, and power state, and does not reboot.

On success Swallow clears membership, deletes only a credential Integration proven to be
Swallow-owned, and clears the Platform integration/sync projection. If Kubernetes owned
exporters, Swallow separately queues `install-exporters` for targets that remain present,
deployed, and unlocked. That Operation has its own failure lifecycle and does not roll back
the uninstall.

## List, Get, Update, And Delete

`GET /api/v1/platforms/` returns a plain array, optionally filtered by `siteId`.
`GET /api/v1/platforms/{platformId}` returns one. Lifecycle history is fetched in one batch
for list requests; the API does not fan out one Operation query per Platform.

`PATCH /api/v1/platforms/{platformId}` updates `name`, `integrationId`,
`gpuStackOwner`, or `exporterOwner`; omitted fields do not change.

`DELETE /api/v1/platforms/{platformId}` is record-only and always returns
`{"success":true}` on success. It clears membership and a Swallow-owned credential
Integration, then removes the Platform record. It never runs Ansible, changes hosts, waits
for Uninstall, restores exporters, or cancels accepted Operations. It remains
available while targets are locked because it does not mutate them. Pending/running
Operations may finish after Delete; retrying a finished deploy/uninstall Operation whose
Platform no longer exists is refused.

Legacy records delete their linked Integration only when deployment provenance and the
complete auto-generated Kubernetes Integration signature both match. Operator-owned
Integrations are never deleted.

## Sync Membership

`POST /api/v1/platforms/{platformId}/sync` reads membership now and returns:

```json
{
  "platformId": "platform-id",
  "platformName": "lab-k0s",
  "members": 7,
  "matched": 7,
  "cleared": 0,
  "unmatched": [],
  "error": null
}
```

`POST /api/v1/platforms/sync` syncs every registered platform. One failing platform does
not stop the rest. For Kubernetes, nodes are read from `/api/v1/nodes`; enabled k0s
control-plane lease discovery also reads dedicated controllers from `k0s-ctrl-*` leases.

## Errors

Unknown Platform, Site, or Integration returns `not_found`. Duplicate names and uninstall
eligibility/target conflicts return `conflict`. Invalid resource values or deployment
topology return `validation_error`. Missing/disabled automation, missing automation
credential, or unavailable release playbook returns `provider_unavailable`. Platform API
transport/auth failures return `provider_unavailable`; provider rejection returns
`validation_error`.

## Compatibility Notes

- The aggregate was renamed from Cluster to Platform. For one release the former surface
  remains available as deprecated aliases:
  - Route alias: every `/api/v1/clusters...` path mirrors the corresponding
    `/api/v1/platforms...` path and is served by the same handler. Responses to the alias
    carry a `Deprecation` header and a `Link` header pointing at the canonical path. New
    clients must use `/api/v1/platforms`.
  - Field alias: responses include `clusterId` alongside the canonical `platformId` with
    the same value; requests accept either, and `platformId` wins when both are sent.
  - The aliases are removed after the one-release deprecation window; see
    [ADR-014](../../../../../docs/decisions/014-platform-resource-language.md).
- Uninstall and lifecycle fields are additive. Existing paths and Delete response remain
  unchanged.
- Delete remains non-destructive to hosts.
- `roleAssignments` continues to use `control-plane | worker`; the k0s `controller` term is
  an implementation detail. The additive `runWorkloads` field defaults to `false`, so
  existing HA requests retain dedicated control-plane behavior.
- `apiVip` remains accepted exactly as before for HA requests and is now optional for a
  one-control-plane deployment.
- Control-plane lease discovery remains per Integration because its naming is k0s-specific.
