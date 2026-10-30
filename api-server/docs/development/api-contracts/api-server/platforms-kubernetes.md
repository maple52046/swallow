# Platforms — Kubernetes Cluster Explorer

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Manage the in-cluster resources of a Swallow-deployed Kubernetes Platform live, through the
Platform's own Kubernetes API. This is the *explorer plane* described in
[decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md): a
read/write surface for namespaces, applications (aggregated workloads), pods and pod logs,
subsidiary resources (Services, Ingresses, ConfigMaps, Secrets, PVCs), YAML apply, and node
cordon. It is separate from the *lifecycle plane* (deploy/uninstall/repair, in
[platforms.md](platforms.md) and [workflows.md](workflows.md)).

Every route here reads or writes the deployed cluster's Kubernetes API on demand and stores
nothing in Swallow (ADR 001). Writes are synchronous against the cluster API and do not create
Workflows. This surface never writes the Server membership axis or the sync counters, and
never runs on the reconcile interval (ADR 021).

## Related Glossary Terms

- [Platform](../../../../../docs/development/glossaries/terms/platform.md)
- [Kubernetes Application](../../../../../docs/development/glossaries/terms/kubernetes-application.md)
- [Kubernetes Topology](../../../../../docs/development/glossaries/terms/kubernetes-topology.md)
- [Node Role](../../../../../docs/development/glossaries/terms/node-role.md)
- [Server](../../../../../docs/development/glossaries/terms/server.md)

## Endpoints

```text
GET    /api/v1/platforms/{platformId}/kubernetes
GET    /api/v1/platforms/{platformId}/kubernetes/nodes
POST   /api/v1/platforms/{platformId}/kubernetes/nodes/{nodeName}/cordon
POST   /api/v1/platforms/{platformId}/kubernetes/nodes/{nodeName}/uncordon

GET    /api/v1/platforms/{platformId}/kubernetes/namespaces
POST   /api/v1/platforms/{platformId}/kubernetes/namespaces
DELETE /api/v1/platforms/{platformId}/kubernetes/namespaces/{namespace}

GET    /api/v1/platforms/{platformId}/kubernetes/applications
GET    /api/v1/platforms/{platformId}/kubernetes/applications/{namespace}/{kind}/{name}
DELETE /api/v1/platforms/{platformId}/kubernetes/applications/{namespace}/{kind}/{name}
POST   /api/v1/platforms/{platformId}/kubernetes/applications/{namespace}/{kind}/{name}/scale
POST   /api/v1/platforms/{platformId}/kubernetes/applications/{namespace}/{kind}/{name}/restart

GET    /api/v1/platforms/{platformId}/kubernetes/pods
GET    /api/v1/platforms/{platformId}/kubernetes/pods/{namespace}/{name}/logs
DELETE /api/v1/platforms/{platformId}/kubernetes/pods/{namespace}/{name}

GET    /api/v1/platforms/{platformId}/kubernetes/services
GET    /api/v1/platforms/{platformId}/kubernetes/ingresses
GET    /api/v1/platforms/{platformId}/kubernetes/configmaps
GET    /api/v1/platforms/{platformId}/kubernetes/secrets
GET    /api/v1/platforms/{platformId}/kubernetes/persistentvolumeclaims

POST   /api/v1/platforms/{platformId}/kubernetes/apply
```

