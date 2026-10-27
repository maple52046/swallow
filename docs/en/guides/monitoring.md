# Monitoring

[繁體中文](../../zh-TW/guides/monitoring.md) · [Documentation home](../README.md)

swallow shows how much of a Server is being used without you leaving swallow. It
integrates the Site's Prometheus-compatible metrics backend and draws basic usage
charts — CPU, memory, disk, network, and GPU — for one Server, a selection of
Servers, a Pool, a Zone, or a Platform. swallow does not store metrics or alerts and
does not offer an arbitrary PromQL proxy; it evaluates a fixed set of named queries.
Grafana is optional: connect one when you want exploration beyond the basic charts.

> The Dashboard's monitoring screens are in development. Release builds of the
> Dashboard hide them and show health as "Not available in this release"; see
> [Features in development](dashboard.md#features-in-development). Registering
> the Integration, Monitoring Links, the API, and the CLI work as described here.

## From a deployed Server to a chart

1. **Run a Prometheus for the Site.** Use the per-Site template in
   [`deploy/third-party/prometheus`](../../../deploy/third-party/prometheus/README.md).
   It discovers targets from swallow, so you never list hosts by hand.
2. **Register it as the Site's metrics Integration** (below). Until a metrics
   Integration exists, the charts say that no metrics backend is configured —
   they do not report your Servers as down.
3. **Deploy Servers as usual.** When a Server reaches `deployed`, swallow installs
   its exporters automatically (see [Exporters](#exporters)); a few minutes later
   the Server's **Monitoring** tab shows its usage history.

## Configure the integration

Register a Site-scoped Prometheus-compatible metrics Integration. Optionally set the
Alertmanager endpoint (for alerts and silences) and a Grafana URL (for the optional
"Explore in Grafana" links). The Integration sync state reports upstream reachability
and staleness.

Without an Alertmanager, Overview's Alerts card reads "Not configured"; that is a normal
state, not a failure. Overview warns "Alerts unavailable" only when a configured
Alertmanager cannot be read.

## Service discovery and labels

Prometheus consumes:

```text
GET /api/v1/discovery/prometheus
```

Use the configured machine bearer token for machine-to-machine access. Targets
carry stable `server_id`, `site`, and canonical Platform labels. Do not
replace these with hostname or address joins.

## Exporters

swallow uses fixed ports, so the scrape target of a Server never depends on who
installed its exporters:

| Exporter | Port | Installed on |
| --- | --- | --- |
| node-exporter | 9100 | every managed Server (CPU, memory, disk, network) |
| [rdc-exporter](https://github.com/maple52046/rdc-exporter) | 5000 | AMD GPU Servers, identified by the tag `amd-gpu` |

Exactly one owner installs each Server's exporters:

- a Server in no Platform is owned by swallow, which installs the exporters after OS
  deployment;
- a member of a Kubernetes Platform follows the Platform's `exporterOwner`
  (`ansible` — swallow installs them — or `k8s` — a DaemonSet on the same ports);
- a locked Server is unmanaged: swallow neither installs nor removes anything.

## Usage charts

- **Server detail → Monitoring** shows the latest values and the Server's usage
  history. GPU charts appear on GPU Servers.
- **Monitoring → Usage charts** charts a scope: tick Servers in the Server metrics
  table, or choose a Pool, a Zone, or a Platform. The scope and the time window are
  part of the URL, so a link reproduces the view.

Choose a window of 1 hour, 6 hours, 24 hours, or 7 days; swallow picks the
resolution (about 180 points per chart). For a group of Servers each chart is one
combined line: utilisation and fill levels are averaged, network throughput and GPU
power are added up, and GPU temperature shows the hottest Server. A Pool, Zone, or
Platform is charted by its **current** members.

A chart reads "No data" when the backend has nothing for that metric — for example a
GPU chart on a Server without a GPU exporter. A gap in a line means the Server was not
reporting then. Neither is drawn as zero.

## Platforms with their own monitoring

Some runtimes bring a complete monitoring stack of their own — for example
[Omnistat](https://github.com/ROCm/omnistat) on a Slurm Platform, with its own
Prometheus and Grafana. When you deploy a Slurm Platform you can let swallow install
Omnistat for you (turn on **Features → Omnistat GPU monitoring** in the Slurm workspace, or
`omnistat: { enabled: true }` in the deploy file): the primary controller gets Prometheus, which
answers only on that host's loopback address because its API has no login, and a read-only
Grafana with Omnistat's job dashboards; every compute node gets the exporter and Omnistat's
`omnistat-query` job report tool, which reads Prometheus through that Grafana (its report needs
GPU utilization, which SR-IOV virtual functions do not report, so on those GPUs use the job
dashboards instead), and the
Platform page shows the Omnistat state and an `Omnistat Grafana` link. Prometheus and Grafana run
as containers by default; choose the native install method to install them from their official
release and apt/yum repository instead. swallow does not scrape or query such a stack. Instead:

- Register its UI as a **Monitoring Link** on the Platform. The Platform page shows
  it next to a shortcut to the Platform's usage charts in swallow.
- Keep swallow's exporters running. The Server charts keep working before, during,
  and after the Platform exists, and are the same for every Platform type.
- Do not bind the runtime's own collectors to port 9100 or 5000. Omnistat's collector
  listens on its own port by default; keep it that way so it never contends with
  swallow's exporters.

Monitoring Links are set with the Platform update API. With the CLI:

```yaml
# patch.yaml
monitoringLinks:
  - name: Omnistat Grafana
    url: https://omnistat.example.internal/grafana
```

```bash
swallow platforms update plat1 -f patch.yaml
```

An empty list (`monitoringLinks: []`) removes the links. swallow never connects to a
Monitoring Link; it is a shortcut for operators.

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
swallow monitoring metrics range --server server1 --window 6h
swallow monitoring metrics range --pool pool1 --window 24h
swallow discovery prometheus --machine-token "$SWALLOW_MACHINE_TOKEN"
```

See the active [metrics contract](../../../api-server/docs/development/api-contracts/api-server/server-metrics.md),
[alerts contract](../../../api-server/docs/development/api-contracts/api-server/monitoring-alerts.md),
and [metrics label decision](../../decisions/003-metrics-label-contract.md).
