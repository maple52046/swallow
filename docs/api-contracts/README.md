# API Contract

**Version:** 2026-03-27  
**Status:** Living document — update when API boundaries change.

---

## 1. Purpose

This document defines the API contract between the frontend dashboard and the backend control plane. It translates the shared domain glossary and active normalized frontend types into concrete HTTP API resource boundaries, request/response shapes, and conventions.

**Primary audience:** frontend engineers, backend engineers, and AI-generated implementation plans.

**Use this document to:**
- Design backend API routes and payloads
- Ensure frontend integration maps cleanly to domain types
- Resolve naming ambiguities before implementation
- Track unresolved design decisions explicitly

**Do not use this document to:**
- Design database schemas
- Define backend package structure
- Generate OpenAPI specs (that is a separate tooling concern)

---

## 2. Scope

### In Scope (Active Paths)

The following resource groups correspond to active frontend routes and use cases:

- Authentication (login, session)
- Servers (inventory, detail, container workloads)
- Teams (organization, membership)
- Users (authentication, profiles)
- Topology (Datacenter, Room, Rack)
- Provisioning (Image, Profile, Job)
- Alerts (observability, lifecycle management)
- Management Planes (Kubernetes, Slurm)
- GPU Observability (devices, metrics, profiles)
- Access & SSH Keys

### Out of Scope

The following are **excluded** from this contract. They belong to deprecated paths or are not yet ready for backend integration:

- Missions, Runs
- Agents, Models, Plugins
- Audit Events
- Asset domain (`Host`, `StorageDevice`, `NetworkSwitch` — legacy, see Alignment Notes in glossary)
- Analysis API (deferred; see Open Questions)

---

## 3. References

| Document | Role |
|----------|------|
| [`docs/architecture/shared-domain-glossary.md`](./shared-domain-glossary.md) | Canonical domain language |
| `src/domain/server/types.ts` | `Server`, `ServerStatus`, `AllocationState`, `OwnerType` |
| `src/domain/team/types.ts` | `Team` |
| `src/domain/user/types.ts` | `User`, `UserRole`, `UserStatus` |
| `src/domain/workload/types.ts` | `Container`, `WorkloadStatus` |
| `src/domain/topology/types.ts` | `Datacenter`, `Room`, `Rack` |
| `src/domain/alert/types.ts` | `Alert`, `AlertSeverity`, `AlertStatus`, `AlertCategory` |
| `src/domain/gpu/types.ts` | `GPUDevice`, `GPUMetrics`, `GPUStatus` |
| `src/domain/plane/types.ts` | `Plane`, `K8sCluster`, `SlurmCluster` |
| `src/domain/platform/types.ts` | `ProvisioningImage`, `ProvisioningProfile`, `ProvisioningJob` |
| `src/domain/asset/types.ts` | `SSHKey`, `Connection` (active paths only) |

---

## 4. Design Principles

### 4.1 Glossary-Aligned Naming

All resource names, field names, and enum values **must** match the terminology in `shared-domain-glossary.md`.

| Correct | Avoid |
|---------|-------|
| `server` | `host`, `machine`, `node` (non-K8s context) |
| `datacenter` | `site`, `dc` |
| `ownerTeamId` / `ownerUserId` | `assignedTo`, `allocatedTo`, `assignee` |
| `status` | `health` (for operational state) |
| `powerState` | do not use `status` for power |
| `workload` (abstraction) | do not use as synonym for `container` |

### 4.2 Resource-Oriented REST

Design around nouns (resources), not verbs. Use HTTP methods for actions:
- `GET` — read
- `POST` — create, or trigger an action on a sub-resource
- `PATCH` — partial update
- `DELETE` — remove

Named actions (e.g., `acknowledge`, `assign`) use `POST /{resource}/{id}/actions/{action}` or `POST /{resource}/{id}/{action}`.

### 4.3 Summary vs Detail

List endpoints return **summary shapes** (lighter, no credentials, no large nested objects). Detail endpoints return **full shapes**. Never expose credential fields (IPMI password, SSH password) in list responses.

### 4.4 No UI-Only State

API responses must not include:
- Badge colors
- Expanded/collapsed row state
- Column visibility preferences
- Display-only computed strings that belong in the frontend

---

## 5. Global Conventions

### 5.0 Route Prefix

All backend API routes use the prefix `/api/v1`. Examples:

```
POST /api/v1/auth/login
GET  /api/v1/servers
```

