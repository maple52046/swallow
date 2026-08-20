# Servers Delete

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Removes a Server from the platform registry by ID.

## Related Glossary Terms

- `Server`

## Endpoint / RPC

```text
DELETE /api/v1/servers/{id}
```

## Authentication

Bearer token required. See [`conventions.md`](conventions.md).

## Authorization

`admin` only. A non-admin authenticated caller receives `403 forbidden`.

## Request

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Authorization` | Yes | Bearer token. |

### Path Parameters

| Name | Type | Required | Description |
| --- | --- | -------: | --- |
| `id` | string | Yes | Opaque Server ID. |

No query parameters or body.

## Response

### Success Response

`200 OK`

```json
{
  "success": true
}
```

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `unauthorized` | 401 | Token is missing, malformed, or expired. |
| `forbidden` | 403 | Caller's role is not `admin`. |
| `not_found` | 404 | No Server exists with the given ID. |

Deleting an already-deleted Server returns `404`, not `200`. This endpoint is not
idempotent in its response, only in its effect.

## Compatibility Notes

The deletion semantics are not yet specified beyond registry removal. Before
implementing or relying on any of the following, this contract must be extended:

- whether deletion is hard or soft,
- what happens to a connected `agent` whose node record is deleted,
- what happens to workloads, alerts, or GPU records that reference the Server.

Do not infer any of these from the implementation.

## Implementation Notes

The handler maps the domain `ErrServerNotFound` to `404 not_found`; the driver's
"no documents" error never reaches the transport layer.
