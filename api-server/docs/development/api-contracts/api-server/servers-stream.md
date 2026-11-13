# Servers Stream

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)

## Purpose

Streams live Server projection changes as Server-Sent Events (SSE) so a consumer keeps its
Server list fresh by patching individual rows, instead of polling and re-reading the whole
list. It is the streaming complement to [`servers-list.md`](servers-list.md): the list is the
initial snapshot, this stream carries the deltas.

## Related Glossary Terms

- `Server`
- `Server Status`

## Endpoint / RPC

```text
GET /api/v1/servers/stream
```

Media type `text/event-stream`. The connection stays open; the server emits an SSE frame per
change and a periodic comment heartbeat.

The wire shape is the `streamServerChanges` operation of the OpenAPI contract
([`../openapi/areas/servers.yaml`](../openapi/areas/servers.yaml)); each `data:` payload is
its `ServerChange` schema, whose `server` is the `Server` schema of the list.

## Authentication

Bearer credential required (a Session access token or an API Key in the `Authorization`
header). Because a browser `EventSource` cannot set an `Authorization` header, this endpoint
additionally accepts a Session **access token** in the `access_token` query parameter, which
is promoted to the standard Bearer header before verification. An API Key is never accepted
in the query string, so key secrets do not appear in URLs or logs; a value starting with
`swk_` there is ignored and the request is `401`. A caller that can set headers SHOULD use
`Authorization` instead. See [`conventions.md`](conventions.md).

The access token is checked when the stream opens. An open stream is not cut off when that
token later expires; a client that reconnects must present a current access token (refresh
first, see [auth-refresh.md](auth-refresh.md)).

## Authorization

Pool read ([conventions](conventions.md#authorization)), filtered: a User that is not an admin
receives events only for Servers placed in Pools it belongs to. Its memberships are read when the
stream opens; a change to them takes effect when the client reconnects. A Server moved out of those
Pools is sent as a `removed` event; a Server it never saw is never mentioned.

## Request

### Query Parameters

| Name | Type | Required | Description |
| --- | --- | -------: | --- |
| `access_token` | string | No | Session access token for `EventSource` callers that cannot send an `Authorization` header. Ignored when the header is present. API Keys are not accepted here. |
| `siteId` | string | No | Scope the stream to Servers at this Site. Omit to receive every Site. Removals may arrive unscoped and are safe to ignore for a Server the client is not showing. |

## Response

### Success Response

`200 OK` with `Content-Type: text/event-stream`. The body is an unbounded SSE stream:

- An opening comment (`: connected`) is sent immediately.
- Each change is one `data:` frame whose payload is JSON:

```json
{
  "type": "upsert",
  "id": "server-id",
  "server": { "...": "the same object shape as one servers-list item" }
}
```

```json
{
  "type": "removed",
  "id": "server-id"
}
```

- `upsert` — the Server was created or changed; `server` is the complete projection, in the
  exact shape of a [`servers-list.md`](servers-list.md) item. The client replaces or inserts
  the row (subject to its active filters).
- `removed` — the Server left the default list (deleted, or marked absent from provider
  inventory); only `id` is set and the client drops the row.
- Comment lines (`: ping`) are heartbeats and carry no data.

The stream does not classify health. In an `upsert`, `status.health` is always `null`, which
means "not resolved in this event", never a health state: health is worked out only when a Server
is read through [`servers-list.md`](servers-list.md#health) or the detail endpoint, where it is
always present. A consumer SHOULD keep the row's previously known health when patching from an
`upsert`, and MAY re-read the Server to refresh it, for example when the event changed its
provisioning state, its lock, or its `absent` flag, any of which can move it between a monitored
state and `not_applicable`. A health change that does not touch the Server record, such as its
Node Exporter install finishing or its exporter's scrape failing, emits no frame; the next read of
the list or the detail shows it.

Unchanged projections are suppressed server-side: re-observing a Server with no UI-relevant
change (only its observation timestamps advanced) emits no frame.

### Error Response

Authentication and authorization failures are returned as the standard JSON error envelope
with the normal status code before the stream is established. See
[`conventions.md`](conventions.md).

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `unauthorized` | 401 | Token is missing, malformed, or expired (header or `access_token`). |
| `forbidden` | 403 | Caller's role is not `admin`. |

## Compatibility Notes

The `upsert` `server` object is the same projection as the Server list item, so the two
contracts evolve together: adding an optional projection field is backward compatible; adding
a credential field is forbidden. New `type` values may be added; a consumer MUST ignore a
frame whose `type` it does not recognise.

On 2026-10-31 the `server` object took the spec/status shape of the list item
([servers-list.md](servers-list.md#spec-and-status)), a breaking change made in both contracts
together.

On 2026-11-03 delivery moved to the store's change feed: another process's write no longer waits
for a reconcile pass, and the server may also end a stream when its change feed reopens. The
wire shape did not change.

On 2026-11-04 the list and the detail started to classify `status.health` into four states that
are always present ([servers-list.md](servers-list.md#health)). The stream is unchanged and still
sends `status.health: null`; a consumer must not read that `null` as a health state, nor overwrite
a classified health with it.

A dropped or reconnecting client MUST treat the stream as best-effort delivery: on reconnect
it re-reads [`servers-list.md`](servers-list.md) once to resync any changes missed while
disconnected. The server may drop a subscriber that cannot keep up; the client simply
reconnects.

## Implementation Notes

Changes come from the store's change feed: each API process watches the Server records from
the moment it starts and re-reads every changed Server, so the writes of every process (the
reconcile pass, the worker's controllers and Workflow Tasks, the Ansible executor) reach the
stream as they commit. A write that changes only observation timestamps is not delivered. An
in-process broker fans the events out with per-subscriber buffering; a subscriber that
overflows is dropped rather than allowed to stall the relay. When the API process's watch opens
again (after a store interruption), it ends every open stream, because changes made meanwhile
were not delivered; clients reconnect and resync from the list.
