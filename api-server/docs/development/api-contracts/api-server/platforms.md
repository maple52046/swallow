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
  "containerRuntime": null,
  "gpuDriver": null,
  "gpuGresType": null,
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
Omnistat, and removes only that link when the Platform is uninstalled (see [Omnistat](#omnistat)).

`omnistat` is the Swallow-managed Omnistat of a Slurm Platform (see [Omnistat](#omnistat)). It is
`null` when Swallow never installed Omnistat on the Platform (always for Kubernetes).

`containerRuntime` is the container runtime a Slurm deploy installed on its compute and login
nodes, `docker-ce` or `podman` (see [Container Runtime](#container-runtime)). It is `null` when
the feature was off (always for Kubernetes).

`gpuDriver` is the operator's amdgpu driver intent, `{ "moduleOptions": { "<name>": "<value>" } }`
(see [GPU driver options](#gpu-driver-options)); `null` when the deploy requested none. Options a
topology rule adds on a node are not listed here.

`gpuGresType` is the GRES type name a Slurm deploy gave every GPU node (see
[GPU GRES](#gpu-gres)); `null` when the type is derived per node from the GPU architecture
(always for Kubernetes).

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

Every deploy, with `existing_os` or `provision_os`, first requires the installation's
Deployment Key, because Swallow logs in to the targets with it (see [ssh-keys.md](ssh-keys.md)).
Without one the request is rejected with `409 conflict` before any validation, Platform, or
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
(`mode` `automatic`, `dhcp`, or `static`) for the targets that are provisioned.

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

`gpuGresType` starts with a letter and contains only letters, digits, or underscores; anything
else is a `validation_error`. Jobs see GPUs only when they request them (`--gres=gpu:N`, which
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

A name is lower-case letters, digits, and underscores; a value is letters, digits, and `_.,:-`; at
most 16 options. Anything else is a `validation_error`. Swallow also derives options from what a
node's KFD topology shows, never from a GPU model or a virtualization mode. Today's only rule:
when a GPU node reaches the same peer through both an XGMI `io_link` and a PCIe `p2p_link`, the
ROCm thunk overflows its peer array and HIP processes corrupt the heap at exit, so the rule adds
`pcie_p2p=0`. A rule option is kept once applied (the condition disappears with it); an option the
operator names always wins over a rule. The options are written to
`/etc/modprobe.d/swallow-amdgpu.conf` before the driver loads; a driver already loaded with other
parameters is never unloaded — the node reboots once to apply them — and every option is then
verified against `/sys/module/amdgpu/parameters` (`amdgpu_option_not_applied` otherwise). The
options apply to the Slurm GPU nodes and to the GPU Operator's host driver step on Kubernetes.

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
Prolog/Epilog pair records each job for Omnistat, `PrologFlags=Alloc,Contain`, and the `main`
partition is `OverSubscribe=EXCLUSIVE`, because Omnistat attributes a whole node to one job. The
deployment succeeds only when every composed Job does. A composed software install that the
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
never queries it. Uninstalling the Platform removes the exporter and the Slurm side, marks the state
`absent`, and removes the link; the container runtime, Prometheus, and Grafana Software
Assignments stay until they are uninstalled through the software API.

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

- `400 validation_error` for invalid settings.
- `404 not_found` when the Platform does not exist.
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
Nodes no longer managed lose the unit and the ordering; a loaded driver is never unloaded.
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
removes Swallow-created k0s state, units, join token, installer, and binary, and the GPU
Operator's DRA links, DRA common CDI spec, and amdgpu loader (a loaded driver stays loaded until
the next boot), and reloads systemd. It preserves the OS, user data, `conntrack`, unrelated
packages, and power state, and does not reboot. When servers are released, this uninstall
playbook is skipped entirely. Either way a successful uninstall sets `gpuOperator.state` to
`absent` when Swallow managed one.

On success Swallow clears membership, deletes only a credential Integration proven to be
Swallow-owned, and clears the Platform integration/sync projection. If Kubernetes owned
exporters, Swallow separately queues `install-exporters` for targets that remain present,
deployed, and unlocked. That Operation has its own failure lifecycle and does not roll back
the uninstall.

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
- Since 2026-10-21 an HA `apiVipPrefix` defaults to the control-plane subnet's prefix instead
  of `24`, and a VIP or prefix that does not fit that subnet is a `validation_error`; such a
  deploy could never form its etcd cluster.
- Control-plane lease discovery remains per Integration because its naming is k0s-specific.
- `monitoringLinks` was added on 2026-10-12. It is additive: a Platform stored before the field
  existed reads as `[]`, and a `PATCH` that omits it leaves the links unchanged.
