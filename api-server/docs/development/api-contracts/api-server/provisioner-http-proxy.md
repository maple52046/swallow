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
boot images ([decision 056](../../../../../docs/decisions/056-provisioner-http-proxy.md)). swallow
owns the setting as desired state
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)): a write
stores the spec, and the HTTP Proxy controller writes it to the provisioner (for MAAS, the region
configuration `enable_http_proxy`, `use_peer_proxy`, and `http_proxy`) and reads it back. A change
made in the provisioner is drift, put back at swallow's periodic resync (every 10 minutes by
default). The first read of an Integration adopts the provisioner's current configuration as the
spec, without writing to it. It is not part of the Integration's `settings`
([sites-integrations.md](sites-integrations.md)).

Base path, bearer authentication, error envelope, and timestamps follow
[conventions.md](conventions.md); only endpoint-specific behavior is described below.

## Related Glossary Terms

- Provisioner HTTP Proxy
- Integration
- OS Provisioning Provider
- Controller
- Condition

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
therefore observed by their effect, and `httpProxy` is `""` unless the mode is `external` or
`peer`, so a spec can be written back unchanged.

## Read

`GET /api/v1/provisioning/integrations/{id}/http-proxy`

```json
{
  "integrationId": "c3d4…",
  "generation": 2,
  "spec": { "mode": "external", "httpProxy": "http://10.170.168.20:18080" },
  "status": {
    "observedGeneration": 2,
    "conditions": [
      { "type": "realized", "status": "true", "reason": "converged", "message": "", "lastTransitionTime": "2026-10-31T04:00:00Z", "observedGeneration": 2 },
      { "type": "drifted", "status": "false", "reason": "in_sync", "message": "", "lastTransitionTime": "2026-10-31T04:00:00Z", "observedGeneration": 2 },
      { "type": "requires_attention", "status": "false", "reason": "none", "message": "", "lastTransitionTime": "2026-10-31T04:00:00Z", "observedGeneration": 2 }
    ],
    "observed": { "mode": "external", "httpProxy": "http://10.170.168.20:18080" },
    "observedAt": "2026-10-31T04:10:00Z"
  }
}
```

| Field | Meaning |
| --- | --- |
| `integrationId` | The Integration addressed. |
| `generation` | Counts spec changes; `1` once adopted or first written. |
| `spec.mode`, `spec.httpProxy` | What swallow wants: one of the modes above, and the proxy URL for `external` and `peer` (`""` otherwise). |
| `status.observedGeneration` | The generation the provisioner holds. |
| `status.conditions` | The controller's judgement, with the meanings of [servers-list.md](servers-list.md#spec-and-status): `realized` once the provisioner holds the spec; `drifted` when it was changed outside swallow; `requires_attention` when the provisioner refused the spec (the message carries its explanation), until the spec changes. |
| `status.observed`, `status.observedAt` | What the provisioner held when swallow last read it, and when; both `null` before the first read. |

When swallow holds no setting for the Integration yet, the read takes the provisioner's current
configuration as the spec (generation `1`, realized) before answering. This is the only read
that calls the provisioner.

## Write

`PUT /api/v1/provisioning/integrations/{id}/http-proxy` replaces the spec and returns `200 OK` with
the Read shape: the new spec and generation, and the status as it was (the provisioner has not been
written yet).

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

swallow validates the body and stores it; the controller then writes it to the provisioner,
which validates the value again and may refuse it (reported as `requires_attention`). A write does
not test that the proxy is reachable, and it affects only deployments the provisioner starts
afterwards; it never changes a deployed machine.

The provisioner applies the three settings one at a time. If a call fails part-way, the
controller retries until the provisioner holds the spec.

## Error Codes

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | The body is not a JSON object; `mode` is missing or not a mode; `httpProxy` is missing, malformed, or does not apply to `mode`; the Integration is not a `provisioner` or has no adapter; or the provisioner lacks the capability. |
| 404 | `not_found` | The Integration does not exist. |
| 503 | `provider_unavailable` | The Integration has no credential, or an adopting Read could not reach the provisioner or its credential may not read provisioner settings. |

The Read answers `404` the same way, and `400` only for an Integration that is not a capable
provisioner.

## Compatibility Notes

New in 2026-10-13; additive. Adding a `mode` value is a glossary change first.

On 2026-10-31 the setting became desired state
([decision 071](../../../../../docs/decisions/071-provisioner-realized-desired-state.md)). Breaking,
with no alias: the response is `{integrationId, generation, spec, status}` instead of
`{integrationId, mode, httpProxy}`; a write stores the spec and no longer reports the read-back or
returns a provisioner refusal (the conditions do).

## Implementation Notes

The setting is stored in `provisioner_http_proxies`, keyed by Integration. The HTTP Proxy controller
runs in the worker; it reads the provisioner on every pass (a spec change, an Integration change,
and the resync) and writes the status. The MAAS adapter reads with
`GET /maas/?op=get_config&name=<setting>` and writes with `POST /maas/?op=set_config` (multipart
`name`, `value`), in the order `enable_http_proxy`, `http_proxy`, `use_peer_proxy`. Clearing
`http_proxy` sends an explicit empty `value`.
