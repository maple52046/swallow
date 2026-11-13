# Users

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

An admin's management of Users: list, create, read, change the role of, reset the password of, and
delete a User ([decision 082](../../../../../docs/decisions/082-accounts-pool-access-and-directory.md)
(accounts, pool access, and directory);
[identity-and-access.md](../../../../../docs/development/identity-and-access.md) §3). A User is also the
person's Linux login on the Servers the Directory reaches.

## Related Glossary Terms

- [User](../../../../../docs/development/glossaries/terms/user.md)
- [User Role](../../../../../docs/development/glossaries/terms/user-role.md)
- [Pool Membership](../../../../../docs/development/glossaries/terms/pool-membership.md)
- [Directory](../../../../../docs/development/glossaries/terms/directory.md)

## Endpoint / RPC

```text
GET    /api/v1/users
POST   /api/v1/users
GET    /api/v1/users/{id}
PATCH  /api/v1/users/{id}
DELETE /api/v1/users/{id}
```

## Wire Shape

Defined in `openapi/areas/access.yaml`.

| Operation | operationId |
| --------- | ----------- |
| `GET /api/v1/users` | `listUsers` |
| `POST /api/v1/users` | `createUser` |
| `GET /api/v1/users/{id}` | `getUser` |
| `PATCH /api/v1/users/{id}` | `updateUser` |
| `DELETE /api/v1/users/{id}` | `deleteUser` |

## Authentication

Bearer credential required (see [conventions.md](conventions.md)).

## Authorization

Admin: every route requires User Role `admin`.

## Behavior

- **Create**: the username follows the POSIX login-name rule (`^[a-z_][a-z0-9_-]{0,31}$`) and may not
  be a reserved name (system accounts, the automation accounts of the supported OS Images, the
  service accounts swallow deploys, `systemd-*`, `swallow-*`); the password has at least 8 characters
  and at most 72 bytes; the role is `admin` or `user`. swallow allocates the User's `uidNumber` in the
  same write; it never changes and is never reused.
- **Change**: `role` changes the User Role; `password` replaces the password. Demoting the last admin
  is `409 conflict`. Existing Sessions stay valid; every request is judged against the new role.
- **Delete**: removes the User, its Pool Memberships, Sessions, API Keys, and Access Keys in one
  write, and records it in the access audit. Deleting oneself or the last admin is `409 conflict`.
  The `uidNumber` stays allocated.
- Every create, change, and delete writes an access audit entry ([access.md](access.md)).

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | An invalid username, password, or role. |
| `forbidden` | 403 | The caller is not an admin. |
| `not_found` | 404 | No User has the id. |
| `conflict` | 409 | The username is taken; the change would leave no admin; the caller tried to delete itself. |

## Compatibility Notes

Added 2026-11-13 (decision 082); new surface.
