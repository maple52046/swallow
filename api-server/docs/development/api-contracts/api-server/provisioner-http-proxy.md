# Provisioner HTTP Proxy

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client; no command yet)

## Purpose

Read and write the Provisioner HTTP Proxy of one provisioner Integration: the proxy its provider
hands to the machines it deploys for package and HTTP/HTTPS traffic, and itself uses to download
boot images ([decision 056](../../../../../docs/decisions/056-provisioner-http-proxy.md)). The
provisioner owns the setting (for MAAS, the region configuration `enable_http_proxy`,
`use_peer_proxy`, and `http_proxy`); swallow reads it live on every request, writes it through,
and stores nothing. It is not part of the Integration's `settings`
([sites-integrations.md](sites-integrations.md)).

Base path, bearer authentication, error envelope, and timestamps follow
[conventions.md](conventions.md); only endpoint-specific behavior is described below.

## Related Glossary Terms

- Provisioner HTTP Proxy
- Integration
- OS Provisioning Provider

## Authentication and Authorization

Bearer token; both endpoints require `admin`.

## Endpoints

```text
GET /api/v1/provisioning/integrations/{id}/http-proxy
PUT /api/v1/provisioning/integrations/{id}/http-proxy
```

| Path parameter | Meaning |
| --- | --- |
| `id` | The `integrationId` of a `provisioner` Integration. |

Both routes need the provisioner's HTTP Proxy capability (MAAS has it). Responses carry
`Cache-Control: no-store`.

## Modes

`mode` is a closed set (glossary Provisioner HTTP Proxy):

| `mode` | Deployed machines use | `httpProxy` |
| --- | --- | --- |
| `none` | No proxy; they reach package repositories directly. | `""` |
| `builtin` | The provisioner's own proxy (MAAS port 8000), which goes out directly. | `""` |
| `external` | The proxy at `httpProxy`, directly. The provisioner uses it too. | Required |
| `peer` | The provisioner's own proxy, which forwards to `httpProxy` as its upstream. | Required |

For MAAS the modes map onto its settings as follows; a write always sets all three:

| `mode` | `enable_http_proxy` | `use_peer_proxy` | `http_proxy` |
| --- | --- | --- | --- |
| `none` | `false` | `false` | `""` |
| `builtin` | `true` | `false` | `""` |
| `external` | `true` | `false` | `httpProxy` |
| `peer` | `true` | `true` | `httpProxy` |

A read derives the mode the way MAAS decides which proxy a deployed machine gets:
`enable_http_proxy` false is `none`; otherwise an empty `http_proxy` is `builtin`; otherwise
`use_peer_proxy` decides between `peer` and `external`. Settings changed outside swallow are
therefore reported by their effect, and `httpProxy` is `""` unless the mode is `external` or
`peer`, so a read can be written back unchanged.

## Read

`GET /api/v1/provisioning/integrations/{id}/http-proxy`

```json
{
  "integrationId": "c3d4…",
  "mode": "external",
  "httpProxy": "http://10.170.168.20:18080"
}
```

| Field | Meaning |
| --- | --- |
| `integrationId` | The Integration addressed. |
| `mode` | One of the modes above. |
| `httpProxy` | The proxy URL the provisioner holds for `external` and `peer`; `""` otherwise. |

## Write

`PUT /api/v1/provisioning/integrations/{id}/http-proxy` replaces the Provisioner HTTP Proxy and
returns `200 OK` with the Read shape, read back from the provisioner after the write.

```json
{
  "mode": "external",
  "httpProxy": "http://10.170.168.20:18080"
}
```

| Field | Required | Meaning |
| --- | --- | --- |
| `mode` | Yes | One of the modes above, matched case-insensitively and with surrounding whitespace removed; the response reports the lower-case value. |
| `httpProxy` | `external` and `peer` | An absolute `http` or `https` URL with a host and optional port (1–65535), for example `http://proxy.example.com:3128/`, stored and reported as `scheme://host[:port]`: a trailing `/` and surrounding whitespace are removed. Refused when it contains spaces or carries a user or password, a query, a fragment, or a path other than `/`. Must be absent or `""` for `none` and `builtin`. |

swallow validates the body before calling the provisioner; the provisioner validates the value
again and may refuse it. A write does not test that the proxy is reachable, and it affects only
deployments the provisioner starts afterwards; it never changes a deployed machine.

The provisioner applies the three settings one at a time. If a call fails part-way, the error is
returned and the provisioner may hold a mix of old and new settings, which the next Read reports;
repeating the same `PUT` converges.

## Error Codes

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | The body is not a JSON object; `mode` is missing or not a mode; `httpProxy` is missing, malformed, or does not apply to `mode`; the Integration is not a `provisioner` or has no adapter; the provisioner lacks the capability; or the provisioner refused the value (the message carries its explanation). |
| 404 | `not_found` | The Integration does not exist. |
| 503 | `provider_unavailable` | The provisioner could not be reached, the Integration has no credential, or its credential may not change provisioner settings (a MAAS account that is not an administrator). |

The Read answers `404` and `503` the same way, and `400` only for an Integration that is not a
capable provisioner.

## Compatibility Notes

New in 2026-10-13; additive. Adding a `mode` value is a glossary change first.

## Implementation Notes

The MAAS adapter reads with `GET /maas/?op=get_config&name=<setting>` and writes with
`POST /maas/?op=set_config` (multipart `name`, `value`), in the order `enable_http_proxy`,
`http_proxy`, `use_peer_proxy`. Clearing `http_proxy` sends an explicit empty `value`.
