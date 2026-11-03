# Platform Metrics Gateway

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- A Platform's own Grafana, which swallow installs with the Platform and configures with this
  gateway as its Prometheus datasource (a Slurm Platform's on its primary controller, a Kubernetes
  Platform's in its cluster; see [platforms.md](platforms.md#platform-monitoring-dashboards))

## Purpose

Let a Platform's Grafana read the Site Prometheus without reading anyone else's data. Prometheus
has no tenancy, so swallow authenticates the Platform's monitoring token and forwards the read to a
[prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy) in front of the Site
Prometheus, which forces every selector to the Platform's `platform_id` label. A Server's series
carry the `platform_id` of its Platform Claim
([discovery-prometheus.md](discovery-prometheus.md)), so a Platform sees exactly its members'
series, including those recorded while a Server was its member.

See [monitoring.md](../../../../../docs/development/monitoring.md) §7 and
[decision 003](../../../../../docs/decisions/003-metrics-label-contract.md) (metrics label contract).

## Related Glossary Terms

- [Platform](../../../../../docs/development/glossaries/terms/platform.md)
- [Platform Claim](../../../../../docs/development/glossaries/terms/platform-claim.md)
- [Site](../../../../../docs/development/glossaries/terms/site.md)

## Endpoints

The gateway's base is `/api/v1/platform-metrics/{platformId}/prometheus`; a Grafana datasource
uses it as its Prometheus URL. Below it, only the reads the label proxy confines exist:

```text
GET|POST /api/v1/platform-metrics/{platformId}/prometheus/api/v1/query
GET|POST /api/v1/platform-metrics/{platformId}/prometheus/api/v1/query_range
GET|POST /api/v1/platform-metrics/{platformId}/prometheus/api/v1/query_exemplars
GET      /api/v1/platform-metrics/{platformId}/prometheus/api/v1/series
GET|POST /api/v1/platform-metrics/{platformId}/prometheus/api/v1/labels
GET      /api/v1/platform-metrics/{platformId}/prometheus/api/v1/label/{name}/values
GET      /api/v1/platform-metrics/{platformId}/prometheus/federate
```

Any other path below the base is `404`. The wire shape is published in
[`openapi/areas/monitoring.yaml`](../openapi/areas/monitoring.yaml) (operation ids
`getPlatformMetrics*` and `postPlatformMetrics*`).

## Authentication

```text
Authorization: Bearer <the Platform's monitoring token>
```

- A Platform has one monitoring token at a time. swallow issues it when it accepts a deployment
  that installs the Platform's Grafana, hands it to that Grafana's install Task as a sealed secret,
  and stores only its hash. Every accepted deploy or redeploy of such a Platform issues a new one,
  and the previous one stops working; a refused deployment issues none.
- A missing token, another Platform's token, a replaced token, an unknown Platform, or any other
  credential (a Session or an API Key) is `401 unauthorized`, with the same message in every case.
- The token grants only these reads, only for its Platform.

## Behavior

- The query string and the form body are forwarded unchanged, except that a `platform_id` query
  parameter is dropped. The Platform's id is sent to the label proxy from the route, never from the
  request.
- The response is Prometheus's own (status, `Content-Type`, and body), for the Platform's series
  only: a query that selects another Platform's `platform_id` returns that Platform's series no
  more than any other selector does, because the proxy replaces the matcher.
- The label proxy is the one the Platform's Site's `metrics` Integration names in its
  `labelProxyUrl` setting ([sites-integrations.md](sites-integrations.md)).

## Errors

The common error envelope applies to the gateway's own refusals:

| Code | HTTP Status | When |
| --- | ---: | --- |
| `unauthorized` | 401 | No valid monitoring token for this Platform (see Authentication). |
| `not_found` | 404 | A path below the base that is not one of the reads above. |
| `provider_unavailable` | 503 | The Platform's Site has no metrics Integration with a `labelProxyUrl`, or the proxy cannot be reached. |

A query Prometheus or the proxy rejects is answered with their own status and error body.

## Compatibility Notes

- Added on 2026-11-03. Since the same day a Slurm Platform's Grafana reads it, and a Kubernetes
  Platform's Grafana too.
