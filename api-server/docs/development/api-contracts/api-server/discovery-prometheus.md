# Prometheus Service Discovery

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- Prometheus (per-site), via `http_sd_config`
- Swallow installation tooling

## Purpose

Serve the Prometheus HTTP service-discovery target list for host-layer exporters, with
the metrics label contract already attached, so a metric can be joined back to a swallow
server without a lookup. The same projection also targets one server type at a time — the
RDC exporter job asks for AMD GPU servers only — so a single endpoint feeds several scrape
jobs.

## Related Glossary Terms

- [Server](../../../../../docs/development/glossaries/terms/server.md)
- [Server Type](../../../../../docs/development/glossaries/terms/server-type.md)
- [Server Status](../../../../../docs/development/glossaries/terms/server-status.md)

See also [decision 003](../../../../../docs/decisions/003-metrics-label-contract.md) for the
label contract and monitoring topology.

## Endpoints

```text
GET /api/v1/discovery/prometheus
```

## Authentication

This endpoint uses **machine authentication**, not the interactive login flow: the caller
presents a static machine bearer token, or, for operator inspection, an admin JWT.

```text
Authorization: Bearer <machineToken | adminAccessToken>
```

A missing or invalid credential yields `401` with code `unauthorized`. A non-admin JWT
yields `403` with code `forbidden`.

## Query Parameters

| Param | Type | Required | Default | Meaning |
| --- | --- | ---: | --- | --- |
| `port` | integer (1–65535) | No | `9100` | Exporter port to place in each target. `9100` is node-exporter; `5000` is the RDC exporter. |
| `tag` | string | No | — | Restrict targets to servers carrying this provisioner tag. The RDC exporter job passes `tag=amd-gpu` so it only targets AMD GPU servers. |
| `siteId` | string | No | — | Restrict to one site. |
| `provisioningState` | string | No | `deployed` | Provisioning state filter. The literal `all` removes the filter. |

`port` out of range yields `400` with code `validation_error`.

## Response

`200 OK` with a JSON array in Prometheus `http_sd` shape. Each entry is one target with
its labels already attached:

```json
[
  {
    "targets": ["10.0.1.10:9100"],
    "labels": {
      "server_id": "srv-abc123",
      "site": "site-id",
      "cluster": "cluster-id"
    }
  }
]
```

- `server_id` and `site` are always present. `cluster` is present only when the server is
  a known cluster member.
- A server with no known address is omitted rather than emitted with an empty target,
  because a target with no address would create a permanently failing series attributed
  to that server.
- A locked machine is omitted: it is off-limits ("unmanaged"), swallow installs no
  exporter on it, and scraping it would only produce a permanently-down series that makes
  an unmanaged machine look unhealthy. Its health stays unknown instead.
- The result is never paginated: a scrape target list must be complete.
- Label names are the canonical join keys from
  [decision 003](../../../../../docs/decisions/003-metrics-label-contract.md); do not add
  identity labels beyond this set.

## Errors

The common error envelope applies. `validation_error` for an out-of-range `port`;
`unauthorized`/`forbidden` per the authentication section; `internal_error` for an
unexpected failure.

## Compatibility Notes

- `tag` is additive and optional; a caller that omits it gets every discoverable server as
  before.
- The label set is a contract: adding a label used to identify a server is a decision
  document ([decision 003](../../../../../docs/decisions/003-metrics-label-contract.md)),
  not a compatible response change.
