# Server Metrics

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Return current metric values and bounded usage history for Servers from the metrics backend,
and list the fixed set of metric names a client may ask for. swallow stores no metrics; it
evaluates a bounded set of named PromQL queries against the backend and correlates results
by `server_id`.

These endpoints are what swallow's own usage charts are built on: an operator sees CPU,
memory, disk, network, and GPU usage of a Server, a selection of Servers, a Pool, a Zone, or a
Platform without leaving swallow. Grafana is an optional advanced layer for exploration beyond
the named queries; when the metrics Integration configures it, responses carry its link, and
nothing here depends on it.

## Related Glossary Terms

- [Server](../../../../../docs/development/glossaries/terms/server.md)
- [Server Type](../../../../../docs/development/glossaries/terms/server-type.md)
- [Server Status](../../../../../docs/development/glossaries/terms/server-status.md)
- [Pool](../../../../../docs/development/glossaries/terms/pool.md)
- [Zone](../../../../../docs/development/glossaries/terms/zone.md)
- [Platform](../../../../../docs/development/glossaries/terms/platform.md)

See [decision 003](../../../../../docs/decisions/003-metrics-label-contract.md) for why the
query set is fixed rather than a PromQL passthrough.

## Endpoints

```text
GET /api/v1/monitoring/metrics
GET /api/v1/monitoring/metrics/range
GET /api/v1/monitoring/metrics/names
```

All endpoints require the `admin` role, with either bearer credential (a Session access token
or an API Key) according to [conventions](conventions.md). The wire shape is published in
[`openapi/areas/monitoring.yaml`](../openapi/areas/monitoring.yaml) (`getMetricValues`,
`getMetricRange`, `listMetricNames`).

## Metric Names

`GET /api/v1/monitoring/metrics/names` returns the metric names a client may request, as a
plain JSON array. The order is not significant:

```json
["cpuUsagePercent", "memoryUsedPercent", "diskUsedPercent", "networkReceiveBytesPerSecond", "networkTransmitBytesPerSecond", "gpuUtilizationPercent", "gpuTemperatureCelsius", "gpuPowerWatts", "gpuMemoryUsedPercent"]
```

| Metric | Unit | Applies to | Meaning | Scope aggregate |
| --- | --- | --- | --- | --- |
| `cpuUsagePercent` | percent | every server | Host CPU busy percentage (from node-exporter). | average |
| `memoryUsedPercent` | percent | every server | Host memory used percentage (from node-exporter). | average |
| `diskUsedPercent` | percent | every server | Used percentage of the root filesystem (`/`) (from node-exporter). | average |
| `networkReceiveBytesPerSecond` | bytes per second | every server | Received throughput summed across the host's network interfaces (from node-exporter). | sum |
| `networkTransmitBytesPerSecond` | bytes per second | every server | Transmitted throughput summed across the host's network interfaces (from node-exporter). | sum |
| `gpuUtilizationPercent` | percent | GPU servers | GPU utilisation, averaged per server. | average |
| `gpuTemperatureCelsius` | Celsius | GPU servers | Hottest GPU temperature per server. | maximum |
| `gpuPowerWatts` | watts | GPU servers | Total GPU power draw per server. | sum |
| `gpuMemoryUsedPercent` | percent | GPU servers | GPU memory used percentage per server. | average |

The network metrics are a 5-minute rate and exclude the loopback interface and the virtual
devices container runtimes and hypervisors create (`veth*`, `docker*`, `br-*`, `virbr*`,
`cali*`, `flannel*`, `cni*`, `vxlan*`, `tun*`, `tap*`, `kube-*`), so traffic bridged into a
container is not counted twice. A bonded interface and its members are both physical-looking
and are both counted.

The GPU metrics are vendor-neutral: each evaluates the NVIDIA DCGM series or the AMD RDC
series, so one metric name serves both stacks. A server that runs no GPU exporter simply
returns no value for these.

