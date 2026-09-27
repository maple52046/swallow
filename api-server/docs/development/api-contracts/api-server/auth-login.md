# Auth Login

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Exchanges a username and password for an access token. This is the only endpoint
that establishes a session; every other authenticated contract depends on the
token it returns.

## Related Glossary Terms

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
  "password": "string (required)"
}
```

## Response

### Success Response

`200 OK`

```json
{
  "accessToken": "string (JWT)"
}
```

The token is opaque to consumers. Its lifetime is server-configured; consumers
must handle a `401` on any authenticated call by re-authenticating rather than
by predicting expiry.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | `username` or `password` is missing. |
| `unauthorized` | 401 | Credentials are wrong, or the user does not exist. Both cases return the same code and an indistinguishable message. |

## Compatibility Notes

Wrong credentials and unknown username MUST stay indistinguishable. Splitting
them into different codes or messages would be a security regression, not an
improvement in error reporting.

## Implementation Notes

The password comparison and token signing are implementation details behind the
auth use case; the secret and expiry come from config and are never part of this
contract.
