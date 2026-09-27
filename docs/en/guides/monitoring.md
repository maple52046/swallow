# Monitoring

[繁體中文](../../zh-TW/guides/monitoring.md) · [Documentation home](../README.md)

swallow correlates monitoring data with stable resource identities. It does not
store metrics or alerts and does not provide an arbitrary PromQL proxy.

## Configure the integration

Register a Site-scoped Prometheus-compatible metrics Integration. Configure the
Alertmanager endpoint and Grafana URL required by your deployment. The
Integration sync state reports upstream reachability and staleness.

## Service discovery and labels

Prometheus consumes:

```text
GET /api/v1/discovery/prometheus
```

Use the configured machine bearer token for machine-to-machine access. Targets
carry stable `server_id`, `site`, and canonical Platform labels. Do not
replace these with hostname or address joins.

Exporter installation is resolved per Server so exactly one owner installs each
fixed-port exporter. Locked Servers are unmanaged by automatic exporter
installation.

## Metrics

The Dashboard requests a fixed named metric set for selected Servers. Unknown or
missing data stays unknown; it does not imply a Server is down. Grafana remains
the tool for exploratory queries and long-range analysis.

## Alerts and acknowledgements

Alerts are read from Alertmanager on demand and correlated to swallow resources.
Acknowledging an alert creates an Alertmanager silence; swallow does not add an
acknowledged field to a local alert copy.

Before silencing, review matchers, duration, Site scope, and the affected
Servers. Diagnose a provider-unavailable error from the Integration first rather
than treating it as a fleet health result.

## CLI

```bash
swallow monitoring alerts list --site-id site1
swallow monitoring metrics names
swallow monitoring metrics get --server server1
swallow discovery prometheus --machine-token "$SWALLOW_MACHINE_TOKEN"
```

See the active [monitoring contracts](../../../api-server/docs/development/api-contracts/api-server/monitoring-alerts.md)
and [metrics label decision](../../decisions/003-metrics-label-contract.md).
