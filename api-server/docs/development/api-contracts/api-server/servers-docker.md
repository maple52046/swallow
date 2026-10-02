# Servers — Docker Host Explorer

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Manage the Docker Engine of one Server live — images, containers, volumes, and networks — when
swallow installed Docker CE on that Server with the Docker Engine API enabled
([software.md](software.md), `spec.enableApi`). This is the Docker Host Explorer of
[decision 043](../../../../../docs/decisions/043-docker-host-management.md): api-server calls the
host's Engine API on demand; the browser never connects to the host.

Every route reads or writes the host's Docker Engine at request time and stores nothing in
swallow. Writes are synchronous against the Engine and do not create Workflows. Docker objects
carry no swallow identity; their ids and names are the Engine's own.

## Related Glossary Terms

- [Docker Host Explorer](../../../../../docs/development/glossaries/terms/docker-host-explorer.md)
- [Software Assignment](../../../../../docs/development/glossaries/terms/software-assignment.md)
- [Managed Software](../../../../../docs/development/glossaries/terms/managed-software.md)
- [Server](../../../../../docs/development/glossaries/terms/server.md)
- [Server Lock](../../../../../docs/development/glossaries/terms/server-lock.md)
- [Registry Credential](../../../../../docs/development/glossaries/terms/registry-credential.md)

## Endpoints

```text
GET    /api/v1/servers/{serverId}/docker

GET    /api/v1/servers/{serverId}/docker/images
POST   /api/v1/servers/{serverId}/docker/images/pull
DELETE /api/v1/servers/{serverId}/docker/images/{imageId}

GET    /api/v1/servers/{serverId}/docker/containers
POST   /api/v1/servers/{serverId}/docker/containers
POST   /api/v1/servers/{serverId}/docker/containers/{containerId}/start
POST   /api/v1/servers/{serverId}/docker/containers/{containerId}/stop
POST   /api/v1/servers/{serverId}/docker/containers/{containerId}/restart
GET    /api/v1/servers/{serverId}/docker/containers/{containerId}/logs
DELETE /api/v1/servers/{serverId}/docker/containers/{containerId}

GET    /api/v1/servers/{serverId}/docker/volumes
POST   /api/v1/servers/{serverId}/docker/volumes
DELETE /api/v1/servers/{serverId}/docker/volumes/{volumeName}

GET    /api/v1/servers/{serverId}/docker/networks
POST   /api/v1/servers/{serverId}/docker/networks
DELETE /api/v1/servers/{serverId}/docker/networks/{networkId}
```

All endpoints require an admin JWT per [conventions](conventions.md). List responses are plain
objects with an `items` array; one host's Docker objects are a bounded set, so lists are not
paginated. Path identifiers are the Engine's ids or names and must be URL-encoded (image ids
contain `:`).

## Eligibility

Every route first checks swallow-owned facts, in this order:

| Condition | Response |
| --- | --- |
| The Server does not exist. | `404 not_found` |
| The Server is absent, not in provisioning state `deployed`, or has no known address. | `409 conflict` |
| The Server has no `installed` `docker-ce` Software Assignment. | `409 conflict` |
| That assignment's `spec.enableApi` is not `true` (including a legacy record without the field). | `409 conflict` |

Swallow does not probe for a Docker API enabled outside swallow. A consumer should decide whether
to offer the explorer from the Software Assignment (`GET /api/v1/software/assignments`) and treat
a `409` here as "not available" rather than as a failure.

Write routes (every `POST` and `DELETE`) are additionally refused while the Server is locked:
`409 conflict` when the provisioner confirms the lock, `503 provider_unavailable` when the lock
state cannot be confirmed. Reads stay available on a locked Server.

## Docker Engine Errors

After eligibility, the Engine's own outcome is mapped as follows; `error.message` carries the
Engine's reason.

| Engine outcome | Response |
| --- | --- |
| Unreachable, timed out, or an Engine internal error (5xx). | `503 provider_unavailable` |
| The addressed image, container, volume, or network does not exist (Engine 404). | `404 not_found` |
| The object is in use, the name is taken, or the object is protected (Engine 409 or 403). | `409 conflict` |
| The Engine rejected the request parameters (Engine 400 or another 4xx). | `400 validation_error` |

## Summary

`GET /api/v1/servers/{serverId}/docker` returns the live Engine header:

```json
{
  "endpoint": "tcp://192.168.100.7:2375",
  "serverVersion": "27.3.1",
  "apiVersion": "1.47",
  "operatingSystem": "Ubuntu 24.04.1 LTS",
  "osType": "linux",
  "architecture": "x86_64",
  "kernelVersion": "6.8.0-45-generic",
  "storageDriver": "overlay2",
  "cpus": 4,
  "memoryBytes": 33654706176,
  "containers": 3,
  "containersRunning": 1,
  "containersPaused": 0,
  "containersStopped": 2,
  "images": 5
}
```

`endpoint` is the Engine API address swallow dials. String fields are empty when the Engine does
not report them.

## Images

### List Images

`GET /api/v1/servers/{serverId}/docker/images`, newest first:

