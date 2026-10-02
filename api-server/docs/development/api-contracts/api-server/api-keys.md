# API Keys

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Manage the caller's **API Keys**: named, long-lived secrets that let a non-interactive client
(the CLI, scripts, CI) call the swallow API as the caller without a password login. See
[decision 042](../../../../../docs/decisions/042-authentication-sessions-and-api-keys.md).

Base path, bearer authentication, error envelope, and timestamps follow
[conventions.md](conventions.md); only endpoint-specific behavior is described below.

## Related Glossary Terms

- API Key
- Session
- `User` (pending definition in swallow's glossary)

## Authentication and Authorization

Bearer credential; every endpoint requires `admin`. Keys are scoped to the caller: list
returns only the caller's keys, and delete answers `404 not_found` for another user's key.

`POST /api/v1/api-keys` additionally requires a **Session** access token: a request
authenticated by an API Key is refused with `403 forbidden`, so a leaked key cannot create
replacement keys. Listing and deleting work with either credential.

## Endpoints

```text
GET    /api/v1/api-keys
POST   /api/v1/api-keys
DELETE /api/v1/api-keys/{keyId}
```

## Using an API Key

Send the secret as the bearer credential on any authenticated endpoint:

```text
Authorization: Bearer swk_…
```

The key acts with its owner's current role. It stops working when it expires, when it is
deleted, or when its owner no longer exists (`401 unauthorized`). It is never accepted in a
query string (see [servers-stream.md](servers-stream.md)).

## API Key resource

```json
{
  "id": "3f2a…",
  "name": "ci-runner",
  "prefix": "swk_Ab3dEf9h",
  "createdAt": "2026-10-02T02:00:00Z",
  "expiresAt": "2027-01-01T00:00:00Z",
  "lastUsedAt": "2026-10-02T02:05:00Z"
}
```

- `id` is the swallow-issued identifier used in the path.
- `name` is unique among the caller's keys.
- `prefix` is the first characters of the secret, for recognising a key; it cannot
  authenticate.
- `expiresAt` is `null` for a key that never expires.
- `lastUsedAt` is `null` until the key is first used. It is updated at most once per minute,
  so it may lag by up to a minute.

The secret itself is never part of the resource.

## List

`GET /api/v1/api-keys` returns `200` with a plain array (at most 50 keys per user) of the
caller's API Key resources ordered by `createdAt`. Expired keys stay listed, so the caller
can see and delete them.

## Create

`POST /api/v1/api-keys`

```json
{
  "name": "ci-runner",
  "expiresAt": "2027-01-01T00:00:00Z"
}
```

- `name` (required): trimmed, 1–64 characters, no control characters.
- `expiresAt` (optional): a future timestamp; omit or send `null` for a key that never
  expires.

Returns `201`:

```json
{
  "key": { "...": "API Key resource" },
  "secret": "swk_…"
}
```

The response is `Cache-Control: no-store`. `secret` is returned only here; swallow keeps only
a one-way hash of it and can never show it again. A lost secret is replaced by creating a new
key and deleting the old one.

## Delete

`DELETE /api/v1/api-keys/{keyId}` returns `204`. The key stops authenticating immediately.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | `name` is missing, too long, or contains control characters; `expiresAt` is not a timestamp or is not in the future. |
| `forbidden` | 403 | The caller is not `admin`, or (create only) the request was authenticated with an API Key. |
| `not_found` | 404 | Delete: no key with this id belongs to the caller. |
| `conflict` | 409 | Create: the caller already has a key with this name, or already has 50 keys. |

## Compatibility Notes

The secret format (`swk_` followed by URL-safe characters) is stable enough for consumers to
recognise a key, but its length and alphabet after the prefix may change. Scoped keys
(less than the owner's role) are future work and will be additive.

## Implementation Notes

swallow stores a SHA-256 hash of each secret (the secret carries 256 bits of randomness) and
looks keys up by that hash on every request.
