# Server Metrics

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`

## Purpose

Return current metric values for one or more servers from the metrics backend, and list
the fixed set of metric names a client may ask for. swallow stores no metrics; it
evaluates a bounded set of named PromQL queries against the backend and correlates results
by `server_id`. Exploration beyond these named queries belongs in Grafana, which this
contract deep-links to.

## Related Glossary Terms

- [Server](../../../../../docs/development/glossaries/terms/server.md)
- [Server Type](../../../../../docs/development/glossaries/terms/server-type.md)
- [Server Status](../../../../../docs/development/glossaries/terms/server-status.md)

See [decision 003](../../../../../docs/decisions/003-metrics-label-contract.md) for why the
query set is fixed rather than a PromQL passthrough.

## Endpoints

```text
GET /api/v1/monitoring/metrics
GET /api/v1/monitoring/metrics/names
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Metric Names

`GET /api/v1/monitoring/metrics/names` returns the metric names a client may request, as a
plain JSON array:

```json
["cpuUsagePercent", "memoryUsedPercent", "gpuUtilizationPercent", "gpuTemperatureCelsius", "gpuPowerWatts", "gpuMemoryUsedPercent"]
```

| Metric | Unit | Applies to | Meaning |
| --- | --- | --- | --- |
| `cpuUsagePercent` | percent | every server | Host CPU busy percentage (from node-exporter). |
| `memoryUsedPercent` | percent | every server | Host memory used percentage (from node-exporter). |
| `gpuUtilizationPercent` | percent | GPU servers | GPU utilisation, averaged per server. |
| `gpuTemperatureCelsius` | Celsius | GPU servers | Hottest GPU temperature per server. |
| `gpuPowerWatts` | watts | GPU servers | Total GPU power draw per server. |
| `gpuMemoryUsedPercent` | percent | GPU servers | GPU memory used percentage per server. |

The GPU metrics are vendor-neutral: each evaluates the NVIDIA DCGM series or the AMD RDC
series, so one metric name serves both stacks. A server that runs no GPU exporter simply
returns no value for these.

## Query

`GET /api/v1/monitoring/metrics`

| Param | Type | Required | Meaning |
| --- | --- | ---: | --- |
| `serverIds` | comma-separated string | Yes | The servers to query. At most 200 per request. |
| `metrics` | comma-separated string | No | Which metric names to evaluate; defaults to all names above. |

`serverIds` missing yields `validation_error`; more than 200 yields `validation_error`;
an unknown metric name yields `validation_error`.

## Response

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
- `grafana` is the configured Grafana base URL to deep-link into, or `null` when none is
  configured.

## Errors

The common error envelope applies. `validation_error` for missing `serverIds`, too many
servers, or an unknown metric name. `provider_unavailable` when no metrics backend is
registered or the backend cannot be reached. A query the backend rejects surfaces as
`validation_error`.

## Compatibility Notes

- Adding a new named metric is additive: existing callers that pass an explicit `metrics`
  list are unaffected, and callers that omit it receive the new metric.
- Metric values are point-in-time instant reads, not time series; historical exploration
  is a Grafana concern reached through the `grafana` link.