This prefix is omitted in the endpoint tables below for readability; all paths should be read as `/api/v1{path}`.

### 5.1 IDs

All IDs are **opaque strings**. The format is backend-defined (UUID, ULID, or similar). Clients must treat IDs as opaque and never parse structure from them.

### 5.2 Timestamps

All timestamps use **ISO 8601** format in UTC:

```
"createdAt": "2026-03-22T09:00:00Z"
```

Optional timestamps (e.g., `completedAt`) are `null` when not set, not omitted.

### 5.3 Null vs Omitted Fields

- **Required fields** are always present in responses.
- **Optional fields that have not been set** are returned as `null`, not omitted.
- **Optional fields that are structurally absent** (e.g., `bmc` on a server with no BMC configured) may be omitted or `null`; backends should be consistent per field.

### 5.4 Enum Values

Enum values in request and response payloads match the domain type literals exactly. Examples:

- `UserRole`: `"admin" | "owner" | "user"`
- `ServerStatus`: `"live" | "warning" | "error" | "maintain" | "offline" | "unknown"`
- `AllocationState`: `"free" | "team" | "user"`
- `AlertSeverity`: `"critical" | "warning" | "info"`
- `WorkloadStatus` (Container `state`): `"running" | "stopped" | "exited" | "created" | "restarting"`

### 5.5 Pagination

List endpoints that may return large result sets support pagination via query parameters:

```
GET /servers?page=1&pageSize=20
```

Response envelope:

```json
{
  "items": [...],
  "total": 142,
  "page": 1,
  "pageSize": 20
}
```

The `items` field contains the result array. `page` defaults to 1; `pageSize` defaults to 20. Endpoints where result sets are small and bounded (e.g., `/datacenters`) may return a plain array without pagination.

### 5.6 Filtering

Filters are passed as query parameters. Parameter names match the field names defined in the glossary and domain types.

```
GET /servers?status=live&allocation=free&datacenterId=dc-01
```

Multiple values for the same filter (OR logic) use comma-separated strings:

```
GET /alerts?severity=critical,warning
```

### 5.7 Error Response

All error responses use the following shape:

```json
{
  "error": {
    "code": "not_found",
    "message": "Server with id 'srv-999' was not found."
  }
}
```

| Field | Type | Notes |
|-------|------|-------|
| `error.code` | `string` | Machine-readable error code (snake_case) |
| `error.message` | `string` | Human-readable description |

Common codes: `not_found`, `validation_error`, `unauthorized`, `forbidden`, `conflict`, `internal_error`.

HTTP status codes map as follows:

| code | HTTP status |
|------|-------------|
| `validation_error` | 400 |
| `unauthorized` | 401 |
| `forbidden` | 403 |
| `not_found` | 404 |
| `conflict` | 409 |
| `internal_error` | 500 |

---

## 6. Resource Model Overview

```
Datacenter
  └── Room (optional)
       └── Rack
            └── Server (via location.rackId)
                 ├── Container (workload)
                 ├── BMC (ipmi / redfish)
                 └── SSH config

Team  ──ownerTeamId──►  Server
User  ──ownerUserId──►  Server
User  ──memberIds────►  Team

Alert ──relatedAssetIds──► Server / GPU

Plane (Kubernetes | Slurm) ── independent integration target

ProvisioningJob ──targetServerId──► Server
ProvisioningJob ──profileId──────► ProvisioningProfile ──imageId──► ProvisioningImage

GPUDevice ──serverId──► Server
GPUMetrics ──gpuId──► GPUDevice

SSHKey ── owned by User (account-scoped)
```

---

## 7. Resource Groups and Endpoints

> **v1 implemented endpoints** are marked with `[v1]`. All others are planned but not yet implemented.

---

### 7.0 Backend v1 Summary

The following endpoints are implemented in backend v1. All require `Authorization: Bearer <token>` except `/auth/login`.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/auth/login` | None | Login; returns access token |
| `GET` | `/api/v1/auth/me` | Bearer | Current user info |
| `POST` | `/api/v1/servers` | Bearer + admin | Register a new server |
| `GET` | `/api/v1/servers` | Bearer + admin | List servers (paginated) |
| `DELETE` | `/api/v1/servers/{id}` | Bearer + admin | Delete a server |

---

### 7.1 Servers

**Base path:** `/servers`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/servers` | List servers (paginated, filterable) `[v1]` |
| `GET` | `/servers/{serverId}` | Get server detail |
| `POST` | `/servers` | Register a new server `[v1]` |
| `PATCH` | `/servers/{serverId}` | Update server fields |
| `DELETE` | `/servers/{serverId}` | Remove a server `[v1]` |
| `POST` | `/servers/{serverId}/assign` | Assign server to a team or user |
| `POST` | `/servers/{serverId}/unassign` | Remove server owner assignment |
| `GET` | `/servers/{serverId}/containers` | List containers running on server |

