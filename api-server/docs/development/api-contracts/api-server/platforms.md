# Platforms

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

Create Kubernetes (k0s) and Slurm Platforms from a spec, change and delete them through that spec,
read their status and observed membership, and remove swallow records. A Platform is a
desired-state resource ([desired-state-model §5.2](../../../../../docs/development/desired-state-model.md)):
creating it and every destructive change go through a [Change Plan](change-plans.md), which the
user approves with the write (`approve=true`) or afterwards, and the worker's platform controller
converges the rest. swallow owns policy and durable lifecycle intent
for the platforms it deploys. It does not own platform membership, and — per
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md) (self deployed platform management) — it
manages only self-deployed platforms: the public API no longer registers an existing platform.

The in-cluster management of a deployed Kubernetes platform (namespaces, applications, pods,
apply, node cordon) is a separate live surface, [platforms-kubernetes.md](platforms-kubernetes.md).

## Related Glossary Terms

- [Platform](../../../../../docs/development/glossaries/terms/platform.md)
- [Platform Lifecycle State](../../../../../docs/development/glossaries/terms/platform-lifecycle-state.md)
- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)
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
POST   /api/v1/platforms/
GET    /api/v1/platforms/deployment-requirements/slurm
PUT    /api/v1/platforms/deployment-requirements/slurm
GET    /api/v1/platforms/gpu-operator/releases
GET    /api/v1/platforms/{platformId}
PATCH  /api/v1/platforms/{platformId}
PUT    /api/v1/platforms/{platformId}/spec
DELETE /api/v1/platforms/{platformId}
POST   /api/v1/platforms/{platformId}/sync
POST   /api/v1/platforms/sync
GET    /api/v1/platforms/{platformId}/slurm
```

`POST /api/v1/platforms/` creates a Platform from its spec, which swallow then installs;
registering an existing platform was removed in
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
  "monitoringLinks": [
    { "name": "Runbook", "url": "https://wiki.example.internal/lab-k0s" }
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
`gpu-operator`. A Platform has no exporter policy: the deployment installs the Node Exporter on
the Servers it provisions whatever the Platform type (see [Create A Platform](#create-a-platform)),
and the GPU exporter follows the Platform's features (the GPU Operator's `metricsExporter`, or
[Omnistat](#omnistat) or the [Device Metrics Exporter](#device-metrics-exporter) on Slurm).

`gpuOperator` is the swallow-managed GPU Operator of a Kubernetes Platform (see
[Manage The GPU Operator](#manage-the-gpu-operator)). It is `null` when swallow has never managed
one on the Platform. `enabled` is the latest requested intent; `state` is exactly
`installing | ready | failed | uninstalling | absent`; `lastWorkflowId` is the Workflow applying
or last applying it. `draDriverVersion` is `null` unless `allocationMode` is `dra`.

`monitoringLinks` lists the Platform's **Monitoring Links**: links to a monitoring UI that
belongs to the Platform — for example the Platform's own Grafana swallow installs on every Platform
type. It is always an array, empty by default. Each link has a `name` (the label a user
clicks) and an absolute `http`/`https` `url`. Monitoring Links are swallow-owned display data:
swallow never connects to, authenticates against, or checks the URL, and they are not a metrics
Integration — swallow's own usage charts keep reading the Site metrics backend (see
[server-metrics.md](server-metrics.md)). They are set with `PATCH`. swallow itself replaces (or
adds) only the link named `Platform Grafana` when it installs the Platform's Grafana (see
[Platform monitoring dashboards](#platform-monitoring-dashboards)), and removes only the links it
writes when the Platform is uninstalled: `Platform Grafana`, and `Omnistat Grafana` and
`Device Metrics Exporter Grafana`, the names it wrote for the same Grafana before.

`omnistat` is the swallow-managed Omnistat of a Slurm Platform (see [Omnistat](#omnistat)). It is
`null` when swallow never installed Omnistat on the Platform (always for Kubernetes).

`deviceMetricsExporter` is the swallow-managed Device Metrics Exporter of a Slurm Platform (see
[Device Metrics Exporter](#device-metrics-exporter)). It is `null` when swallow never installed it
on the Platform (always for Kubernetes, where the GPU Operator's `metricsExporter` runs it).

`containerRuntime` is the container runtime a Slurm deploy installed on its compute and login
nodes, `docker-ce` or `podman` (see [Container Runtime](#container-runtime)). It is `null` when
the feature was off (always for Kubernetes).

`gpuDriver` is the user's amdgpu driver intent, `{ "moduleOptions": { "<name>": "<value>" } }`
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
`lifecycleState` is exactly `registered | planned | deploying | deploy_failed | active |
uninstalling | uninstall_failed | uninstalled`; `planned` is a Platform created from its spec whose
install has not started: its Change Plan waits for approval, or was approved and its Workflow is not
written yet. `lifecycleOperationId` is the newest deployment or uninstall
Workflow ID, or `null`. These fields are derived by the API in a batch from durable Workflow
history; clients must not infer them from `integrationId`, membership, or sync freshness.

`deployment` is the non-secret topology intent recovered from the latest durable
`deploy-kubernetes` or `configure-slurm` Workflow. It contains `topology` as `standalone`,
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
successful uninstall removes the swallow-owned credential Integration. A legacy `registered`
platform may also have no Integration. `sync.matchedCount` is how many reported members
matched a Server.

## Spec And Status

Every Platform response also carries `generation`, `spec`, and `status`:

```json
{
  "generation": 2,
  "spec": {
    "desired": "present",
    "members": [
      { "serverId": "server-a", "roles": ["control-plane", "workloads"] },
      { "serverId": "server-b", "roles": ["worker"] }
    ],
    "settings": { "k0sVersion": "v1.36.3+k0s.2", "podCidr": "10.244.0.0/16" },
    "machinePreparation": { "mode": "existing_os", "settings": {}, "userData": {} },
    "removal": null
  },
  "status": {
    "observedGeneration": 1,
    "conditions": [
      { "type": "realized", "status": "false", "reason": "converging", "message": "Workflow w-2 carries out generation 2", "lastTransitionTime": "2026-11-01T12:00:00Z", "observedGeneration": 2 },
      { "type": "drifted", "status": "false", "reason": "in_sync", "message": "", "lastTransitionTime": "2026-11-01T11:00:00Z", "observedGeneration": 2 }
    ],
    "applied": { "desired": "present", "members": [ /* generation 1 */ ], "settings": {}, "machinePreparation": null, "removal": null },
    "realizingWorkflowId": "w-2"
  }
}
```

- `spec.members` are the Servers and their roles in the type's vocabulary: `control-plane`,
  `worker`, and `workloads` (a control plane that also runs workloads) for Kubernetes;
  `controller`, `compute`, and `login` for Slurm. They are the Platform's Platform Claims.
- `spec.settings` are the type's own fields: the Kubernetes or Slurm fields of
  [Create A Platform](#create-a-platform) other than the members and the machine preparation.
- `spec.machinePreparation` is how members that are not deployed get their OS. Inline user data
  is stored sealed and never returned; a spec written back with the same `userData.mode` and no
  `value` keeps the stored value.
- `spec.desired` is `absent` once the Platform is deleted; `spec.removal` holds the
  [Delete](#delete-a-platform) choices.
- `status.applied` is the spec the last successful Workflow realized, which the next change is
  planned against; `status.observedGeneration` is its generation. `status.realizingWorkflowId`
  names the Workflow carrying out a newer generation.
- `status.conditions` are the shared `realized` and `drifted` Conditions. `realized` is
  `converging` while a Workflow runs or an approved Change Plan is about to start one,
  `awaiting_approval` while a Change Plan waits for approval (its message names the plan), `plan_required`
  when a destructive change has no plan waiting (it was rejected or expired; write the spec again),
  and `failed` when the Workflow failed (it is not started again for the same generation: rerun it
  or write a new spec). `drifted` lists members the Platform does not report and Servers that
  report membership without being members.

A Platform deployed before specs existed is adopted by the platform controller at its first
observation: its spec is rebuilt from its latest deployment at generation 1, already applied when
it is active. Until then `spec` is `null` and `generation` is `0`, and spec writes are
`409 conflict`.

## Registering An Existing Platform Is Removed

There is no public route to register an existing platform. swallow manages only self-deployed
platforms; a Platform record and its credential Integration are produced only by creating a
Platform from its spec (`POST /api/v1/platforms/`) and installing it
([decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md)). A
`platform`-kind Integration is likewise created only by a successful deployment, so
`POST /api/v1/integrations` with `kind=platform` is rejected — see
[sites-integrations.md](sites-integrations.md). Existing `origin=registered` records remain
readable and deletable for one release but cannot be uninstalled and have no cluster explorer.

## Manage The Slurm Deployment Requirement

`GET /api/v1/platforms/deployment-requirements/slurm` reads the current system-wide Slurm
deployment eligibility policy. A missing stored record is returned as the disabled representation;
it is not a 404. `PUT` replaces the policy.

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

## Create A Platform

`POST /api/v1/platforms/` creates a Platform from its spec. swallow validates it exactly as a
deployment (every rule below), stores the Platform at generation 1 with its members claimed, and
answers `202 Accepted` with the Platform and the [Change Plan](change-plans.md) that installs it.
With `?approve=true` the plan is approved with the request, unless the Change Plan policy requires a
separate approval ([Approval On Submit](change-plans.md#approval-on-submit)); otherwise it waits for
approval. Nothing touches a machine until the plan is approved; the platform controller then starts
the build: `type: "kubernetes"` runs a `deploy-kubernetes` Workflow that builds k0s, `type: "slurm"` a
`configure-slurm` Workflow (see [Slurm](#slurm) below).

```json
{
  "siteId": "site-id",
  "name": "lab-k0s",
  "type": "kubernetes",
  "spec": {
    "members": [
      { "serverId": "server-a", "roles": ["control-plane"] },
      { "serverId": "server-b", "roles": ["control-plane"] },
      { "serverId": "server-c", "roles": ["control-plane"] },
      { "serverId": "server-d", "roles": ["worker"] }
    ],
    "settings": {
      "gpuStackOwner": "provisioning",
      "k0sVersion": "v1.36.3+k0s.2",
      "podCidr": "10.244.0.0/16",
      "serviceCidr": "10.96.0.0/12",
      "apiVip": "192.168.100.200",
      "apiVipPrefix": 24
    }
  }
}
```

```json
{ "platform": { "id": "platform-id", "generation": 1, "...": "..." }, "plan": { "id": "plan-id", "state": "approved", "decidedBy": "admin", "...": "..." } }
```

The plan's `state` is `approved` when the request approved it and `pending_approval` when it waits;
its `stateReason` then says why.

The rules below name the settings and the members. A request without a spec, or whose members or
settings do not make a valid deployment, is `400 validation_error`.

`siteId`, `name`, `type`, `settings.k0sVersion`, and at least one member are
required (`400 validation_error` otherwise); `podCidr` and `serviceCidr` default to `10.244.0.0/16`
and `10.96.0.0/12`. An unknown Site is `404 not_found`. The target Servers must exist (an unknown
one is `400 validation_error`) and belong to one Site. They must also be unlocked in a live
provider read. The complete lock preflight
finishes before swallow creates either the Platform or Workflow; one locked target rejects
the batch with `409 conflict`.

Every deploy, with `existing_os` or `provision_os`, first requires the installation's
Deployment Key, because swallow logs in to the targets with it (see [ssh-keys.md](ssh-keys.md)).
Without one the request is rejected with `409 conflict` before any validation (other than an
unparsable body or a missing `siteId`, which are `400 validation_error` first), Platform, or
Workflow is created; the message names the installation command that creates it
(`swallow-api deployment-key ensure`).

By default the targets must already be `deployed`. To provision the operating system inside
the same Workflow, include an optional `spec.machinePreparation` object (shared by both platform
types):

```json
{
  "machinePreparation": {
    "mode": "provision_os",
    "settings": { "imageId": "os-image-id", "deployTarget": "disk" },
    "network": { "mode": "automatic" }
  }
}
```

`network` carries the OS deployment network intent of [provisioning.md](provisioning.md)
(`mode` `automatic`, `dhcp`, or `static`) for the targets that are provisioned. The object also
accepts the rest of that OS Deployment intent for the provisioned targets: `templateId`, write-only
`userData` (`mode` `inherit`, `replace`, or `omit`, with `value`), and `network.subnetId`,
`network.defaultGateway`, and per-target `network.assignments` (`serverId`, `interfaceId`,
`subnetId`, `ipAddress`). `settings` takes `imageId` and the deploy mode exactly as an OS
Deployment does: `deployTarget` (`"disk"` | `"ram"`), where an unknown value is a
`400 validation_error`.

`mode` is `existing_os` (default) or `provision_os`. In `provision_os` a target that is
already `deployed` is reused as-is while a `ready` target is provisioned first, so one
deploy may mix both. The deploy mode is the `ram` Deploy Target (`settings.deployTarget: "ram"`;
the dashboard labels it "RAM deploy (ephemeral)").
RAM/ephemeral means the provider boots a
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

The Kubernetes automation applies this memory-backed-root handling to every node of a run, so a
Kubernetes Platform's members must all run from RAM or all from disk. A Server the deploy
provisions runs from the requested `deployTarget` (omitted is `disk`); a Server that is already
`deployed` (reused in `provision_os`, or any target in `existing_os`) runs from the root it was
deployed with, its observed `provisioning.ephemeral` ([servers-list.md](servers-list.md)). Members
that would not share one root are a `400 validation_error` before anything is created, whose message
lists the RAM and the disk Servers. The rule holds wherever a Kubernetes deployment is validated:
creating a Platform, a spec that adds a member ([Change A Platform](#change-a-platform)), and the
deploy the platform controller starts. An `existing_os` deploy whose members were all deployed to
RAM gets the memory-backed-root handling as well. Slurm has no such rule; its automation detects
the root on each node.

### Time sync check

A Platform deployment checks time; it does not set it
([decision 065](../../../../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md) (control plane ntp and post provision time sync)).
Time is configured when an OS is provisioned: in `provision_os` mode each Server the Workflow
provisions gets its own `configure-ntp-<serverId>` Task after `provision-<serverId>`, exactly as
in an OS deployment ([provisioning.md](provisioning.md#durable-os-workflows)), so such a deploy
is refused with `409 conflict` when the installation has no NTP server. Servers that are reused
as already deployed, and every target in `existing_os` mode, are not reconfigured.

Each platform checks its own nodes' clocks: the install playbook (`install-platform`) begins, in
both modes, by examining every target before it installs anything (before k0s is downloaded, and
before Slurm's preflight and MUNGE). The check changes nothing on the hosts: on each node it reads
whether the clock is synchronized (`timedatectl` `NTPSynchronized`) and, when the installation has
an NTP server, measures the node's offset from it with an SNTP query (limit 1 second). There is no
separate time-check Task. The optional deploy field `timeSyncCheck` decides what a problem does:

```json
{ "timeSyncCheck": "enforce" }
```

| Value | A node with an unsynchronized clock, an NTP server that does not answer, or a larger offset |
| --- | --- |
| `enforce` (default when omitted) | fails `install-platform` with `ansible_failed` before anything is installed on any node; the run output names each node with `[time_not_synchronized]` |
| `warn` | is reported with `[time_sync_warning]` in the run output, and the installation continues |

Any other value is `400 validation_error`. This applies to Kubernetes and Slurm alike.

**Node Exporter.** As in an OS deployment, each Server a `provision_os` deploy provisions also
gets the Node Exporter as Managed Software ([software.md](software.md#node-exporter)): a
`configure-node-exporter` Job with `install-node-exporter-<serverId>` after that Server's
`configure-ntp-<serverId>`, then `record-node-exporter-<serverId>`, which marks its
`node-exporter` Software Assignment `installed`. `install-platform` does not wait for the Job and
no Task depends on it, so a failed exporter install does not hold back the Platform; the Workflow
ends `requires_attention` until the user retries it. Every Platform type gets the Job; reused or
`existing_os` Servers are not reconfigured.

A Kubernetes member's roles are `control-plane` or `worker`, and a `control-plane` member that also
runs workloads adds `workloads`; a `worker` always runs workloads. (The read model's
`deployment.roleAssignments` shows the same as `role` and `runWorkloads`.)

A target is rejected while another Platform claims it: its Server's `platformClaim`
([servers-list.md](servers-list.md#platform-claim),
[decision 067](../../../../../docs/decisions/067-platform-claim-on-server.md) (platform claim on server)) names that
Platform, and the `400 validation_error` message names it too. When the Platform is created (or a
spec adds a member), swallow claims every member for it with a conditional write per Server in the
same transaction, so of two Platforms created at the same time only one can take a Server; the
other is refused the same way and leaves no Platform record or partial claim behind. The claim stays while the Platform is
`deploying`, `deploy_failed`, `active`, `uninstalling`, or `uninstall_failed`, so a partial
failed deployment remains protected even before membership can be observed; a successful
uninstall and a record-only delete release it. The platform controller keeps the members of a
present spec claimed, including those of a Platform it adopts. A claimed Server cannot be released directly
([provisioning.md](provisioning.md#durable-os-workflows)) or deleted
([server-detail-actions.md](server-detail-actions.md)). A target carrying any observed
Platform membership is also rejected. The current Server projection has one membership axis; allowing
cross-platform co-residency requires a future platform-scoped multi-membership contract
rather than overwriting the existing observation.

The members infer one of these supported topologies:

- Standalone: one `control-plane` member that also has `workloads`, and no worker.
- Non-HA multi-node: one `control-plane` member and one or more `worker` members; the
  control plane may also have `workloads`.
- High availability: an odd number of at least three `control-plane` members and at
  least one workload-capable member.

Two control-plane assignments and even control-plane counts are rejected. Every topology
must supply workload capacity. Pod and service ranges may not cover target addresses.
`apiVip` is required only for high availability; standalone and non-HA multi-node
deployments omit `apiVip` and `apiVipPrefix` and use the initial control-plane Server
address as the API endpoint. An HA virtual IP may not equal a selected Server address.

For HA, swallow reads from the provisioner the IPv4 subnet of each control-plane Server's
boot interface (or, for a Server the deploy provisions, the subnet its network assignment
names). The control-plane Servers must share one subnet, `apiVip` must lie inside it, and
`apiVipPrefix` must equal its prefix; an omitted `apiVipPrefix` takes it. Each violation is a
`validation_error` before anything is created. A different prefix is refused because a longer
one makes the VIP holder reach part of the subnet from the VIP, which etcd rejects as a peer.
When the provisioner reports no subnet, `apiVipPrefix` is used as given (default `24`) and the
deploy fails early on a control-plane host whose own subnet it does not match.

A Kubernetes deploy may install the AMD GPU Operator as part of the same Workflow with an
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
records an operator managed outside swallow, and swallow installs nothing. The `gpuOperator`
validation runs before any Platform or Workflow is created. The Workflow adds a
`configure-gpu-operator` Job after `validate-platform`; the deployment succeeds only when it
does. `monitoringDashboards` (a setting, default `true`) adds the Platform's own Grafana in the
cluster as the last Job (see [Platform monitoring dashboards](#platform-monitoring-dashboards)).

Success is `202 Accepted` after both records are persisted:

```json
{
  "platformId": "platform-id",
  "operationId": "operation-id"
}
```

`operationId` names the Workflow. Progress is read through [workflows](workflows.md). On success swallow creates and marks
ownership of a credential Integration, attaches it to the Platform, and begins membership
reads.

### Slurm

Set `type: "slurm"` and provide a `slurm` settings object in place of the Kubernetes fields. A
member's roles are per daemon because a Server may run the controller daemon (`slurmctld`,
`controller`), the compute daemon (`slurmd`, `compute`), or both, or be a `login` host.

```json
{
  "siteId": "site-id",
  "name": "lab-slurm",
  "type": "slurm",
  "spec": {
    "members": [
      { "serverId": "server-a", "roles": ["controller"] },
      { "serverId": "server-b", "roles": ["compute"] },
      { "serverId": "server-c", "roles": ["compute"] },
      { "serverId": "server-d", "roles": ["login"] }
    ],
    "settings": {
      "gpuStackOwner": "provisioning",
      "slurm": {
        "clusterName": "lab",
        "apiVersion": "v0.0.42",
        "workloadStorage": {
          "mode": "self-hosted",
          "type": "nfs",
          "mountPath": "/shared",
          "nfs": { "url": "10.0.0.9:/export/data", "mountOptions": "" }
        },
        "ociContainers": false,
        "pmix": false
      },
      "monitoringDashboards": true
    },
    "machinePreparation": {
      "mode": "provision_os",
      "settings": { "imageId": "slurm-os-image-id" }
    }
  }
}
```

At least one Server must run `slurmctld` and at least one must run `slurmd`; a Server with no
role (`controller`, `compute`, or `login`) is rejected, as is a Server assigned more than
once. A `login` node is a submission/client host that runs no cluster daemon; it may host the
shared-storage NFS exports and a login-only Server is valid. `gpuStackOwner` defaults to
`provisioning` when omitted (Slurm has no in-platform GPU operator). Every field of the `slurm`
object is optional: `clusterName` defaults to a sanitized
platform name; `apiVersion` pins the `slurmrestd` endpoint version recorded in the credential;
`stateSaveLocation` is an optional override of the `slurmctld` state directory. A highly
available deployment (more than one controller) needs a shared state directory, and swallow
provisions it automatically — it selects a state server (the login node when one is assigned,
otherwise an off-controller node), exports it over NFS, and mounts it on every controller
before `slurmctld` starts — so `stateSaveLocation` is not required for HA; when supplied it
overrides the directory path. The first `controller` member is the primary: it
mints the shared MUNGE key, hosts `slurmrestd`, and produces the reader credential.

`workloadStorage` is optional and configures a shared filesystem for user/job data (distinct
from controller state), mounted on every node at `mountPath` (a non-overlapping path, never
`/home`). Omit it for no shared filesystem. `type` is `nfs`. `mode` is `self-hosted` (swallow
exports NFS from the login node, so it requires one) or `external` (mount the user's
`nfs.url`, `host:/path`, with optional `nfs.mountOptions`). An omitted `type` is `nfs` and an
omitted `mountPath` is `/shared`. swallow provides the mount, not
cluster identity: consistent workload-user UID/GID across nodes is the user's
responsibility.

`ociContainers` (default `false`) sets up native OCI containers (see
[Native OCI Containers](#native-oci-containers)); `pmix` (default `false`) installs the host MPI
stack (see [MPI (PMIx)](#mpi-pmix)). `monitoringDashboards` (a setting beside `slurm`, default
`true`) gives the Platform its own Grafana (see
[Platform monitoring dashboards](#platform-monitoring-dashboards)).

### Node sharing, memory, and job hooks

Jobs share a compute node: the `main` partition is `OverSubscribe=NO` (separate CPUs, memory, and
GPUs per job, never oversubscribed CPUs) unless [Omnistat](#omnistat) needs whole nodes. Each node's
`RealMemory` is what `slurmd -C` detects minus the memory kept for the operating system (5%, at
least 2 GiB and at most 16 GiB, never more than half), and `DefMemPerCPU` is the smallest per-CPU
share of schedulable memory over the compute nodes, so a job that names no memory gets memory in
proportion to its CPUs instead of the whole node. The deployment proves it: two one-CPU jobs without
`--mem` must run on one node at the same time, otherwise the Task fails with
`slurm_shared_node_failed`.

`slurm.conf` names exactly one Prolog and one Epilog, swallow's dispatchers
(`/etc/slurm/swallow-prolog`, `/etc/slurm/swallow-epilog`, `PrologFlags=Alloc,Contain`). On each
compute node they run every executable in `/etc/slurm/prolog.d` and `/etc/slurm/epilog.d` in name
order with the job's environment and a 30-second limit each; a hook that fails or times out is
logged (syslog tag `swallow-prolog`/`swallow-epilog`) and never fails the job or drains the node.
swallow installs only its own hooks there (`10-omnistat`, `20-device-metrics-exporter`) and removes
the ones a redeploy no longer enables; other executables a user places there run too. A rerun
of a converged deployment reports no change.

The same target claim, lock, membership, and Site rules as Kubernetes apply. Before any Platform
or Workflow is created, a Slurm deploy also reads the current minimum resource requirement once
and validates every controller, compute, and login Server against its observed CPU, memory, and
storage. Equality is eligible; unknown or zero hardware fails an enabled positive threshold. Any
shortfall returns the standard validation error naming the Server, actual resources, and minimum,
with no deployment side effects. Failure to read the requirement fails closed. Kubernetes
deployments do not apply this policy. Success is the
same `202 Accepted` shape. When the target image includes `slurm-smd-slurmrestd`, swallow
records a `slurm` platform Integration pointing at `slurmrestd` on success and begins
membership reads; if the image omits it, the cluster still deploys successfully but the
platform has no integration and no members until the image includes it (unlike Kubernetes,
whose credential is required). A Slurm platform is deleted like Kubernetes (see
[Delete A Platform](#delete-a-platform)); its approved plan starts an `uninstall-slurm`
Workflow that removes the Slurm configuration and daemons while keeping the host OS and
image-supplied packages, and can optionally release the member servers.

### GPU GRES

Every Slurm deployment schedules AMD GPUs
([decision 063](../../../../../docs/decisions/063-omnistat-slurm-observability.md) (omnistat slurm observability)). swallow selects
the compute Servers whose GPU Inventory lists AMD `compute` GPUs; on each it loads the image's
`amdgpu` driver after boot (`swallow-amdgpu-load.service` with the
[GPU driver options](#gpu-driver-options); `slurmd` requires it), maps every GPU from the KFD
topology, and configures `GresTypes=gpu`, `Gres=gpu:<type>:<count>`, and a `gres.conf` with
explicit render nodes and `Flags=amd_gpu_env,explicit`. The type is derived per node from the GPU
architecture (for example `gpu:gfx942:8`) unless the deploy names one for every GPU node:

```json
"slurm": { "gpuGresType": "mi308x_vf" }
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
digits, and `_.,:-` (at most 128 characters); at most 16 options. Anything else is a `validation_error`. swallow also derives options from what a
node's KFD topology shows, never from a GPU model or a virtualization mode. Today's only rule:
when a GPU node reaches the same peer through both an XGMI `io_link` and a PCIe `p2p_link`, the
ROCm thunk overflows its peer array and HIP processes corrupt the heap at exit, so the rule adds
`pcie_p2p=0`. A rule option is kept once applied (the condition disappears with it); an option the
user names always wins over a rule. The options apply to the Slurm GPU nodes and to the GPU
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
Task fails with `amdgpu_reload_blocked`, naming the processes that hold the GPUs. A user who
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

