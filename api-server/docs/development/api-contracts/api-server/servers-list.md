# Servers List

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Lists registered Servers with optional filtering and pagination. This is the
inventory surface the dashboard renders.

## Related Glossary Terms

- `Server`
- `Server Status`

## Endpoint / RPC

```text
GET /api/v1/servers/
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

### Query Parameters

| Name | Type | Required | Description |
| --- | --- | -------: | --- |
| `page` | integer | No | Page number; default `1`. See [`conventions.md`](conventions.md). |
| `pageSize` | integer | No | Items per page; default `20`, max `100`. |
| `status` | string | No | Filter by Server Status: `unknown`, `live`, `warning`, `error`, `maintain`, `offline`. |
| `keyword` | string | No | Case-insensitive substring match on `hostname` or `ip`. |

`status` accepts a single value. `keyword` matches either field — it is a search
convenience, not a structured query language.

## Response

### Success Response

`200 OK`, using the shared pagination envelope:

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

This is a **summary shape**. It carries no credentials and no large nested
objects; BMC and SSH configuration are never returned by a list endpoint.

`status` values and their meanings are owned by the `Server Status` glossary
term. `maintain` is the domain value — a consumer may display "Maintenance" but
must send and store `maintain`.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `unauthorized` | 401 | Token is missing, malformed, or expired. |
| `forbidden` | 403 | Caller's role is not `admin`. |

An unknown `status` value or an out-of-range `pageSize` is a client error; the
contract does not currently define a distinct code for it, so consumers must send
only documented values.

## Compatibility Notes

Adding an optional summary field is backward compatible. Adding a credential
field to this response is forbidden by the summary-versus-detail rule, not merely
discouraged.

Widening `status` to accept multiple comma-separated values would be additive,
but must be documented here before consumers rely on it.

## Implementation Notes

Filtering and pagination are pushed to the repository port; the handler only
parses and shapes.
