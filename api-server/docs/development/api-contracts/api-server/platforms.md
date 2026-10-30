# Platforms

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

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
- [Kubernetes Topology](../../../../../docs/development/glossaries/terms/kubernetes-topology.md)
- [Minimum Resource Requirement](../../../../../docs/development/glossaries/terms/minimum-resource-requirement.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)
- [Monitoring Link](../../../../../docs/development/glossaries/terms/monitoring-link.md)
- [GPU Operator](../../../../../docs/development/glossaries/terms/gpu-operator.md)
- [GPU Allocation Mode](../../../../../docs/development/glossaries/terms/gpu-allocation-mode.md)
- [GPU Operator State](../../../../../docs/development/glossaries/terms/gpu-operator-state.md)
- [Omnistat](../../../../../docs/development/glossaries/terms/omnistat.md)
- [Device Metrics Exporter](../../../../../docs/development/glossaries/terms/device-metrics-exporter.md)
- [Managed Software](../../../../../docs/development/glossaries/terms/managed-software.md)

## Endpoints

```text
GET    /api/v1/platforms/
GET    /api/v1/platforms/deployment-requirements/slurm
PUT    /api/v1/platforms/deployment-requirements/slurm
GET    /api/v1/platforms/gpu-operator/releases
POST   /api/v1/platforms/deploy
GET    /api/v1/platforms/{platformId}
PATCH  /api/v1/platforms/{platformId}
DELETE /api/v1/platforms/{platformId}
POST   /api/v1/platforms/{platformId}/uninstall
PUT    /api/v1/platforms/{platformId}/gpu-operator
POST   /api/v1/platforms/{platformId}/sync
POST   /api/v1/platforms/sync
GET    /api/v1/platforms/{platformId}/slurm
```

There is **no** `POST /api/v1/platforms/`. A Platform is created only by
`POST /api/v1/platforms/deploy`; registering an existing platform was removed in
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md). The
Kubernetes cluster explorer routes under `/api/v1/platforms/{platformId}/kubernetes/...` are
specified in [platforms-kubernetes.md](platforms-kubernetes.md).

All endpoints require the `admin` role according to [conventions](conventions.md). The wire shape
(paths, parameters, schemas, status codes) is specified in
[`../openapi/areas/platforms.yaml`](../openapi/areas/platforms.yaml); this document keeps the
behavior.

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
  "gpuStackOwner": "gpu-operator",
  "gpuOperator": {
    "enabled": true,
    "version": "v1.5.1",
    "allocationMode": "device-plugin",
    "draDriverVersion": null,
    "resourceNaming": "single",
    "nodeLabeller": true,
    "metricsExporter": false,
    "state": "ready",
    "lastWorkflowId": "workflow-id",
    "updatedAt": "2026-10-20T00:00:00Z"
  },
  "exporterOwner": "ansible",
  "monitoringLinks": [
    { "name": "Omnistat Grafana", "url": "https://omnistat.example.internal/grafana" }
  ],
  "omnistat": null,
  "deviceMetricsExporter": null,
  "containerRuntime": null,
  "gpuDriver": null,
  "gpuGresType": null,
  "ociContainers": false,
  "pmix": false,
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
`gpu-operator`. `exporterOwner` is `ansible` (default) or `k8s`.