## Platform Monitoring Dashboards

Every Platform may have its own read-only Grafana that shows only its members' series
([monitoring.md](../../../../../docs/development/monitoring.md) §6.2). The setting is
`settings.monitoringDashboards`, a boolean that defaults to `true` (omitted or `null` is on); it
is desired state the user writes with the spec. It applies to CPU-only Platforms as well, and both
Platform types read it. A Platform that has never had its own Grafana (it was deployed before the
setting existed, or its Grafana never installed) carries `false` in its spec and its applied spec;
setting it to `true` is then a change its Platform type realizes by deploying again. What the two
have in common:

- Its datasource is the [Platform metrics gateway](platform-metrics-gateway.md),
  `<public URL>/api/v1/platform-metrics/<platformId>/prometheus`, where the public URL is the
  installation's `SWALLOW_API_PUBLIC_URL` (the address the Servers reach swallow at). It
  authenticates with the Platform's monitoring token: swallow issues a new token each time it
  accepts such a deployment (so every deploy or redeploy rotates it) and hands it to the Grafana
  install Task only as a sealed secret; it is never in the Workflow's parameters or its logs.
- It provisions the `node-exporter` dashboards (CPU, memory, root filesystem, network, and load per
  Server), plus the GPU or job monitoring dashboards of the Platform's monitoring.