**`POST /servers` request (v1 — minimal registry):**

```json
{
  "hostname": "new-node-01",
  "ip": "10.0.2.50"
}
```

Both `hostname` and `ip` are required. `hostname` and `ip` must each be unique across all servers.

**`POST /servers` response (201):**

```json
{
  "id": "srv-abc123"
}
```

**`DELETE /servers/{id}` response (200):**

```json
{
  "success": true
}
```

**`GET /servers` v1 list filters:**

| Param | Type | Description |
|-------|------|-------------|
| `status` | `ServerStatus` | Filter by operational status |
| `keyword` | `string` | Match against hostname or ip (case-insensitive substring) |
| `page` | `number` | Page number (default: 1) |
| `pageSize` | `number` | Page size (default: 20) |

**`GET /servers` v1 list response:**

```json
{
  "items": [
    {
      "id": "srv-abc123",
      "hostname": "new-node-01",
      "ip": "10.0.2.50",
      "status": "unknown",
      "createdAt": "2026-03-27T09:00:00Z",
      "updatedAt": "2026-03-27T09:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "pageSize": 20
}
```

**List filters (`GET /servers`):**

| Param | Type | Description |
|-------|------|-------------|
| `status` | `ServerStatus` | Filter by operational status (`live`, `warning`, `error`, `maintain`, `offline`, `unknown`) |
| `allocation` | `"free" \| "assigned"` | Filter by allocation state |
| `datacenterId` | `string` | Filter by datacenter |
| `gpuType` | `string` | Filter by GPU type string |
| `search` | `string` | Full-text search on hostname/ip |
| `page` | `number` | Page number (default: 1) |
| `pageSize` | `number` | Page size (default: 20) |

**`ServerSummaryResponse`** (used in list):

```json
{
  "id": "srv-001",
  "hostname": "gpu-node-01",
  "ip": "10.0.1.10",
  "status": "live",
  "os": "Ubuntu 22.04",
  "cpuCores": 32,
  "ramGB": 128,
  "cpuUsagePct": 42.5,
  "ramUsagePct": 61.0,
  "gpuType": "NVIDIA A100",
  "gpuCount": 8,
  "owner": {
    "type": "team",
    "id": "team-infra",
    "name": "Infra Team"
  },
  "location": {
    "datacenterId": "dc-east",
    "datacenter": "DC East",
    "roomId": "room-01",
    "room": "Server Room A",
    "rackId": "rack-03",
    "rack": "Rack 03"
  },
  "lastSeenAt": "2026-03-22T08:55:00Z",
  "createdAt": "2025-10-01T00:00:00Z",
  "updatedAt": "2026-03-22T08:55:00Z"
}
```

**`ServerSummaryResponse` — newly registered server (status `unknown`):**

```json
{
  "id": "srv-013",
  "hostname": "new-node-01",
  "ip": "10.0.2.50",
  "status": "unknown",
  "os": null,
  "cpuCores": 0,
  "ramGB": 0,
  "cpuUsagePct": 0,
  "ramUsagePct": 0,
  "gpuType": "",
  "gpuCount": 0,
  "owner": null,
  "location": null,
  "lastSeenAt": "2026-03-22T09:00:00Z",
  "createdAt": "2026-03-22T09:00:00Z",
  "updatedAt": "2026-03-22T09:00:00Z"
}
```

> `unknown` is the default status for a newly registered server before any monitoring data has been received. It is distinct from `offline` (confirmed unreachability) and `maintain` (intentional maintenance).

**`ServerDetailResponse`** (same as summary, plus full SSH and BMC config):

```json
{
  "...": "(all summary fields)",
  "ssh": {
    "host": "10.0.1.10",
    "port": 22,
    "username": "admin",
    "keyAuthEnabled": true,
    "passwordAuthEnabled": false
  },
  "bmc": {
    "address": "10.0.1.110",
    "vendor": "Dell iDRAC",
    "ipmi": {
      "host": "10.0.1.110",
      "port": 623,
      "username": "admin"
    },
    "redfish": {
      "host": "10.0.1.110",
      "port": 443,
      "username": "admin"
    }
  }
}
```

