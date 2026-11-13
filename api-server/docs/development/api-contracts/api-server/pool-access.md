# Pool Access

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

A Pool's people and their `sudo`: list, add, change, and remove Pool Memberships, and read and
replace the Pool's Sudo Policy defaults
([decision 082](../../../../../docs/decisions/082-accounts-pool-access-and-directory.md) (accounts, pool
access, and directory); [identity-and-access.md](../../../../../docs/development/identity-and-access.md)
§6). The Directory realizes the result on the Pool's Servers.

## Related Glossary Terms

- [Pool](../../../../../docs/development/glossaries/terms/pool.md)
- [Pool Membership](../../../../../docs/development/glossaries/terms/pool-membership.md)
- [Pool Role](../../../../../docs/development/glossaries/terms/pool-role.md)
- [Sudo Policy](../../../../../docs/development/glossaries/terms/sudo-policy.md)
- [Host Access](../../../../../docs/development/glossaries/terms/host-access.md)

## Endpoint / RPC

```text
GET    /api/v1/infrastructure/pools/{id}/members
POST   /api/v1/infrastructure/pools/{id}/members
GET    /api/v1/infrastructure/pools/{id}/member-candidates
PATCH  /api/v1/infrastructure/pools/{id}/members/{userId}
DELETE /api/v1/infrastructure/pools/{id}/members/{userId}
GET    /api/v1/infrastructure/pools/{id}/access-policy
PUT    /api/v1/infrastructure/pools/{id}/access-policy
```

## Wire Shape

Defined in `openapi/areas/access.yaml`.

| Operation | operationId |
| --------- | ----------- |
| `GET /api/v1/infrastructure/pools/{id}/members` | `listPoolMembers` |
| `POST /api/v1/infrastructure/pools/{id}/members` | `addPoolMember` |
| `GET /api/v1/infrastructure/pools/{id}/member-candidates` | `listPoolMemberCandidates` |
| `PATCH /api/v1/infrastructure/pools/{id}/members/{userId}` | `updatePoolMember` |
| `DELETE /api/v1/infrastructure/pools/{id}/members/{userId}` | `removePoolMember` |
| `GET /api/v1/infrastructure/pools/{id}/access-policy` | `getPoolAccessPolicy` |
| `PUT /api/v1/infrastructure/pools/{id}/access-policy` | `setPoolAccessPolicy` |

## Authentication

Bearer credential required (see [conventions.md](conventions.md)).

## Authorization

- `GET members` and `GET access-policy` are Pool read: an admin or any Pool Role in the Pool. Any other
  caller gets `404 not_found`, as for a Pool that does not exist.
- The writes and `GET member-candidates` are Pool operate: an admin or an `owner` of the Pool; a
  `member` gets `403 forbidden`.

## Behavior

- **Add** names an existing User by `username` (only admins create Users, [users.md](users.md)); an
  unknown username is `404`, a User already in the Pool `409`. `sudo` and `sudoPasswordless` default to
  `inherit`.
- **Member candidates** are how an owner, who may not list Users, finds the username to add: the Users
  that are not members of the Pool, as `{"items": [{"userId": "…", "username": "alice"}]}` ordered by
  `username`, at most 20. The optional `query` keeps the Users whose username contains it,
  case-insensitively; without it the first 20 are returned. Only the id and username of a User are
  shown, never its User Role or other memberships.
- An owner may make another User an owner, change any membership, and remove any membership,
  itself included.
- **Sudo Policy** resolution, reported per member as `effective` with its `source`:
  - `sudo`: an owner always has it (`source: role`, whatever the stored `setting`); a member's
    `setting` `on` or `off` decides it (`member`), `inherit` takes the Pool's `memberSudo` (`pool`).
  - `sudoPasswordless`: the `setting` `on` or `off` decides it (`member`), `inherit` takes the Pool's
    `sudoPasswordless` (`pool`).
  - A Platform may still deny a member `sudo` on its nodes (for example every Slurm node); that limit
    applies on the Servers and is not part of these values.
- A Pool without stored defaults has `memberSudo: false` and `sudoPasswordless: false`, with
  `updatedAt: null`.
- Deleting a Pool deletes its memberships and defaults.
- Every write records an access audit entry ([access.md](access.md)). Changes reach the Pool's Servers
  at the Directory controller's next pass and the Servers' cache refresh (minutes).

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | An invalid Pool Role or sudo setting, or a malformed body. |
| `forbidden` | 403 | The caller may read the Pool but is not an owner of it or an admin. |
| `not_found` | 404 | The Pool does not exist or is not visible to the caller; the username or membership does not exist. |
| `conflict` | 409 | The User already belongs to the Pool. |

## Compatibility Notes

Added 2026-11-13 (decision 082); new surface. `GET member-candidates` was added the same day, additive.
