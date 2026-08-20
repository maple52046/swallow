# Servers Create

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Registers a Server in the platform registry. Registration is deliberately
minimal: it establishes identity, not a complete machine profile.

## Related Glossary Terms

- `Server`
- `Server Status`

## Endpoint / RPC

```text
POST /api/v1/servers/
```

## Authentication

Bearer token required. See [`conventions.md`](conventions.md).

## Authorization

`admin` only. A non-admin authenticated caller receives `403 forbidden`.

## Request

### Headers

| Name | Required | Description |
| --- | -------: | --- |
| `Authorization` | Yes | Bearer token. |
| `Content-Type` | Yes | `application/json`. |

### Body

```json
{
  "hostname": "string (required, must be unique)",
  "ip":       "string (required, must be unique)"
}
```

Both fields are required. `hostname` and `ip` must each be unique across all
Servers; uniqueness is enforced by the platform, not by the caller. Every other
Server attribute — owner, location, BMC, SSH, hardware specs — is optional and is
not accepted here.

## Response

### Success Response

`201 Created`

```json
{
  "id": "string (opaque server ID)"
}
```

The ID is opaque. Consumers must not parse structure from it.

A newly registered Server has status `unknown` until monitoring data arrives; the
create response does not include status.

### Error Response

See [`conventions.md`](conventions.md) for the envelope.

## Error Codes

| Code | HTTP Status | Description |
| --- | ---: | --- |
| `validation_error` | 400 | `hostname` or `ip` is missing. |
| `unauthorized` | 401 | Token is missing, malformed, or expired. |
| `forbidden` | 403 | Caller's role is not `admin`. |
| `conflict` | 409 | `hostname` already exists, or `ip` already exists. |

## Compatibility Notes

Registration must stay non-destructive and explicit: this endpoint never
implicitly updates an existing Server when a duplicate `hostname` or `ip` is
submitted, and the agent path must not auto-register unknown nodes. Turning a
duplicate into an upsert would be a breaking behavior change even though the
response shape stays the same.

Accepting further Server attributes here is an additive change, but making any of
them required is breaking.

## Implementation Notes

Uniqueness is checked before the write; the registration policy is a domain
decision and must not live in the handler.