All endpoints require the `admin` role per [conventions](conventions.md). All list responses are
plain objects with an `items` array (bounded per namespace, so not paginated). The wire shape
(paths, parameters, schemas, status codes) is specified in
[`../openapi/areas/platforms.yaml`](../openapi/areas/platforms.yaml); this document keeps the
behavior. Every route is also served under the deprecated `/api/v1/clusters/{platformId}/kubernetes`
alias (see [platforms.md](platforms.md#compatibility-notes)).

## Eligibility

The explorer is available only for a Swallow-deployed Kubernetes Platform with a recorded
credential:

- A Platform whose `type` is not `kubernetes` returns `404 not_found`.
- A Platform whose `origin` is not `deployed` (a compatibility `registered` record) returns
  `409 conflict`.
- A `deployed` Platform whose credential Integration is not yet recorded (still `deploying`)
  returns `409 conflict`.
- When the cluster API is unreachable or rejects the credential, the standard reader mapping
  applies: `503 provider_unavailable` (unreachable or 5xx / auth) or `400 validation_error`
  (any other 4xx the cluster API answered: an invalid manifest, but also an object the cluster
  reports missing or already existing). The `error.message` carries the cluster's reason.

## Cluster Summary

`GET /api/v1/platforms/{platformId}/kubernetes` returns a small live summary:

```json
{
  "version": "v1.30.2+k0s",
  "nodeCount": 3,
  "readyNodeCount": 3,
  "namespaceCount": 8
}
```

`version` is the cluster's reported Kubernetes/kubelet version (empty when the API did not
report one). Counts are observed at read time.

## Nodes

`GET /api/v1/platforms/{platformId}/kubernetes/nodes` lists nodes correlated to Server identity:

```json
{
  "items": [
    {
      "name": "node-1",
      "role": "control-plane",
      "ready": true,
      "unschedulable": false,
      "serverId": "server-a",
      "addresses": ["10.0.0.1"],
      "kubeletVersion": "v1.30.2+k0s",
      "gpu": {
        "allocatable": { "amd.com/gpu": 8 },
        "draDevices": 0
      }
    }
  ]
}
```

`role` is `control-plane` or `worker`. `serverId` is Swallow's matched Server, or `null` when
no Server projection matches the node's name or addresses. This read is separate from
membership sync and does not write the membership axis.

`gpu` reports the node's GPUs as Kubernetes exposes them, for the GPU Operator (see
[platforms.md](platforms.md#manage-the-gpu-operator)). `gpu.allocatable` holds every
`status.allocatable` resource whose name starts with `amd.com/` (an extended resource from the
device plugin), as an integer; it is empty when the node has none. `gpu.draDevices` counts the
devices in this node's `ResourceSlice`s published by the `gpu.amd.com` DRA driver. It is `null`
when the cluster has no `resource.k8s.io/v1` API or the slices could not be read; the rest of
the node list is still returned.

`POST .../nodes/{nodeName}/cordon` marks a node unschedulable; `POST .../nodes/{nodeName}/uncordon`
clears it. Both return the updated node object above, read from the write's answer: it is not
correlated to a Server and carries no DRA count, so `serverId` and `gpu.draDevices` are `null`.
Draining (evicting pods) is not part of this surface.

## Namespaces

`GET .../kubernetes/namespaces` lists namespaces:

```json
{
  "items": [
    { "name": "default", "phase": "Active", "system": false }
  ]
}
```

`system` is `true` for `kube-system`, `kube-public`, and `kube-node-lease`; a client hides
them by default and can reveal them.

`POST .../kubernetes/namespaces` creates one:

```json
{ "name": "web" }
```

Success is `201 Created` returning the namespace. A missing or blank name is
`400 validation_error` before any cluster call; a name the cluster refuses, including one that
already exists, is also `400 validation_error` with the cluster's reason.

`DELETE .../kubernetes/namespaces/{namespace}` deletes a namespace and returns
`{"success": true}`. Deleting a system namespace is refused with `409 conflict` before any
cluster call.

## Applications

An Application is a live aggregation of a workload and the Pods it owns (see the glossary
term). `GET .../kubernetes/applications` lists them, optionally filtered by `namespace`; when
`namespace` is omitted, system namespaces are excluded unless `includeSystem=true`.

```json
{
  "items": [
    {
      "namespace": "web",
      "name": "nginx",
      "kind": "Deployment",
      "images": ["nginx:1.27"],
      "replicas": 3,
      "readyReplicas": 3,
      "createdAt": "2026-09-19T00:00:00Z"
    }
  ]
}
```

`kind` is `Deployment`, `DaemonSet`, `StatefulSet`, or `Pod` (an owner-less Pod is its own
Application). For a DaemonSet, `replicas` is the desired scheduled count and `readyReplicas`
the ready count. For a bare Pod, both are `1`/`0..1`. `createdAt` is the object's creation time
as the cluster reported it (RFC 3339).

`GET .../applications/{namespace}/{kind}/{name}` returns one Application with its pods:

```json
{
  "namespace": "web",
  "name": "nginx",
  "kind": "Deployment",
  "images": ["nginx:1.27"],
  "replicas": 3,
  "readyReplicas": 3,
  "createdAt": "2026-09-19T00:00:00Z",
  "pods": [
    {
      "namespace": "web",
      "name": "nginx-6d8-abcde",
      "phase": "Running",
      "ready": true,
      "nodeName": "node-2",
      "restarts": 0,
      "containers": ["nginx"],
      "startedAt": "2026-09-19T00:01:00Z"
    }
  ]
}
```

`pods` uses the Pod shape of [Pods](#pods) and is omitted when the Application currently owns no
Pod; the list and the scale response never carry it. `kind` must be one of the four; any other
value is `400 validation_error` before any cluster call. A workload the cluster reports missing is
also `400 validation_error`, with the cluster's reason.

`POST .../applications/{namespace}/{kind}/{name}/scale` sets the replica count:

```json
{ "replicas": 5 }
```

Valid only for `Deployment` and `StatefulSet`; a `DaemonSet` or `Pod` returns
`400 validation_error`. `replicas` must be a non-negative integer. Success returns the updated
Application summary.

`POST .../applications/{namespace}/{kind}/{name}/restart` triggers a rolling restart (a pod
template annotation for Deployment/DaemonSet/StatefulSet). A bare `Pod` returns
`400 validation_error` (delete it instead). Success returns `{"success": true}`.

`DELETE .../applications/{namespace}/{kind}/{name}` deletes the workload object and returns
`{"success": true}`.

## Pods

`GET .../kubernetes/pods` lists pods, optionally filtered by `namespace` (system namespaces
excluded when `namespace` is omitted unless `includeSystem=true`):

```json
{
  "items": [
    {
      "namespace": "web",
      "name": "nginx-6d8-abcde",
      "phase": "Running",
      "ready": true,
      "nodeName": "node-2",
      "restarts": 0,
      "containers": ["nginx"],
      "startedAt": "2026-09-19T00:01:00Z"
    }
  ]
}
```

`startedAt` is the Pod's start time as the cluster reported it (RFC 3339), empty before the Pod
started.

`GET .../pods/{namespace}/{name}/logs` returns a bounded log snapshot (not a stream):

```json
{
  "container": "nginx",
  "logs": "... up to tailLines lines ..."
}
```

Query parameters: `container` (defaults to the pod's first container) and `tailLines`
(default `200`; a value above `2000` is read as `2000`; anything but a positive integer is
`400 validation_error`). Following logs, `exec`, and a kubectl shell are not part of this
surface.

`DELETE .../pods/{namespace}/{name}` deletes a pod (a controller recreates it) and returns
`{"success": true}`.

## Subsidiary Resources

Each of `services`, `ingresses`, `configmaps`, `secrets`, and `persistentvolumeclaims` is a
list, optionally filtered by `namespace` (system namespaces excluded when `namespace` is
omitted unless `includeSystem=true`):

```json
{ "items": [ { "namespace": "web", "name": "nginx", "...": "type-specific fields" } ] }
```

- Services: `type`, `clusterIP`, `ports` (`"80/TCP"`), and `externalIPs`.
- Ingresses: `hosts`, `ingressClass`, and `addresses`.
- ConfigMaps: `type` (always empty for a ConfigMap; the field is shared with Secrets), `keys`
  (the data key names), and `dataCount`.
- Secrets: `type`, `keys` (the data key names), and `dataCount`. **Secret values are never
  returned** — only the key names — because a credential surface must not leak.
- PersistentVolumeClaims: `phase`, `capacity`, `storageClass`, and `accessModes`.

These are read-only lists; create and update go through `apply`.

## Apply

`POST .../kubernetes/apply` server-side applies a YAML manifest (one or more documents):

```json
{
  "manifest": "apiVersion: apps/v1\nkind: Deployment\n...",
  "dryRun": false
}
```

`manifest` is required. When `dryRun` is `true`, the cluster validates without persisting
(server-side dry run). Success returns per-object results:

```json
{
  "results": [
    { "kind": "Deployment", "namespace": "web", "name": "nginx", "action": "configured" }
  ]
}
```

`action` is `created` when the object did not exist and `configured` when it did, whether or not
the apply changed it (`validated` for every object when `dryRun`); `unchanged` is never reported.
`namespace` is empty for a cluster-scoped object. A manifest the cluster rejects returns
`400 validation_error` with the cluster's reason in `error.message`; nothing is applied past the
first rejected document.

## Errors

Beyond the [shared envelope](conventions.md) and the eligibility rules above:

- `404 not_found` — unknown Platform, non-Kubernetes Platform, or an API process without the
  explorer wired.
- `409 conflict` — a `registered` Platform, a Platform without a recorded credential, or a
  refused system-namespace delete.
- `400 validation_error` — an invalid body (name, replicas, kind, manifest), an invalid
  `tailLines`, or any 4xx the cluster API answered, including an addressed object it reports
  missing or a namespace that already exists.
- `503 provider_unavailable` — the cluster API is unreachable, answered 5xx, or rejected the
  credential.
