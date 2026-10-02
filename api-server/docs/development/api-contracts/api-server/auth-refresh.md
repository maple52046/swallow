# Auth Refresh

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Exchanges a Session's refresh token for a new access token, so a client renews its
credential without asking the user for a password again. Every successful refresh also
replaces the refresh token (rotation). See
[decision 042](../../../../../docs/decisions/042-authentication-sessions-and-api-keys.md).

## Related Glossary Terms

- Session
- `User` (pending definition in swallow's glossary)

## Endpoint / RPC

```text
POST /api/v1/auth/refresh
```

## Authentication

The refresh token itself, presented one of two ways. No `Authorization` header is needed;
one that is present is ignored.

- **Cookie** (browsers): the `swallow_refresh` cookie set by
  [auth-login.md](auth-login.md). Send the request with credentials included and no body
  (or an empty JSON object).
- **Body** (CLI and other non-browser clients): `{ "refreshToken": "…" }`.

When both are present the body wins.

## Authorization

None beyond holding a valid refresh token. The new access token carries the User's current
role.

## Request

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Content-Type` | When a body is sent | `application/json`. |
| `Cookie` | For cookie delivery | `swallow_refresh=<refresh token>`. |

### Body

```json
{
  "refreshToken": "string (optional; required unless the cookie is sent)"
}
```

## Response

### Success Response

`200 OK`

```json
{
  "accessToken": "string (JWT)",
  "accessTokenExpiresAt": "2026-10-02T03:30:00Z",
  "refreshToken": "string (only for body delivery, and only when rotated)"
}
```

The response uses the same delivery the request used:

- Cookie delivery: a new `swallow_refresh` cookie is set with the same attributes as at login
  (the idle limit restarts); the body has no `refreshToken`.
- Body delivery: the body carries the new `refreshToken`. The client must replace the stored
  token with it.

**Rotation grace.** If the presented refresh token was replaced less than 30 seconds earlier
(another tab or CLI process refreshed first), the response contains a new access token but
**no** new refresh token and sets no cookie: the client keeps the newest refresh token it
already has (a browser's cookie jar already holds it). This lets concurrent clients of one
Session refresh without logging each other out.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | The body is not valid JSON. |
| `unauthorized` | 401 | No refresh token was presented, or it is unknown, expired (idle limit or maximum age), revoked by logout, or the Session's User no longer exists. Also returned — and the whole Session revoked — when a refresh token replaced more than 30 seconds ago is presented again, which indicates it was copied. With cookie delivery the response clears the cookie. |

A client that receives `401` from this endpoint must discard its tokens and send the user to
login (or tell the CLI user to run `swallow login`).

## Compatibility Notes

The refresh token format is opaque and may change. The 30-second grace and the default
lifetimes are server configuration of this contract's behavior, not wire fields; clients use
`accessTokenExpiresAt` and react to `401` rather than hard-coding them.

## Implementation Notes

swallow stores only one-way hashes of the current and the immediately previous refresh token
per Session. Expired Sessions are removed automatically.
