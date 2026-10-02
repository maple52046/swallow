# Auth Login

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)
- Swallow installation tooling (post-install bootstrap and `swallowctl doctor`)

## Purpose

Exchanges a username and password for a new **Session**: a short-lived access token and a
refresh token that obtains new access tokens through [auth-refresh.md](auth-refresh.md).
This is the only endpoint that creates a Session; [API Keys](api-keys.md) are the
non-interactive alternative. See
[decision 042](../../../../../docs/decisions/042-authentication-sessions-and-api-keys.md).

## Related Glossary Terms

- Session
- `User` (pending definition in swallow's glossary)

## Endpoint / RPC

```text
POST /api/v1/auth/login
```

## Authentication

None. This endpoint issues credentials and must remain reachable without a token.

## Authorization

None. Any caller may attempt to log in. The response must not reveal whether a
username exists.

## Request

Shared conventions: [`conventions.md`](conventions.md).

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Content-Type` | Yes | `application/json`. |

### Body

```json
{
  "username": "string (required)",
  "password": "string (required)",
  "refreshTokenDelivery": "cookie | body (optional, default cookie)"
}
```

`refreshTokenDelivery` chooses how the refresh token reaches the client:

- `cookie` (default, for browsers): the response sets the refresh cookie described below and
  the body carries no `refreshToken`. Page scripts can never read it.
- `body` (for the CLI and other non-browser clients): the body carries `refreshToken` and no
  cookie is set. The client must store it as a secret.

Any other value is `400 validation_error`.

## Response

### Success Response

`200 OK`

```json
{
  "accessToken": "string (JWT)",
  "accessTokenExpiresAt": "2026-10-02T03:15:00Z",
  "refreshToken": "string (only when refreshTokenDelivery is body)"
}
```

- `accessToken` is sent as `Authorization: Bearer <accessToken>` (see
  [`conventions.md`](conventions.md)). It is opaque; its lifetime is server-configured
  (default 15 minutes).
- `accessTokenExpiresAt` is when the access token stops being accepted. Consumers may refresh
  shortly before it instead of waiting for a `401`.
- `refreshToken` is an opaque secret. It is valid until the Session ends: after the idle limit
  without a refresh (default 7 days), at the Session's maximum age (default 30 days from
  login), or at logout.

With cookie delivery the response also carries:

```text
Set-Cookie: swallow_refresh=<refresh token>; Path=/api/v1/auth; Max-Age=<idle limit in seconds>; HttpOnly; SameSite=Strict[; Secure]
```

`Secure` is set whenever the request reached swallow over HTTPS, including through a
TLS-terminating proxy that sends `X-Forwarded-Proto: https`. The cookie is only ever sent
back to `/api/v1/auth/*`.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | `username` or `password` is missing, or `refreshTokenDelivery` is not `cookie` or `body`. |
| `unauthorized` | 401 | Credentials are wrong, or the user does not exist. Both cases return the same code and an indistinguishable message. |

## Compatibility Notes

Wrong credentials and unknown username MUST stay indistinguishable. Splitting
them into different codes or messages would be a security regression, not an
improvement in error reporting.

`accessToken` keeps its name and meaning, so a client that reads only it (installation
tooling) keeps working; it just receives a shorter-lived token. `accessTokenExpiresAt`,
`refreshToken`, the cookie, and `refreshTokenDelivery` are additive. Tokens issued before
this change stay valid until they expire.

## Implementation Notes

The password comparison and token signing are implementation details behind the
auth use case; the secret and lifetimes come from config and are never part of this
contract. swallow stores only a one-way hash of each refresh token.