> **Note:** Passwords (`ssh.password`, `bmc.ipmi.password`, `bmc.redfish.password`) are **never returned** in API responses. They are write-only fields accepted in create/update requests.

**`OwnerRef`** (embedded in server responses):

```json
{
  "type": "team",
  "id": "team-infra",
  "name": "Infra Team"
}
```

The `owner` field in API responses replaces the separate `ownerTeamId` / `ownerUserId` fields with a unified `OwnerRef`. The `null` case (unassigned) is represented as `"owner": null`.

**`POST /servers/{serverId}/assign` request:**

```json
{
  "ownerType": "team",
  "ownerId": "team-infra"
}
```

---

### 7.2 Authentication

**Base path:** `/auth`

Authentication endpoints handle user login and current user resolution. In backend v1, the login request accepts a raw `password` which is verified server-side using bcrypt.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/auth/login` | Authenticate with username and password; returns JWT access token |
| `GET` | `/auth/me` | Return current authenticated user info (requires Bearer token) |

**`POST /auth/login` request:**

```json
{
  "username": "alice",
  "password": "my-raw-password"
}
```

> **v1 note:** The backend receives the raw password and performs bcrypt verification. The password is never stored or returned. Future frontend integration may pre-hash before transmission; that decision is deferred.

**`POST /auth/login` response (200):**

```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

The access token is a signed JWT (HS256) containing:

| Claim | Value |
|-------|-------|
| `sub` | user ID (string) |
| `username` | username string |
| `role` | `UserRole` value |
| `exp` | expiry (Unix timestamp) |

**Error (401) — invalid credentials:**

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Invalid credentials."
  }
}
```

---

**`GET /auth/me` request:**

Header: `Authorization: Bearer <accessToken>`

**`GET /auth/me` response (200):**

```json
{
  "id": "user-abc123",
  "username": "alice",
  "role": "admin"
}
```

**Error (401) — missing or invalid token:**

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Missing or invalid token."
  }
}
```

---

### 7.4 Teams

**Base path:** `/teams`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/teams` | List all teams |
| `GET` | `/teams/{teamId}` | Get team detail |
| `POST` | `/teams` | Create a team |
| `PATCH` | `/teams/{teamId}` | Update team name/description |
| `DELETE` | `/teams/{teamId}` | Delete a team |
| `POST` | `/teams/{teamId}/members` | Add a member |
| `DELETE` | `/teams/{teamId}/members/{userId}` | Remove a member |
| `POST` | `/teams/{teamId}/owners` | Add a team owner (manager) |
| `DELETE` | `/teams/{teamId}/owners/{userId}` | Remove a team owner |

**`TeamResponse`:**

```json
{
  "id": "team-infra",
  "name": "Infra Team",
  "description": "Infrastructure and platform team",
  "ownerIds": ["user-alice", "user-bob"],
  "memberIds": ["user-alice", "user-bob", "user-carol"],
  "createdAt": "2025-08-01T00:00:00Z",
  "updatedAt": "2026-01-15T00:00:00Z"
}
```

**`POST /teams` request:**

```json
{
  "name": "Infra Team",
  "description": "Infrastructure and platform team",
  "ownerIds": ["user-alice"],
  "memberIds": ["user-alice"]
}
```

---

### 7.4 Users

**Base path:** `/users`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/users` | List all users |
| `GET` | `/users/{userId}` | Get user detail |
| `PATCH` | `/users/{userId}/role` | Update user role |
| `PATCH` | `/users/{userId}/status` | Enable or disable user |

> User creation and password management are handled by the authentication system, not this resource endpoint.

**`UserResponse`:**

```json
{
  "id": "user-alice",
  "username": "alice",
  "displayName": "Alice Chen",
  "role": "owner",
  "status": "active"
}
```

**`PATCH /users/{userId}/role` request:**

```json
{
  "role": "member"
}
```

---

### 7.5 Topology

**Base paths:** `/datacenters`, `/rooms`, `/racks`

