# Servers Stream

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

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

`admin` only. A non-admin authenticated caller receives `403 forbidden`.

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

The `health` axis is resolved at list-query time from the metrics backend and is not part of
this stream; an `upsert` may carry `health: null`. A consumer SHOULD preserve the row's
previously known health when patching from an `upsert`.

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

A dropped or reconnecting client MUST treat the stream as best-effort delivery: on reconnect
it re-reads [`servers-list.md`](servers-list.md) once to resync any changes missed while
disconnected. The server may drop a subscriber that cannot keep up; the client simply
reconnects.

## Implementation Notes

Changes are published from the single Server persistence boundary and fanned out by an
in-process broker with per-subscriber buffering; a subscriber that overflows is dropped
rather than allowed to stall writers. Cross-process writes (durable Operations in the worker)
surface through the reconcile pass within `reconcileInterval`, deduplicated by the broker.