```json
{
  "items": [
    {
      "id": "sha256:3b25b682ea82b2db3cc4fd48db818be788ee3f902ac7378090cf2624ec2442df",
      "repoTags": ["nginx:1.27"],
      "repoDigests": ["nginx@sha256:9f2b..."],
      "sizeBytes": 192004589,
      "createdAt": "2026-09-30T10:00:00Z",
      "dangling": false
    }
  ]
}
```

`repoTags` omits Docker's `<none>:<none>` placeholder; `dangling` is `true` when the image has no
remaining tag.

### Pull Image

`POST /api/v1/servers/{serverId}/docker/images/pull`

```json
{ "reference": "nginx:1.27" }
```

- `reference` is required: `name[:tag]` or `name@digest`, optionally prefixed by a registry host.
  A reference without a tag or digest pulls `latest` (never every tag).
- The pull is synchronous and bounded to 55 minutes (GPU images run to tens of gigabytes); a
  timeout is `503 provider_unavailable`.
- The reference resolves to a registry (see
  [Registry Names](registry-credentials.md#registry-names)). When a
  [Registry Credential](registry-credentials.md) exists for it, api-server sends it to the Engine
  for this pull; otherwise the pull is anonymous. The credential is never written to the host.

On success the response is `200 OK`:

```json
{
  "reference": "harbor.lab.local/team/app:1.4",
  "status": "Status: Downloaded newer image for harbor.lab.local/team/app:1.4",
  "registry": "harbor.lab.local",
  "authenticated": true
}
```

`status` is the Engine's final progress message; `registry` is the resolved registry and
`authenticated` says whether a stored credential was sent. The Engine reports registry refusals
with different statuses: a repository Docker Hub does not know is `404 not_found`, while a private
registry refusing access — no credential, or a rejected one ("401 Unauthorized") — and failures
while downloading surface as `503 provider_unavailable`. In every case `error.message` carries the
registry's own reason, which is what a consumer should show, followed by the credential the pull
used — `(pulled anonymously: no registry credential is saved for docker.io)` or
`(signed in to docker.io as robot)` — because a registry's refusal reads the same either way.

### Remove Image

`DELETE /api/v1/servers/{serverId}/docker/images/{imageId}?force=true`

- `imageId` is the image `id` from the list.
- `force` (default `false`) also removes an image tagged in several repositories or referenced by
  stopped containers. An image used by a running container is `409 conflict` either way.

Response `200 OK`: `{ "success": true }`.

## Containers

### List Containers

`GET /api/v1/servers/{serverId}/docker/containers` lists every container, running or not, by
name:

```json
{
  "items": [
    {
      "id": "4f1c2d0e9b7a...",
      "name": "web",
      "image": "nginx:1.27",
      "imageId": "sha256:3b25b682...",
      "command": "/docker-entrypoint.sh nginx -g 'daemon off;'",
      "state": "running",
      "status": "Up 2 hours",
      "createdAt": "2026-10-02T03:10:00Z",
      "ports": [{ "ip": "0.0.0.0", "privatePort": 80, "publicPort": 8080, "protocol": "tcp" }],
      "networks": ["bridge"],
      "mounts": [
        { "type": "volume", "source": "web-data", "destination": "/usr/share/nginx/html", "readOnly": false }
      ]
    }
  ]
}
```

- `state` is the Docker Engine's value, passed through unchanged: `created | restarting |
  running | removing | paused | exited | dead` (glossary: Docker Host Explorer). `status` is the
  Engine's human-readable summary.
- `publicPort` is `null` for a port that is exposed but not published. `ip` is empty when the
  Engine reports none.
- `mounts[].source` is the volume name for `volume` mounts and the host path for `bind` mounts.

### Create Container

`POST /api/v1/servers/{serverId}/docker/containers`

```json
{
  "name": "web",
  "image": "nginx:1.27",
  "command": [],
  "env": ["NGINX_PORT=80"],
  "ports": [{ "containerPort": 80, "hostPort": 8080, "protocol": "tcp", "hostIp": "" }],
  "volumes": [{ "source": "web-data", "target": "/usr/share/nginx/html", "readOnly": false }],
  "network": "bridge",
  "restartPolicy": "unless-stopped",
  "start": true
}
```

| Field | Required | Rule |
| --- | --- | --- |
| `image` | Yes | Must already be present on the host (pull it first); otherwise `404 not_found`. |
| `name` | No | `[a-zA-Z0-9][a-zA-Z0-9_.-]*`; the Engine generates one when omitted. A taken name is `409 conflict`. |
| `command` | No | Argument vector replacing the image's default command. |
| `env` | No | Each entry `KEY=value` with a non-empty key. |
| `ports` | No | `containerPort` 1–65535; `hostPort` 0–65535 (0 or omitted lets the Engine choose); `protocol` `tcp` (default), `udp`, or `sctp`; `hostIp` optional. |
| `volumes` | No | `source` is a volume name (`[a-zA-Z0-9][a-zA-Z0-9_.-]*`, created on demand) or an absolute host path (bind mount); `target` is an absolute container path. |
| `network` | No | Network to attach to; the Engine default (`bridge`) when omitted. |
| `restartPolicy` | No | `no` (default), `always`, `unless-stopped`, or `on-failure`. |
| `start` | No | Default `true`: start the container after creating it. |

A request that breaks these rules is `400 validation_error` before the Engine is called.

On success the response is `201 Created`:

```json
{ "id": "4f1c2d0e9b7a...", "warnings": [], "started": true }
```

When the container was created but could not be started, the container remains (state
`created`) and the response is the start failure's error, whose message says so.

### Start, Stop, Restart

`POST /api/v1/servers/{serverId}/docker/containers/{containerId}/start|stop|restart`

- A container already in the requested state is a success.
- `stop` and `restart` wait up to 10 seconds for the process to exit before the Engine kills it.

Response `200 OK`: `{ "success": true }`.

### Container Logs

`GET /api/v1/servers/{serverId}/docker/containers/{containerId}/logs?tailLines=200`

- `tailLines` defaults to `200` and is capped at `2000`; a non-positive or non-integer value is
  `400 validation_error`.
- The response is a bounded snapshot (not a stream) of standard output and standard error in
  arrival order, truncated at 4 MiB:

```json
{ "logs": "2026/10/02 03:10:01 [notice] 1#1: start worker processes\n" }
```

### Remove Container

`DELETE /api/v1/servers/{serverId}/docker/containers/{containerId}?force=true&removeVolumes=true`

- `force` (default `false`) kills a running container first; without it, removing a running
  container is `409 conflict`.
- `removeVolumes` (default `false`) also removes the container's anonymous volumes. Named volumes
  are never removed by this route.

Response `200 OK`: `{ "success": true }`.

## Volumes

### List Volumes

`GET /api/v1/servers/{serverId}/docker/volumes`, by name:

```json
{
  "items": [
    {
      "name": "web-data",
      "driver": "local",
      "mountpoint": "/var/lib/docker/volumes/web-data/_data",
      "scope": "local",
      "createdAt": "2026-10-02T03:09:00Z",
      "labels": {}
    }
  ]
}
```

`createdAt` is `null` when the Engine does not report it.

### Create Volume

`POST /api/v1/servers/{serverId}/docker/volumes`

```json
{ "name": "web-data", "driver": "local", "labels": { "team": "web" } }
```

All fields are optional: the Engine generates a name when `name` is omitted (it must match
`[a-zA-Z0-9][a-zA-Z0-9_.-]*` when given), and `driver` defaults to `local`. Response
`201 Created` with the Volume object.

### Remove Volume

`DELETE /api/v1/servers/{serverId}/docker/volumes/{volumeName}?force=true`

A volume used by a container is `409 conflict`. Response `200 OK`: `{ "success": true }`.

## Networks

### List Networks

`GET /api/v1/servers/{serverId}/docker/networks`, by name:

```json
{
  "items": [
    {
      "id": "7d86a1b2c3...",
      "name": "app-net",
      "driver": "bridge",
      "scope": "local",
      "internal": false,
      "attachable": false,
      "predefined": false,
      "subnets": [{ "subnet": "172.20.0.0/16", "gateway": "172.20.0.1" }],
      "createdAt": "2026-10-02T03:08:00Z"
    }
  ]
}
```

`predefined` is `true` for the Engine's own `bridge`, `host`, and `none` networks, which cannot be
removed. `gateway` is empty when none is configured; `createdAt` is `null` when unreported.

### Create Network

`POST /api/v1/servers/{serverId}/docker/networks`

```json
{
  "name": "app-net",
  "driver": "bridge",
  "internal": false,
  "attachable": false,
  "subnet": "172.20.0.0/16",
  "gateway": "172.20.0.1"
}
```

- `name` is required (`[a-zA-Z0-9][a-zA-Z0-9_.-]*`) and must not be `bridge`, `host`, or `none`.
- `driver` defaults to `bridge`.
- `subnet` is an optional CIDR; `gateway` is an optional IP address and requires `subnet`.

Response `201 Created` with the Network object. A taken name is `409 conflict`.

### Remove Network

`DELETE /api/v1/servers/{serverId}/docker/networks/{networkId}`

- `networkId` is the network `id` (a name is also accepted).
- A predefined network is `409 conflict`; so is a network with attached containers.

Response `200 OK`: `{ "success": true }`.

## Compatibility Notes

- Container `state` values belong to the Docker Engine and are listed in the glossary; a newer
  Engine value is passed through and must be displayed as-is, never treated as a failure.
- Adding optional response fields is backward compatible. The route set is deliberately
  opinionated: it is not a transparent Docker API proxy, and new Engine capabilities are added as
  explicit routes.

## Implementation Notes

api-server dials `http://{primary address}:2375` (the port is the domain constant the install
playbook opens) using unversioned Engine API paths, so any Engine version the Docker CE packages
install is served by its own current API version. Requests are bounded to 60 seconds, and image
pulls to 55 minutes. A pull is not tied to the caller's connection: if the client goes away, the
pull still runs to completion or to that bound. When the bound ends a pull, the Engine aborts it;
layers already downloaded stay in the host's content store and a retry reuses them.
