# Implemented API Reference

**Last updated:** 2026-05-02  
**Base URL prefix:** `/api/v1`  
**Status:** Documents only routes that are fully implemented and registered in `cmd/api/main.go`.

---

## Global Conventions

### Authentication

Endpoints marked **Auth required** expect the following request header:

```
Authorization: Bearer <accessToken>
```

Missing or invalid token → `401 unauthorized`  
Non-admin role on admin-only endpoint → `403 forbidden`

### Error Response Shape

All error responses share a single envelope:

```json
{
  "error": {
    "code": "string (snake_case)",
    "message": "string (human-readable)"
  }
}
```

| `error.code`       | HTTP Status |
|--------------------|-------------|
| `validation_error` | 400         |
| `unauthorized`     | 401         |
| `forbidden`        | 403         |
| `not_found`        | 404         |
| `conflict`         | 409         |
| `internal_error`   | 500         |

### Timestamps

All timestamp fields use ISO 8601 UTC format: `"2026-05-02T15:00:00Z"`

### Pagination

List endpoints that support pagination accept these query parameters:

| Param      | Type    | Default | Max |
|------------|---------|---------|-----|
| `page`     | integer | `1`     | —   |
| `pageSize` | integer | `20`    | `100` |

Paginated response envelope:

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "pageSize": 20
}
```

---

## Auth

### POST /api/v1/auth/login

Login and obtain an access token.

- **Auth required:** No

**Request Body**

```json
{
  "username": "string (required)",
  "password": "string (required)"
}
```

**Response `200 OK`**

```json
{
  "accessToken": "string (JWT)"
}
```

**Error Cases**

| Condition                        | Status | `error.code`       |
|----------------------------------|--------|--------------------|
| Missing `username` or `password` | 400    | `validation_error` |
| Wrong credentials / user not found | 401  | `unauthorized`     |

---

### GET /api/v1/auth/me

Return the currently authenticated user.

- **Auth required:** Yes (any role)

**Response `200 OK`**

```json
{
  "id": "string",
  "username": "string",
  "role": "admin | owner | user"
}
```

**Error Cases**

| Condition               | Status | `error.code`   |
|-------------------------|--------|----------------|
| Missing / invalid token | 401    | `unauthorized` |
| User deleted from DB    | 404    | `not_found`    |

---

## Servers

> All server endpoints require `role: admin`. Non-admin tokens receive `403 forbidden`.

### POST /api/v1/servers/

Create a new server entry.

- **Auth required:** Yes — **Admin only**

**Request Body**

```json
{
  "hostname": "string (required, must be unique)",
  "ip":       "string (required, must be unique)"
}
```

**Response `201 Created`**

```json
{
  "id": "string (opaque server ID)"
}
```

**Error Cases**

| Condition                  | Status | `error.code`       |
|----------------------------|--------|--------------------|
| Missing `hostname` or `ip` | 400    | `validation_error` |
| Missing / invalid token    | 401    | `unauthorized`     |
| Role is not admin          | 403    | `forbidden`        |
| `hostname` already exists  | 409    | `conflict`         |
| `ip` already exists        | 409    | `conflict`         |

---

### GET /api/v1/servers/

List servers with optional filtering and pagination.

- **Auth required:** Yes — **Admin only**

**Query Parameters**

| Param      | Type    | Required | Description                                                                               |
|------------|---------|----------|-------------------------------------------------------------------------------------------|
| `page`     | integer | No       | Page number, default `1`                                                                  |
| `pageSize` | integer | No       | Items per page, default `20`, max `100`                                                   |
| `status`   | string  | No       | Filter by status: `unknown \| live \| warning \| error \| maintain \| offline`           |
| `keyword`  | string  | No       | Case-insensitive substring match on `hostname` or `ip`                                   |

**Response `200 OK`**

```json
{
  "items": [
    {
      "id":        "string",
      "hostname":  "string",
      "ip":        "string",
      "status":    "unknown | live | warning | error | maintain | offline",
      "createdAt": "2026-05-02T15:00:00Z",
      "updatedAt": "2026-05-02T15:00:00Z"
    }
  ],
  "total":    0,
  "page":     1,
  "pageSize": 20
}
```

**Server `status` Values**

| Value      | Meaning                              |
|------------|--------------------------------------|
| `unknown`  | Status not yet determined (default)  |
| `live`     | Operational                          |
| `warning`  | Operational with warnings            |
| `error`    | In error state                       |
| `maintain` | Under maintenance                    |
| `offline`  | Unreachable / powered off            |

**Error Cases**

| Condition               | Status | `error.code`   |
|-------------------------|--------|----------------|
| Missing / invalid token | 401    | `unauthorized` |
| Role is not admin       | 403    | `forbidden`    |

---

### DELETE /api/v1/servers/:id

Delete a server by ID.

- **Auth required:** Yes — **Admin only**

**Path Parameters**

| Param | Type   | Description               |
|-------|--------|---------------------------|
| `id`  | string | Opaque server ID (required) |

**Response `200 OK`**

```json
{
  "success": true
}
```

**Error Cases**

| Condition               | Status | `error.code`   |
|-------------------------|--------|----------------|
| Missing / invalid token | 401    | `unauthorized` |
| Role is not admin       | 403    | `forbidden`    |
| Server ID not found     | 404    | `not_found`    |

---

## Not Yet Implemented

The following resource groups are defined in `docs/architecture/api-contract.md` but have **no registered routes** in the current backend. Do not attempt to call these from the frontend:

- Teams
- Users (management)
- Topology (Datacenter / Room / Rack)
- Provisioning (Image / Profile / Job)
- Alerts
- Management Planes (Kubernetes / Slurm)
- GPU Observability
- Access & SSH Keys
