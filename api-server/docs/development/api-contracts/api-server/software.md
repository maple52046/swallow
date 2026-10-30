# Software Deployment

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- External API integrators

## Purpose

Install and uninstall a single piece of host software (Managed Software) on one or more
already-deployed Servers, and read the swallow-owned Software Assignment records that track
what is installed where. This is deliberately distinct from platform deployment: the deploy
target is one software and its variants, not a multi-component runtime (see
[decision 038](../../../../../docs/decisions/038-software-deployment.md)).

Every install/uninstall creates a durable Workflow; the response returns its `operationId`,
and progress is observed through the [workflows](workflows.md) surface.

The wire shape of these endpoints (paths, parameters, schemas, status codes) is described by
[`../openapi/areas/software.yaml`](../openapi/areas/software.yaml); this document holds their
behavior semantics.

## Related Glossary Terms

- [Managed Software](../../../../../docs/development/glossaries/terms/managed-software.md)
- [Software Assignment](../../../../../docs/development/glossaries/terms/software-assignment.md)
- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)
- [Server Status](../../../../../docs/development/glossaries/terms/server-status.md)
- [Docker Host Explorer](../../../../../docs/development/glossaries/terms/docker-host-explorer.md)
- [Device Metrics Exporter](../../../../../docs/development/glossaries/terms/device-metrics-exporter.md)
- [Platform Claim](../../../../../docs/development/glossaries/terms/platform-claim.md)

## Endpoints

```text
GET  /api/v1/software/catalog
GET  /api/v1/software/assignments
POST /api/v1/software/assignments
POST /api/v1/software/uninstall
```

All endpoints require the `admin` role (an access token or an API Key) according to
[conventions](conventions.md).

## Software Catalog

`GET /api/v1/software/catalog` returns the fixed set of installable software kinds and their
rules: an object whose `items` array is small and bounded, so it is not paginated.

```json
{
  "items": [
    {
      "kind": "docker-ce",
      "label": "Docker CE",
      "roles": [],
      "mutuallyExclusiveWith": ["podman"],
      "refusedForKubernetesMembers": true,
      "installMethods": [],
      "containerRuntimes": [],
      "specFields": ["version", "enableApi"]
    },
    {
      "kind": "podman",
      "label": "Podman",
      "roles": [],
      "mutuallyExclusiveWith": ["docker-ce"],
      "refusedForKubernetesMembers": true,
      "installMethods": [],
      "containerRuntimes": [],
      "specFields": ["version"]
    },
    {
      "kind": "nfs",
      "label": "NFS",
      "roles": ["server", "client"],
      "mutuallyExclusiveWith": [],
      "refusedForKubernetesMembers": false,
      "installMethods": [],
      "containerRuntimes": [],
      "specFields": ["exportPath", "exportOptions", "source", "mountPath", "mountOptions"]
    },
    {
      "kind": "prometheus",
      "label": "Prometheus",
      "roles": [],
      "mutuallyExclusiveWith": [],
      "refusedForKubernetesMembers": false,
      "installMethods": ["container", "native"],
      "containerRuntimes": ["docker-ce", "podman"],
      "specFields": ["installMethod", "containerRuntime", "version", "listen", "retention", "scrapeJob", "scrapeTargets", "scrapeServerIds", "scrapePort"]
    },
    {
      "kind": "grafana",
      "label": "Grafana",
      "roles": [],
      "mutuallyExclusiveWith": [],
      "refusedForKubernetesMembers": false,
      "installMethods": ["container", "native"],
      "containerRuntimes": ["docker-ce", "podman"],
      "specFields": ["installMethod", "containerRuntime", "version", "prometheusUrl", "dashboards"]
    },
    {
      "kind": "device-metrics-exporter",
      "label": "AMD Device Metrics Exporter",
      "roles": [],
      "mutuallyExclusiveWith": [],
      "refusedForKubernetesMembers": true,
      "refusedForClaimedServers": true,
      "requiresAmdComputeGpu": true,
      "installMethods": [],
      "containerRuntimes": [],
      "specFields": []
    }
  ]
}
```

Every entry carries `refusedForClaimedServers` and `requiresAmdComputeGpu` (`false` unless
shown). `refusedForClaimedServers: true` means a standalone install or uninstall refuses a Server
with a [Platform Claim](../../../../../docs/development/glossaries/terms/platform-claim.md),
because that Platform manages the kind on its Servers. `requiresAmdComputeGpu: true` means every
target must list AMD `compute` GPUs in its GPU Inventory.

