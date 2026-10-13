# Per-site Prometheus

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

This Prometheus is what swallow's usage charts read: once it runs and is registered as
the Site's `metrics` Integration, every deployed Server's Monitoring tab shows its CPU,
memory, disk, network, and GPU history. Grafana is optional.

Create one independent Compose project or native systemd installation per site. Replace
the Swallow URL, create `secrets/` with mode 0700, and install `secrets/machine-token`
(the installation's `secrets/machine-token`) with mode 0444. Directory traversal remains
root-only while the non-root container can read the bind-mounted file. Set
`PROMETHEUS_IMAGE` to an exact OCI digest.

The template scrapes swallow over plain HTTP, matching the production installation. When a
TLS terminator fronts swallow, switch the URLs to `https` and add a `tls_config` with its CA.

The default retention is 30 days, which bounds the longest usage history swallow can
chart (its largest window is 7 days). `node_exporter` (port 9100) and, on `amd-gpu`
Servers, the RDC exporter (port 5000) are installed by versioned Swallow playbooks under
the exporter-owner policy; the two scrape jobs in the template match those ports. A later
central TSDB can be added through `remote_write` without changing discovery labels.
