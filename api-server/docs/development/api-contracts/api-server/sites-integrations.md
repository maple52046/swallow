# Sites and Integrations

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)
- Swallow installation tooling (registers the co-located MAAS and waits for its sync)

## Purpose

Manage Site identity and the provider integrations attached to each Site. All
routes require an admin bearer token and use the shared error envelope.

## Endpoints

```text
GET    /api/v1/sites
POST   /api/v1/sites
GET    /api/v1/sites/{id}
PATCH  /api/v1/sites/{id}
DELETE /api/v1/sites/{id}

GET    /api/v1/integrations?siteId=<optional>&kind=<optional>
POST   /api/v1/integrations
GET    /api/v1/integrations/{id}
PATCH  /api/v1/integrations/{id}
PUT    /api/v1/integrations/{id}/credential
DELETE /api/v1/integrations/{id}
```

A Site response contains `id`, `name`, `description`, `createdAt`, and
`updatedAt`. An Integration response contains `id`, `siteId`, `kind`,
`providerKind`, `name`, `endpoint`, `settings`, `enabled`, `hasCredential`, `sync`,
`createdAt`, and `updatedAt`; `settings` is `{}` when none are set. Credentials are write-only
and never appear in a response; `hasCredential` only reports whether one is stored.

`sync` reports provider freshness so a reader can tell "last synced 14 minutes ago" from
"up to date":

```json
{
  "lastStartedAt": "2026-10-01T00:00:00Z",
  "lastSucceededAt": "2026-10-01T00:00:01Z",
  "lastError": null
}
```

Each field is `null` until it first applies. A failed sync sets `lastError` and keeps the
previous `lastSucceededAt`, so a reader sees both that the last attempt failed and how old
the data is; the next successful sync clears `lastError` back to `null`.

Create returns `201`; reads and updates return `200`. Deletes and credential
replacement return `{"success": true}`. Missing resources return
`404 not_found`; dependency conflicts return `409 conflict`; invalid
kind/provider pairs return `400 validation_error`.

Lists are plain arrays: Sites ordered by `name`, Integrations by `siteId` then `name`.

## Sites

`name` is required on create, trimmed, and unique across Sites: a create or rename onto a
taken name is `409 conflict`, and a name that is empty after trimming is `400 validation_error`
on create and on `PATCH`. `description` is stored as sent and is `""` when absent.
`PATCH` changes only the fields present. A Site that still has Integrations cannot be deleted
(`409 conflict`).

## Integrations

Create requires `siteId`, `kind`, `providerKind`, `name`, and `endpoint`; `name` and `endpoint`
are trimmed and must not be blank. The valid pairs are `provisioner`/`maas` and
`metrics`/`prometheus`. `credential` is optional (omitted or `""` stores none), `enabled`
defaults to `true`, and `settings` is a string map (see below). An unknown `siteId` is
`404 not_found`. `name` is unique within a Site: a create or a rename onto a name another
Integration of the Site has is `409 conflict`.

`GET /api/v1/integrations` does not validate `kind`: a value no Integration has returns `[]`.
The deprecated alias `kind=cluster` is read as `platform` and answered with the header
`Deprecation: true`; the same alias on create also sets the header and is then refused like
`platform`.

`PATCH` changes only the fields present and not `null`: `name`, `endpoint` (neither may be
blank), `enabled`, and `settings`, which replaces the whole map when present. It never touches
the credential or `sync`.

`PUT /api/v1/integrations/{id}/credential` takes `{"credential": "..."}` and requires a
non-empty value (`400 validation_error` otherwise), so a credential can be replaced but not
cleared.

An Integration cannot be deleted while Servers are projected from it or Deployment Templates
reference it (`409 conflict`).

`POST /api/v1/integrations` accepts `kind` of `provisioner` or `metrics` only. `kind=platform`
(and its `cluster` deprecated alias) is rejected with `400 validation_error`: a `platform`
Integration reads a Kubernetes/Slurm runtime's live state, and Swallow creates it only from a
successful deployment because it manages only self-deployed platforms
([decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md)).
Existing `platform` Integrations remain readable, updatable, and deletable; only creating a new
one through this route is refused.

## Provisioner settings

`settings` is a string map of non-secret adapter options. Keys a provisioner Integration
recognises:

| Key | Values | Meaning |
| --- | --- | --- |
| `timeout` | Go duration, e.g. `"45s"` | Bound on one provider API call; absent means 30 seconds. |
| `insecureSkipVerify` | `"true"` or absent | Skip TLS verification of the provider endpoint. |
| `autoInspect` | `"false"` or absent | Absent (or any value other than `"false"`) starts hardware inspection automatically for newly enrolled Servers ([server-enrollment.md](server-enrollment.md)). `"false"` turns it off; Inspect stays available on request. |

`autoInspect` is read on every sweep, so an update takes effect without restarting anything. An
Integration created before the key existed has it absent, which means on.

| Key | Value | Meaning |
| --- | --- | --- |
| `virshSshPublicKey` | An OpenSSH public key line, or absent | The public key the provisioner's rack controller uses for `virsh` power over `qemu+ssh` ([decision 055](../../../../../docs/decisions/055-libvirt-virtual-machine-enrollment.md)). Virtual-machine enrollment authorizes it for the login account on the hypervisor ([server-enrollment.md](server-enrollment.md#virtual-machines-libvirt)). The installation sets it for a co-located MAAS. It is a public key, not a secret. |

## Compatibility Notes

- Since 2026-10-31 a Site name that is empty after trimming is refused on create and on `PATCH`
  (DF-13). Before, a whitespace-only name was stored as `""` and `PATCH` accepted an empty name.
- Since 2026-10-31 a duplicate Integration name in one Site is `409 conflict` on create and on
  rename (DF-05); it was `500 internal_error`.
