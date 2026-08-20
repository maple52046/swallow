# API Server HTTP Conventions

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Defines the conventions shared by every HTTP REST contract owned by
`api-server`: base path, authentication scheme, error envelope, timestamp
format, and the pagination envelope.

Individual endpoint contracts reference this document instead of repeating these
rules, and list only the error cases specific to that endpoint. A change here is a
change to every HTTP contract and must be treated as a compatibility event.

## Base Path

All HTTP routes are served under the prefix `/api/v1`. Endpoint contracts write
the full path including the prefix.

## Authentication

Endpoints that require authentication expect a bearer token issued by
`POST /api/v1/auth/login`:

```text
Authorization: Bearer <accessToken>
```

- A missing, malformed, or expired token yields `401` with code `unauthorized`.
- The token is a JWT. Consumers must treat it as opaque and must not parse or
  depend on its claims.

## Authorization

Authorization is role-based. The roles are `admin`, `owner`, and `user`.

- An endpoint contract states the required role explicitly.
- An authenticated caller whose role is insufficient yields `403` with code
  `forbidden`. It must never yield `404` to hide the resource unless the
  endpoint contract says so.

## Error Response

Every error response uses one envelope:

```json
{
  "error": {
    "code": "string (snake_case)",
    "message": "string (human-readable)"
  }
}
```

`error.code` is the machine-readable value consumers may branch on.
`error.message` is human-readable and must not be parsed.

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | The request is malformed or a required field is missing or invalid. |
| `unauthorized` | 401 | No valid credentials were presented. |
| `forbidden` | 403 | The caller is authenticated but lacks the required role. |
| `not_found` | 404 | The addressed resource does not exist. |
| `conflict` | 409 | The request violates a uniqueness or state constraint. |
| `internal_error` | 500 | An unexpected server-side failure. |

Adding a new `error.code` is a contract change: add it here first, then in the
endpoint contract that returns it.

## Timestamps

All timestamp fields use ISO 8601 in UTC, for example `"2026-05-02T15:00:00Z"`.

## Pagination

List endpoints that may return large result sets accept:

| Param | Type | Required | Default | Max |
| --- | --- | ---: | --- | --- |
| `page` | integer | No | `1` | — |
| `pageSize` | integer | No | `20` | `100` |

and return this envelope:

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "pageSize": 20
}
```

`items` holds the result array; `total` is the number of matching records across
all pages. Endpoints whose result sets are small and bounded may return a plain
array, and must say so in their own contract.

## Compatibility Notes

- Adding an optional response field is backward compatible. Removing or renaming
  a field, changing a field's type, or changing an `error.code` mapping is
  breaking and requires a `BREAKING CHANGE` commit plus a consumer migration
  note.
- Enum values in payloads are domain language. They come from the platform
  glossary, and adding a value is a model change, not just an API change.
