# Auth Logout

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Ends a Session on the server: its refresh token stops working immediately. See
[decision 042](../../../../../docs/decisions/042-authentication-sessions-and-api-keys.md).

## Related Glossary Terms

- Session

## Endpoint / RPC

```text
POST /api/v1/auth/logout
```

## Authentication

Optional; the endpoint identifies the Session from whichever of these is present, in order:

1. Body `{ "refreshToken": "…" }` (CLI).
2. The `swallow_refresh` cookie (browsers).
3. `Authorization: Bearer <accessToken>` — the Session that issued the access token.

An API Key identifies no Session; logging out with one only answers `204`. To revoke an API
Key, delete it ([api-keys.md](api-keys.md)).

## Authorization

None beyond holding one of the credentials above.

## Request

### Body

```json
{
  "refreshToken": "string (optional)"
}
```

The body may be omitted.

## Response

### Success Response

`204 No Content`. Always returned, including when the credential is unknown, expired, or the
Session was already revoked, so logout is idempotent and reveals nothing. When the request
carried the cookie, the response clears it:

```text
Set-Cookie: swallow_refresh=; Path=/api/v1/auth; Expires=Thu, 01 Jan 1970 00:00:00 GMT; HttpOnly; SameSite=Strict[; Secure]
```

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | The body is not valid JSON. |

## Compatibility Notes

Access tokens issued before logout remain valid until they expire (minutes); clients must
discard their own copy. Revoking every Session of a User is future work.

## Implementation Notes

Revocation marks the Session revoked; the record is removed with other expired Sessions.