`gpuOperator` is the Swallow-managed GPU Operator of a Kubernetes Platform (see
[Manage The GPU Operator](#manage-the-gpu-operator)). It is `null` when Swallow has never managed
one on the Platform. `enabled` is the latest requested intent; `state` is exactly
`installing | ready | failed | uninstalling | absent`; `lastWorkflowId` is the Workflow applying
or last applying it. `draDriverVersion` is `null` unless `allocationMode` is `dra`.

`monitoringLinks` lists the Platform's **Monitoring Links**: operator-registered links to an
external monitoring UI that belongs to the runtime itself — for example the Grafana that Omnistat
brings for a Slurm Platform. It is always an array, empty by default. Each link has a `name`
(the label an operator clicks) and an absolute `http`/`https` `url`. Monitoring Links are
swallow-owned display data: swallow never connects to, authenticates against, or checks the
URL, and they are not a metrics Integration — swallow's own usage charts keep reading the Site
metrics backend (see [server-metrics.md](server-metrics.md)). They are set with `PATCH`.
Swallow itself replaces (or adds) only the link named `Omnistat Grafana` when it installs
Omnistat, or `Device Metrics Exporter Grafana` when it installs the Device Metrics Exporter, and
removes only that link when the Platform is uninstalled (see [Omnistat](#omnistat) and
[Device Metrics Exporter](#device-metrics-exporter)).

`omnistat` is the Swallow-managed Omnistat of a Slurm Platform (see [Omnistat](#omnistat)). It is
`null` when Swallow never installed Omnistat on the Platform (always for Kubernetes).

`deviceMetricsExporter` is the Swallow-managed Device Metrics Exporter of a Slurm Platform (see
[Device Metrics Exporter](#device-metrics-exporter)). It is `null` when Swallow never installed it
on the Platform (always for Kubernetes, where the GPU Operator's `metricsExporter` runs it).

`containerRuntime` is the container runtime a Slurm deploy installed on its compute and login
nodes, `docker-ce` or `podman` (see [Container Runtime](#container-runtime)). It is `null` when
the feature was off (always for Kubernetes).

`gpuDriver` is the operator's amdgpu driver intent, `{ "moduleOptions": { "<name>": "<value>" } }`
(see [GPU driver options](#gpu-driver-options)); `null` when the deploy requested none. Options a
topology rule adds on a node are not listed here.

`gpuGresType` is the GRES type name a Slurm deploy gave every GPU node (see
[GPU GRES](#gpu-gres)); `null` when the type is derived per node from the GPU architecture
(always for Kubernetes).

`ociContainers` and `pmix` are whether a Slurm deploy set up native OCI containers (see
[Native OCI Containers](#native-oci-containers)) and the host MPI stack (see [MPI (PMIx)](#mpi-pmix));
both are `false` when the deploy did not request them (always for Kubernetes).

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
`runWorkloads`. For a Slurm platform the projection is derived from the recorded daemons:
`topology` is `standalone` with one controller and `high-availability` with more, a controller is a
`control-plane` assignment (with `runWorkloads=true` when it also runs `slurmd`), and a compute-only
Server is a `worker`. It also carries `loginServerIds` (present only when the
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

This body deploys `type: "kubernetes"`. `siteId`, `name`, `k0sVersion`, and `roleAssignments` are
required (`400 validation_error` otherwise); `podCidr` and `serviceCidr` default to `10.244.0.0/16`
and `10.96.0.0/12`. An unknown Site is `404 not_found`. The target Servers must exist (an unknown
one is `400 validation_error`) and belong to one Site. They must also be unlocked in a live
provider read. The complete lock preflight
finishes before Swallow creates either the Platform or Operation; one locked target rejects
the batch with `409 conflict`.

Every deploy, with `existing_os` or `provision_os`, first requires the installation's
Deployment Key, because Swallow logs in to the targets with it (see [ssh-keys.md](ssh-keys.md)).
Without one the request is rejected with `409 conflict` before any validation (other than an
unparsable body or a missing `siteId`, which are `400 validation_error` first), Platform, or
Operation is created; the message names the installation command that creates it
(`swallow-api deployment-key ensure`).

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

`network` carries the OS deployment network intent of [provisioning.md](provisioning.md)
(`mode` `automatic`, `dhcp`, or `static`) for the targets that are provisioned. The object also
accepts the rest of that OS Deployment intent for the provisioned targets: `templateId`, write-only
`userData` (`mode` `inherit`, `replace`, or `omit`, with `value`), and `network.subnetId`,
`network.defaultGateway`, and per-target `network.assignments` (`serverId`, `interfaceId`,
`subnetId`, `ipAddress`). `settings` takes only `imageId` and `ephemeral`; the provisioning
contract's `settings.deployTarget` is not read by a Platform deploy.

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

### Time sync check

A Platform deployment checks time; it does not set it
([decision 065](../../../../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md)).
Time is configured when an OS is provisioned: in `provision_os` mode each Server the Operation
provisions gets its own `configure-ntp-<serverId>` Step after `provision-<serverId>`, exactly as
in an OS deployment ([provisioning.md](provisioning.md#durable-os-operations)), so such a deploy
is refused with `409 conflict` when the installation has no NTP server. Servers that are reused
as already deployed, and every target in `existing_os` mode, are not reconfigured.

Each platform checks its own nodes' clocks: the install playbook (`install-platform`) begins, in
both modes, by examining every target before it installs anything (before k0s is downloaded, and
before Slurm's preflight and MUNGE). The check changes nothing on the hosts: on each node it reads
whether the clock is synchronized (`timedatectl` `NTPSynchronized`) and, when the installation has
an NTP server, measures the node's offset from it with an SNTP query (limit 1 second). There is no
separate time-check Step. The optional deploy field `timeSyncCheck` decides what a problem does:

```json
{ "timeSyncCheck": "enforce" }
```

| Value | A node with an unsynchronized clock, an NTP server that does not answer, or a larger offset |
| --- | --- |
| `enforce` (default when omitted) | fails `install-platform` with `ansible_failed` before anything is installed on any node; the run output names each node with `[time_not_synchronized]` |
| `warn` | is reported with `[time_sync_warning]` in the run output, and the installation continues |

Any other value is `400 validation_error`. This applies to Kubernetes and Slurm alike.

A role assignment uses `control-plane | worker`. `runWorkloads` is optional,
defaults to `false`, and is valid only on a `control-plane` assignment; a `worker` always
runs workloads.

A target is rejected while another Platform claims it: its Server's `platformClaim`
([servers-list.md](servers-list.md#platform-claim),
[decision 067](../../../../../docs/decisions/067-platform-claim-on-server.md)) names that
Platform, and the `400 validation_error` message names it too. When the deployment is accepted,
Swallow claims every target of its `targetServerIds` for the new Platform with a conditional write
per Server, so of two deployments accepted at the same time only one can take a Server; the other
is refused the same way and leaves no Platform record or partial claim behind. A refused launch
also removes the new Platform and its claims. The claim stays while the Platform is
`deploying`, `deploy_failed`, `active`, `uninstalling`, or `uninstall_failed`, so a partial
failed deployment remains protected even before membership can be observed; a successful
Uninstall (with or without `deleteRecord`) and Delete release it. Platforms deployed before
claims existed get their claims from the latest deployment Operation's `targetServerIds` when the
API starts. A claimed Server cannot be released directly
([provisioning.md](provisioning.md#durable-os-operations)) or deleted
([server-detail-actions.md](server-detail-actions.md)). A target carrying any observed
Platform membership is also rejected. The current Server projection has one membership axis; allowing
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
`apiVip` is required only for high availability; standalone and non-HA multi-node
deployments omit `apiVip` and `apiVipPrefix` and use the initial control-plane Server
address as the API endpoint. An HA virtual IP may not equal a selected Server address.

For HA, Swallow reads from the provisioner the IPv4 subnet of each control-plane Server's
boot interface (or, for a Server the deploy provisions, the subnet its network assignment
names). The control-plane Servers must share one subnet, `apiVip` must lie inside it, and
`apiVipPrefix` must equal its prefix; an omitted `apiVipPrefix` takes it. Each violation is a
`validation_error` before anything is created. A different prefix is refused because a longer
one makes the VIP holder reach part of the subnet from the VIP, which etcd rejects as a peer.
When the provisioner reports no subnet, `apiVipPrefix` is used as given (default `24`) and the
deploy fails early on a control-plane host whose own subnet it does not match.

A Kubernetes deploy may install the AMD GPU Operator as part of the same Operation with an
optional `gpuOperator` object (the settings of [Manage The GPU Operator](#manage-the-gpu-operator)):

```json
{
  "gpuOperator": {
    "enabled": true,
    "version": "v1.5.1",
    "allocationMode": "device-plugin",
    "resourceNaming": "single",
    "nodeLabeller": true,
    "metricsExporter": false
  }
}
```

Omitting it, or `enabled=false`, installs no operator. `gpuStackOwner` is optional for
Kubernetes: it defaults to `gpu-operator` when `gpuOperator.enabled` is true and to
`provisioning` otherwise; `gpuStackOwner=provisioning` together with an enabled `gpuOperator` is
a `validation_error`. A client that sends `gpuStackOwner=gpu-operator` without `gpuOperator`
records an operator managed outside Swallow, and Swallow installs nothing. The `gpuOperator`
validation runs before any Platform or Operation is created. The Operation adds a
`configure-gpu-operator` Job after `validate-platform`; the deployment succeeds only when it
does.

Success is `202 Accepted` after both records are persisted:

```json
{
  "platformId": "platform-id",
  "clusterId": "platform-id",
  "operationId": "operation-id"
}
```

`operationId` names the Workflow; `clusterId` is the deprecated alias of `platformId` (see
[Compatibility Notes](#compatibility-notes)). Progress is read through [operations](operations.md). On success Swallow creates and marks
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
    },
    "ociContainers": false,
    "pmix": false
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
`nfs.url`, `host:/path`, with optional `nfs.mountOptions`). An omitted `type` is `nfs` and an
omitted `mountPath` is `/shared`. Swallow provides the mount, not
cluster identity: consistent workload-user UID/GID across nodes is the operator's
responsibility.

`ociContainers` (default `false`) sets up native OCI containers (see
[Native OCI Containers](#native-oci-containers)); `pmix` (default `false`) installs the host MPI
stack (see [MPI (PMIx)](#mpi-pmix)).

### Node sharing, memory, and job hooks

Jobs share a compute node: the `main` partition is `OverSubscribe=NO` (separate CPUs, memory, and
GPUs per job, never oversubscribed CPUs) unless [Omnistat](#omnistat) needs whole nodes. Each node's
`RealMemory` is what `slurmd -C` detects minus the memory kept for the operating system (5%, at
least 2 GiB and at most 16 GiB, never more than half), and `DefMemPerCPU` is the smallest per-CPU
share of schedulable memory over the compute nodes, so a job that names no memory gets memory in
proportion to its CPUs instead of the whole node. The deployment proves it: two one-CPU jobs without
`--mem` must run on one node at the same time, otherwise the Task fails with
`slurm_shared_node_failed`.

`slurm.conf` names exactly one Prolog and one Epilog, Swallow's dispatchers
(`/etc/slurm/swallow-prolog`, `/etc/slurm/swallow-epilog`, `PrologFlags=Alloc,Contain`). On each
compute node they run every executable in `/etc/slurm/prolog.d` and `/etc/slurm/epilog.d` in name
order with the job's environment and a 30-second limit each; a hook that fails or times out is
logged (syslog tag `swallow-prolog`/`swallow-epilog`) and never fails the job or drains the node.
Swallow installs only its own hooks there (`10-omnistat`, `20-device-metrics-exporter`) and removes
the ones a redeploy no longer enables; other executables an operator places there run too. A rerun
of a converged deployment reports no change.

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

### GPU GRES

Every Slurm deployment schedules AMD GPUs
([decision 063](../../../../../docs/decisions/063-omnistat-slurm-observability.md)). Swallow selects
the compute Servers whose GPU Inventory lists AMD `compute` GPUs; on each it loads the image's
`amdgpu` driver after boot (`swallow-amdgpu-load.service` with the
[GPU driver options](#gpu-driver-options); `slurmd` requires it), maps every GPU from the KFD
topology, and configures `GresTypes=gpu`, `Gres=gpu:<type>:<count>`, and a `gres.conf` with
explicit render nodes and `Flags=amd_gpu_env,explicit`. The type is derived per node from the GPU
architecture (for example `gpu:gfx942:8`) unless the deploy names one for every GPU node:

```json
"slurm": { "gpuGresType": "mi308x_vf", "nodeAssignments": [ /* ... */ ] }
```

`gpuGresType` starts with a letter and contains only letters, digits, or underscores, at most 64
characters; anything else is a `validation_error`. Jobs see GPUs only when they request them (`--gres=gpu:N`, which
matches any type, or `--gres=gpu:<type>:N`), receive `ROCR_VISIBLE_DEVICES`, and are confined to
their GPUs by the device cgroup; the GPU device nodes are opened to every job user because that
cgroup is the access boundary. A selected Server whose KFD topology shows no GPU fails the
deployment (`slurm_gpu_missing`).

GPUs must be usable, not merely listed: on every GPU node the HIP probe
(`/usr/local/libexec/swallow/hip-probe`) calls `hipGetDeviceCount` (which must equal the KFD GPU
count), then `hipSetDevice`, `hipMalloc`, `hipMemset`, `hipDeviceSynchronize`, and `hipFree` on
every GPU, three times, each in its own process so a crash at exit fails it. It runs directly on
the node and again through Slurm: a job without a GPU request sees no GPU, a `--gres=gpu:1` job
sees exactly one as device `0`, and a job requesting every GPU of the node passes the probe. A
failure drains the node (`reason=swallow: GPU probe failed`) and fails the deployment with
`gpu_probe_failed` (`hip_runtime_missing` when the image has no HIP runtime). The live
[Slurm cluster read](#read-slurm-cluster-state) reports each node's `gres`.

### GPU driver options

A deploy of either platform type may set amdgpu module options for its GPU nodes:

```json
"gpuDriver": { "moduleOptions": { "pcie_p2p": "0" } }
```

A name is lower-case letters, digits, and underscores (at most 64 characters); a value is letters,
digits, and `_.,:-` (at most 128 characters); at most 16 options. Anything else is a `validation_error`. Swallow also derives options from what a
node's KFD topology shows, never from a GPU model or a virtualization mode. Today's only rule:
when a GPU node reaches the same peer through both an XGMI `io_link` and a PCIe `p2p_link`, the
ROCm thunk overflows its peer array and HIP processes corrupt the heap at exit, so the rule adds
`pcie_p2p=0`. A rule option is kept once applied (the condition disappears with it); an option the
operator names always wins over a rule. The options apply to the Slurm GPU nodes and to the GPU
Operator's host driver step on Kubernetes.

Every name is first checked against the installed driver's own list (`modinfo -p amdgpu`); a name it
does not have fails the Task with `amdgpu_option_unknown`, naming the closest real option, before
anything is written or loaded. The options are then written to
`/etc/modprobe.d/swallow-amdgpu.conf`, and a driver that is not loaded yet loads with them. A rule is
only known once the driver is loaded, so a driver running with other parameters loads again: the
units that require it (`slurmd`, Omnistat) stop, and when nothing else holds the driver it is
unloaded and loaded in place, which keeps the operating system of a RAM (ephemeral) deployment.
When the driver is still held, a disk-deployed node reboots once to apply the options; a node whose
root filesystem is in memory never reboots, because that would erase its operating system, and the
Task fails with `amdgpu_reload_blocked`, naming the processes that hold the GPUs. An operator who
names the options in `gpuDriver.moduleOptions` gets them on the first load, with no second load at
all. Every readable option is then verified against `/sys/module/amdgpu/parameters`
(`amdgpu_option_not_applied` otherwise); an option the driver has but does not expose for reading
is reported as unverifiable.

| Code | Meaning |
| --- | --- |
| `amdgpu_option_unknown` | an option name the installed driver does not have |
| `amdgpu_reload_blocked` | the driver must load again but is held, and the root filesystem is in memory |
| `amdgpu_option_not_applied` | a known option did not take effect, for example because the initramfs loaded the driver |
| `amdgpu_driver_missing` | the driver did not load or exposed no `/dev/kfd` |

## Container Runtime

Slurm runs no containers itself, but its workloads (AI ones especially) usually do, so a Slurm
deploy may install a container runtime on every compute and login node
([decision 063](../../../../../docs/decisions/063-omnistat-slurm-observability.md)):

```json
"containerRuntime": { "kind": "podman" }
```

`kind` is `docker-ce` or `podman`. Omitting `containerRuntime`, or an empty `kind`, installs no
runtime; another kind, or `containerRuntime` on a Kubernetes deploy, is a `validation_error`. The
runtime is Managed Software, not a Platform component: after the Slurm install the Workflow
composes a `configure-docker-ce` or `configure-podman` Job (see
[software](software.md#composition-into-platform-deployments)) onto every compute and login node
where that runtime is not already `installed` (Docker CE with its Engine API off). Each Server's
install state is its Software Assignment, and the Platform reports the choice as
`containerRuntime`. A target where the other, mutually exclusive runtime is installed returns
`409 conflict` before anything is created. Uninstalling the Platform leaves the runtime's Software
Assignments until they are uninstalled through the software API.

The runtime is independent of [native OCI containers](#native-oci-containers) (`slurm.ociContainers`):
a deploy may request either, both, or neither, and the two do not conflict (Docker CE brings its own
`runc`; native OCI uses `/usr/bin/crun`). Both only give the cluster a capability; the deployment does
not decide whether or how jobs use containers, which is up to the cluster's users. For reference,
the Docker CE Swallow installs is the system (rootful) Docker Engine with its default runtime, and
Swallow configures no Slurm integration for it (Slurm's rootless Docker with `scrun` is not set
up), so a container that `docker run` starts is created by that daemon rather than by Slurm.

## Omnistat

A Slurm deploy may install [Omnistat](../../../../../docs/development/glossaries/terms/omnistat.md),
AMD's Slurm-aware GPU monitoring, in the same Workflow
([decision 063](../../../../../docs/decisions/063-omnistat-slurm-observability.md)):

```json
{
  "siteId": "site-id",
  "name": "lab-slurm",
  "type": "slurm",
  "omnistat": { "enabled": true, "installMethod": "container" },
  "slurm": { "nodeAssignments": [ /* ... */ ] },
  "machinePreparation": { "mode": "provision_os", "settings": { "imageId": "slurm-os-image-id" } }
}
```

Omitting `omnistat`, or `enabled: false`, installs nothing. `omnistat` on a Kubernetes deploy is a
`validation_error`. `installMethod` is how the Prometheus and Grafana Omnistat uses are installed,
`container` (default) or `native` (see
[software](software.md#prometheus-and-grafana-spec)); another value is a `validation_error`. The
primary controller (the first controller in `nodeAssignments`) becomes the observability Server.
After the Slurm install the Workflow composes Managed Software Jobs onto it (see
[software](software.md#composition-into-platform-deployments)): with the `container` method a
container runtime Job first — the [container runtime](#container-runtime) the deploy chose, or
else the runtime already assigned to the observability Server, or Docker CE — then
`configure-prometheus` and `configure-grafana`, and finally a `configure-omnistat` Job that
installs Omnistat `1.15.0` on every compute node (exporter on port `8001`, reachable only from the
node itself and the observability Server). The Slurm side is part of the Slurm install: a
job hook of the Prolog/Epilog dispatcher records each job for Omnistat, `PrologFlags=Alloc,Contain`, and the `main`
partition is `OverSubscribe=EXCLUSIVE`, because Omnistat attributes a whole node to one job. The
deployment succeeds only when every composed Job does. Omnistat and the
[Device Metrics Exporter](#device-metrics-exporter) are mutually exclusive. A composed software install that the
software rules refuse (for example Docker CE requested where Podman is installed, the two being
mutually exclusive) returns `409 conflict` before anything is created.

The Platform then reports:

```json
"omnistat": {
  "enabled": true,
  "version": "1.15.0",
  "installMethod": "container",
  "observabilityServerId": "server-a",
  "state": "ready",
  "lastWorkflowId": "operation-id",
  "updatedAt": "2026-10-21T00:00:00Z"
}
```

`state` is the Omnistat State, exactly `installing | ready | failed | absent`: `installing` from
acceptance until the Omnistat Task finishes, `ready` when it succeeded (every exporter is scraped
and the dashboards are provisioned), `failed` when it failed or never ran because an earlier Job
failed, and `absent` after the Platform was uninstalled. Only `lastWorkflowId` may move it. On
success Swallow also sets the Monitoring Link `Omnistat Grafana` to the observability Server's
Grafana (`http://<primary address>:3000`, read-only, no login). Omnistat's Prometheus listens only
on the observability Server's loopback address (`listen: loopback`, see
[software](software.md#prometheus-and-grafana-spec)), so only that Grafana reads it, and Swallow
never queries it. `omnistat-query` on a compute node reads it through that Grafana's read-only
datasource proxy (`http://<primary address>:3000/api/datasources/proxy/uid/prometheus`), which
serves the same data the anonymous dashboards already show. Omnistat 1.15.0's report needs GPU
utilization, so on GPUs that do not report it (for example SR-IOV virtual functions, which expose
only memory) `omnistat-query` cannot build one; the Grafana job dashboards still show memory per
job. Uninstalling the Platform removes the exporter and the Slurm side, marks the state
`absent`, and removes the link; the container runtime, Prometheus, and Grafana Software
Assignments stay until they are uninstalled through the software API.

## Device Metrics Exporter

A Slurm deploy may install AMD's
[Device Metrics Exporter](../../../../../docs/development/glossaries/terms/device-metrics-exporter.md)
with Slurm job attribution, in the same Workflow
([decision 066](../../../../../docs/decisions/066-slurm-job-hooks-and-device-metrics-exporter.md)):

```json
{
  "siteId": "site-id",
  "name": "lab-slurm",
  "type": "slurm",
  "deviceMetricsExporter": { "enabled": true, "installMethod": "container" },
  "slurm": { "nodeAssignments": [ /* ... */ ] },
  "machinePreparation": { "mode": "provision_os", "settings": { "imageId": "slurm-os-image-id" } }
}
```

Omitting `deviceMetricsExporter`, or `enabled: false`, installs nothing. It is a `validation_error`
on a Kubernetes deploy (the GPU Operator's `metricsExporter` runs it there), together with an
enabled `omnistat`, when no compute Server lists AMD `compute` GPUs in its GPU Inventory, or with an
`installMethod` other than `container` (default) or `native`. Like Omnistat, the primary controller
becomes the observability Server and the Workflow composes, after the Slurm install, the container
runtime (container method), `configure-prometheus` (`listen: loopback`, job
`device-metrics-exporter`, the GPU compute nodes on port `5001`), and `configure-grafana`
(dashboards `device-metrics-exporter`), and finally the Managed Software
[`device-metrics-exporter`](software.md#device-metrics-exporter) on every GPU compute node, as a
`configure-device-metrics-exporter` Job ([decision 068](../../../../../docs/decisions/068-device-metrics-exporter-managed-software.md)).
Each GPU compute node gets an ordinary Software Assignment, `installed` by the Job's record Task.
That Job installs AMD's pinned native package (`amdgpu-exporter 1.5.3~24.04`, SHA-256 verified; Ubuntu
24.04 only, `device_metrics_exporter_unsupported_os` otherwise) with Slurm job attribution on:
`gpuagent` starts after the amdgpu loader, and the exporter serves Prometheus metrics on the node's
address at port `5001` (AMD's default `5000` belongs to the host GPU exporter Swallow scrapes).
Hardware profiler counters stay off. While the Platform claims its Servers, the standalone software
install and uninstall refuse them, so the exporter there stays the Platform's. The Slurm side is
part of the Slurm install: the
`20-device-metrics-exporter` job hook writes `/run/exporter/<GRES index>` for every GPU a job is
allocated and removes it at the job's end only while it still names that job, and jobs keep
sharing nodes (`OverSubscribe=NO`). An exporter that serves no GPU health metrics fails the Task
(`device_metrics_exporter_no_metrics`).

The Platform then reports:

```json
"deviceMetricsExporter": {
  "enabled": true,
  "version": "1.5.3",
  "installMethod": "container",
  "observabilityServerId": "server-a",
  "state": "ready",
  "gpuIdentity": "collapsed",
  "lastWorkflowId": "operation-id",
  "updatedAt": "2026-10-29T00:00:00Z"
}
```

`state` is the Device Metrics Exporter State, exactly `installing | ready | failed | absent`, with
the same meaning and fence (`lastWorkflowId`) as the Omnistat State. On success Swallow sets the
Monitoring Link `Device Metrics Exporter Grafana` (`http://<primary address>:3000`, read-only, no
login) and records `gpuIdentity`:

| Value | Meaning |
| --- | --- |
| `distinct` | every GPU node's exporter reports one distinct `gpu_id` per GPU its KFD topology lists, so a job's metrics carry its own GPUs |
| `collapsed` | on at least one node the exporter reports fewer distinct `gpu_id`s than GPUs (for example MI308X SR-IOV virtual functions, which all report `gpu_id` `0`); the metrics endpoint works, but per-GPU job attribution there is wrong: a job on GPU 0 is attributed to every GPU, a job on another GPU to none |

`gpuIdentity` is `null` until an install succeeded. A collapsed identity does not fail the
deployment; clients must show it rather than present per-job GPU panels as reliable. Swallow never
queries the exporter's Prometheus. Uninstalling the Platform removes the package, its
configuration, and the job hook from every target, marks the state `absent`, removes the link, and
marks the Servers' `device-metrics-exporter` Software Assignments `absent`; the container runtime,
Prometheus, and Grafana Software Assignments stay until they are uninstalled through the software
API.

## Native OCI Containers

`"slurm": { "ociContainers": true }` sets up Slurm's native OCI container support
(`srun --container=<bundle>`) on the cluster
([decision 066](../../../../../docs/decisions/066-slurm-job-hooks-and-device-metrics-exporter.md)):
`crun` on every compute node and an `oci.conf` the controllers distribute with the configless
configuration. slurmstepd runs each container rootless as the job user (`/usr/bin/crun
--rootless=true`, state under `/run/user/<uid>/`), with `ContainerPath=/run/user/%U/slurm-oci` (the
same string on every node, which multi-node PMIx needs). Each step's spool, with the PMIx server
directory, appears at that same path inside the container, so a bundle must let the runtime create
it (a writable rootfs, or a tmpfs on `/run`); MPI in a container also needs the host's
`/run/munge` (PMIx authenticates ranks with MUNGE) and the host network (slurmstepd's PMIx server
listens on the loopback address). The host keeps `kernel.apparmor_restrict_unprivileged_userns=1`: user namespaces are
granted only to `/usr/bin/crun` by the distribution's path-scoped AppArmor profile, and a node that
restricts them without that profile fails the deployment (`slurm_oci_failed`). The job's device
cgroup applies inside the container: a container that binds `/dev` still opens only its allocated
GPUs. Swallow does not build, pull, or distribute OCI bundles: a bundle must exist at the same
absolute path on every node the job runs on. The deployment verifies the path with a bundle built
from the node's own filesystem (`/var/lib/swallow/oci/probe`): a container step runs as an
unprivileged user, and on every GPU node a one-GPU container sees exactly one GPU
(`slurm_oci_failed` otherwise). Each rank of a step runs in its own container, so ranks on one node
share no memory: an MPI program that places several ranks on a node selects a transport between
them, for example `OMPI_MCA_btl=self,tcp` with OpenMPI. Turning the option off removes `oci.conf` and the probe bundle; the
`crun` package stays, because Podman uses it too. Podman through `scrun` is not set up.

## MPI (PMIx)

`"slurm": { "pmix": true }` installs the distribution's OpenMPI with PMIx on every compute and login
node so `srun --mpi=pmix_v5` launches MPI programs
([decision 066](../../../../../docs/decisions/066-slurm-job-hooks-and-device-metrics-exporter.md)).
The cluster's `MpiDefault` stays `none`: jobs ask for PMIx explicitly. The deployment verifies the
launch path with an MPI probe: two ranks on one node and one rank on each of two compute nodes must
pass a barrier under one PMIx namespace, and a failing rank must fail the step (`slurm_pmix_failed`
otherwise). With `ociContainers` the probe also runs as one rank in a native OCI container on each of
two compute nodes (one rank on one node when there is a single compute node). Turning the option off
removes the probe; the OpenMPI packages stay.

## Manage The GPU Operator

Swallow installs the AMD GPU Operator into a Swallow-deployed Kubernetes Platform, either during
the deploy or later, and changes or removes it on request
([decision 062](../../../../../docs/decisions/062-swallow-managed-gpu-operator.md)). The operator
uses the GPU driver the OS image provides; Swallow does not build or install drivers. An image may
keep `amdgpu` out of the boot path, so Swallow loads the image's driver on every managed node,
now and after each boot.

### Managed Nodes

No request names nodes or Servers. The operator manages every node of the Platform that its own
Node Feature Discovery marks as an AMD GPU node: `feature.node.kubernetes.io/amd-gpu=true` for a
physical GPU or `feature.node.kubernetes.io/amd-vgpu=true` for an SR-IOV virtual function, by the
chart's PCI device rules. Each Operation re-reads that result, so a node that joins the cluster is
managed after the next change. A per-node control, if one is ever added, excludes named Servers
from that result; it never asks a client to enumerate the nodes to manage.

### Settings

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `enabled` | boolean | — (required on `PUT`) | `true` installs or changes the operator, `false` uninstalls it |
| `version` | string | the release marked `default` | a `version` from the release catalog |
| `allocationMode` | `device-plugin` \| `dra` | `device-plugin` | the GPU Allocation Mode |
| `draDriverVersion` | string | the release's `defaultDraDriverVersion` | used only when `allocationMode` is `dra`; must be one of the release's `draDriverVersions` |
| `resourceNaming` | `single` \| `mixed` | `single` | device plugin resource names: `single` always uses `amd.com/gpu`; `mixed` names partitioned GPUs per partition type |
| `nodeLabeller` | boolean | `true` | label nodes with `amd.com/gpu.*` GPU properties |
| `metricsExporter` | boolean | `false` | run AMD's Device Metrics Exporter (pod network only; Swallow does not scrape it) |

Enabling the operator requires at least one workload-capable target of the Platform's deployment
whose GPU inventory has an AMD `compute` GPU; otherwise it is a `validation_error`. This only
decides whether the feature is available; the nodes it manages are the ones described in
[Managed Nodes](#managed-nodes). `dra` requires the release's `draMinKubernetesVersion`, and every release requires its
`minKubernetesVersion`; the Kubernetes version is the deployment's `k0sVersion` without its
`+k0s` suffix. Unknown values, an unknown `version` or `draDriverVersion`, a too-old Kubernetes
version, or enabling the operator on a Slurm Platform is a `validation_error`.

### List The Supported Releases

`GET /api/v1/platforms/gpu-operator/releases` lists the operator releases Swallow ships and has
verified, newest first:

```json
{
  "items": [
    {
      "version": "v1.5.1",
      "default": true,
      "minKubernetesVersion": "1.29",
      "draMinKubernetesVersion": "1.34",
      "draDriverVersions": ["v1.0.1"],
      "defaultDraDriverVersion": "v1.0.1"
    }
  ]
}
```

### Change The GPU Operator

`PUT /api/v1/platforms/{platformId}/gpu-operator` replaces the settings and starts a
`configure-gpu-operator` Operation that applies them. The body is the settings object above.
`{ "enabled": false }` uninstalls the operator and keeps the other recorded settings, so a later
install can start from them.

```json
{
  "platformId": "platform-id",
  "workflowId": "workflow-id"
}
```

Success is `202 Accepted` once the Operation is persisted; `gpuOperator.state` becomes
`installing` (or `uninstalling`) and `gpuOperator.lastWorkflowId` names the Operation. The
Operation targets the Platform's deployment targets: it verifies SSH readiness, then runs one
ansible Task (`configure-gpu-operator`). Errors:

- `400 validation_error` for invalid settings, or a body without `enabled`.
- `404 not_found` when the Platform does not exist, or when the API process runs without GPU
  Operator management wired (no durable orchestration).
- `409 conflict` when the Platform is not a Swallow-deployed Kubernetes Platform whose lifecycle
  is `active`; when `enabled=false` and Swallow manages no operator on it (`gpuOperator` is
  `null` or `absent`); when a target is locked or held by another unfinished Operation; or when
  the installation has no Deployment Key.

### What The Operation Does

The Task fails (`ansible_failed`, retryable) with a message that starts with a bracketed code when
a precondition does not hold. The cluster-level refusals (`gpu_operator_unmanaged`,
`dra_unavailable`, `gpu_workloads_present`) are reported before the Task changes anything; the
node-level ones need Node Feature Discovery, so they are reported after the release is installed
or upgraded and before the `DeviceConfig` is applied:

| Code | Meaning |
| --- | --- |
| `gpu_operator_unmanaged` | the cluster already runs an AMD GPU Operator, a `DeviceConfig`, or a hand-applied AMD device plugin or labeller that Swallow did not install |
| `dra_unavailable` | `dra` was requested but the cluster has no `resource.k8s.io/v1` API |
| `gpu_workloads_present` | the change switches the allocation mode or uninstalls while a Pod requests an AMD GPU resource or a `gpu.amd.com` `ResourceClaim` is allocated |
| `amd_gpu_not_detected` | Node Feature Discovery marked no node as an AMD GPU node, for example because no node has a GPU the chart supports |
| `amdgpu_driver_missing` | a managed node's image has no `amdgpu` kernel module, or the driver did not load or exposed no `/dev/kfd` when Swallow loaded it |
| `gpu_operator_not_ready` | the operator did not become Ready, or a managed node did not expose its GPUs, within the timeout |

Applying the settings installs or upgrades the pinned chart with KMM and remediation disabled,
waits for its Node Feature Discovery to evaluate every node, and labels the nodes it marks as AMD
GPU nodes `swallow.dev/amd-gpu-operator=managed`; a node that carries the label but is no longer
marked loses it together with its `amd.com/gpu.*` / `beta.amd.com/gpu.*` labels. One
`DeviceConfig` named `swallow` in namespace `kube-amd-gpu` selects that label, so physical GPUs and
SR-IOV virtual functions are managed by the same `DeviceConfig`. A change of allocation mode turns
the old mode off and waits for it to disappear before the new one starts.
Before the `DeviceConfig` is applied, every managed node gets `swallow-amdgpu-load.service`, which
loads the image's `amdgpu` once SSH is up, with the k0s unit ordered after it; the Task starts it,
so the GPUs exist now and after every reboot even when the image blacklists `amdgpu` at boot.
Nodes no longer managed lose the unit and the ordering, and their loaded driver stays loaded.
For `dra` on k0s, the Task links `/var/lib/kubelet/plugins` and `/var/lib/kubelet/plugins_registry`
to the k0s kubelet directories on managed nodes and removes those exact links when DRA is turned
off or the operator is uninstalled. The Task succeeds when the `DeviceConfig` is Ready and every
managed node exposes a positive `amd.com/*` allocatable (`device-plugin`) or a `gpu.amd.com`
`ResourceSlice` with devices (`dra`). Uninstalling removes the `DeviceConfig`, the Helm release,
the node labels, the DRA links, and the amdgpu loader; the Platform stays `active`.

`gpuOperator.state` follows the Task: `installing` / `uninstalling` while it waits or runs,
`ready` / `absent` when it succeeds, and `failed` when it fails, needs attention, or is canceled.
Only the Operation named by `lastWorkflowId` moves the state. A refused or failed Task leaves its
Operation in `requires_attention`, still holding the targets: retry the Task once the cause is
fixed, or cancel the Operation before submitting other settings, which are `409 conflict` until
then. While the state is anything other
than `absent`, `PATCH` cannot set `gpuStackOwner` back to `provisioning` (`409 conflict`).

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
  },
  "cancelActiveWorkflows": false,
  "deleteRecord": false
}
```

`cancelActiveWorkflows` is described below the eligibility rules, and `deleteRecord` after
the success cleanup.

When `releaseServers` is true, Swallow releases each member server directly instead of
uninstalling the platform software first: releasing wipes the operating system, so the
software removal would be redundant. The Operation runs a `release-os` step per member (in
parallel) and then an internal finalize step that clears the Platform projections once every
release succeeds. `releaseOptions` mirrors the standalone Release action (disk erase and
static-IP unbinding) and is ignored when `releaseServers` is false. Releasing wipes the
operating system, so Swallow does not restore host exporters for released servers. This
shortcut applies only to a whole-platform uninstall. A release requires the durable
orchestration topology; if it is unavailable the request is rejected. The accepted response is
unchanged, the same shape as a deploy's:

```json
{
  "platformId": "platform-id",
  "clusterId": "platform-id",
  "operationId": "operation-id"
}
```

Eligibility requires a Platform with durable deployment provenance (`deploy-kubernetes` or
`configure-slurm`). The target set is always the complete `targetServerIds` snapshot from its latest
deployment Operation, never current membership. All targets must still exist, be present,
be unlocked, and have no overlapping active Operation. Lock is checked live before
the uninstall Operation is created. No single provisioning state is
required because a failed deployment can leave a mixed target set.

A failed deployment waiting for operator attention (`requires_attention`) still holds its
targets, as does a deployment still running. Send `"cancelActiveWorkflows": true` to have
Swallow cancel every unfinished Workflow holding a target, with the same semantics as Release and
Recover ([decision 061](../../../../../docs/decisions/061-recovery-supersedes-blocking-work.md)):
after every other check passes, Swallow requests their cancellation and creates the uninstall at
once, ignoring them in the busy check. The uninstall takes the same resource leases, so it waits in
`waiting_dependency` and starts by itself once they end; a canceled deployment first returns the
Servers it was installing to Ready and keeps finished OS deployments
([decision 060](../../../../../docs/decisions/060-cancel-and-rerun-converge.md)). A cancel refused
for a Workflow that is still running fails the request as `409 conflict` naming it. With the flag,
a Platform whose lifecycle is `deploying` may be uninstalled; without it that is a conflict. The
flag needs the durable orchestration topology, like `releaseServers`.

A running uninstall, an already successful uninstall, a missing, absent, locked, or busy target
(without `cancelActiveWorkflows`), a deployment still running (without the flag), or a registered
Platform returns `409 conflict`. The busy conflict names the Workflow holding the target, its
intent, and its status. Failed, canceled, or indeterminate uninstalls may be submitted again; the
new Operation records `retryOfOperationId` pointing to the latest uninstall.

When servers are kept, the release-owned playbook stops k0s services, runs `k0s reset`,
removes Swallow-created k0s state, units, join token, installer, and binary, and the GPU
Operator's DRA links, DRA common CDI spec, and amdgpu loader (a loaded driver stays loaded until
the next boot), and reloads systemd. It preserves the OS, user data, `conntrack`, unrelated
packages, and power state, and does not reboot. When servers are released, this uninstall
playbook is skipped entirely. Either way a successful uninstall sets `gpuOperator.state` to
`absent` when Swallow managed one. For Slurm, `uninstall-slurm` also removes the job hook
dispatchers and Swallow's hooks, Omnistat, the Device Metrics Exporter package, `oci.conf` and the
OCI probe bundle, and the MPI probe (the `crun` and OpenMPI packages stay), and a successful uninstall sets
`omnistat.state` and `deviceMetricsExporter.state` to `absent` when Swallow managed them.

On success Swallow clears membership, releases the Platform's Platform Claims on its Servers,
deletes only a credential Integration proven to be Swallow-owned, and clears the Platform
integration/sync projection. If Kubernetes owned
exporters, Swallow separately queues `install-exporters` for targets that remain present,
deployed, and unlocked. That Operation has its own failure lifecycle and does not roll back
the uninstall.

`deleteRecord: true` asks Swallow to also remove the Platform record once the uninstall
succeeds, so an operator who wants both host cleanup and record removal does not have to come
back for a separate Delete. It is recorded on the uninstall Operation when the request is
accepted and applies to both the kept-servers and the `releaseServers` path. It changes
nothing until that success cleanup: the Platform stays addressable (`uninstalling`) while the
Operation runs, and a failed, canceled, or indeterminate uninstall keeps the record
(`uninstall_failed`) for diagnosis and retry. Retrying a Step of, or rerunning, that same
Workflow keeps the choice it was accepted with; submitting the uninstall again is a new
request, which deletes the record only if it also sends `deleteRecord: true`. The removal runs
as the last part of the success cleanup
and removes the same record and projections as Delete, but it never cancels Operations,
because the only Operation still holding the targets is the uninstall that is completing.
Exporter restoration is still queued as described above. A missing or `false` value keeps the
record, which stays `uninstalled` until Delete. A request whose Operation is refused deletes
nothing.

Do not send `DELETE` while an uninstall is running to get the same result: Delete cancels the
Platform's in-flight Operations first, so the uninstall would stop instead of finishing.

## List, Get, Update, And Delete

`GET /api/v1/platforms/` returns a plain array, optionally filtered by `siteId`.
`GET /api/v1/platforms/{platformId}` returns one. Lifecycle history is fetched in one batch
for list requests; the API does not fan out one Operation query per Platform.

`PATCH /api/v1/platforms/{platformId}` updates `name`, `gpuStackOwner`, `exporterOwner`, or
`monitoringLinks`; omitted fields do not change. `PATCH` never changes `gpuOperator`; use
`PUT .../gpu-operator`.

`monitoringLinks` replaces the whole list; `[]` clears it. At most 10 links. Each `name` is
trimmed, required, at most 64 characters, and unique within the Platform (case-insensitive).
Each `url` is trimmed, at most 2048 characters, and must be an absolute `http` or `https` URL
with a host. Any violation is `validation_error` and changes nothing.

```json
{ "monitoringLinks": [{ "name": "Omnistat Grafana", "url": "https://omnistat.example.internal/grafana" }] }
```

`PATCH` does not accept `integrationId`: a Platform's credential
Integration is set only by its deployment, and repointing it at an operator-owned Integration
would be a register-existing back door, which
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md) removes.

`DELETE /api/v1/platforms/{platformId}` removes only the Swallow record; it is not a way to
finish an uninstall (use the uninstall's `deleteRecord` for that). It returns
`{"success":true}` on success. It first
cancels the Platform's in-flight durable Operations so their resource leases are released
and the member servers are freed, then clears membership, the Platform's Platform Claims, and a
Swallow-owned credential Integration, and finally removes the Platform record. Cancellation only stops durable work
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
  "clusterId": "platform-id",
  "platformName": "lab-k0s",
  "members": 7,
  "matched": 7,
  "cleared": 0,
  "unmatched": [],
  "error": null
}
```

`clusterId` is the deprecated alias of `platformId`. A failure to read the Platform's API is
reported in `error` with `200 OK`, not as an error status; only an unknown Platform is
`404 not_found`.

`POST /api/v1/platforms/sync` syncs every Platform that has a recorded credential Integration
(the others are skipped) and returns an array of these reports. One failing platform does
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
deployment intent plus membership. An unknown Platform, or an API process without the Slurm read
wired, is `not_found`. `slurmrestd` transport/auth failures return `provider_unavailable`.

## Errors

Unknown Platform, Site, or Integration returns `not_found`; an unknown deployment target Server is
`validation_error`. Duplicate names and uninstall
eligibility/target conflicts return `conflict`. Invalid resource values or deployment
topology return `validation_error`. Missing/disabled automation, missing automation
credential, or unavailable release playbook returns `provider_unavailable`. Platform API
transport/auth failures return `provider_unavailable`; provider rejection returns
`validation_error`.

## Compatibility Notes

- The aggregate was renamed from Cluster to Platform. For one release the former surface
  remains available as deprecated aliases:
  - Route alias: every `/api/v1/clusters...` path mirrors the corresponding
    `/api/v1/platforms...` path and is served by the same handler, including the Slurm read and
    the whole [Kubernetes explorer](platforms-kubernetes.md); the Slurm deployment requirement
    and GPU Operator routes have no alias. Responses to the alias carry a `Deprecation: true`
    header and a `Link: </api/v1/platforms>; rel="successor-version"` header. New clients must
    use `/api/v1/platforms`.
  - Field alias: in this contract the deploy and uninstall acceptance bodies and the membership
    reports include `clusterId` alongside the canonical `platformId`, with the same value. The
    Platform resource (identified by `id`) and the GPU Operator acceptance, added after the
    rename, carry no `clusterId`. No request body here carries a Platform reference; requests
    elsewhere that accept one accept either name, and `platformId` wins when both are sent.
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
- Since 2026-10-21 an HA `apiVipPrefix` defaults to the control-plane subnet's prefix instead
  of `24`, and a VIP or prefix that does not fit that subnet is a `validation_error`; such a
  deploy could never form its etcd cluster.
- Control-plane lease discovery remains per Integration because its naming is k0s-specific.
- `monitoringLinks` was added on 2026-10-12. It is additive: a Platform stored before the field
  existed reads as `[]`, and a `PATCH` that omits it leaves the links unchanged.
