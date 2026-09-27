# Server Tags

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

List the tags known for a **Site** and edit the tags on one or more **Servers**, individually or
in batch. Tag ownership is capability-first with a swallow-owned fallback (see
[decision 031](../../../../../docs/decisions/031-provider-capability-first-with-swallow-owned-fallback.md)):
when the Site's provisioner owns tags (MAAS), swallow drives the provisioner and mirrors the result
onto each Server; when it does not, swallow owns the tags itself. Either way the caller sees one
effective tag set. This surface backs the tag editor on the servers list (batch) and the server
detail (single).

These conventions are inherited from [conventions.md](conventions.md): base path, bearer
authentication, error envelope and code-to-status map, and timestamp format. Only the
endpoint-specific behavior is described below.

## Related Glossary Terms

- `Tag`
- `Server`
- `Server Type`
- `Site`
- `OS Provisioning Provider`

## Authentication

Bearer token, per [conventions.md](conventions.md).

## Authorization

`admin` only, consistent with the servers, provisioning, and infrastructure surfaces.

## Endpoints

```text
GET  /api/v1/provisioning/tags
POST /api/v1/provisioning/tags
```

Both live under `/provisioning` rather than `/servers` so the batch write does not collide with the
`GET /api/v1/servers/{id}` route, and because tags are a provisioner-owned concept.

## List known tags

`GET /api/v1/provisioning/tags`

| Query | Type | Required | Description |
| --- | --- | ---: | --- |
| `siteId` | string | Yes | The Site whose provisioner tag catalog to list. |

Returns the tags the editor may offer. `editable` is `false` for a provider-computed automatic tag
(a MAAS tag with an XPath definition, such as one that may back `amd-gpu`): it is real and drives
Server Type, but swallow must not assign or unassign it, so the editor shows it disabled. When the
Site's provisioner cannot own tags, the list is the union of swallow-owned tags for that provisioner
and every entry is `editable`. A Site with no enabled provisioner returns an empty `tags` array.

### Success Response

```json
{
  "tags": [
    { "name": "amd-gpu", "editable": false },
    { "name": "rack-a", "editable": true }
  ]
}
```

## Edit tags

`POST /api/v1/provisioning/tags`

Applies a tri-state edit, like email labels: `add` is the set of tags to apply to **every** listed
Server, `remove` is the set to unapply from **every** listed Server, and any tag not named is left
untouched on each Server. The editor computes this diff from each tag's all / some / none state
across the selection and sends only the tags whose state changed; a new name typed by the operator
is created and applied. A single edit is the same request with one `serverId`.

```json
{
  "serverIds": ["a1b2…", "c3d4…"],
  "add": ["rack-a"],
  "remove": ["decommission"]
}
```

- `serverIds` is required and must not be empty.
- At least one of `add` / `remove` must be non-empty.
- A tag name is trimmed and validated (letters, digits, and `_-.`, length ≤ 100); duplicates are
  ignored. The same name in both `add` and `remove` is contradictory and rejected.
- Tag editing is metadata, not a host state change, so it is **not** gated on Server Lock.

### Success Response

`200` with each listed Server's effective tags after the edit, in request order, so the caller can
update its rows without re-reading:

```json
{
  "servers": [
    { "serverId": "a1b2…", "tags": ["gpu", "rack-a"] },
    { "serverId": "c3d4…", "tags": ["rack-a"] }
  ]
}
```

## Error Codes

Beyond the shared codes in [conventions.md](conventions.md):

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | Missing `siteId`; empty `serverIds`; no `add`/`remove`; an invalid or contradictory tag name; or the provisioner refused the change (for example an attempt to assign an automatic tag). |
| `not_found` | 404 | A referenced Server does not exist, or the provisioner no longer has its machine. |
| `provider_unavailable` | 503 | The Site's provisioner could not be reached or has no usable credential. |

## Compatibility Notes

New surface introduced with decision 031. Field names and enum-like values follow swallow's glossary
(`Tag`, `Server`, `Site`). Adding an optional field is backward compatible; see
[conventions.md](conventions.md) for the breaking-change rules.

## Implementation Notes

- Editing groups the target Servers by their provisioner integration and branches per group on the
  `ProviderCapabilities.Tagging` flag: a capable provisioner is driven through the optional
  `MachineTagController` (ensure tag, then one batched `update_nodes` add/remove per tag across the
  group's machines), after which each Server is refreshed from the provider so `observed.tags`
  updates immediately; a non-capable provisioner writes a swallow-owned `ServerTagOverlay` that
  reconcile unions into `observed.tags`.
- The `amd-gpu` tag drives Server Type and the Prometheus/Ansible discovery filters, so a tag edit
  can change which exporters a Server runs and which scrape jobs include it.
- No provider vocabulary appears in this contract; MAAS specifics stay inside the provisioning
  adapter.