#### Datacenters

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/datacenters` | List all datacenters |
| `GET` | `/datacenters/{datacenterId}` | Get datacenter detail |
| `POST` | `/datacenters` | Create a datacenter |
| `PATCH` | `/datacenters/{datacenterId}` | Update datacenter |
| `DELETE` | `/datacenters/{datacenterId}` | Delete a datacenter |
| `GET` | `/datacenters/{datacenterId}/rooms` | List rooms in datacenter |
| `GET` | `/datacenters/{datacenterId}/racks` | List racks in datacenter |

**`DatacenterResponse`:**

```json
{
  "id": "dc-east",
  "name": "DC East",
  "code": "DC-E",
  "location": "Tainan, Taiwan",
  "description": "Primary production datacenter",
  "createdAt": "2024-01-01T00:00:00Z",
  "updatedAt": "2024-01-01T00:00:00Z"
}
```

#### Rooms

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/rooms/{roomId}` | Get room detail |
| `POST` | `/datacenters/{datacenterId}/rooms` | Create a room |
| `PATCH` | `/rooms/{roomId}` | Update room |
| `DELETE` | `/rooms/{roomId}` | Delete a room |

**`RoomResponse`:**

```json
{
  "id": "room-01",
  "datacenterId": "dc-east",
  "name": "Server Room A",
  "floor": "B1",
  "description": null,
  "createdAt": "2024-01-01T00:00:00Z",
  "updatedAt": "2024-01-01T00:00:00Z"
}
```

#### Racks

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/racks/{rackId}` | Get rack detail |
| `POST` | `/datacenters/{datacenterId}/racks` | Create a rack |
| `PATCH` | `/racks/{rackId}` | Update rack |
| `DELETE` | `/racks/{rackId}` | Delete a rack |

**`RackResponse`:**

```json
{
  "id": "rack-03",
  "datacenterId": "dc-east",
  "roomId": "room-01",
  "name": "Rack 03",
  "rackUnit": 42,
  "description": null,
  "createdAt": "2024-01-01T00:00:00Z",
  "updatedAt": "2024-01-01T00:00:00Z"
}
```

> `roomId` is `null` for racks not assigned to any room (unassigned racks).

---

### 7.6 Provisioning

**Base path:** `/provisioning`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/provisioning/images` | List provisioning images |
| `GET` | `/provisioning/images/{imageId}` | Get image detail |
| `GET` | `/provisioning/profiles` | List provisioning profiles |
| `GET` | `/provisioning/profiles/{profileId}` | Get profile detail |
| `POST` | `/provisioning/profiles` | Create a provisioning profile |
| `GET` | `/provisioning/jobs` | List provisioning jobs (filterable) |
| `GET` | `/provisioning/jobs/{jobId}` | Get job detail (includes logs) |
| `POST` | `/provisioning/jobs` | Start a provisioning job |

**List filters (`GET /provisioning/jobs`):**

| Param | Type | Description |
|-------|------|-------------|
| `status` | `ProvisioningJobStatus` | Filter by job status |
| `targetServerId` | `string` | Filter by target server |
| `profileId` | `string` | Filter by profile |

**`ProvisioningImageResponse`:**

```json
{
  "id": "img-ubuntu-22",
  "name": "Ubuntu 22.04 LTS",
  "os": "Ubuntu",
  "version": "22.04",
  "arch": "x86_64",
  "size": "2.1GB",
  "tags": ["lts", "server"],
  "createdAt": "2025-06-01T00:00:00Z"
}
```

**`ProvisioningProfileResponse`:**

```json
{
  "id": "prof-gpu-baseline",
  "name": "GPU Baseline",
  "description": "Standard CUDA + monitoring setup",
  "imageId": "img-ubuntu-22",
  "imageName": "Ubuntu 22.04 LTS",
  "packages": ["cuda-toolkit", "nvidia-fabricmanager"],
  "scripts": ["install-dcgm.sh"],
  "targetVendors": ["NVIDIA"],
  "createdAt": "2025-06-15T00:00:00Z",
  "updatedAt": "2025-06-15T00:00:00Z"
}
```

**`ProvisioningJobResponse`:**

```json
{
  "id": "job-001",
  "profileId": "prof-gpu-baseline",
  "profileName": "GPU Baseline",
  "targetServerId": "srv-001",
  "targetServerName": "gpu-node-01",
  "status": "running",
  "progress": 62,
  "startedAt": "2026-03-22T08:00:00Z",
  "completedAt": null,
  "createdAt": "2026-03-22T07:59:00Z",
  "logs": [
    "[08:00:01] Starting MAAS provision sequence",
    "[08:00:15] Downloading image..."
  ]
}
```

**`POST /provisioning/jobs` request:**

```json
{
  "profileId": "prof-gpu-baseline",
  "targetServerId": "srv-001"
}
```