- Anyone on the management network can view it anonymously; there is no login form.
- When the install Task succeeds, swallow sets the Monitoring Link `Platform Grafana` to the URL the
  run reports. An install that reports no URL fails the Task.
- That URL opens the Grafana's landing dashboard: the Grafana address followed by
  `/d/<uid>/<slug>`, the dashboard's own address, chosen from the dashboards it provisions. It is
  the Device Metrics Exporter's Overview (`/d/fe0ayyeeoxvy8f/overview`) when it has the
  `device-metrics-exporter` dashboards, else Omnistat's Global - RMS
  (`/d/edqnz7pce7togc-rms/global-rms`) when it has `omnistat`, else Servers
  (`/d/swallow-node-exporter/servers`). The install checks Grafana at the Grafana address itself.

On a **Slurm** Platform the deploy Workflow composes the Managed Software `grafana` onto the primary
controller after the Slurm install (a `configure-grafana` Job, see
[software](software.md#composition-into-platform-deployments)), whether or not a monitoring feature
is chosen:

- Its install method is the enabled monitoring feature's `installMethod` (see [Omnistat](#omnistat)
  and [Device Metrics Exporter](#device-metrics-exporter)), and `native` when neither is enabled, so
  a Platform without monitoring gets no container runtime only for its Grafana. The `container`
  method first composes a container runtime onto the primary controller:
  the [container runtime](#container-runtime) the deploy chose, else the runtime already assigned
  to that Server, else Docker CE (with its Engine API off), skipped where it is `installed`.
- The token is not in the Software Assignment's `spec`. The dashboards add `omnistat` or
  `device-metrics-exporter` for the enabled feature.
- The Grafana address is `http://<primary address>:3000`, so the link is
  `http://<primary address>:3000/d/<uid>/<slug>`: Omnistat's or the Device Metrics Exporter's
  overview with that feature, Servers without one. It replaces a link swallow wrote for the same
  Grafana as `Omnistat Grafana` or `Device Metrics Exporter Grafana`.

On a **Kubernetes** Platform the deploy Workflow ends with a `configure-platform-grafana` Job, after
`validate-platform` and after the [GPU Operator](#manage-the-gpu-operator) when the deploy installs
one. Its one Task, `install-platform-grafana`, targets every member and installs the pinned
grafana-community Helm chart in the cluster from the initial controller:

- Grafana runs in namespace `swallow-grafana`, without persistent storage, and is reached at node
  port `30300` of every workload member; the Grafana address is
  `http://<first workload member's address>:30300`, so the link opens the Device Metrics Exporter's
  Overview there when the GPU Operator's `metricsExporter` is on, and Servers otherwise.
  On a member with an active firewalld the Task opens that port.
- The dashboards add `device-metrics-exporter` when the GPU Operator's `metricsExporter` is on.
- The token and a generated admin password are Kubernetes Secrets in that namespace; neither is in
  the chart values. The admin account exists for cluster administrators who read the Secret; there
  is still no login form.
- The Task fails (`ansible_failed`, retryable) with a message that starts with
  `[platform_grafana_node_port_in_use]` when another Service in the cluster holds node port `30300`,
  and it succeeds only once Grafana answers at the Grafana address and serves every dashboard there.
- Uninstalling the Platform removes the release with the cluster.

An installation without `SWALLOW_API_PUBLIC_URL` cannot plan the Grafana: the deployment is
refused when the platform controller plans it, and the Platform's `realized` Condition (`failed`)
names the setting. Turning the setting on for a deployed Platform redeploys it (see
[Change A Platform](#change-a-platform)); turning it off after deployment is not carried out yet.
Uninstalling the Platform removes the `Platform Grafana` link; on Slurm the Grafana's Software
Assignment stays until it is uninstalled through the software API.

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
the Docker CE swallow installs is the system (rootful) Docker Engine with its default runtime, and
swallow configures no Slurm integration for it (Slurm's rootless Docker with `scrun` is not set
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
  "slurm": {},
  "machinePreparation": { "mode": "provision_os", "settings": { "imageId": "slurm-os-image-id" } }
}
```

Omitting `omnistat`, or `enabled: false`, installs nothing: the API enables a monitoring feature
only when the request does (the dashboard's deploy wizard proposes Omnistat, which also suits a
CPU-only Platform). `omnistat` on a Kubernetes deploy is a `validation_error`. `installMethod` is
how the Platform's Grafana is installed, `container` (default) or `native` (see
[Platform monitoring dashboards](#platform-monitoring-dashboards) and
[software](software.md#prometheus-and-grafana-spec)); another value is a `validation_error`. The
primary controller (the first `controller` member) becomes the observability Server, which hosts
the Platform's Grafana. After the Slurm install, and after the Grafana when the Platform has one,
the Workflow runs a `configure-omnistat` Job that installs Omnistat `1.15.0` on every compute node:
the exporter listens on port `8001` and answers only the node itself and the addresses the Site
Prometheus scrapes from (the installation's `SWALLOW_API_MONITORING_SCRAPE_ADDRESSES`; an
installation without them cannot plan Omnistat and the deployment is refused when it is planned,
like a missing public URL). The Site Prometheus scrapes it once Omnistat is `ready`
([discovery-prometheus.md](discovery-prometheus.md)); no Prometheus is installed on the Platform.
The Slurm side is part of the Slurm install: a
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
acceptance until the Omnistat Task finishes, `ready` when it succeeded (every compute node's
exporter answers on the node), `failed` when it failed or never ran because an earlier Job failed,
and `absent` after the Platform was uninstalled. Only `lastWorkflowId` may move it. The Monitoring
Link is the Platform Grafana's, recorded by its own install. swallow never queries Omnistat.
`omnistat-query` on a compute node reads the Site Prometheus through the Platform Grafana's
read-only datasource proxy (`http://<primary address>:3000/api/datasources/proxy/uid/prometheus`),
which serves the same data the anonymous dashboards already show, so it needs
`monitoringDashboards` on. Omnistat 1.15.0's report needs GPU utilization, so on GPUs that do not
report it (for example SR-IOV virtual functions, which expose only memory) `omnistat-query` cannot
build one; the Grafana job dashboards still show memory per job. Uninstalling the Platform removes
the exporter and the Slurm side and marks the state `absent`; the container runtime and Grafana
Software Assignments (and a Prometheus an earlier deployment composed) stay until they are
uninstalled through the software API.

## Device Metrics Exporter

A Slurm deploy may install AMD's
[Device Metrics Exporter](../../../../../docs/development/glossaries/terms/device-metrics-exporter.md)
with Slurm job attribution, in the same Workflow
([decision 066](../../../../../docs/decisions/066-slurm-job-hooks-and-device-metrics-exporter.md) (slurm job hooks and device metrics exporter)):

```json
{
  "siteId": "site-id",
  "name": "lab-slurm",
  "type": "slurm",
  "deviceMetricsExporter": { "enabled": true, "installMethod": "container" },
  "slurm": {},
  "machinePreparation": { "mode": "provision_os", "settings": { "imageId": "slurm-os-image-id" } }
}
```

Omitting `deviceMetricsExporter`, or `enabled: false`, installs nothing. It is a `validation_error`
on a Kubernetes deploy (the GPU Operator's `metricsExporter` runs it there), together with an
enabled `omnistat`, when no compute Server lists AMD `compute` GPUs in its GPU Inventory, or with an
`installMethod` other than `container` (default) or `native`; like Omnistat's, `installMethod` is
how the Platform's Grafana is installed. Like Omnistat, the primary controller becomes the
observability Server, and the Workflow composes, after the Slurm install and the Platform's
Grafana (with the `device-metrics-exporter` dashboards), the Managed Software
[`device-metrics-exporter`](software.md#device-metrics-exporter) on every GPU compute node, as a
`configure-device-metrics-exporter` Job ([decision 068](../../../../../docs/decisions/068-device-metrics-exporter-managed-software.md) (device metrics exporter managed software)).
Each GPU compute node gets an ordinary Software Assignment, `installed` by the Job's record Task.
That Job installs AMD's pinned native package (`amdgpu-exporter 1.5.3~24.04`, SHA-256 verified; Ubuntu
24.04 only, `device_metrics_exporter_unsupported_os` otherwise) with Slurm job attribution on:
`gpuagent` starts after the amdgpu loader, and the exporter serves Prometheus metrics on the node's
address at port `5001` (AMD's default `5000` belongs to the host GPU exporter swallow scrapes).
Hardware profiler counters stay off. While the Platform claims its Servers, the standalone software
install and uninstall refuse them, so the exporter there stays the Platform's. The Slurm side is
part of the Slurm install: the
`20-device-metrics-exporter` job hook writes `/run/exporter/<GRES index>` for every GPU a job is
allocated and removes it at the job's end only while it still names that job, and jobs keep
sharing nodes (`OverSubscribe=NO`). Each GPU node checks that its own exporter answers on its
address; an exporter that serves no GPU health metrics fails the Task
(`device_metrics_exporter_no_metrics`). The Site Prometheus scrapes the exporters through their
Software Assignments ([discovery-prometheus.md](discovery-prometheus.md)).

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
the same meaning and fence (`lastWorkflowId`) as the Omnistat State (`ready`: every GPU node's
exporter answers and serves GPU health metrics). On success swallow records `gpuIdentity`:

| Value | Meaning |
| --- | --- |
| `distinct` | every GPU node's exporter reports one distinct `gpu_id` per GPU its KFD topology lists, so a job's metrics carry its own GPUs |
| `collapsed` | on at least one node the exporter reports fewer distinct `gpu_id`s than GPUs (for example MI308X SR-IOV virtual functions, which all report `gpu_id` `0`); the metrics endpoint works, but per-GPU job attribution there is wrong: a job on GPU 0 is attributed to every GPU, a job on another GPU to none |

`gpuIdentity` is `null` until an install succeeded. A collapsed identity does not fail the
deployment; clients must show it rather than present per-job GPU panels as reliable. swallow never
queries the exporters. Uninstalling the Platform removes the package, its configuration, and the
job hook from every target, marks the state `absent`, and marks the Servers'
`device-metrics-exporter` Software Assignments `absent`; the container runtime and Grafana Software
Assignments (and a Prometheus an earlier deployment composed) stay until they are uninstalled
through the software API.

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
GPUs. swallow does not build, pull, or distribute OCI bundles: a bundle must exist at the same
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

swallow installs the AMD GPU Operator into a swallow-deployed Kubernetes Platform, either during
the deploy or later, and changes or removes it on request
([decision 062](../../../../../docs/decisions/062-swallow-managed-gpu-operator.md) (swallow managed gpu operator)). The operator
uses the GPU driver the OS image provides; swallow does not build or install drivers. An image may
keep `amdgpu` out of the boot path, so swallow loads the image's driver on every managed node,
now and after each boot.

### Managed Nodes

No request names nodes or Servers. The operator manages every node of the Platform that its own
Node Feature Discovery marks as an AMD GPU node: `feature.node.kubernetes.io/amd-gpu=true` for a
physical GPU or `feature.node.kubernetes.io/amd-vgpu=true` for an SR-IOV virtual function, by the
chart's PCI device rules. Each Workflow re-reads that result, so a node that joins the cluster is
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
| `metricsExporter` | boolean | `false` | run AMD's Device Metrics Exporter on each managed node's host network at port `5001`, where the Site Prometheus scrapes it (see [What The Workflow Does](#what-the-workflow-does)) |

Enabling the operator requires at least one workload-capable target of the Platform's deployment
whose GPU inventory has an AMD `compute` GPU; otherwise it is a `validation_error`. This only
decides whether the feature is available; the nodes it manages are the ones described in
[Managed Nodes](#managed-nodes). `dra` requires the release's `draMinKubernetesVersion`, and every release requires its
`minKubernetesVersion`; the Kubernetes version is the deployment's `k0sVersion` without its
`+k0s` suffix. Unknown values, an unknown `version` or `draDriverVersion`, a too-old Kubernetes
version, or enabling the operator on a Slurm Platform is a `validation_error`.

### List The Supported Releases

`GET /api/v1/platforms/gpu-operator/releases` lists the operator releases swallow ships and has
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

The GPU Operator is the Platform's `settings.gpuOperator`, changed with
[Change A Platform](#change-a-platform). Turning it on is not destructive: the platform controller
starts a `configure-gpu-operator` Workflow that applies the settings. Changing its settings or
turning it off (`enabled: false`, or omitting it, uninstalls the operator) is destructive and goes
through a Change Plan, whose approval (with the write, or afterwards) starts the same Workflow.

Once the Workflow is persisted `gpuOperator.state` becomes `installing` (or `uninstalling`) and
`gpuOperator.lastWorkflowId` names it. The Workflow targets the Platform's deployment targets: it
verifies SSH readiness, then runs one ansible Task (`configure-gpu-operator`). The spec write
refuses invalid settings with `400 validation_error`; a refusal when the Workflow is started (the
Platform is not `active`, a target is locked or held by another unfinished Workflow, or the
installation has no Deployment Key) is reported by the Platform's `realized` Condition (`failed`)
and the controller retries it.

### What The Workflow Does

The Task fails (`ansible_failed`, retryable) with a message that starts with a bracketed code when
a precondition does not hold. The cluster-level refusals (`gpu_operator_unmanaged`,
`dra_unavailable`, `gpu_workloads_present`) are reported before the Task changes anything; the
node-level ones need Node Feature Discovery, so they are reported after the release is installed
or upgraded and before the `DeviceConfig` is applied:

| Code | Meaning |
| --- | --- |
| `gpu_operator_unmanaged` | the cluster already runs an AMD GPU Operator, a `DeviceConfig`, or a hand-applied AMD device plugin or labeller that swallow did not install |
| `dra_unavailable` | `dra` was requested but the cluster has no `resource.k8s.io/v1` API |
| `gpu_workloads_present` | the change switches the allocation mode or uninstalls while a Pod requests an AMD GPU resource or a `gpu.amd.com` `ResourceClaim` is allocated |
| `amd_gpu_not_detected` | Node Feature Discovery marked no node as an AMD GPU node, for example because no node has a GPU the chart supports |
| `amdgpu_driver_missing` | a managed node's image has no `amdgpu` kernel module, or the driver did not load or exposed no `/dev/kfd` when swallow loaded it |
| `metrics_exporter_port_conflict` | `metricsExporter` is on and a managed node has AMD's standalone Device Metrics Exporter (`amd-metrics-exporter.service`), which already holds port `5001` |
| `gpu_operator_not_ready` | the operator did not become Ready, a managed node did not expose its GPUs, or (with `metricsExporter`) a managed node's exporter did not answer at port `5001`, within the timeout |

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
`ResourceSlice` with devices (`dra`) and, with `metricsExporter`, every managed node's exporter
answers at `http://<node address>:5001/metrics`. The exporter runs on the host network at port
`5001` with the metric prefix `amd_` of every Device Metrics Exporter swallow manages (its
configuration is the ConfigMap `swallow-metrics-exporter` in `kube-amd-gpu`, removed when the
exporter is turned off), so the same dashboards read it; while the GPU Operator is `ready` with
the exporter on, the Site Prometheus scrapes it on every workload member whose GPU Inventory lists
AMD compute GPUs ([discovery-prometheus.md](discovery-prometheus.md)). Uninstalling removes the `DeviceConfig`, the Helm release,
the node labels, the DRA links, and the amdgpu loader; the Platform stays `active`.

`gpuOperator.state` follows the Task: `installing` / `uninstalling` while it waits or runs,
`ready` / `absent` when it succeeds, and `failed` when it fails, needs attention, or is canceled.
Only the Workflow named by `lastWorkflowId` moves the state. A refused or failed Task leaves its
Workflow in `requires_attention`, still holding the targets: retry the Task once the cause is
fixed, or cancel the Workflow before submitting other settings, which are `409 conflict` until
then. While the state is anything other
than `absent`, `PATCH` cannot set `gpuStackOwner` back to `provisioning` (`409 conflict`).

## Change A Platform

`PUT /api/v1/platforms/{platformId}/spec` replaces the spec. The body names the `generation` the
spec was read at and the new spec in the shape of [Create A Platform](#create-a-platform):

```json
{
  "generation": 2,
  "spec": { "members": [ /* ... */ ], "settings": { /* ... */ }, "machinePreparation": { /* ... */ } }
}
```

swallow classifies the change against `status.applied` as
[desired-state-model §5.2](../../../../../docs/development/desired-state-model.md) lists it, and
validates the new spec as a deployment:

- A non-destructive change (adding a member, turning on the GPU Operator, a Slurm monitoring
  feature, a Platform's `monitoringDashboards`, or a first container runtime or workload
  storage) is written and answered `200 OK` with `plan: null`; the platform controller converges
  it. Adding a member runs the type's deploy
  Workflow over every member; members already in the Platform are left as they are.
- A destructive change (changing or turning off the GPU Operator, and the changes below) is written
  together with a Change Plan and answered `202 Accepted`; with `?approve=true` the plan is approved
  with the write as in [Create A Platform](#create-a-platform). When swallow cannot carry the change out
  yet — removing a member, changing a member's roles, changing `k0sVersion`, `gpuStackOwner`, or
  `gpuDriver`, and changing or turning off Slurm workload storage, the container runtime, OCI
  containers, PMIx, `stateSaveLocation`, or `gpuGresType` — the plan carries the blocker
  `swallow cannot carry out <action> yet` and cannot be approved.
- A non-destructive change swallow cannot carry out yet (turning off or changing a Slurm monitoring
  feature, or turning off a Platform's `monitoringDashboards`) is `409 conflict` and writes
  nothing.
- An immutable field (`podCidr`, `serviceCidr`, `apiVip`, `apiVipPrefix`, Slurm `clusterName`) is
  `409 conflict`: changing it means deleting the Platform and creating it again.

A stale `generation` is `409 conflict`; read the Platform again. A spec write after the uninstall
started, or before the controller adopted the Platform, is `409 conflict`. A Platform marked absent
whose uninstall has not started may be written back to present, which withdraws its plan. Every
write that adds a member claims it in the same transaction; a member a spec drops keeps its claim
until the change removing it is carried out.

## Delete A Platform

`DELETE /api/v1/platforms/{platformId}` marks the spec `absent` and proposes the uninstall as a
Change Plan (`202 Accepted`, with the Platform and the plan); with `?approve=true` the plan is
approved with the request as in [Create A Platform](#create-a-platform). Once the plan is approved
the platform controller starts an `uninstall-kubernetes` or `uninstall-slurm` Workflow, and the
record is deleted when the uninstall succeeds. A Platform whose install was never approved touched
no machine, so it is deleted at once (`204 No Content`). Deleting a Platform already marked absent
returns the plan it waits on, and ignores the body; with `?approve=true` it approves that plan when
the plan is `pending_approval`, has no blockers, and the policy allows approving on submit.

The request body is optional. An absent or empty body removes the platform software only. To also
return the member servers to the provider in the same Workflow, send:

```json
{
  "releaseServers": true,
  "releaseOptions": {
    "erase": false,
    "secureErase": false,
    "quickErase": false,
    "unbindStaticIps": false
  },
  "cancelActiveWorkflows": false
}
```

The choices are stored as `spec.removal`. When `releaseServers` is true, the plan's effects say how
many members are released to the provisioner, that releasing removes their operating system,
whether their disks are erased and their static IP bindings removed or kept, and that the Platform
record is deleted once the release succeeds.

When `releaseServers` is true, swallow releases each member server directly instead of
uninstalling the platform software first: releasing wipes the operating system, so the
software removal would be redundant. The Workflow runs a `release-os` step per member (in
parallel) and then an internal finalize step that clears the Platform projections once every
release succeeds. `releaseOptions` mirrors the standalone Release action (disk erase and
static-IP unbinding) and is ignored when `releaseServers` is false. Releasing wipes the
operating system, so swallow does not restore host exporters for released servers. This
shortcut applies only to a whole-platform uninstall. A release requires the durable
orchestration topology.

Eligibility, checked when the approved plan starts the uninstall, requires a Platform with durable deployment provenance (`deploy-kubernetes` or
`configure-slurm`). The target set is always the complete `targetServerIds` snapshot from its latest
deployment Workflow, never current membership. All targets must still exist, be present,
be unlocked, and have no overlapping active Workflow. Lock is checked live before
the uninstall Workflow is created. No single provisioning state is
required because a failed deployment can leave a mixed target set.

A failed deployment waiting for user attention (`requires_attention`) still holds its
targets, as does a deployment still running. A plan waits while a deploy or uninstall of the
Platform runs. Send `"cancelActiveWorkflows": true` to have swallow cancel every unfinished Workflow
holding a target instead, with the same semantics as Release and
Recover ([decision 061](../../../../../docs/decisions/061-recovery-supersedes-blocking-work.md) (recovery supersedes blocking work)):
after every other check passes, swallow requests their cancellation and creates the uninstall at
once, ignoring them in the busy check. The uninstall takes the same resource leases, so it waits in
`waiting_dependency` and starts by itself once they end; a canceled deployment first returns the
Servers it was installing to Ready and keeps finished OS deployments
(decision 061). A cancel refused
for a Workflow that is still running is reported by the `realized` Condition. With the flag, a
Platform whose lifecycle is `deploying` may be uninstalled. The flag needs the durable
orchestration topology, like `releaseServers`.

A missing, absent, locked, or busy target (without `cancelActiveWorkflows`) keeps the plan from
starting the uninstall; the controller reports it with the `realized` Condition and retries. A
failed or canceled uninstall is rerun as a Workflow; the Platform stays `uninstall_failed` and its
record remains until an uninstall succeeds.

When servers are kept, the release-owned playbook stops k0s services, runs `k0s reset`,
removes swallow-created k0s state, units, join token, installer, and binary, and the GPU
Operator's DRA links, DRA common CDI spec, and amdgpu loader (a loaded driver stays loaded until
the next boot), and reloads systemd. It preserves the OS, user data, `conntrack`, unrelated
packages, and power state, and does not reboot. When servers are released, this uninstall
playbook is skipped entirely. Either way a successful uninstall sets `gpuOperator.state` to
`absent` when swallow managed one. For Slurm, `uninstall-slurm` also removes the job hook
dispatchers and swallow's hooks, Omnistat, the Device Metrics Exporter package, `oci.conf` and the
OCI probe bundle, and the MPI probe (the `crun` and OpenMPI packages stay), and a successful uninstall sets
`omnistat.state` and `deviceMetricsExporter.state` to `absent` when swallow managed them.

On success swallow clears membership, releases the Platform's Platform Claims on its Servers,
deletes only a credential Integration proven to be swallow-owned, and clears the Platform
integration/sync projection. The uninstall does not remove the Node Exporter. When the servers
are kept, the uninstall Workflow also has a `configure-node-exporter` Job after
`uninstall-platform` that installs the Node Exporter as Managed Software
([software.md](software.md#node-exporter)) on the targets that were present, deployed, and
unlocked when the uninstall was accepted: a target that has it keeps it unchanged, and one that
does not gets it. The cleanup is done once `uninstall-platform`
succeeds, so a failure of that Job does not roll back the uninstall.

The record is removed as the last part of the success cleanup, with the same projections as a
record-only delete, but without canceling Workflows, because the only Workflow still holding the
targets is the uninstall that is completing. The Node Exporter Job still runs as described
above.

## List, Get, Update, And Delete Only The Record

`GET /api/v1/platforms/` returns a plain array, optionally filtered by `siteId`.
`GET /api/v1/platforms/{platformId}` returns one. Lifecycle history is fetched in one batch
for list requests; the API does not fan out one Workflow query per Platform.

`PATCH /api/v1/platforms/{platformId}` updates `name`, `gpuStackOwner`, or `monitoringLinks`;
omitted fields do not change. `PATCH` never changes the spec or `gpuOperator`; use
[Change A Platform](#change-a-platform).

`monitoringLinks` replaces the whole list; `[]` clears it. At most 10 links. Each `name` is
trimmed, required, at most 64 characters, and unique within the Platform (case-insensitive).
Each `url` is trimmed, at most 2048 characters, and must be an absolute `http` or `https` URL
with a host. Any violation is `validation_error` and changes nothing.

```json
{ "monitoringLinks": [{ "name": "Runbook", "url": "https://wiki.example.internal/lab-slurm" }] }
```

`PATCH` does not accept `integrationId`: a Platform's credential
Integration is set only by its deployment, and repointing it at a user-owned Integration
would be a register-existing back door, which
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md) removes.

`DELETE /api/v1/platforms/{platformId}?recordOnly=true` removes only the swallow record; it is not a
way to uninstall (use [Delete A Platform](#delete-a-platform)). It returns `204 No Content`. It
first
cancels the Platform's in-flight durable Workflows so their resource leases are released
and the member servers are freed, then clears membership, the Platform's Platform Claims, and a
swallow-owned credential Integration, and finally removes the Platform record. Cancellation only stops durable work
and frees leases; Delete still never runs Ansible, changes hosts, waits for Uninstall, or
restores exporters, and it remains available while targets are locked because it does not
mutate them. If cancellation cannot be performed the Delete is refused and the record is
kept, so a Platform is never removed while its Workflows keep holding its servers. A
finished Workflow observed after cancellation is left as-is; retrying a finished
deploy/uninstall Workflow whose Platform no longer exists is refused.

Legacy records delete their linked Integration only when deployment provenance and the
complete auto-generated Kubernetes Integration signature both match. User-owned
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

A failure to read the Platform's API is
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
topology, including Kubernetes members that would not all run from RAM or all from disk, return
`validation_error`. Missing/disabled automation, missing automation
credential, or unavailable release playbook returns `provider_unavailable`. Platform API
transport/auth failures return `provider_unavailable`; provider rejection returns
`validation_error`.

## Compatibility Notes

- Since 2026-11-05 `POST /platforms`, `PUT /platforms/{id}/spec`, and `DELETE /platforms/{id}` take
  `?approve=true`, which approves the proposed Change Plan with the write
  ([Approval On Submit](change-plans.md#approval-on-submit)). This is additive: without it a
  destructive write waits for approval as before. A Platform whose plan is approved but not applied
  yet reports `realized` reason `converging` instead of `awaiting_approval`.
- Since 2026-11-04 a Kubernetes deployment whose members would not all run from RAM or all from
  disk (see [Create A Platform](#create-a-platform)) is a `400 validation_error` when it is
  accepted: creating the Platform, writing a spec that adds such a member, or the controller's
  redeploy. Such a deployment used to be accepted and then fail late: its disk nodes in
  `install-platform`, its RAM nodes on unreliable pod mounts. This is not breaking for a request
  that forms a working cluster. Since the same day an `existing_os` deploy over members
  all deployed to RAM gets the memory-backed-root handling, which it used to miss. The request and
  response shapes are unchanged.
- Since 2026-11-04 the `Platform Grafana` link swallow writes opens the Grafana's landing dashboard
  (`/d/<uid>/<slug>`, see [Platform monitoring dashboards](#platform-monitoring-dashboards)) instead
  of the Grafana root. Schema v14 points the `Platform Grafana` link of every Platform deployed
  before at the same landing dashboard, from its applied settings, unless a user pointed it at
  another page. The response shape is unchanged.
- Since 2026-11-04 (schema v13) every Platform without the Monitoring Link `Platform Grafana`
  has `settings.monitoringDashboards: false` in its spec and applied spec; turn it on to install
  the Platform's Grafana with a new deployment. Writing `true` on such a Platform used to be no
  change and installed nothing.
- Since 2026-11-03 a Kubernetes deployment reads `settings.monitoringDashboards` (default on) and
  installs the Platform's own Grafana in the cluster, so a Kubernetes deploy is refused when it is
  planned if the installation lacks `SWALLOW_API_PUBLIC_URL`. A Kubernetes Platform deployed before
  gets its Grafana when its `monitoringDashboards` is turned on (schema v13 records it off). The same day the GPU Operator's
  `metricsExporter` moved from the pod network to each managed node's host network at port `5001`,
  where the Site Prometheus scrapes it; a Platform whose exporter is on moves the next time its
  GPU Operator is applied. The request and response shapes are unchanged.
- Since 2026-11-03 a Slurm deployment composes no Prometheus: the Site Prometheus scrapes Omnistat
  and the Device Metrics Exporter. It composes the Platform's own Grafana whenever
  `settings.monitoringDashboards` is on, which is the default, so a Platform without a monitoring
  feature gets one too, on the `container` method (with a container runtime on the primary
  controller) unless a feature's `installMethod` says otherwise; `installMethod` now chooses only
  the Grafana's method. A Platform deployed before keeps its composed Prometheus Software
  Assignment until it is uninstalled through the software API, and keeps its `Omnistat Grafana` or
  `Device Metrics Exporter Grafana` link until its `monitoringDashboards` is turned on and its
  deployment replaces the link with `Platform Grafana`.
  Omnistat's exporter now admits the Site Prometheus's scrape addresses instead of the primary
  controller, and its `ready` state means every exporter answers on its node. A Slurm deploy is
  refused when it is planned if the installation lacks `SWALLOW_API_PUBLIC_URL` (with the Grafana
  on) or `SWALLOW_API_MONITORING_SCRAPE_ADDRESSES` (with Omnistat). Omitting both monitoring
  features still installs neither.
- `exporterOwner` was removed on 2026-11-03 (breaking). Platform responses no longer carry it, and
  `PATCH` no longer reads it: a request that still sends it is answered as if it were absent, like
  any other unknown field. OS provisioning installs the Node Exporter on every Server, so a host never
  has a second node exporter; the Kubernetes exporter DaemonSets that `k8s` selected were retired
  with it (their Workflow kinds are in [workflows.md](workflows.md)). The schema v12 migration
  removes the stored field.
- Since 2026-11-03 a `provision_os` deploy installs the Node Exporter on the Servers it provisions
  (a `configure-node-exporter` Job), and a kept-servers uninstall installs it on the kept Servers in
  the same Workflow instead of queueing a separate `install-exporters` Workflow afterwards. The
  request and response shapes are unchanged.
- The aggregate was renamed from Cluster to Platform
  ([decision 015](../../../../../docs/decisions/015-platform-term-disambiguation.md) (platform term disambiguation)). Its one-release
  aliases were removed on 2026-10-31 ([decision 074](../../../../../docs/decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases)),
  which is breaking: the `/api/v1/clusters...` routes (including the Slurm read and the
  [Kubernetes explorer](platforms-kubernetes.md)), and `clusterId` in the deploy and uninstall
  acceptance bodies and the membership reports.
- Since 2026-11-01 a Platform is written as a spec (breaking, no aliases): `POST /platforms/`
  with a spec replaces `POST /platforms/deploy`, `DELETE /platforms/{id}` marks the Platform
  absent and replaces `POST /platforms/{id}/uninstall` (the record-only delete moved to
  `?recordOnly=true`, which answers `204`), and the GPU Operator is changed through the spec instead
  of `PUT /platforms/{id}/gpu-operator`. Creating a Platform and destructive changes wait on a
  Change Plan. `roleAssignments` and `slurm.nodeAssignments` became `spec.members` with roles;
  the read model's `deployment.roleAssignments` is unchanged.
- The k0s `controller` term is an implementation detail; members use `control-plane`.
- `apiVip` remains accepted exactly as before for HA requests and is now optional for a
  one-control-plane deployment.
- Since 2026-10-21 an HA `apiVipPrefix` defaults to the control-plane subnet's prefix instead
  of `24`, and a VIP or prefix that does not fit that subnet is a `validation_error`; such a
  deploy could never form its etcd cluster.
- Control-plane lease discovery remains per Integration because its naming is k0s-specific.
- `monitoringLinks` was added on 2026-10-12. It is additive: a Platform stored before the field
  existed reads as `[]`, and a `PATCH` that omits it leaves the links unchanged.
- Since 2026-10-31 `machinePreparation.settings.deployTarget` is read. Before, a deploy
  that named only `deployTarget: "ram"` was silently deployed to disk, and an unknown value was
  ignored; it is now a `validation_error`. Since the same day's decision 074 the deprecated
  `settings.ephemeral` alias is no longer read (breaking): send `deployTarget`.
