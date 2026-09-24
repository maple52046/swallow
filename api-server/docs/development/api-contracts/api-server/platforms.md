# Platforms

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Deploy Kubernetes (k0s) and Slurm platforms, read observed membership, uninstall
Swallow-deployed platforms, and delete Swallow records. Swallow owns policy and durable
lifecycle intent for the platforms it deploys. It does not own platform membership, and — per
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md) — it
manages only self-deployed platforms: the public API no longer registers an existing platform.

The in-cluster management of a deployed Kubernetes platform (namespaces, applications, pods,
apply, node cordon) is a separate live surface, [platforms-kubernetes.md](platforms-kubernetes.md).

## Related Glossary Terms

- [Platform](../../../../../docs/development/glossaries/terms/platform.md)
- [Platform Lifecycle State](../../../../../docs/development/glossaries/terms/platform-lifecycle-state.md)
- [Operation](../../../../../docs/development/glossaries/terms/operation.md)
- [Node Role](../../../../../docs/development/glossaries/terms/node-role.md)
- [Platform Topology](../../../../../docs/development/glossaries/terms/platform-topology.md)
- [Minimum Resource Requirement](../../../../../docs/development/glossaries/terms/minimum-resource-requirement.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)

## Endpoints

```text
GET    /api/v1/platforms/
GET    /api/v1/platforms/deployment-requirements/slurm
PUT    /api/v1/platforms/deployment-requirements/slurm
POST   /api/v1/platforms/deploy
GET    /api/v1/platforms/{platformId}
PATCH  /api/v1/platforms/{platformId}
DELETE /api/v1/platforms/{platformId}
POST   /api/v1/platforms/{platformId}/uninstall
POST   /api/v1/platforms/{platformId}/sync
POST   /api/v1/platforms/sync
GET    /api/v1/platforms/{platformId}/slurm
```

There is **no** `POST /api/v1/platforms/`. A Platform is created only by
`POST /api/v1/platforms/deploy`; registering an existing platform was removed in
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md). The
Kubernetes cluster explorer routes under `/api/v1/platforms/{platformId}/kubernetes/...` are
specified in [platforms-kubernetes.md](platforms-kubernetes.md).

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

`origin` is `registered | deployed`. New Platforms are always `deployed`; `origin=registered`
is a one-release compatibility value for records created before the register-existing path was
removed ([decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md)).
`lifecycleState` is exactly `registered | deploying | deploy_failed | active | uninstalling |
uninstall_failed | uninstalled`. `lifecycleOperationId` is the newest deployment or uninstall
Operation ID, or `null`. These fields are derived by the API in a batch from durable Operation
history; clients must not infer them from `integrationId`, membership, or sync freshness.

`deployment` is the non-secret topology intent recovered from the latest durable
`deploy-kubernetes` or `configure-slurm` Operation. It contains `topology` as `standalone`,
`multi-node`, or `high-availability` and the original `roleAssignments`, including
`runWorkloads`. For a Slurm platform it also carries `loginServerIds` (present only when the
deploy assigned login hosts): the login (submission) hosts, which run no cluster daemon and are
neither role assignments nor members, so a client resolves them to a Server address to show
which host to use to operate the cluster.
It is `null` for registered Platforms and legacy deployment history that cannot be
projected completely. A control-plane assignment with `runWorkloads=true` retains the
`control-plane` Node Role while also being workload-capable; clients must not treat the
number of worker-only assignments as total workload capacity.

`integrationId` is `null` before a deployment produces a credential and again after a
successful uninstall removes the Swallow-owned credential Integration. A legacy `registered`
platform may also have no Integration. `sync.matchedCount` is how many reported members
matched a Server.

## Registering An Existing Platform Is Removed

There is no public route to register an existing platform. Swallow manages only self-deployed
platforms; a Platform record and its credential Integration are produced only by
`POST /api/v1/platforms/deploy`
([decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md)). A
`platform`-kind Integration is likewise created only by a successful deployment, so
`POST /api/v1/integrations` with `kind=platform` (or the `cluster` alias) is rejected — see
[sites-integrations.md](sites-integrations.md). Existing `origin=registered` records remain
readable and deletable for one release but cannot be uninstalled and have no cluster explorer.