---

### 7.7 Alerts

**Base path:** `/alerts`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/alerts` | List alerts (filterable, paginated) |
| `GET` | `/alerts/{alertId}` | Get alert detail |
| `POST` | `/alerts/{alertId}/acknowledge` | Acknowledge an alert |
| `POST` | `/alerts/{alertId}/resolve` | Resolve an alert |

**List filters (`GET /alerts`):**

| Param | Type | Description |
|-------|------|-------------|
| `status` | `AlertStatus` | Filter by lifecycle status |
| `severity` | `AlertSeverity` | Filter by severity (comma-separated for multiple) |
| `category` | `AlertCategory` | Filter by category |
| `search` | `string` | Full-text search on title/message |
| `limit` | `number` | Max results (for lightweight polling) |

**`AlertResponse`:**

```json
{
  "id": "alert-001",
  "title": "GPU Critical Temperature",
  "message": "GPU 0 on gpu-node-01 has exceeded 90°C threshold.",
  "severity": "critical",
  "status": "active",
  "category": "gpu",
  "relatedAssetIds": ["srv-001"],
  "relatedAssetNames": ["gpu-node-01"],
  "createdAt": "2026-03-22T09:00:00Z",
  "acknowledgedAt": null,
  "resolvedAt": null,
  "acknowledgedBy": null
}
```

**`POST /alerts/{alertId}/acknowledge` request:**

```json
{
  "actor": "alice"
}
```

---

### 7.8 Management Planes

**Base path:** `/planes`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/planes` | List all registered planes |
| `GET` | `/planes/{planeId}` | Get plane detail (type-specific) |
| `POST` | `/planes` | Register a new plane |
| `PATCH` | `/planes/{planeId}` | Update plane metadata |
| `DELETE` | `/planes/{planeId}` | Deregister a plane |

**`PlaneSummaryResponse`:**

```json
{
  "id": "plane-k8s-prod",
  "name": "Production K8s",
  "type": "kubernetes",
  "status": "connected",
  "version": "1.29.2",
  "registeredAt": "2025-11-01T00:00:00Z",
  "lastSyncAt": "2026-03-22T09:00:00Z"
}
```

**`PlaneDetailResponse`** is discriminated by `type`.

For `type: "kubernetes"`:

```json
{
  "id": "plane-k8s-prod",
  "name": "Production K8s",
  "type": "kubernetes",
  "status": "connected",
  "endpointRef": "k8s-prod-endpoint",
  "authRef": "k8s-prod-kubeconfig",
  "labels": ["production", "gpu"],
  "version": "1.29.2",
  "registeredAt": "2025-11-01T00:00:00Z",
  "lastSyncAt": "2026-03-22T09:00:00Z",
  "nodeCount": 24,
  "gpuNodeCount": 16,
  "nodes": [
    {
      "name": "k8s-gpu-node-01",
      "status": "ready",
      "role": "worker",
      "gpuCount": 8,
      "cpuCount": 32,
      "memoryGB": 256,
      "osImage": "Ubuntu 22.04",
      "kubeletVersion": "v1.29.2"
    }
  ],
  "addons": [
    {
      "name": "nvidia-device-plugin",
      "version": "0.14.5",
      "status": "healthy",
      "latestVersion": "0.15.0"
    }
  ]
}
```

For `type: "slurm"`:

```json
{
  "id": "plane-slurm-hpc",
  "name": "HPC Slurm Cluster",
  "type": "slurm",
  "status": "connected",
  "...": "(base Plane fields)",
  "partitions": [
    {
      "name": "gpu",
      "state": "up",
      "nodeCount": 8,
      "idleNodes": 3,
      "allocNodes": 5,
      "totalCPUs": 256,
      "totalGPUs": 64
    }
  ],
  "totalNodes": 8,
  "idleNodes": 3,
  "allocNodes": 5,
  "recentJobs": []
}
```

**`POST /planes` request:**

```json
{
  "name": "Production K8s",
  "type": "kubernetes",
  "endpointRef": "k8s-prod-endpoint",
  "authRef": "k8s-prod-kubeconfig",
  "labels": ["production"]
}
```

---

### 7.9 GPU Observability

