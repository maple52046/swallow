# Access Settings, Audit, and Directory

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

The installation's access settings, the audit log of access changes, and the Directory's
configuration and status
([decision 082](../../../../../docs/decisions/082-accounts-pool-access-and-directory.md) (accounts, pool
access, and directory); [identity-and-access.md](../../../../../docs/development/identity-and-access.md)
§6, §7).

## Related Glossary Terms

- [Sudo Policy](../../../../../docs/development/glossaries/terms/sudo-policy.md)
- [Directory](../../../../../docs/development/glossaries/terms/directory.md)
- [Condition](../../../../../docs/development/glossaries/terms/condition.md)

## Endpoint / RPC

```text
GET  /api/v1/access/settings
PUT  /api/v1/access/settings
GET  /api/v1/access/audit
GET  /api/v1/access/directory
POST /api/v1/access/directory/sync
```

## Wire Shape

Defined in `openapi/areas/access.yaml`.

| Operation | operationId |
| --------- | ----------- |
| `GET /api/v1/access/settings` | `getAccessSettings` |
| `PUT /api/v1/access/settings` | `setAccessSettings` |
| `GET /api/v1/access/audit` | `listAccessAudit` |
| `GET /api/v1/access/directory` | `getDirectory` |
| `POST /api/v1/access/directory/sync` | `syncDirectory` |

## Authentication

Bearer credential required (see [conventions.md](conventions.md)).

## Authorization

Admin: every route requires User Role `admin`.

## Behavior

- **Settings**: `adminSudoPasswordless` (default `false`) decides whether admins' `sudo` on every
  Server asks for their password. `posixIdStart` is the first uidNumber and gidNumber of the
  installation's range, recorded at the first allocation; it is read-only here and changes nothing once
  recorded.
- **Audit**: the most recent access changes (User create, change, delete; membership add, change,
  remove; Pool Sudo Policy and access settings changes), newest first, `limit` of them (100 when omitted,
  at most 500). Entries older than a year are removed. An entry never carries a password.
- **Directory**: `configured` says whether the installation has a Directory. Its status is the
  Directory controller's: the `realized` condition (`provider_unreachable` while the Directory cannot be
  reached, `provider_rejected` when it refused a change, with `requires_attention` `true`), when it last
  applied the content and last checked the Directory, and the counts of accounts, groups, sudo rules, and
  distinct host names. Without a Directory the counts are `0`, `conditions` is empty, and the URLs are
  `""`.
- **Sync** records a request; the worker's Directory controller applies the full content soon after,
  even when nothing changed. Without a Directory it is `409 conflict`.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | A malformed body or `limit`. |
| `forbidden` | 403 | The caller is not an admin. |
| `conflict` | 409 | `sync` without a configured Directory. |

## Compatibility Notes

Added 2026-11-13 (decision 082); new surface.