**Scope aggregate** is how a metric combines several Servers into one series in the range
query's `aggregate` mode (below): utilisation and fill levels average, throughput and power
add up, and temperature keeps the hottest. At each timestamp the aggregate covers only the
Servers that had a value then; a Server without data is not counted as zero.

## Current Values

`GET /api/v1/monitoring/metrics`

| Param | Type | Required | Meaning |
| --- | --- | ---: | --- |
| `serverIds` | comma-separated string | Yes | The servers to query. At most 200 per request. |
| `metrics` | comma-separated string | No | Which metric names to evaluate; defaults to all names above. |

`serverIds` missing yields `validation_error`; more than 200 yields `validation_error`;
a Server id containing a character other than a letter, digit, `-`, or `_` yields
`validation_error` (ids are interpolated into a query, so they are validated rather than
escaped); an unknown metric name yields `validation_error`.

### Response

`200 OK`:

```json
{
  "items": [
    { "serverId": "srv-abc123", "metrics": { "cpuUsagePercent": 12.5, "memoryUsedPercent": 47.1 } }
  ],
  "grafana": "https://grafana.example.internal"
}
```

- `items` has one entry per requested server, in the requested order.
- `metrics` contains only the values the backend answered. A metric with no data is
  **absent** from the map rather than zero, because "no data" and "zero" are different
  facts — a CPU server has no `gpuUtilizationPercent`, and a server that is not scraped
  yet has no `cpuUsagePercent`. A consumer must render absence as "no data", not `0`.
- `grafana` is the configured Grafana base URL for optional advanced exploration, or `null`
  when none is configured.

## Usage History

`GET /api/v1/monitoring/metrics/range` returns bounded time series for the named metrics over a
**scope**: an explicit list of Servers, or the Servers currently in a Pool, a Zone, or a
Platform.

| Param | Type | Required | Meaning |
| --- | --- | ---: | --- |
| `serverIds` | comma-separated string | One scope parameter | Explicit Servers. At most 200, with the same id character set as current values. |
| `poolId` | string | One scope parameter | The Servers currently placed in this Pool. |
| `zoneId` | string | One scope parameter | The Servers currently placed in this Zone. |
| `platformId` | string | One scope parameter | The Servers whose membership axis names this Platform. |
| `metrics` | comma-separated string | No | Which metric names to evaluate; defaults to all names above. |
| `window` | string | No | `1h`, `6h`, `24h`, or `7d`, ending now. Default `1h` when no `start`/`end` is given. |
| `start` | timestamp | With `end` | Explicit range start (RFC 3339, the ISO 8601 profile of the conventions). Replaces `window`. |
| `end` | timestamp | With `start` | Explicit range end (RFC 3339). |
| `mode` | string | No | `aggregate` (default) returns one series per metric for the whole scope; `perServer` returns one series per metric and Server. |

### Scope resolution

Exactly one scope parameter must be present.

- A Pool or Zone contains the non-absent Servers of its Site whose provider-observed resource
  pool or zone (`providerResourcePool` / `providerZone` on the Server projection) equals the
  grouping's name — the observed effect of a placement, see [infrastructure.md](infrastructure.md).
- A Platform contains the non-absent Servers whose membership axis names it, the same set as
  `GET /api/v1/servers/?platformId=`.

The scope is resolved when the request is served, so the history shown is that of the
grouping's **current** members: a Server that left the Pool yesterday is not part of today's
query, and one that joined an hour ago contributes its whole history. A resolved scope larger
than 1000 Servers is refused with `validation_error`; ask for a narrower scope.

### Time range and resolution

`window` and `start`/`end` are mutually exclusive. `start` and `end` must be sent together,
`start` must be before `end`, and the span must be between 5 minutes and 7 days.

