# API Server HTTP Conventions

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

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

Endpoints that require authentication expect one bearer credential:

```text
Authorization: Bearer <credential>
```

The credential is either:

- an **access token** of a Session, issued by `POST /api/v1/auth/login` or
  `POST /api/v1/auth/refresh` ([auth-login.md](auth-login.md),
  [auth-refresh.md](auth-refresh.md)). It is short-lived (minutes, server-configured).
- an **API Key** secret created through [api-keys.md](api-keys.md). API Key secrets always
  start with `swk_`; access tokens never do.

Rules:

- A missing, malformed, expired, or revoked credential yields `401` with code
  `unauthorized`. An API Key whose owner no longer exists is also `401`.
- Both credentials act with the owning User's role; endpoint contracts state the role they
  require, not the credential type, unless they say otherwise.
- Access tokens are JWTs. Consumers must treat them as opaque and must not parse or depend
  on their claims; `accessTokenExpiresAt` in the issuing response is the only expiry a
  consumer may use.
- A consumer holding a refresh token handles a `401` on an authenticated call by refreshing
  once and retrying the call once; if the refresh also fails it re-authenticates. A consumer
  without a refresh token (an API Key, or an older token) re-authenticates.

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
    "message": "string (human-readable)",
    "requestId": "opaque correlation ID"
  }
}
```

`error.code` is the machine-readable value consumers may branch on.
`error.message` is human-readable and must not be parsed.
`error.requestId` matches the `X-Request-ID` response header and lets an
operator correlate the failure with server logs. Consumers must treat it as opaque.

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | The request is malformed or a required field is missing or invalid. |
| `unauthorized` | 401 | No valid credentials were presented. |
| `forbidden` | 403 | The caller is authenticated but lacks the required role. |
| `not_found` | 404 | The addressed resource does not exist. |
| `conflict` | 409 | The request violates a uniqueness or state constraint. |
| `internal_error` | 500 | An unexpected server-side failure. |
| `provider_unavailable` | 503 | An upstream integration swallow depends on (e.g. the metrics backend) is not configured or cannot be reached. Distinct from `internal_error` so a client can tell "not wired up / upstream down" from "swallow has a bug". |

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
- Enum values in payloads are domain language. They come from swallow's
  glossary, and adding a value is a model change, not just an API change.
