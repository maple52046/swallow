# Auth Me

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

Returns the identity, User Role, and Pool Memberships of the currently authenticated caller, and lets
the caller change its own password. Consumers use it to resolve the current user after a page load
and to decide which role-gated surfaces and actions to render.

## Related Glossary Terms

- [User](../../../../../docs/development/glossaries/terms/user.md)
- [User Role](../../../../../docs/development/glossaries/terms/user-role.md)
- [Pool Membership](../../../../../docs/development/glossaries/terms/pool-membership.md)
- Session
- API Key

## Endpoint / RPC

```text
GET /api/v1/auth/me
PUT /api/v1/auth/me/password
```

## Wire Shape

Defined in `openapi/areas/auth.yaml`.

| Operation | operationId |
| --------- | ----------- |
| `GET /api/v1/auth/me` | `getCurrentUser` |
| `PUT /api/v1/auth/me/password` | `changeOwnPassword` |

## Authentication

Bearer credential required: a Session access token or an API Key. See
[`conventions.md`](conventions.md). `PUT /api/v1/auth/me/password` accepts only a Session access
token; an API Key is `403 forbidden`.

## Authorization

Own account: any authenticated User. The response always describes the caller — it can never be used
to read another user — and the password change acts on the caller alone.

## Request

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Authorization` | Yes | Bearer access token or API Key. |

`GET` has no path parameters, query parameters, or body. `PUT .../password` takes
`{ "currentPassword": "…", "newPassword": "…" }`.

## Response

### Success Response

`GET`: `200 OK`

```json
{
  "id": "string",
  "username": "string",
  "role": "admin | user",
  "authMethod": "session | api_key",
  "memberships": [
    { "poolId": "string", "poolName": "string", "role": "owner | member" }
  ]
}
```

`role` is the User Role and drives consumer-side visibility. A consumer must treat an unrecognised
role as least-privileged (`user`) rather than failing open. `memberships` lists the caller's Pool
Memberships with each Pool's name; a consumer offers a Pool's operations only to its owners
and to admins, who may act in every Pool whether or not it is listed. An unrecognised Pool Role must be
treated as `member`. The server judges every request on its own; this list only decides what to offer.

`authMethod` says which credential authenticated this request: `session` for an access
token from login or refresh, `api_key` for an API Key. The CLI uses it to confirm a key
before saving it. A consumer must treat an unrecognised value as `session`.

`PUT .../password`: `204 No Content`. Existing Sessions stay valid.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | `PUT`: a field is missing, the current password is wrong, or the new password is shorter than 8 characters or longer than 72 bytes. |
| `unauthorized` | 401 | The credential is missing, malformed, expired, or revoked. |
| `forbidden` | 403 | `PUT`: the request was authenticated with an API Key. |
| `not_found` | 404 | `GET`: the token is valid but the referenced user no longer exists. |
| `internal_error` | 500 | The user store could not be read. The Session is not ended; retry later. |

## Compatibility Notes

The `404` case is deliberate: a structurally valid token for a deleted user is
not an authentication failure, and consumers should clear the session rather than
retry. Collapsing it into `401` would hide the distinction.

Since 2026-10-31 a failing user store is `500 internal_error`. It was `404`, so a store
outage signed the dashboard out.

Adding a role value is a change to swallow's glossary first, then to this
contract, then to every consumer that branches on role.

`authMethod` is additive; older consumers ignore it.

Since 2026-11-13 ([decision 082](../../../../../docs/decisions/082-accounts-pool-access-and-directory.md)
(accounts, pool access, and directory)) `role` is `admin` or `user` (`owner` became a Pool Role, which
`memberships` reports), `memberships` is always present, and `PUT /api/v1/auth/me/password` exists.
Removing `owner` from the enum is a breaking change. The same day a membership lost `siteId`, because a
Pool belongs to the installation rather than to a Site (breaking, no alias).

## Implementation Notes

The token claim set is an implementation detail. Consumers must read identity
from this response, never by decoding the token.