## Manage The Slurm Deployment Requirement

`GET /api/v1/platforms/deployment-requirements/slurm` reads the current system-wide Slurm
deployment eligibility policy. A missing stored record is returned as the disabled representation;
it is not a 404. `PUT` replaces the policy. These routes exist only on the canonical Platforms
surface and have no `/clusters` compatibility alias.

```json
{
  "platformType": "slurm",
  "minimumResources": {
    "cpuCores": 4,
    "memoryMiB": 24576,
    "storageGB": 80
  },
  "updatedAt": "2026-09-13T00:00:00Z"
}
```

The PUT request contains `minimumResources` with the same object shape. All three fields are
required when enabled and must be greater than zero; `cpuCores` and `memoryMiB` are integers.
Sending `{ "minimumResources": null }` disables the policy. The response is the complete shape
above; while disabled, both `minimumResources` and the never-configured `updatedAt` are `null`
(the timestamp remains populated after an explicit disable). Omitting `minimumResources` is a
validation error.

This policy is a hardware eligibility floor, not a reservation, Slurm scheduler capacity, or image
size calculation.

## Deploy A New Platform

`POST /api/v1/platforms/deploy` creates a platform and starts the Operation that builds
it. `type` selects the platform: `kubernetes` (the default when omitted) starts a
`deploy-kubernetes` Operation that builds k0s; `slurm` starts a `configure-slurm`
Operation (see [Slurm](#slurm) below).

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

This body deploys `type: "kubernetes"`. The target Servers must exist and belong to one
Site. They must also be unlocked in a live provider read. The complete lock preflight
finishes before Swallow creates either the Platform or Operation; one locked target rejects
the batch with `409 conflict`.

By default the targets must already be `deployed`. To provision the operating system inside
the same Operation, include an optional `machinePreparation` object (shared by both platform
types):

```json
{
  "machinePreparation": {
    "mode": "provision_os",
    "settings": { "imageId": "os-image-id", "ephemeral": false },
    "network": { "mode": "automatic" }
  }
}
```

`mode` is `existing_os` (default) or `provision_os`. In `provision_os` a target that is
already `deployed` is reused as-is while a `ready` target is provisioned first, so one
deploy may mix both. The deploy mode is the `ram` Deploy Target — `settings.ephemeral=true`,
the platform contract's boundary field (the dashboard's Deploy Target selector maps
"RAM deploy (ephemeral)" onto it). RAM/ephemeral means the provider boots a
memory-backed root filesystem and leaves disks untouched; all OS and platform-local state is
lost when a Server reboots. A platform `provision_os` deploy of a *custom* image is subject
to the same custom-image verification gate as an OS deployment: the image must be verified
for the requested Deploy Target (see the provisioning contract), synced images bypass. An
unverified custom image is rejected with `409 conflict` and an actionable message
("this custom image is not verified for <target> deployment; verify it on a ready Server
first"), not a generic error. Kubernetes automation performs a booted-host compatibility gate
before starting any controller or worker. It also selects containerd's `native` snapshotter
for a memory-backed root, because an `overlayfs` snapshotter cannot reliably nest inside a
MAAS overlay root. MAAS custom `root.tgz` boots may expose `/` as either overlayfs or tmpfs;
both are volatile and leave physical disks untouched.

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

### Slurm

Set `type: "slurm"` and provide a `slurm` object in place of the Kubernetes fields. Node
roles are per-daemon flags because a Server may run the controller daemon (`slurmctld`),
the compute daemon (`slurmd`), or both.

```json
{
  "siteId": "site-id",
  "name": "lab-slurm",
  "type": "slurm",
  "gpuStackOwner": "provisioning",
  "slurm": {
    "clusterName": "lab",
    "apiVersion": "v0.0.42",
    "stateSaveLocation": "",
    "nodeAssignments": [
      { "serverId": "server-a", "controller": true, "compute": false, "login": false },
      { "serverId": "server-b", "controller": false, "compute": true, "login": false },
      { "serverId": "server-c", "controller": false, "compute": true, "login": false },
      { "serverId": "server-d", "controller": false, "compute": false, "login": true }
    ],
    "workloadStorage": {
      "mode": "self-hosted",
      "type": "nfs",
      "mountPath": "/shared",
      "nfs": { "url": "10.0.0.9:/export/data", "mountOptions": "" }
    }
  },
  "machinePreparation": {
    "mode": "provision_os",
    "settings": { "imageId": "slurm-os-image-id" }
  }
}
```

At least one Server must run `slurmctld` and at least one must run `slurmd`; a Server with no
role (`controller`, `compute`, or `login`) is rejected, as is a Server assigned more than
once. A `login` node is a submission/client host that runs no cluster daemon; it may host the
shared-storage NFS exports and a login-only Server is valid. `gpuStackOwner` defaults to
`provisioning` when omitted (Slurm has no in-platform GPU operator). In the `slurm` object
every field except `nodeAssignments` is optional: `clusterName` defaults to a sanitized
platform name; `apiVersion` pins the `slurmrestd` endpoint version recorded in the credential;
`stateSaveLocation` is an optional override of the `slurmctld` state directory. A highly
available deployment (more than one controller) needs a shared state directory, and Swallow
provisions it automatically — it selects a state server (the login node when one is assigned,
otherwise an off-controller node), exports it over NFS, and mounts it on every controller
before `slurmctld` starts — so `stateSaveLocation` is not required for HA; when supplied it
overrides the directory path. The first controller in `nodeAssignments` is the primary: it
mints the shared MUNGE key, hosts `slurmrestd`, and produces the reader credential.

`workloadStorage` is optional and configures a shared filesystem for user/job data (distinct
from controller state), mounted on every node at `mountPath` (a non-overlapping path, never
`/home`). Omit it for no shared filesystem. `type` is `nfs`. `mode` is `self-hosted` (Swallow
exports NFS from the login node, so it requires one) or `external` (mount the operator's
`nfs.url`, `host:/path`, with optional `nfs.mountOptions`). Swallow provides the mount, not
cluster identity: consistent workload-user UID/GID across nodes is the operator's
responsibility.

The same target claim, lock, membership, and Site rules as Kubernetes apply. Before any Platform
or Workflow is created, a Slurm deploy also reads the current minimum resource requirement once
and validates every controller, compute, and login Server against its observed CPU, memory, and
storage. Equality is eligible; unknown or zero hardware fails an enabled positive threshold. Any
shortfall returns the standard validation error naming the Server, actual resources, and minimum,
with no deployment side effects. Failure to read the requirement fails closed. Kubernetes
deployments do not apply this policy. Success is the
same `202 Accepted` shape. When the target image includes `slurm-smd-slurmrestd`, Swallow
records a `slurm` platform Integration pointing at `slurmrestd` on success and begins
membership reads; if the image omits it, the cluster still deploys successfully but the
platform has no integration and no members until the image includes it (unlike Kubernetes,
whose credential is required). A Slurm platform can be uninstalled like Kubernetes (see
[Uninstall A Deployed Platform](#uninstall-a-deployed-platform)); it starts an `uninstall-slurm`
Operation that removes the Slurm configuration and daemons while keeping the host OS and
image-supplied packages, and can optionally release the member servers.

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

When `releaseServers` is true, Swallow releases each member server directly instead of
uninstalling the platform software first: releasing wipes the operating system, so the
software removal would be redundant. The Operation runs a `release-os` step per member (in
parallel) and then an internal finalize step that clears the Platform projections once every
release succeeds. `releaseOptions` mirrors the standalone Release action (disk erase and
static-IP unbinding) and is ignored when `releaseServers` is false. Releasing wipes the
operating system, so Swallow does not restore host exporters for released servers. This
shortcut applies only to a whole-platform uninstall. A release requires the durable
orchestration topology; if it is unavailable the request is rejected. The accepted response is
unchanged:

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

When servers are kept, the release-owned playbook stops k0s services, runs `k0s reset`,
removes Swallow-created k0s state, units, join token, installer, and binary, and reloads
systemd. It preserves the OS, user data, `conntrack`, unrelated packages, and power state, and
does not reboot. When servers are released, this uninstall playbook is skipped entirely.

On success Swallow clears membership, deletes only a credential Integration proven to be
Swallow-owned, and clears the Platform integration/sync projection. If Kubernetes owned
exporters, Swallow separately queues `install-exporters` for targets that remain present,
deployed, and unlocked. That Operation has its own failure lifecycle and does not roll back
the uninstall.

## List, Get, Update, And Delete

`GET /api/v1/platforms/` returns a plain array, optionally filtered by `siteId`.
`GET /api/v1/platforms/{platformId}` returns one. Lifecycle history is fetched in one batch
for list requests; the API does not fan out one Operation query per Platform.

`PATCH /api/v1/platforms/{platformId}` updates `name`, `gpuStackOwner`, or `exporterOwner`;
omitted fields do not change. It does not accept `integrationId`: a Platform's credential
Integration is set only by its deployment, and repointing it at an operator-owned Integration
would be a register-existing back door, which
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md) removes.

`DELETE /api/v1/platforms/{platformId}` returns `{"success":true}` on success. It first
cancels the Platform's in-flight durable Operations so their resource leases are released
and the member servers are freed, then clears membership and a Swallow-owned credential
Integration, and finally removes the Platform record. Cancellation only stops durable work
and frees leases; Delete still never runs Ansible, changes hosts, waits for Uninstall, or
restores exporters, and it remains available while targets are locked because it does not
mutate them. If cancellation cannot be performed the Delete is refused and the record is
kept, so a Platform is never removed while its Operations keep holding its servers. A
finished Operation observed after cancellation is left as-is; retrying a finished
deploy/uninstall Operation whose Platform no longer exists is refused.

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

## Read Slurm Cluster State

`GET /api/v1/platforms/{platformId}/slurm` reads a Slurm platform's live cluster state on
demand from `slurmrestd`. It is Slurm-native and deliberately separate from the generic
member list (`GET /api/v1/servers?platformId=`, the membership axis, which carries only
`slurmd` scheduler nodes) and from the `deployment` intent projection. It performs no writes
and does not touch the membership axis or sync counters. It carries no monitoring health;
node/controller state here is Slurm scheduler/RPC state only.

```json
{
  "controllers": [
    { "hostname": "control-1", "primary": true, "status": "up" },
    { "hostname": "control-2", "primary": false, "status": "up" }
  ],
  "partitions": [
    { "name": "main", "state": "up", "nodeSpec": "compute[1-4]", "totalNodes": 4 }
  ],
  "nodes": [
    {
      "name": "compute-1",
      "state": "idle",
      "cpus": 8,
      "realMemoryMiB": 16000,
      "gres": "gpu:8",
      "partitions": ["main"],
      "address": "192.168.100.10"
    }
  ]
}
```

`controllers` lists each `slurmctld` in SlurmctldHost failover order from `slurmrestd`
`ping`; `primary` marks the active controller and `status` is `up | down | unknown` (RPC
liveness). `partitions` and `nodes` come from `slurmrestd` `partitions` and `nodes`; node
`state` is the collapsed Slurm scheduler state (`idle | allocated | mixed | down | drain |
...`). Collections are always arrays.

This endpoint is Slurm-only: a Kubernetes platform, or a Slurm platform whose `slurmrestd`
integration is not recorded yet (`slurm-smd-slurmrestd` absent from the image, so
`integrationId` is `null`), returns `validation_error`, and the dashboard degrades to the
deployment intent plus membership. `slurmrestd` transport/auth failures return
`provider_unavailable`.

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