`kind`, `roles`, `mutuallyExclusiveWith`, `installMethods`, and `containerRuntimes` values are
domain language from the
[Managed Software](../../../../../docs/development/glossaries/terms/managed-software.md)
glossary term; adding a value is a model change, not just an API change. `installMethods` lists
the ways a kind can be installed, the first being the default (always an array, empty for a kind
with a single built-in way). `containerRuntimes` lists the runtime kinds the `container` method can
run on; the runtime must already be `installed` on every target of a standalone install (always an
array, empty when the kind has no container method).

## Software Assignment

A Software Assignment is the swallow-owned record of one Managed Software kind on one Server,
keyed by `(serverId, kind)`.

```json
{
  "serverId": "srv-abc",
  "kind": "nfs",
  "roles": ["client"],
  "spec": {
    "source": "10.0.0.9:/export/data",
    "mountPath": "/shared",
    "mountOptions": "rw,_netdev"
  },
  "state": "installed",
  "lastWorkflowId": "op-123",
  "lastAppliedAt": "2026-09-24T06:00:00Z",
  "createdAt": "2026-09-24T05:59:00Z",
  "updatedAt": "2026-09-24T06:00:00Z"
}
```

A Docker CE assignment records the normalized spec (see [Docker CE spec](#docker-ce-spec)):

```json
{
  "serverId": "srv-abc",
  "kind": "docker-ce",
  "roles": [],
  "spec": { "enableApi": true },
  "state": "installed",
  "lastWorkflowId": "op-125",
  "lastAppliedAt": "2026-10-02T03:00:00Z",
  "createdAt": "2026-10-02T02:58:00Z",
  "updatedAt": "2026-10-02T03:00:00Z"
}
```

`state` is one of `pending | installed | failed | uninstalling | absent`. These are domain
values from the
[Software Assignment](../../../../../docs/development/glossaries/terms/software-assignment.md)
term. `absent` means Swallow no longer considers the software present (for example the Server
left `deployed`).

Every field is always present. `spec` is the normalized spec the last install recorded; it is
`null` when an install of a kind that adds no defaults (NFS, Podman) sent no `spec`.
`lastAppliedAt` is `null` until an install succeeds.

### List Assignments

`GET /api/v1/software/assignments` returns an object whose `items` array holds the Software
Assignments (small, bounded per fleet), ordered by `serverId` then `kind`. Optional query
filters:

| Param | Type | Meaning |
| --- | --- | --- |
| `serverId` | string | Only assignments on this Server. |
| `kind` | string | Only assignments of this software kind. The value is not validated: one that is not a catalog kind matches nothing. |

By default `absent` assignments are omitted; pass `includeAbsent=true` to include them (any
other value is treated as `false`).

```json
{ "items": [ /* Software Assignment objects */ ] }
```

## Install Software

`POST /api/v1/software/assignments` installs one software kind on one or more deployed
Servers, creating a durable Workflow and a `pending` assignment per Server.

```json
{
  "kind": "nfs",
  "assignments": [
    { "serverId": "srv-a", "roles": ["server"] },
    { "serverId": "srv-b", "roles": ["client"] }
  ],
  "spec": {
    "exportPath": "/export/data",
    "exportOptions": "rw,sync,no_subtree_check,root_squash",
    "source": "srv-a:/export/data",
    "mountPath": "/shared",
    "mountOptions": "rw,_netdev"
  }
}
```

- `kind` is required and must be a catalog kind.
- `assignments[]` lists the target Servers and must not be empty. Each `serverId` is required
  and may appear only once; `roles` is required for a kind that declares roles (NFS) and must be
  a subset of that kind's roles (duplicates are dropped), and must be empty/omitted for a
  role-less kind (Docker CE, Podman).
- `spec` is kind-specific and optional per field. For NFS, at least one `server` role in the
  batch requires `exportPath`, and a `client` role requires `source` and `mountPath`. Docker CE
  is described in [Docker CE spec](#docker-ce-spec), Prometheus and Grafana in
  [Prometheus and Grafana spec](#prometheus-and-grafana-spec), and the Device Metrics Exporter
  in [Device Metrics Exporter](#device-metrics-exporter).
- Every target Server must be present and in provisioning state `deployed`; the request checks
  this first and refuses it as `400 validation_error`. A kind installed by its
  `container` method also needs one of the entry's `containerRuntimes` in state `installed` on
  every target (see [Prometheus and Grafana spec](#prometheus-and-grafana-spec)).
- All targets must belong to one Site, whose Ansible automation must be runnable.
- Installing a kind that is already `installed` on a target is a convergent re-apply: the new
  `spec` replaces the recorded one and the playbook ensures it. A client changing one field must
  therefore send the complete spec it wants (for example the existing `version` together with a
  changed `enableApi`). The record turns `pending` but keeps its previous `lastAppliedAt` until
  the new ensure succeeds; an `absent` record starts over with `null`.

### Docker CE spec

```json
{
  "kind": "docker-ce",
  "assignments": [{ "serverId": "srv-abc" }],
  "spec": { "version": "5:27.3.1-1~ubuntu.24.04~noble", "enableApi": true }
}
```

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `version` | string | latest | Pins the apt package version (dnf installs the latest). |
| `enableApi` | boolean | `true` | Makes the Docker Engine API listen on TCP port `2375` of every interface, unauthenticated plain HTTP, so the [Docker Host Explorer](servers-docker.md) can manage the host. `false` keeps only the local socket. |

- A non-string `version` or a non-boolean `enableApi` is `400 validation_error`.
- The recorded assignment `spec` always carries an explicit `enableApi` (an omitted value is
  recorded as `true`). An assignment recorded before this field existed has no `enableApi`;
  consumers must read that as **disabled**.
- Changing `enableApi` on an installed Server restarts the Docker daemon; containers without a
  restart policy stop. Uninstall removes swallow's listener configuration.
- The listener is intended for internal networks only; it grants root-equivalent control of the
  host to anyone who can reach the port. Consumers should warn before enabling it.

### Prometheus and Grafana spec

Both run one pinned instance per Server, never on all interfaces: Grafana on the Server's primary
address, Prometheus (whose API has no authentication) on the loopback address unless `listen` asks
for the primary address. Every method uses the host network, so an address means the same thing
for a container and a native process. Each is installed by one of two methods:

- `container` (the default): the official image (`docker.io/prom/prometheus:v<version>`,
  `docker.io/grafana/grafana:<version>`) under a Docker Compose project on `docker-ce` or a Podman
  Quadlet unit on `podman`. The runtime must already be `installed` on every target, and every
  target must run the same one; an omitted `containerRuntime` is the runtime installed there.
- `native`: Prometheus's official release binary (verified against the release's published
  checksums) under a Swallow systemd unit, and Grafana's pinned package from Grafana's official
  apt or yum repository. No container runtime is needed. The native method owns the Server's
  `grafana` package.

```json
{
  "kind": "prometheus",
  "assignments": [{ "serverId": "srv-abc" }],
  "spec": { "installMethod": "native", "version": "3.15.0", "scrapeJob": "nodes", "scrapeTargets": ["10.0.0.21:9100", "10.0.0.22:9100"] }
}
```

| Kind | Field | Type | Default | Meaning |
| --- | --- | --- | --- | --- |
| both | `installMethod` | string | `container` | `container` or `native`. |
| both | `containerRuntime` | string | the runtime installed on the targets | `docker-ce` or `podman`; only valid with the `container` method. |
| `prometheus` | `version` | string | `3.15.0` | Release version (`x.y.z`; a leading `v` is accepted and dropped). |
| `prometheus` | `listen` | string | `loopback` | `loopback` (only processes on the Server, such as its Grafana) or `management` (the Server's primary address; anyone who reaches it can read every series). |
| `prometheus` | `retention` | string | `15d` | How long samples are kept: a Prometheus duration such as `7d` or `1w2d`. |
| `prometheus` | `scrapeJob` | string | `swallow` | Job name of the static targets: a letter or `_`, then letters, digits, `_`, `-` (at most 64). |
| `prometheus` | `scrapeTargets` | string[] | `[]` | `host:port` endpoints scraped every 15 s. |
| `prometheus` | `scrapeServerIds` | string[] | `[]` | Servers of the target's Site scraped on `scrapePort`. Each is resolved to the Server's current primary address every time the playbook runs, so a redeploy that changes an address converges on the next run. |
| `prometheus` | `scrapePort` | integer | — | Port of every `scrapeServerIds` Server (1–65535); required with, and only valid with, `scrapeServerIds`. |
| `grafana` | `version` | string | `12.4.12` | Release version (`x.y.z`; a leading `v` is accepted and dropped): the image tag or the package version. |
| `grafana` | `prometheusUrl` | string | Prometheus on the same Server (where it listens) | Absolute `http`/`https` URL of the datasource (UID `prometheus`). |
| `grafana` | `dashboards` | string[] | `[]` | Swallow-shipped dashboard bundles to provision: `omnistat` (Omnistat's RMS global, node, and job dashboards) and `device-metrics-exporter` (the Device Metrics Exporter's overview, GPU, node, and job dashboards). |

- Prometheus listens on port `9090` (loopback by default) and Grafana on port `3000` of the
  Server's primary address. Prometheus has no authentication; Grafana is read-only: anonymous
  Viewer access, no login form, no basic auth, and no admin account.
- An unknown `installMethod` or `listen`, a `retention` that is not a Prometheus duration, a
  `containerRuntime` that is not a container runtime or comes with the `native` method, targets on
  different runtimes, a version that is not `x.y.z`, a wrong type,
  a malformed target or URL, an unknown bundle, more than 512 `scrapeTargets` plus
  `scrapeServerIds`, a `scrapeServerIds` entry at another Site, or a missing/unexpected
  `scrapePort` is `400 validation_error`; a `scrapeServerIds` entry that names no Server is
  `404 not_found`. The `container` method on a target without an `installed` runtime, or with a
  different one than `containerRuntime` names, is `409 conflict`.
- The recorded `spec` always carries the applied `installMethod`, `containerRuntime` (container
  method only), `version`, Prometheus's `listen` and `retention`, the `scrapeJob`, and the lists as
  arrays; Grafana's `prometheusUrl` is recorded as given (empty means the local Prometheus).
- Re-installing with another method or runtime replaces the previous one on the Server and keeps
  Prometheus's data.
- Uninstall removes the instance whichever method placed it (container, Quadlet unit, or native
  release, unit, and package), its configuration, and its data; the container runtime stays.

### Device Metrics Exporter

`device-metrics-exporter` installs AMD's
[Device Metrics Exporter](../../../../../docs/development/glossaries/terms/device-metrics-exporter.md)
on each target: AMD's pinned native package (`amdgpu-exporter 1.5.3~24.04`, SHA-256 verified),
with `gpuagent` started after Swallow's post-boot amdgpu loader (`swallow-amdgpu-load.service`,
installed when the Server has none). The exporter serves per-GPU Prometheus metrics on the
Server's primary address at port `5001` (`5000` belongs to the host GPU exporter Swallow scrapes),
with Slurm job attribution and the hardware profiler off. It installs only the exporter: to graph
it, install `prometheus` with `scrapeServerIds` naming these Servers and `scrapePort: 5001`, and
`grafana` with the `device-metrics-exporter` dashboard bundle.

```json
{
  "kind": "device-metrics-exporter",
  "assignments": [{ "serverId": "srv-abc" }]
}
```

- The request takes no `spec` fields; any field is `400 validation_error`. The recorded `spec` is
  `{ "version": "1.5.3" }`.
- Every target must list AMD `compute` GPUs in its GPU Inventory, or the request is
  `409 conflict`. A Kubernetes Platform member (whose GPU Operator runs its own exporter) and a
  Server with a Platform Claim (whose Platform manages the exporter) are `409 conflict` too.
- Only Ubuntu 24.04 is supported. The install Task fails with
  `device_metrics_exporter_unsupported_os` on another distribution, and with
  `device_metrics_exporter_no_metrics` when the exporter serves no GPU health metrics. When a
  node's exporter reports fewer distinct `gpu_id`s than GPUs (for example MI308X SR-IOV virtual
  functions), the install still succeeds and the Task output says so.
- Uninstall removes the package, its configuration, and Swallow's amdgpu loader unless another
  unit (slurmd or k0s) still depends on the loader. A Server with a Platform Claim is refused with
  `409 conflict`.

On success the response is `202 Accepted`:

```json
{ "operationId": "op-123" }
```

### Install Errors

In addition to the shared codes in [conventions](conventions.md):

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | The body is not valid JSON; unknown kind; no target, a target without `serverId`, or a duplicate target; missing/invalid roles for the kind; a missing, malformed, or wrong-typed spec field, or one the kind does not accept; or a target Server is absent or not in provisioning state `deployed`. |
| `not_found` | 404 | A target Server, or a Prometheus `scrapeServerIds` entry, does not exist. |
| `conflict` | 409 | A mutually exclusive software kind is already installed on a target, the `container` method's runtime is not `installed` on a target, a target is already a Kubernetes Platform member for a kind that refuses members, a target has a Platform Claim for a kind that refuses claimed Servers, or a target lacks the AMD compute GPUs the Device Metrics Exporter needs. |
| `conflict` | 409 | When the Workflow is created: a target already has an active durable Workflow, is locked, or (re-checked) is not in the required `deployed` provisioning state. |
| `provider_unavailable` | 503 | The targets' Site cannot run the Workflow's Ansible automation (no automation configuration, automation disabled, automation not logging in with the Deployment Key, or a playbook outside the release manifest), or the targets belong to more than one Site. |

## Uninstall Software

`POST /api/v1/software/uninstall` removes one software kind from one or more Servers on which
it is installed, creating a durable Workflow that runs the `uninstall-<kind>` playbook and
marks the assignments `absent`.

```json
{
  "kind": "nfs",
  "serverIds": ["srv-a", "srv-b"]
}
```

- `kind` is required and must be a catalog kind.
- `serverIds[]` lists the target Servers and must not be empty; duplicate ids are ignored. Each
  must have an existing, non-`absent` assignment of that kind.

On success the response is `202 Accepted`:

```json
{ "operationId": "op-124" }
```

### Uninstall Errors

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | The body is not valid JSON, unknown kind, or empty `serverIds`. |
| `not_found` | 404 | A target Server or its assignment does not exist (an `absent` assignment counts as missing). |
| `conflict` | 409 | A target has active durable Workflow, is not `deployed`, is locked, or has a Platform Claim for a kind that refuses claimed Servers. |
| `provider_unavailable` | 503 | As for install: the targets' Site cannot run the Ansible automation, or the targets belong to more than one Site. |

## Compatibility Notes

- Software kinds, roles, and assignment states are glossary-owned enums; adding a value is a
  model change per [conventions](conventions.md).
- The install/uninstall responses intentionally return only `operationId`; clients read
  progress and the per-Task Job grouping through the [workflows](workflows.md) surface.
- 2026-10-30: added the `device-metrics-exporter` kind and the catalog fields
  `refusedForClaimedServers` and `requiresAmdComputeGpu`
  ([decision 068](../../../../../docs/decisions/068-device-metrics-exporter-managed-software.md)).
  Clients that do not know a field read it as `false`.
- 2026-10-30: corrected to the implemented behavior when the OpenAPI description was written (no
  wire change): the catalog and assignment lists are `{ "items": [...] }` objects, an
  assignment's `spec` can be `null`, a not-`deployed` target is refused at install with `400`
  (not `409`), and install and uninstall can return `503 provider_unavailable`.

## Composition Into Platform Deployments

A platform deployment may compose a software kind as a Job of its own Workflow, sharing these
playbooks and roles. The composed install records ordinary Software Assignments (pending when the
platform Workflow is accepted, `installed` by its record Task, `failed` when its install Task
fails), so this surface lists and uninstalls them like any other. Today a Slurm deployment
composes:

- with the container runtime feature, `docker-ce` (`enableApi: false`) or `podman` onto its
  compute and login nodes (see [platforms](platforms.md#container-runtime));
- with Omnistat, `prometheus` (`listen: loopback`, job `omnistat`, `scrapeServerIds` = the compute nodes,
  `scrapePort` = `8001`) and `grafana` (dashboards `omnistat`) onto its primary controller by
  Omnistat's install method; the `container` method also composes the runtime onto the primary
  controller (see [platforms](platforms.md#omnistat));
- with the Device Metrics Exporter, the same two kinds the same way, with job
  `device-metrics-exporter`, `scrapeServerIds` = the GPU compute nodes, `scrapePort` = `5001`, and
  dashboards `device-metrics-exporter`, and then `device-metrics-exporter` itself onto the GPU
  compute nodes with Slurm job attribution on (see [platforms](platforms.md#device-metrics-exporter)).

A composed runtime skips a Server where it is already `installed`, so an operator's own runtime
spec is never reverted. Composition adds no endpoint. A Slurm Platform's uninstall removes the
Device Metrics Exporter from its Servers, and a successful one marks their
`device-metrics-exporter` assignments `absent`; the composed Prometheus, Grafana, and runtime stay
for the software uninstall.

Slurm's embedded NFS (HA state and workload storage) is still not the generic `nfs` software
above; extracting it into the shared `configure-nfs` Job is deferred. Until then, generic NFS and
Slurm-managed state NFS are distinct.
