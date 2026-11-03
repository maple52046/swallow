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

1. **The Site's Prometheus.** The [production installation](../installation.md) runs one
   Prometheus for its Site and registers it as that Site's metrics Integration
   (`site-prometheus`). It discovers the Site's exporters from swallow, so you never list
   hosts by hand, and it is not published on the host: swallow reads it for you. A Site has
   at most one metrics Integration.
2. **Other Sites.** In this version only the installation's Site has a Prometheus. The
   charts of any other Site say that no metrics backend is configured — they do not
   report its Servers as down.
3. **Deploy Servers as usual.** Every OS deployment installs the Node Exporter on the
   Servers it deploys (see [Exporters](#exporters)); a few minutes later the Server's
   **Monitoring** tab shows its usage history. A Server swallow did not deploy (for
   example one enrolled with its OS kept) gets it when you install the **Node Exporter**
   as Managed Software.

## Configure the integration

The installation's Site already has its metrics Integration. Optionally set the
Alertmanager endpoint (for alerts and silences) and a Grafana URL (for the optional
"Explore in Grafana" links) on it; rerunning the installation keeps them and only
points the endpoint back at the Site's Prometheus. The Integration sync state reports
upstream reachability and staleness.

Without an Alertmanager, Overview's Alerts card reads "Not configured"; that is a normal
state, not a failure. Overview warns "Alerts unavailable" only when a configured
Alertmanager cannot be read.

## Service discovery and labels

The Site's Prometheus consumes:

```text
GET /api/v1/discovery/prometheus/exporters?siteId=<site>
```

It lists only the exporters swallow knows are installed in that Site, so a Server
without an exporter is never a down target. Use the configured machine bearer token for
machine-to-machine access. Targets carry stable `server_id`, `site`, `platform_id`
(when the Server belongs to a Platform), and `exporter` labels. Do not replace these
with hostname or address joins.

## Exporters

swallow uses fixed ports, so the scrape target of a Server never depends on how its
exporters were installed:

| Exporter | Port | Installed on |
| --- | --- | --- |
| node-exporter | 9100 | every Server swallow deploys (CPU, memory, disk, network), from the OS package: `prometheus-node-exporter` on Debian and Ubuntu, `node_exporter` on the RHEL family (from EPEL) |
| AMD Device Metrics Exporter | 5001 | AMD GPU Servers: as Managed Software on a single Server, on a Slurm Platform's GPU compute nodes, or by the AMD GPU Operator on a Kubernetes Platform's GPU nodes when its Device Metrics Exporter is on; the same metric names everywhere |
| Omnistat | 8001 | a Slurm Platform's compute nodes when Omnistat is its job monitoring |
| [rdc-exporter](https://github.com/maple52046/rdc-exporter) | 5000 | swallow neither installs it on hosts nor collects it: the Site's Prometheus collects only the exporters swallow installed |

A Server has one Node Exporter, whatever Platform it belongs to: each OS deployment
installs it on the Servers it deploys, uninstalling a Platform keeps it (and installs it on
kept Servers that lack it), and you can install it as Managed Software on a Server swallow
did not deploy. A locked Server is left as it is: swallow neither installs nor removes
anything on it.

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

## A Platform's own Grafana

Every Platform gets its own read-only Grafana unless you turn it off when you deploy (the
**Platform Grafana** switch in the workspace's Features step, or `monitoringDashboards: false` in
the Platform's settings). CPU-only Platforms get one too. It reads the same Site Prometheus as
swallow, through swallow, and sees only the Platform's own Servers. It always has a **Servers**
dashboard (CPU, memory, root filesystem, network, and load per Server), plus the dashboards of the
Platform's GPU or job monitoring. The Platform page links to it as **Platform Grafana**; anyone on
the management network can view it without a login. Each deploy or redeploy gives it a new access
credential, so a credential copied from an older deployment stops working.

- **Slurm**: Grafana runs on the primary controller, at port 3000. It runs as a container by
  default; choose the native install method to install it from Grafana's apt/yum repository
  instead.
- **Kubernetes**: Grafana runs in the cluster and is reached at port 30300 of every workload node;
  the link names the first one. It keeps no data of its own, so it loses nothing when its Pod moves.
  When the AMD GPU Operator runs its Device Metrics Exporter, the Grafana adds the exporter's GPU
  dashboards. Uninstalling the Platform removes it with the cluster.

Turning the Grafana on later redeploys the Platform; turning it off after deployment is not
supported yet. A Slurm Platform deployed before this keeps its own Prometheus and the Grafana link
it had until its next deployment; a Kubernetes Platform deployed before this gets its Grafana at
its next deployment.

## GPU and job monitoring on Slurm

Choose the monitoring exporter in the Slurm workspace's Features step:

- **Omnistat** (proposed by default) collects host and Slurm job metrics on every compute node,
  and GPU metrics where there are AMD GPUs, so it also suits a CPU-only Platform. Each compute node
  runs one job at a time. Omnistat's `omnistat-query` job report tool on the compute nodes reads
  the metrics through the Platform's Grafana, so keep the Grafana on to use it (its report needs
  GPU utilization, which SR-IOV virtual functions do not report; on those GPUs use the job
  dashboards instead).
- **AMD Device Metrics Exporter** collects per-GPU metrics labelled with the job on each GPU, and
  jobs keep sharing nodes. It needs a compute node with AMD GPUs.
- **None** installs neither.

The Site Prometheus collects the chosen exporter like any other; the Platform page shows its
state. In a deploy file, `omnistat: { enabled: true }` or `deviceMetricsExporter: { enabled: true }`
chooses one; omitting both installs neither.

## Other monitoring UIs

You can link any other monitoring UI that belongs to a Platform:

- Register it as a **Monitoring Link** on the Platform. The Platform page shows
  it next to a shortcut to the Platform's usage charts in swallow.
- Keep swallow's exporters running. The Server charts keep working before, during,
  and after the Platform exists, and are the same for every Platform type.
- Do not bind a runtime's own collectors to port 9100, 5000, 5001, or 8001, which
  swallow's exporters use, or a Kubernetes Service to node port 30300, which the Platform Grafana
  uses.

Monitoring Links are set with the Platform update API. With the CLI:

```yaml
# patch.yaml
monitoringLinks:
  - name: Platform Grafana
    url: http://10.0.0.11:3000
  - name: Runbook
    url: https://wiki.example.internal/research-slurm
```

```bash
swallow platforms update plat1 -f patch.yaml
```

An empty list (`monitoringLinks: []`) removes the links. swallow never connects to a
Monitoring Link; it is a shortcut for users. swallow itself sets only the
`Platform Grafana` link, when it installs the Platform's Grafana.

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