**Base path:** `/gpus`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/gpus` | List GPU devices (filterable) |
| `GET` | `/gpus/{gpuId}` | Get GPU device detail |
| `GET` | `/gpus/{gpuId}/metrics` | Get latest metrics for a GPU |
| `GET` | `/gpus/metrics` | Get metrics for multiple GPUs |
| `GET` | `/gpus/profiles` | List GPU profiling sessions |
| `GET` | `/gpus/profiles/{profileId}` | Get profiling session detail |

**List filters (`GET /gpus`):**

| Param | Type | Description |
|-------|------|-------------|
| `vendor` | `GPUVendor` | Filter by `nvidia` or `amd` |
| `status` | `GPUStatus` | Filter by status |
| `datacenter` | `string` | Filter by datacenter name |
| `serverId` | `string` | Filter by server |
| `search` | `string` | Search by model/serial/uuid |

**`GPUDeviceResponse`:**

```json
{
  "id": "gpu-001",
  "serverId": "srv-001",
  "serverName": "gpu-node-01",
  "index": 0,
  "vendor": "nvidia",
  "model": "A100-SXM4-80GB",
  "serial": "SN-GPU-001",
  "uuid": "GPU-abc123",
  "driverVersion": "535.104.12",
  "cudaVersion": "12.2",
  "rocmVersion": null,
  "status": "healthy",
  "datacenter": "DC East",
  "rack": "Rack 03",
  "memoryGB": 80
}
```

**`GPUMetricsResponse`:**

```json
{
  "gpuId": "gpu-001",
  "utilization": 87.5,
  "memoryUsedMB": 71680,
  "memoryTotalMB": 81920,
  "temperatureC": 72,
  "powerDrawW": 380,
  "powerLimitW": 400,
  "fanSpeedPct": null,
  "eccErrors": 0,
  "xidErrors": 0,
  "throttling": false,
  "timestamp": "2026-03-22T09:00:00Z"
}
```

**`GET /gpus/metrics` query:**

```
GET /gpus/metrics?gpuIds=gpu-001,gpu-002,gpu-003
```

Returns an array of `GPUMetricsResponse`.

---

### 7.10 Containers (Workloads)

Containers are accessed as a sub-resource of Server.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/servers/{serverId}/containers` | List containers on server |
| `POST` | `/servers/{serverId}/containers/{containerId}/start` | Start a container |
| `POST` | `/servers/{serverId}/containers/{containerId}/stop` | Stop a container |
| `POST` | `/servers/{serverId}/containers/{containerId}/restart` | Restart a container |

**`ContainerResponse`:**

```json
{
  "id": "ctr-001",
  "name": "training-job",
  "image": "pytorch/pytorch:2.3.0-cuda12.1-cudnn8-runtime",
  "state": "running",
  "restarts": 1,
  "startedAt": "2026-03-21T10:00:00Z"
}
```

**List response example (`GET /servers/srv-001/containers`):**

```json
[
  {
    "id": "ctr-001",
    "name": "training-job",
    "image": "pytorch/pytorch:2.3.0-cuda12.1-cudnn8-runtime",
    "state": "running",
    "restarts": 1,
    "startedAt": "2026-03-21T10:00:00Z"
  }
]
```

> Containers are returned as a plain array (not paginated) because a single server typically runs a bounded number of containers.

---

### 7.11 SSH Keys

