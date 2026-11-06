# Sites and Integrations

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)
- swallow installation tooling (registers the co-located MAAS and waits for its sync)

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
`providerKind`, `name`, `endpoint`, `settings`, `enabled`, `hasCredential`,
`hasOperatorCredential`, `sync`, `createdAt`, and `updatedAt`; `settings` is `{}` when none are
set. Credentials are write-only and never appear in a response; `hasCredential` only reports
whether one is stored.

`hasOperatorCredential` only reports whether the Integration stores an operator credential: the
second credential a Kubernetes Platform's deployment records on its `platform` Integration for the
cluster explorer's writes ([platforms-kubernetes.md](platforms-kubernetes.md#cluster-credentials)).
swallow reads it from the stored record without opening the credential, so it says nothing else
about it. It is `false` for every other Integration: a `provisioner`, `metrics`, or `repository` Integration, a
Slurm Platform's, and a Kubernetes Platform's whose latest successful deployment ran before
2026-11-05 and recorded one credential. No route writes it: `PUT .../credential` replaces only the
credential, and a later successful deployment of the Platform records or removes the operator
credential.

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
are trimmed and must not be blank. The valid pairs are `provisioner`/`maas`,
`metrics`/`prometheus`, and `repository`/`pulp`. `credential` is optional (omitted or `""` stores
none), `enabled` defaults to `true`, and `settings` is a string map (see below). An unknown `siteId`
is `404 not_found`. `name` is unique within a Site: a create or a rename onto a name another
Integration of the Site has is `409 conflict`. A Site has at most one `metrics` Integration (its
Prometheus) and at most one `repository` Integration (its Local Repository): creating a second of
either kind in the same Site is `409 conflict`.

`GET /api/v1/integrations` does not validate `kind`: a value no Integration has returns `[]`.

`PATCH` changes only the fields present and not `null`: `name`, `endpoint` (neither may be
blank), `enabled`, and `settings`, which replaces the whole map when present. It never touches
the credential or `sync`.

`PUT /api/v1/integrations/{id}/credential` takes `{"credential": "..."}` and requires a
non-empty value (`400 validation_error` otherwise), so a credential can be replaced but not
cleared.

An Integration cannot be deleted while Servers are projected from it, Deployment Templates
reference it, Boot ISOs were built for it, or a Platform reads its state through it (its reader
Integration or the credential Integration swallow created for it); each is `409 conflict` with a
message naming what to delete first. Deleting it also deletes, in the same transaction, the
records that only describe its provider: the metadata and the verification records of its OS
Images.

`POST /api/v1/integrations` accepts `kind` of `provisioner`, `metrics`, or `repository` only. `kind=platform`
is rejected with `400 validation_error`: a `platform`
Integration reads a Kubernetes/Slurm runtime's live state, and swallow creates it only from a
successful deployment because it manages only self-deployed platforms
([decision 032](../../../../../docs/decisions/032-self-deployed-platform-management.md) (self deployed platform management)).
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

`autoInspect` is read each time the worker's `auto-inspect` controller looks at a candidate
Server, and any change of the Integration makes it look at the Integration's candidates again, so
an update takes effect without restarting anything. An Integration created before the key existed
has it absent, which means on.

| Key | Value | Meaning |
| --- | --- | --- |
| `virshSshPublicKey` | An OpenSSH public key line, or absent | The public key the provisioner's rack controller uses for `virsh` power over `qemu+ssh` ([decision 055](../../../../../docs/decisions/055-libvirt-virtual-machine-enrollment.md) (libvirt virtual machine enrollment)). Virtual-machine enrollment authorizes it for the login account on the hypervisor ([server-enrollment.md](server-enrollment.md#virtual-machines-libvirt)). The installation sets it for a co-located MAAS. It is a public key, not a secret. |

## Metrics settings

A `metrics` Integration (provider kind `prometheus`) is its Site's Prometheus; the installation
registers it ([installation.md](../../../../../docs/development/installation.md) §2.5). Keys it
recognises:

| Key | Values | Meaning |
| --- | --- | --- |
| `timeout` | Go duration, e.g. `"45s"` | Bound on one of swallow's reads; absent means 30 seconds. |
| `insecureSkipVerify` | `"true"` or absent | Skip TLS verification of the endpoint. |
| `alertmanagerUrl` | URL, or absent | The Alertmanager swallow reads alerts from and silences on acknowledge; without it alerts are unavailable. |
| `grafanaUrl` | URL, or absent | A Grafana base address for links from swallow's charts. |
| `labelProxyUrl` | URL, or absent | The label proxy in front of this Prometheus that the Platform metrics gateway forwards to ([platform-metrics-gateway.md](platform-metrics-gateway.md)); without it no Platform of the Site can read its metrics. |

## Repository settings

A `repository` Integration (provider kind `pulp`) is its Site's Local Repository
([local-repository.md](local-repository.md)). Its `endpoint` is where swallow calls the provider's
API, and its credential is the provider's admin password. The installation registers it
([installation.md](../../../../../docs/development/installation.md)). Key it recognises:

| Key | Values | Meaning |
| --- | --- | --- |
| `contentOrigin` | URL with a scheme, host, and port and no path | Where the Site's Servers read the Local Repository's content, which may differ from `endpoint` (an address only the control plane reaches). The host addresses a deployment receives are built on it. Required: without one the worker cannot converge the Local Repository (its `realized` condition is `content_origin_missing`, or `integration_invalid` for a malformed one), and browsing its content is `400 validation_error` naming the setting. |

Disabling the Integration pauses the Local Repository: the worker stops converging it, its last
observed state is kept, and while its spec serves deployments they are refused until it is
enabled again (a disabled Integration does not switch them to the upstreams).
Deleting it clears the observed state; the Site's spec (its user sources) is kept for the next
`repository` Integration of the Site.

## Compatibility Notes

- Since 2026-11-06 an Integration may have `kind` `repository` (provider kind `pulp`); a Site has at
  most one, and a second is `409 conflict`. The values are additive for readers.

On 2026-11-03 the metrics setting `labelProxyUrl` was added, and a second `metrics` Integration in a Site became `409 conflict`; a Site has one
Prometheus, which reads every one of its Servers' metrics.

- Since 2026-11-05 every Integration response carries `hasOperatorCredential`. The field is
  additive; a client that ignores it reads every other field as before.
- Since 2026-10-31 ([decision 074](../../../../../docs/decisions/074-retire-schema-v2-and-deprecated-aliases.md) (retire schema v2 and deprecated aliases),
  breaking) `kind=cluster` is no longer read as `platform`: a list filter returns what any other
  unknown kind returns, and a create is refused as an unknown kind.

- Since 2026-10-31 a Site name that is empty after trimming is refused on create and on `PATCH`.
  Before, a whitespace-only name was stored as `""` and `PATCH` accepted an empty name.
- Since 2026-11-03 deleting an Integration is also `409 conflict` while Boot ISOs were built for it
  or a Platform reads through it, and a successful delete removes its OS Image metadata and
  verification records; before, those records were left behind.
- Since 2026-10-31 a duplicate Integration name in one Site is `409 conflict` on create and on
  rename; it was `500 internal_error`.
