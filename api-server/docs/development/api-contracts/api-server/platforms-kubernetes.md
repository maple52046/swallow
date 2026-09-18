# Platforms — Kubernetes Cluster Explorer

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

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

All endpoints require an admin JWT per [conventions](conventions.md). All list responses are
plain objects with an `items` array (bounded per namespace, so not paginated).

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
  (the cluster API rejected the request, for example an invalid manifest). The
  `error.message` carries the cluster's reason.

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
      "kubeletVersion": "v1.30.2+k0s"
    }
  ]
}
```

`role` is `control-plane` or `worker`. `serverId` is Swallow's matched Server, or `null` when
no Server projection matches the node's name or addresses. This read is separate from
membership sync and does not write the membership axis.

`POST .../nodes/{nodeName}/cordon` marks a node unschedulable; `POST .../nodes/{nodeName}/uncordon`
clears it. Both return the updated node object above. Draining (evicting pods) is not part of
this surface.

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

Success is `201 Created` returning the namespace. An invalid name is `400 validation_error`;
an existing name surfaces the cluster's `409 conflict`.

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
the ready count. For a bare Pod, both are `1`/`0..1`.

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

`kind` must be one of the four; any other value is `400 validation_error`. A missing workload
is `404 not_found`.

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

`GET .../pods/{namespace}/{name}/logs` returns a bounded log snapshot (not a stream):

```json
{
  "container": "nginx",
  "logs": "... up to tailLines lines ..."
}
```

Query parameters: `container` (defaults to the pod's first container) and `tailLines`
(default `200`, max `2000`). Following logs, `exec`, and a kubectl shell are not part of this
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
- ConfigMaps: `keys` (the data key names) and `dataCount`.
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

`action` is `created`, `configured`, or `unchanged` (`validated` when `dryRun`). A manifest
the cluster rejects returns `400 validation_error` with the cluster's reason in
`error.message`; nothing is applied past the first rejected document.

## Errors

Beyond the [shared envelope](conventions.md) and the eligibility rules above:

- `404 not_found` — unknown Platform, non-Kubernetes Platform, or a missing addressed object.
- `409 conflict` — a `registered` Platform, a Platform without a recorded credential, or a
  refused system-namespace delete.
- `400 validation_error` — an invalid body (name, replicas, kind, manifest) or a request the
  cluster API rejected.
- `503 provider_unavailable` — the cluster API is unreachable or rejected the credential.
