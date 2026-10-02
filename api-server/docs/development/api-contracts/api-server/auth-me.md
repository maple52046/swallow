# Auth Me

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Returns the identity and role of the currently authenticated caller. Consumers
use it to resolve the current user after a page load and to decide which
role-gated surfaces to render.

## Related Glossary Terms

- `User` (pending definition in swallow's glossary)
- Session
- API Key

## Endpoint / RPC

```text
GET /api/v1/auth/me
```

## Authentication

Bearer credential required: a Session access token or an API Key. See
[`conventions.md`](conventions.md).

## Authorization

Any authenticated role (`admin`, `owner`, `user`). The response always describes
the caller — it can never be used to read another user.

## Request

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Authorization` | Yes | Bearer access token or API Key. |

No path parameters, query parameters, or body.

## Response

### Success Response

`200 OK`

```json
{
  "id": "string",
  "username": "string",
  "role": "admin | owner | user",
  "authMethod": "session | api_key"
}
```

`role` is domain language and drives consumer-side visibility. A consumer must
treat an unrecognised role as least-privileged rather than failing open.

`authMethod` says which credential authenticated this request: `session` for an access
token from login or refresh, `api_key` for an API Key. The CLI uses it to confirm a key
before saving it. A consumer must treat an unrecognised value as `session`.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `unauthorized` | 401 | The credential is missing, malformed, expired, or revoked. |
| `not_found` | 404 | The token is valid but the referenced user no longer exists. |

## Compatibility Notes

The `404` case is deliberate: a structurally valid token for a deleted user is
not an authentication failure, and consumers should clear the session rather than
retry. Collapsing it into `401` would hide the distinction.

Adding a role value is a change to swallow's glossary first, then to this
contract, then to every consumer that branches on role.

`authMethod` is additive; older consumers ignore it.

## Implementation Notes

The token claim set is an implementation detail. Consumers must read identity
from this response, never by decoding the token.
