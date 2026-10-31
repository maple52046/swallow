# Server Tags

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

List the tags known for a **Site** and edit the manual tags on one or more **Servers**,
individually or in batch. Swallow owns every manual tag
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)): an edit
writes the Servers' `spec.tags`, and the Server controller then makes the provisioner carry exactly
those tags. A tag the provisioner computes itself (an automatic tag, such as a MAAS tag with a
definition) is observed only; swallow never assigns or removes it. This surface backs the tag
editor on the servers list (batch) and the server detail (single).

These conventions are inherited from [conventions.md](conventions.md): base path, bearer
authentication, error envelope and code-to-status map, and timestamp format. Only the
endpoint-specific behavior is described below.

## Related Glossary Terms

- `Tag`
- `Server`
- `Controller`
- `Condition`
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
`GET /api/v1/servers/{id}` route, and because tags are realized by the provisioner.

The wire shape is the `listServerTags` and `editServerTags` operations of the OpenAPI contract
([`../openapi/areas/servers.yaml`](../openapi/areas/servers.yaml)).

## List known tags

`GET /api/v1/provisioning/tags`

| Query | Type | Required | Description |
| --- | --- | ---: | --- |
| `siteId` | string | Yes | The Site whose provisioner tag catalog to list. |

Returns the tags the editor may offer. `editable` is `false` for a provider-computed automatic tag
(a MAAS tag with an XPath definition, such as one that may back `amd-gpu`): it is real and drives
Server Type, but swallow must not assign or unassign it, so the editor shows it disabled. When the
Site's provisioner cannot set tags, the list is the union of the `spec.tags` of that provisioner's
Servers and every entry is `editable`. A Site with no enabled provisioner, including a `siteId` that names
no Site, returns an empty `tags` array rather than `404`.

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

Writes a tri-state edit into each listed Server's spec, like email labels: `add` is the set of tags to apply to **every** listed
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
- An automatic tag name, in `add` or `remove`, is refused.
- Tag editing is metadata, not a host state change, so it is **not** gated on Server Lock. A locked
  Server, one in Rescue Mode, or one a running Workflow holds takes the spec at once and converges
  when the hold ends (its `held` condition says why it waits).
- The edit is atomic: every listed Server's spec changes, or none does. A Server that has no spec
  yet (not adopted) takes its observed manual tags as the base, so its other tags are kept.

The response returns before the provisioner has changed. Each Server's `status.conditions` reports
convergence: `realized` becomes `true` with `status.observedGeneration` equal to the returned
`generation` once the provisioner carries the tags, or `false` with a reason
([servers-list.md](servers-list.md#spec-and-status)). A provisioner that cannot set tags keeps the
spec and reports `capability_missing`.

### Success Response

`200` with each listed Server's spec after the edit, in request order: its new `generation` and the
complete `tags` (manual tags only; `[]` when none). A `serverId` listed more than once appears once,
at its first position:

```json
{
  "servers": [
    { "serverId": "a1b2…", "generation": 4, "tags": ["gpu", "rack-a"] },
    { "serverId": "c3d4…", "generation": 2, "tags": ["rack-a"] },
    { "serverId": "e5f6…", "generation": 2, "tags": [] }
  ]
}
```

## Error Codes

Beyond the shared codes in [conventions.md](conventions.md):

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | Missing `siteId`; empty `serverIds`; no `add`/`remove`; an invalid, contradictory, or automatic tag name. |
| `not_found` | 404 | A referenced Server does not exist. |
| `conflict` | 409 | A listed Server's spec changed while the edit was being written; nothing was changed, so read the Servers and retry. |

A provisioner that refuses or cannot be reached no longer fails the edit; it shows on the Server's
conditions instead.

## Compatibility Notes

New surface introduced with decision 031. Field names and enum-like values follow swallow's glossary
(`Tag`, `Server`, `Site`). Adding an optional field is backward compatible; see
[conventions.md](conventions.md) for the breaking-change rules.

On 2026-10-30 this contract was aligned with the implementation, with no wire change: an edit
response's `tags` can be `null` for a Server left without tags, a repeated `serverId` is answered
once, and an unknown `siteId` lists no tags instead of failing.

On 2026-10-31 the edit moved from driving the provisioner to writing the spec
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)). Breaking
for clients that read the result as the observed tags: each result item gained `generation`, its
`tags` is the manual-tag spec and never `null`, `503` is no longer returned, and `409` was added.
The swallow-owned tag fallback for provisioners without tag support was retired; such tags are now
the Server's `spec.tags`.

## Implementation Notes

- The edit writes `spec.tags` and the generation in one store transaction, each write conditional
  on the generation it read. It reads the provisioner's tag catalog only to recognize automatic tags,
  best-effort and before the transaction.
- The Server controller (in the worker process) converges a spec change as soon as the change
  feed reports it: for a provisioner advertising `ProviderCapabilities.Tagging` it ensures and
  assigns each missing tag and unassigns each extra manual tag through `MachineTagController`, then
  re-reads the machine. A manual tag added or removed outside swallow is marked `drifted` at the
  next reconcile and corrected at the controller's periodic resync.
- The `amd-gpu` tag drives Server Type and the Prometheus/Ansible discovery filters, so a tag edit
  can change which exporters a Server runs and which scrape jobs include it.
- No provider vocabulary appears in this contract; MAAS specifics stay inside the provisioning
  adapter.
