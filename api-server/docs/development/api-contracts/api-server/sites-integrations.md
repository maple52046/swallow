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
`createdAt`, and `updatedAt`. Credentials are write-only and never appear in a response;
`hasCredential` only reports whether one is stored.

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

`POST /api/v1/integrations` accepts `kind` of `provisioner` or `metrics` only. `kind=platform`
(and its `cluster` deprecated alias) is rejected with `400 validation_error`: a `platform`
Integration reads a Kubernetes/Slurm runtime's live state, and Swallow creates it only from a
successful deployment because it manages only self-deployed platforms
([decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md)).
Existing `platform` Integrations remain readable, updatable, and deletable; only creating a new
one through this route is refused.