swallow chooses the resolution: `stepSeconds = max(30, ceil(span / 180 / 30) × 30)`, so a series
has at most about 180 points and the step is a whole multiple of 30 seconds. For the windows
this gives 30 s (`1h`), 120 s (`6h`), 480 s (`24h`), and 3360 s (`7d`). A client cannot ask for a
finer step; the bound is what keeps a chart request from becoming a fleet-wide scan.

### Mode

`aggregate` combines the scope's Servers per metric using the **Scope aggregate** column above;
for a one-Server scope it is that Server's own series. `perServer` returns each Server's own
series and is limited to scopes of at most 50 Servers; a larger scope yields
`validation_error`.

### Response

`200 OK`:

```json
{
  "scope": { "kind": "pool", "id": "pool-id", "serverCount": 12 },
  "mode": "aggregate",
  "start": "2026-10-12T00:00:00Z",
  "end": "2026-10-12T01:00:00Z",
  "stepSeconds": 30,
  "series": [
    {
      "metric": "cpuUsagePercent",
      "serverId": null,
      "points": [
        { "timestamp": "2026-10-12T00:00:00Z", "value": 12.5 },
        { "timestamp": "2026-10-12T00:00:30Z", "value": 13.1 }
      ]
    }
  ],
  "grafana": null
}
```

- `scope.kind` is `servers`, `pool`, `zone`, or `platform`; `scope.id` is the grouping id, or
  `null` for `servers`. `scope.serverCount` is how many Servers the scope resolved to.
- `start` and `end` are the evaluated range; `end` is the request time for a `window`.
- `series` contains only series with at least one point, ordered by the requested metric order
  and, in `perServer` mode, by the scope's Server order. `serverId` is `null` in `aggregate`
  mode. A metric with no data has **no series** — a consumer renders it as "no data", never as
  a flat zero line.
- `points` are in ascending time order at `stepSeconds` spacing. A gap (missing timestamps)
  means the backend had no value then, for example while a host was down; consumers must not
  interpolate across it as if it were data.
- A scope that resolves to zero Servers returns `200` with `serverCount: 0` and no series
  without contacting the metrics backend.
- `grafana` is the same optional advanced-exploration link as above; charts do not depend on it.

## Errors

The common error envelope applies.

| Code | HTTP Status | When |
| --- | ---: | --- |
| `validation_error` | 400 | Missing `serverIds` (current values); no or more than one scope parameter; more than 200 `serverIds`; a Server id outside the letter, digit, `-`, `_` set; an unknown metric name; an unknown `window` or `mode`; `window` together with `start`/`end`; only one of `start`/`end`, an unparsable timestamp, `start` not before `end`, or a span outside 5 minutes–7 days; a scope over 1000 Servers; `perServer` over 50 Servers; a query the backend rejects. |
| `not_found` | 404 | The `poolId`, `zoneId`, or `platformId` does not exist. |
| `provider_unavailable` | 503 | No metrics backend is registered, or the backend cannot be reached or rejects swallow's credential. A client tells the operator to register a metrics Integration for the Site rather than reporting the Servers as down. |
| `unauthorized` / `forbidden` | 401 / 403 | Missing or invalid credential / a non-admin caller (conventions). |
| `internal_error` | 500 | An unexpected failure. |

## Compatibility Notes

- Adding a new named metric is additive: existing callers that pass an explicit `metrics`
  list are unaffected, and callers that omit it receive the new metric.
- `diskUsedPercent`, `networkReceiveBytesPerSecond`, `networkTransmitBytesPerSecond`, and the
  range endpoint were added on 2026-10-12. They are additive.
- Current values are point-in-time instant reads; usage history is the bounded range endpoint.
  Neither is an arbitrary PromQL passthrough.

## Implementation Notes

- The current-value and range endpoints evaluate the same named expressions, so a chart and a
  tile for the same metric cannot disagree about what they measure. `aggregate` wraps the
  per-Server expression in the metric's scope aggregate.
- Range queries are sent to the backend as a form-encoded `POST /api/v1/query_range`, so a
  large scope does not hit URL-length limits.
