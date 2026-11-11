# Server Tags

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

List the tags known for a **Site**, edit the tags on one or more **Servers**, individually or in
batch, and delete or rename a tag across the Site. swallow owns every manual tag
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md) (provisioner realized desired state)): an edit
writes the Servers' `spec.tags`, and the Server controller then makes the provisioner carry exactly
those tags. A tag the provisioner computes or maintains itself (an automatic tag) is observed only
and never part of a spec: swallow never assigns it, and a change the provisioner makes to it is not
drift. swallow can remove an automatic tag from Servers or delete it, as an action written to the
provisioner at once; when the provisioner applies it again later, swallow accepts that. This surface
backs the tag editor on the servers list (batch) and the server detail (single), and the tag manager
on the servers list.

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
GET    /api/v1/provisioning/tags
POST   /api/v1/provisioning/tags
DELETE /api/v1/provisioning/tags/{name}
PATCH  /api/v1/provisioning/tags/{name}
```

They live under `/provisioning` rather than `/servers` so the batch write does not collide with the
`GET /api/v1/servers/{id}` route, and because tags are realized by the provisioner.

The wire shape is the `listServerTags`, `editServerTags`, `deleteServerTag`, and `renameServerTag`
operations of the OpenAPI contract ([`../openapi/areas/servers.yaml`](../openapi/areas/servers.yaml)).

## Manual and automatic tags

Every tag a Site knows is one of two kinds; the list reports which, and every route below follows it:

- A **manual** tag (`editable: true`) is swallow's: the Servers' `spec.tags` decide which machines
  carry it, and a change made in the provisioner is drift, put back at the next periodic resync.
- An **automatic** tag (`editable: false`) is the provisioner's: it applies the tag itself, from a
  rule (for MAAS, a tag with a definition) or from what it learns about the machine (for MAAS, the
  `virtual` tag its commissioning keeps on every machine that is not physical). It is observed in
  each Server's `status.tags` and `status.automaticTags` and never enters a spec, so adding or
  re-adding it in the provisioner is not drift. swallow cannot assign or rename it. It can remove it
  from Servers when `removable` is `true`, or delete it; either is written to the provisioner at
  once, and the provisioner may apply the tag again later (for `virtual`, at the next
  commissioning).

Which provider tags are automatic is the provisioner adapter's judgment; this contract names MAAS
only as an example. A Server adopted before swallow recognized a tag as automatic can still name it
in `spec.tags`. Such a tag is not converged, and every route here that writes that Server's spec
drops it.

## List known tags

`GET /api/v1/provisioning/tags`

| Query | Type | Required | Description |
| --- | --- | ---: | --- |
| `siteId` | string | Yes | The Site whose provisioner tag catalog to list. |

Returns the tags the editor and the tag manager offer: the provisioner's tag catalog, plus every tag
in the `spec.tags` of the provisioner's Servers that the catalog does not hold yet (a tag just added
and not created on the provisioner). When the Site's provisioner cannot set tags, the list is the
union of those `spec.tags` alone. A Site with no enabled provisioner, including a `siteId` that names
no Site, returns an empty `tags` array rather than `404`.

| Field | Description |
| --- | --- |
| `name` | The tag name. |
| `editable` | `true` for a manual tag; `false` for an automatic tag (such as one that may back `amd-gpu`): it is real and drives Server Type, but swallow never assigns it. |
| `removable` | Whether swallow can remove the tag from a Server. Always `true` for a manual tag. For an automatic tag, `true` when the provisioner maintains it without a rule (`virtual`); `false` for one it computes from a rule, because the provisioner applies the rule again and a removal would not last. Such a tag can only be deleted. |
| `serverCount` | How many of the provisioner's Servers carry the tag: in their spec for a manual tag, as observed for an automatic one. `0` for a tag no Server carries. |

### Success Response

```json
{
  "tags": [
    { "name": "amd-gpu", "editable": false, "removable": false, "serverCount": 4 },
    { "name": "rack-a", "editable": true, "removable": true, "serverCount": 2 },
    { "name": "virtual", "editable": false, "removable": true, "serverCount": 1 }
  ]
}
```

## Edit tags

`POST /api/v1/provisioning/tags`

Writes a tri-state edit into each listed Server's spec, like email labels: `add` is the set of tags to apply to **every** listed
Server, `remove` is the set to unapply from **every** listed Server, and any tag not named is left
untouched on each Server. The editor computes this diff from each tag's all / some / none state
across the selection and sends only the tags whose state changed; a new name typed by the user
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
- An automatic tag in `add` is refused, and so is one in `remove` whose `removable` is `false`.
- Tag editing is metadata, not a host state change, so it is **not** gated on Server Lock. A locked
  Server, one in Rescue Mode, or one a running Workflow holds takes the spec at once and converges
  when the hold ends (its `held` condition says why it waits).
- The spec part of the edit is atomic: every listed Server's spec changes, or none does. A Server
  not adopted yet (a newly discovered one has no spec for a moment) makes the whole edit
  `409 conflict`; retry. The rest of each Server's spec, such as its placement, is kept. A Server
  whose manual tags the edit leaves as they were keeps its spec and generation.
- An automatic tag in `remove` is removed from the listed Servers that carry it by the provisioner,
  after the specs are written and before the response. This is the only part of an edit that calls
  the provisioner. When it cannot be reached, the edit answers `503`: the specs are already written,
  so repeating the same request finishes it. The Servers' `status.tags` show the removal at the next
  observation of the provisioner.

For manual tags the response returns before the provisioner has changed. Each Server's
`status.conditions` reports convergence: `realized` becomes `true` with `status.observedGeneration`
equal to the returned `generation` once the provisioner carries the tags, or `false` with a reason
([servers-list.md](servers-list.md#spec-and-status)). A provisioner that cannot set tags keeps the
spec and reports `capability_missing`.

### Success Response

`200` with each listed Server's spec after the edit, in request order: its `generation` and the
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

## Delete a tag

`DELETE /api/v1/provisioning/tags/{name}?siteId=`

| Parameter | In | Required | Description |
| --- | --- | ---: | --- |
| `name` | path | Yes | The tag to delete, validated like an edit's tag names. |
| `siteId` | query | Yes | The Site whose tag to delete. |

Deletes the tag from the Site and from every Server of the Site's provisioner that carries it:

- A **manual** tag is removed from every Server's `spec.tags` in one transaction (all or none), then
  deleted from the provisioner. The Servers stop carrying it as the provisioner deletes it, so they
  need no further convergence for it.
- An **automatic** tag is deleted from the provisioner only. Deleting one the provisioner computes
  from a rule removes the rule as well, so the tag does not come back on its own, and Server Type and
  monitoring targets that read it change with it. One the provisioner maintains without a rule
  (`virtual`) comes back when the provisioner applies it again.

The provisioner is called after the specs are written and before the response, and deleting a tag it
no longer holds is satisfied. When it cannot be reached the request answers `503` with the specs
already written; repeating it finishes the deletion. A provisioner that cannot set tags is not
called: the deletion only changes the specs. Not gated on Server Lock, like an edit.

### Success Response

`200` with each Server whose spec the deletion changed, in the shape of an edit's response (`[]` when
none, as for an automatic tag):

```json
{
  "servers": [
    { "serverId": "a1b2…", "generation": 5, "tags": ["gpu"] }
  ]
}
```

## Rename a tag

`PATCH /api/v1/provisioning/tags/{name}?siteId=`, with the parameters of a deletion and the new name
in the body:

```json
{ "name": "rack-a1" }
```

Renames a **manual** tag across the Site. Every Server whose `spec.tags` names it gets the new name
instead, in one transaction; then the provisioner renames the tag in place, so the machines keep
carrying it without a gap. Renaming to a manual tag that already exists merges the two: the Servers
carry the existing tag, and the provisioner deletes the old one while the Server controller adds the
existing tag to the machines that lacked it.

- `name` follows the tag name rule and must differ from the current name.
- An automatic tag cannot be renamed, and no tag can be renamed to an automatic tag's name.
- The provisioner call, its `503`, and repeating the request behave as for a deletion. Not gated on
  Server Lock.

### Success Response

`200` with each Server whose spec the rename changed, in the shape of an edit's response.

## Error Codes

Beyond the shared codes in [conventions.md](conventions.md):

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | Missing `siteId`; empty `serverIds`; no `add`/`remove`; an invalid or contradictory tag name; an automatic tag in `add`; an automatic tag in `remove` that is not removable; a rename to the same name, of an automatic tag, or to an automatic tag's name; or the provisioner refused the change, with its explanation. |
| `not_found` | 404 | A referenced Server does not exist; or the tag to delete or rename is neither in the Site's provisioner catalog nor in any of its Servers' specs (also for a Site with no enabled provisioner). |
| `conflict` | 409 | A listed Server is not adopted yet, or its spec changed while the request was being written; for a deletion or rename, a Server of the Site not adopted yet carries the tag. Nothing was changed, so read the Servers and retry. |
| `provider_unavailable` | 503 | A deletion or rename could not read the provisioner's tag catalog (nothing was changed), or a deletion, a rename, or an edit that removes an automatic tag could not reach the provisioner after writing the specs. Repeat the request. |

A provisioner that refuses or cannot be reached while converging manual tags does not fail the
request; it shows on the Server's conditions instead.

## Compatibility Notes

New surface introduced with [decision 069](../../../../../docs/decisions/069-swallow-owns-desired-state.md) (swallow owns desired state). Field names and enum-like values follow swallow's glossary
(`Tag`, `Server`, `Site`). Adding an optional field is backward compatible; see
[conventions.md](conventions.md) for the breaking-change rules.

On 2026-10-30 this contract was aligned with the implementation, with no wire change: an edit
response's `tags` can be `null` for a Server left without tags, a repeated `serverId` is answered
once, and an unknown `siteId` lists no tags instead of failing.

On 2026-10-31 the edit moved from driving the provisioner to writing the spec
(decision 071). Breaking
for clients that read the result as the observed tags: each result item gained `generation`, its
`tags` is the manual-tag spec and never `null`, `503` is no longer returned, and `409` was added.
The swallow-owned tag fallback for provisioners without tag support was retired; such tags are now
the Server's `spec.tags`.

On 2026-11-11 the surface gained tag deletion and rename (`DELETE` and `PATCH
/api/v1/provisioning/tags/{name}`), and each listed tag gained the required `removable` and
`serverCount`; the list now also holds spec tags the catalog does not. An automatic tag became one
the provisioner computes or maintains, so MAAS's `virtual` is automatic: it is observed only and no
longer removed as drift. An edit may now remove a removable automatic tag, calling the provisioner,
and can answer `503`; an edit that leaves a Server's manual tags as they were no longer increments
its generation. Additive for clients except that a `remove` of `virtual` now removes it from the
machines instead of the spec.

## Implementation Notes

- The edit writes `spec.tags` and the generation in one store transaction, each write conditional
  on the generation it read. It reads the provisioner's tag catalog to tell manual from automatic
  tags, best-effort and before the transaction; when the catalog cannot be read, the Servers'
  observed automatic tags still tell them apart.
- A deletion or rename reads the catalog first (failing with `503` before any write), writes the
  specs in one transaction, then calls the provisioner outside the transaction. Writing the specs
  first keeps the Server controller from re-creating a tag being deleted from a spec it read before.
  Every provider call is an ensure (deleting or renaming an absent tag is satisfied, and so is
  removing a tag the provisioner no longer holds), so a repeated request converges.
- The Server controller (in the worker process) converges a spec change as soon as the change
  feed reports it: for a provisioner advertising `ProviderCapabilities.Tagging` it ensures and
  assigns each missing tag and unassigns each extra manual tag through `MachineTagController`, then
  re-reads the machine; a spec tag the Server observes as automatic is skipped. A manual tag added
  or removed outside swallow is marked `drifted` at the next reconcile and corrected at the
  controller's periodic resync.
- The `amd-gpu` tag drives Server Type and the Prometheus/Ansible discovery filters, so a tag edit,
  deletion, or rename can change which exporters a Server runs and which scrape jobs include it.
- Apart from the examples above, no provider vocabulary appears in this contract; MAAS specifics,
  including which of its tags are automatic, stay inside the provisioning adapter.
