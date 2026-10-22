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

## Related Glossary Terms

- [Managed Software](../../../../../docs/development/glossaries/terms/managed-software.md)
- [Software Assignment](../../../../../docs/development/glossaries/terms/software-assignment.md)
- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)
- [Server Status](../../../../../docs/development/glossaries/terms/server-status.md)
- [Docker Host Explorer](../../../../../docs/development/glossaries/terms/docker-host-explorer.md)

## Endpoints

```text
GET  /api/v1/software/catalog
GET  /api/v1/software/assignments
POST /api/v1/software/assignments
POST /api/v1/software/uninstall
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Software Catalog

`GET /api/v1/software/catalog` returns the fixed set of installable software kinds and their
rules. The result is a small bounded array, not paginated.

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
      "specFields": ["installMethod", "containerRuntime", "version", "scrapeJob", "scrapeTargets", "scrapeServerIds", "scrapePort"]
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
    }
  ]
}
```

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

### List Assignments

`GET /api/v1/software/assignments` returns a plain array of Software Assignments (small,
bounded per fleet). Optional query filters:

| Param | Type | Meaning |
| --- | --- | --- |
| `serverId` | string | Only assignments on this Server. |
| `kind` | string | Only assignments of this software kind. |

By default `absent` assignments are omitted; pass `includeAbsent=true` to include them.

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
- `assignments[]` lists the target Servers. Each `serverId` is required; `roles` is required
  for a kind that declares roles (NFS) and must be a subset of that kind's roles, and must be
  empty/omitted for a role-less kind (Docker CE, Podman).
- `spec` is kind-specific and optional per field. For NFS, at least one `server` role in the
  batch requires `exportPath`, and a `client` role requires `source` and `mountPath`. Docker CE
  is described in [Docker CE spec](#docker-ce-spec), Prometheus and Grafana in
  [Prometheus and Grafana spec](#prometheus-and-grafana-spec).
- Every target Server must be in provisioning state `deployed`. A kind installed by its
  `container` method also needs one of the entry's `containerRuntimes` in state `installed` on
  every target (see [Prometheus and Grafana spec](#prometheus-and-grafana-spec)).
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

Both run one pinned instance per Server on the Server's primary address (never all interfaces),
installed by one of two methods:

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
| `prometheus` | `scrapeJob` | string | `swallow` | Job name of the static targets: a letter or `_`, then letters, digits, `_`, `-` (at most 64). |
| `prometheus` | `scrapeTargets` | string[] | `[]` | `host:port` endpoints scraped every 15 s. |
| `prometheus` | `scrapeServerIds` | string[] | `[]` | Servers of the target's Site scraped on `scrapePort`. Each is resolved to the Server's current primary address every time the playbook runs, so a redeploy that changes an address converges on the next run. |
| `prometheus` | `scrapePort` | integer | — | Port of every `scrapeServerIds` Server (1–65535); required with, and only valid with, `scrapeServerIds`. |
| `grafana` | `version` | string | `12.4.12` | Release version (`x.y.z`; a leading `v` is accepted and dropped): the image tag or the package version. |
| `grafana` | `prometheusUrl` | string | Prometheus on the same Server | Absolute `http`/`https` URL of the datasource (UID `prometheus`). |
| `grafana` | `dashboards` | string[] | `[]` | Swallow-shipped dashboard bundles to provision; the only bundle is `omnistat` (Omnistat's RMS global, node, and job dashboards). |

- Prometheus listens on port `9090` and Grafana on port `3000` of the Server's primary address.
  Prometheus has no authentication; Grafana is read-only: anonymous Viewer access, no login form,
  no basic auth, and no admin account.
- An unknown `installMethod`, a `containerRuntime` that is not a container runtime or comes with
  the `native` method, targets on different runtimes, a version that is not `x.y.z`, a wrong type,
  a malformed target or URL, an unknown bundle, more than 512 `scrapeTargets` plus
  `scrapeServerIds`, a `scrapeServerIds` entry at another Site, or a missing/unexpected
  `scrapePort` is `400 validation_error`; a `scrapeServerIds` entry that names no Server is
  `404 not_found`. The `container` method on a target without an `installed` runtime, or with a
  different one than `containerRuntime` names, is `409 conflict`.
- The recorded `spec` always carries the applied `installMethod`, `containerRuntime` (container
  method only), `version`, the `scrapeJob`, and the lists as arrays; Grafana's `prometheusUrl` is
  recorded as given (empty means the local Prometheus).
- Re-installing with another method or runtime replaces the previous one on the Server and keeps
  Prometheus's data.
- Uninstall removes the instance whichever method placed it (container, Quadlet unit, or native
  release, unit, and package), its configuration, and its data; the container runtime stays.

On success the response is `202 Accepted`:

```json
{ "operationId": "op-123" }
```

### Install Errors

In addition to the shared codes in [conventions](conventions.md):

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | Unknown kind, missing/invalid roles for the kind, missing required spec field, or a spec field of the wrong type. |
| `conflict` | 409 | A target already has an active durable Workflow, a mutually exclusive software kind is already installed on a target, the `container` method's runtime is not `installed` on a target, or a target is already a Kubernetes Platform member for a kind that refuses members. |
| `not_found` | 404 | A target Server does not exist. |
| `conflict` | 409 | A target Server is not in the required `deployed` provisioning state, or is locked. |

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
- `serverIds[]` lists the target Servers; each must have an existing, non-`absent` assignment
  of that kind.

On success the response is `202 Accepted`:

```json
{ "operationId": "op-124" }
```

### Uninstall Errors

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | Unknown kind or empty `serverIds`. |
| `not_found` | 404 | A target Server or its assignment does not exist. |
| `conflict` | 409 | A target has active durable Workflow, is not `deployed`, or is locked. |

## Compatibility Notes

- Software kinds, roles, and assignment states are glossary-owned enums; adding a value is a
  model change per [conventions](conventions.md).
- The install/uninstall responses intentionally return only `operationId`; clients read
  progress and the per-Task Job grouping through the [workflows](workflows.md) surface.

## Composition Into Platform Deployments

A platform deployment may compose a software kind as a Job of its own Workflow, sharing these
playbooks and roles. The composed install records ordinary Software Assignments (pending when the
platform Workflow is accepted, `installed` by its record Task, `failed` when its install Task
fails), so this surface lists and uninstalls them like any other. Today a Slurm deployment
composes:

- with the container runtime feature, `docker-ce` (`enableApi: false`) or `podman` onto its
  compute and login nodes (see [platforms](platforms.md#container-runtime));
- with Omnistat, `prometheus` (job `omnistat`, `scrapeServerIds` = the compute nodes,
  `scrapePort` = `8001`) and `grafana` (dashboards `omnistat`) onto its primary controller by
  Omnistat's install method; the `container` method also composes the runtime onto the primary
  controller (see [platforms](platforms.md#omnistat)).

A composed runtime skips a Server where it is already `installed`, so an operator's own runtime
spec is never reverted. Composition adds no endpoint.

Slurm's embedded NFS (HA state and workload storage) is still not the generic `nfs` software
above; extracting it into the shared `configure-nfs` Job is deferred. Until then, generic NFS and
Slurm-managed state NFS are distinct.
