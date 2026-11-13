# Monitoring

[繁體中文](../../zh-TW/guides/monitoring.md) · [Documentation home](../README.md)

swallow shows how much of a Server is being used without you leaving swallow. It
integrates the Site's Prometheus-compatible metrics backend and shows basic
monitoring — Server health and CPU, memory, disk, and network usage — for one Server,
a selection of Servers, a Pool, a Zone, or a Platform. swallow does not store metrics
or alerts and does not offer an arbitrary PromQL proxy; it evaluates a fixed set of
named queries. A Platform's complete monitoring, GPU metrics included, is in its own
[Grafana](#a-platforms-own-grafana). Grafana is optional for swallow itself: connect
one when you want exploration beyond the basic charts.

> Alerts in the Dashboard are in development. Release builds of the Dashboard show
> health, current metrics, and usage charts, but hide the alert list and its
> acknowledgements, and Overview's Alerts card reads "Not available in this
> release"; see [Features in development](dashboard.md#features-in-development).
> Alerts through the API and the CLI work as described here.

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
   **Monitoring** tab shows its usage history. swallow also keeps it installed on every
   other deployed Server it automates (for example one enrolled with its OS kept), so you
   do not install it by hand.

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
| node-exporter | 9100 | every deployed Server swallow automates (CPU, memory, disk, network), from the OS package: `prometheus-node-exporter` on Debian and Ubuntu, `node_exporter` on the RHEL family (from EPEL) |
| AMD Device Metrics Exporter | 5001 | AMD GPU Servers: as Managed Software on a single Server, on a Slurm Platform's GPU compute nodes, or by the AMD GPU Operator on a Kubernetes Platform's GPU nodes when its Device Metrics Exporter is on; the same metric names everywhere |
| Omnistat | 8001 | a Slurm Platform's compute nodes when Omnistat is its job monitoring |
| [rdc-exporter](https://github.com/maple52046/rdc-exporter) | 5000 | swallow neither installs it on hosts nor collects it: the Site's Prometheus collects only the exporters swallow installed |

A Server has one Node Exporter, whatever Platform it belongs to: each OS deployment
installs it on the Servers it deploys, and uninstalling a Platform keeps it (and installs it
on kept Servers that lack it).

### The Node Exporter is kept installed

swallow keeps the Node Exporter installed on every deployed, unlocked Server of a Site whose
automation is enabled, including Servers it did not deploy. The worker checks every Server
about every ten minutes, and straight away when a Server or its Node Exporter record changes:

- When the Server has no Node Exporter, or it was uninstalled, swallow installs it. Only an admin
  can uninstall it.
- When an install failed, swallow tries again after 15 minutes, 1 hour, 6 hours, and then
  once a day, so a Server whose OS lacks the package does not get an install loop.
- When the Site's Prometheus could not scrape it for 10 minutes, for example because the
  package was removed or its service stopped, swallow installs it again. A Server its
  provisioner reports powered off is left alone.

These installs show up as ordinary **Install Node Exporter** Workflows requested by
`swallow:node-exporter-controller`. When one of them fails, swallow cancels it with the reason
"The Node Exporter install failed; swallow retries it later." so the Server is free for other
work; it never cancels a Workflow anyone else started, and it waits while other work holds the
Server. swallow never removes the Node Exporter by itself. A locked Server is left as it is:
swallow neither installs nor removes anything on it. To keep the Node Exporter off one Server,
lock it; uninstalling it from a Server swallow keeps it on only lasts until the next check.

While an install is pending or after it failed, the Server is not scraped, so its health shows
as Unknown rather than Down.

## Server health

Every Server has one of four health values, worked out from swallow's own monitoring policy
each time you look at it. The Dashboard labels them **Up**, **Down**, **Unknown**, and
**N/A**; the API values are `up`, `down`, `unknown`, and `not_applicable`.

| Health | Meaning |
| --- | --- |
| **Up** | swallow has the Site's Prometheus scrape the Server's Node Exporter, and the latest scrape succeeded. |
| **Down** | The latest scrape of the Server's Node Exporter failed: the host, its network, or the exporter is not answering. This is a failure. |
| **Unknown** | swallow expects to monitor the Server but has no answer: the Node Exporter install is pending or failed, an imported Server has not got it yet, or the Site's Prometheus has no sample for it or cannot be read. This is a warning, not a failure. |
| **N/A** | swallow does not monitor the Server, for example because it is not deployed (ready, releasing), it is locked, it is absent, or its Site's automation is off. This says nothing about the machine. |

- Only the Node Exporter decides health; GPU exporters do not.
- When swallow releases a Server, its health turns N/A at once, even though the Site's
  Prometheus keeps the Server's last data for a while.
- A locked Server that swallow already scrapes keeps showing Up or Down.
- With no Prometheus for the Site, a monitored Server shows Unknown; it is never shown as
  Down for that reason.

## Usage charts

- **Server detail → Monitoring** shows the latest values and the Server's usage
  history: CPU, memory, root filesystem, and network.
- **Monitoring → Usage charts** charts a scope: tick Servers in the Server metrics
  table, or choose a Pool, a Zone, or a Platform. The scope and the time window are
  part of the URL, so a link reproduces the view.

Choose a window of 1 hour, 6 hours, 24 hours, or 7 days; swallow picks the
resolution (about 180 points per chart). For a group of Servers each chart is one
combined line: utilisation and fill levels are averaged, and network throughput is
added up. A Pool, Zone, or Platform is charted by its **current** members. A Pool
may hold Servers of several Sites, and each Site keeps its own metrics, so a
combined chart of such a Pool is refused; chart its Servers per Site instead.

swallow's charts are the same for every Server, with or without GPUs; GPU metrics are
shown by the Platform's own Grafana, not in swallow.

A chart reads "No data" when the backend has nothing for that metric — for example
while a newly deployed Server's Node Exporter has not been scraped yet. A gap in a
line means the Server was not reporting then. Neither is drawn as zero.

## A Platform's own Grafana

Every Platform gets its own read-only Grafana unless you turn it off when you deploy (the
**Platform Grafana** switch in the workspace's Features step, or `monitoringDashboards: false` in
the Platform's settings). CPU-only Platforms get one too. It reads the same Site Prometheus as
swallow, through swallow, and sees only the Platform's own Servers. It always has a **Servers**
dashboard (CPU, memory, root filesystem, network, and load per Server), plus the dashboards of the
Platform's GPU or job monitoring. The Platform page links to it as **Platform Grafana**, which
opens its main dashboard directly: the Device Metrics Exporter's **Overview** or Omnistat's
**Global - RMS** when the Platform has that monitoring, otherwise **Servers**. Anyone on the
management network can view it without a login. Each deploy or redeploy gives it a new access
credential, so a credential copied from an older deployment stops working.

The Platform workspace's **Monitoring** route collects these destinations without embedding
their dashboards. It links to Swallow usage charts, the Platform Grafana, other Monitoring
Links, and the recorded installation state and Workflow for Omnistat or GPU monitoring.
On an active Platform, the workspace hero also offers **Grafana**, which opens the Platform Grafana.

- **Slurm**: Grafana runs on the primary controller, at port 3000. It runs as a container by
  default, so the deploy installs a container engine on the primary controller first: the one
  the deploy requests under **Container runtime**, otherwise Docker CE, unless that Server
  already has one. Choose the native install method to install it from Grafana's apt/yum
  repository instead.
- **Kubernetes**: Grafana runs in the cluster and is reached at port 30300 of every workload node;
  the link opens it on the first one. It keeps no data of its own, so it loses nothing when its Pod moves.
  When the AMD GPU Operator runs its Device Metrics Exporter, the Grafana adds the exporter's GPU
  dashboards. Uninstalling the Platform removes it with the cluster.

Turning the Grafana on later redeploys the Platform; turning it off after deployment is not
supported yet. A Platform deployed before this has the Grafana turned off: turn it on to redeploy
the Platform with its Grafana. Until then a Slurm Platform keeps its own Prometheus and the Grafana
link it had, and a Kubernetes Platform has no Grafana. A **Platform Grafana** link that still opens
Grafana's home page switches to the main dashboard on the Platform's next deploy.

## GPU and job monitoring on Slurm

Choose the monitoring exporter in the Slurm workspace's Features step:

- **Omnistat** (proposed by default) collects host and Slurm job metrics on every compute node,
  and GPU metrics where there are AMD GPUs, so it also suits a CPU-only Platform. Each compute node
  runs one job at a time. Omnistat's `omnistat-query` job report tool on the compute nodes reads
  the metrics through the Platform's Grafana, so keep the Grafana on to use it (its report needs
  GPU utilization, which SR-IOV virtual functions do not report; on those GPUs use the job
  dashboards instead).
  - **GPU hardware counters**, checked by default under Omnistat, turns on Omnistat's GPU hardware
    counter collection. Which counters it samples is fixed by swallow; today that is `VALUBusy`, how
    busy each GPU's vector ALUs are, which the Grafana job dashboards show as hardware counters. Each
    compute node collects them only when it can: it needs AMD GPUs, a ROCm version swallow ships the
    counter extension for (ROCm 10.0 and 10.1), and a test sample that works on its GPUs. A node that
    cannot leaves them off and keeps every other metric; the deployment never fails because of them.
    The Platform page lists, per compute node, whether it collects them or why not. While a node
    collects them, its exporter holds the `CAP_PERFMON` capability and keeps a profiling session on
    every GPU (about 160 MB of GPU memory each). A GPU allows one profiler session at a time, so a
    user's own profiler on that node (`rocprofv3`, Omniperf) may not get the counters while they
    are on; uncheck the option if you do not want that.
    On a deployed Platform, turn them on or off on its **Monitoring** tab: under **Omnistat**, the
    **GPU hardware counters** part has a **Collect GPU hardware counters** switch above each
    compute node's outcome, and a confirmation before anything changes. The CLI does the same with
    `swallow platforms hardware-counters enable|disable <platformId>`. Swallow reconfigures
    Omnistat on every compute node while the Platform stays active, which usually takes a few
    minutes. Slurm and running jobs are not touched; each node's exporter restarts once near the
    end, so its metrics can miss a scrape. Turned off, every compute node runs Omnistat without
    hardware counters.
- **AMD Device Metrics Exporter** collects per-GPU metrics labelled with the job on each GPU, and
  jobs keep sharing nodes. It needs a compute node with AMD GPUs.
- **None** installs neither.

The Site Prometheus collects the chosen exporter like any other; the Platform page shows its
state. In a deploy file, `omnistat: { enabled: true }` or `deviceMetricsExporter: { enabled: true }`
chooses one; omitting both installs neither. A deploy file asks for the hardware counters with
`omnistat: { enabled: true, hardwareCounters: true }`; unlike the wizard, a file that omits it gets
none.

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
    url: http://10.0.0.11:3000/d/edqnz7pce7togc-rms/global-rms
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
acknowledged field to a local alert copy. In the Dashboard, the alert list and
acknowledging are in development and hidden in release builds; the CLI and the API
offer them in every release.

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