SSH Keys are user-scoped and managed via account settings.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/ssh-keys` | List current user's SSH keys |
| `POST` | `/ssh-keys` | Import a new SSH key |
| `DELETE` | `/ssh-keys/{keyId}` | Delete an SSH key |

**`SSHKeyResponse`:**

```json
{
  "id": "key-001",
  "name": "work-laptop",
  "fingerprint": "SHA256:abc123...",
  "createdAt": "2025-12-01T00:00:00Z"
}
```

> `publicKey` is accepted in the import request but is not returned in list/detail responses. `vaultRef` is backend-internal and not exposed to the frontend.

**`POST /ssh-keys` request:**

```json
{
  "name": "work-laptop",
  "publicKey": "ssh-ed25519 AAAA... alice@workstation"
}
```

---

## 8. Filtering and Sorting Summary

| Resource | Supported Filters | Supported Sort |
|----------|------------------|----------------|
| `GET /servers` | `status`, `allocation`, `datacenterId`, `gpuType`, `search` | `hostname`, `status`, `createdAt` |
| `GET /alerts` | `status`, `severity`, `category`, `search`, `limit` | `severity`, `createdAt` |
| `GET /provisioning/jobs` | `status`, `targetServerId`, `profileId` | `createdAt`, `status` |
| `GET /gpus` | `vendor`, `status`, `datacenter`, `serverId`, `search` | `model`, `status` |
| `GET /planes` | `type`, `status` | `name`, `registeredAt` |

Sort is specified via `sortBy=<field>&sortOrder=asc|desc` query parameters.

---

## 9. Frontend Normalized Type Mapping

The API contract is designed so that API responses map directly to frontend normalized types without ad-hoc transformation.

| API Response Shape | Frontend Normalized Type | Notes |
|-------------------|------------------------|-------|
| `ServerSummaryResponse` | `Server` (partial) | `owner` in API → `ownerTeamId`/`ownerUserId` on frontend type |
| `ServerDetailResponse` | `Server` | Full mapping including `bmc`, `ssh` |
| `ContainerResponse` | `Container` | Direct field-for-field mapping |
| `TeamResponse` | `Team` | Direct mapping |
| `UserResponse` | `User` | Direct mapping |
| `DatacenterResponse` | `Datacenter` | Direct mapping |
| `RoomResponse` | `Room` | Direct mapping |
| `RackResponse` | `Rack` | Direct mapping |
| `AlertResponse` | `Alert` | Direct mapping |
| `GPUDeviceResponse` | `GPUDevice` | Direct mapping |
| `GPUMetricsResponse` | `GPUMetrics` | Direct mapping |
| `PlaneSummaryResponse` | `Plane` | Direct mapping |
| `K8sPlaneDetailResponse` | `K8sCluster` | Direct mapping |
| `SlurmPlaneDetailResponse` | `SlurmCluster` | Direct mapping |
| `ProvisioningJobResponse` | `ProvisioningJob` | Direct mapping |

### Owner Field Note

The API uses a unified `OwnerRef` object (`{ type, id, name }`) where the frontend domain type uses separate `ownerTeamId` / `ownerUserId` fields. The frontend adapter layer should expand `OwnerRef` into the appropriate fields when mapping API responses to `Server` domain types.

---

## 10. Open Questions / Deferred Items

### 10.1 Analysis API

The Analysis Dashboard (`/analysis`) requires aggregated usage data (GPU utilization by team/user, trends, top ranking). This API is not yet designed. Deferred pending:
- Backend Prometheus proxy design
- Scope model (`global | team | user`) parameter contract
- Time range and resolution parameter contract

### 10.2 Container Lifecycle Actions

Container start/stop/restart endpoints (`/servers/{id}/containers/{id}/start` etc.) are proposed above but need:
- Error behavior on non-existent containers
- Async vs synchronous response decision
- Potential WebSocket or polling mechanism for state tracking

### 10.3 SSH Key Vault Integration

The `SSHKey.vaultRef` field references a secret backend (Vault or similar). The specific vault integration contract (key path format, access token scope) is deferred to the access management track.

### 10.4 Pagination Defaults

Default `page` and `pageSize` values need confirmation. Suggested: `page=1`, `pageSize=20`, max `pageSize=100`.

### 10.5 Server Container Creation

`POST /servers/{serverId}/containers` (creating a new container) is not yet specified. The `CreateContainerPage` in the frontend collects: name, image, command, environment variables, GPU allocation, volume mounts, restart policy. This endpoint design is deferred to the workload management track.

### 10.6 Real-time Updates

The current design is request/response only. Future consideration: WebSocket or SSE for live server status, container state changes, and alert streams.

---

## 11. Alignment Notes

### 11.1 `OwnerRef` vs Separate Owner Fields

The frontend `Server` type uses `ownerTeamId: string | null` and `ownerUserId: string | null`. The API contract proposes a unified `owner: OwnerRef | null` to reduce ambiguity. The frontend repository adapter is responsible for translating between the two shapes.

### 11.2 `ServerLocation` Display Strings

`ServerLocation` includes both `id`-based fields (authoritative) and display name strings (`datacenter`, `room`, `rack`). The backend should resolve and return display name strings at query time — they must not be stored on the Server record and should not be treated as authoritative references.

### 11.3 SSH Credentials Are Write-Only

No API response shape in this contract returns `ssh.password`, `bmc.ipmi.password`, or `bmc.redfish.password`. These fields are accepted in create/update requests but are never surfaced in responses.

### 11.4 `GPUProfile.runId` and `GPUProfile.missionId`

These fields link profiling sessions to Missions and Runs (deprecated paths). For the active frontend, these fields are present in the type but not rendered. The backend may include them as nullable fields for future use without breaking the active contract.
