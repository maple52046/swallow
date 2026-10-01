# Per-site Prometheus

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

Create one independent Compose project or native systemd installation per site. Replace
the Swallow URL, create `secrets/` with mode 0700, and install `secrets/machine-token`
(the installation's `secrets/machine-token`) with mode 0444. Directory traversal remains
root-only while the non-root container can read the bind-mounted file. Set
`PROMETHEUS_IMAGE` to an exact OCI digest.

The template scrapes swallow over plain HTTP, matching the production installation. When a
TLS terminator fronts swallow, switch the URLs to `https` and add a `tls_config` with its CA.

The default retention is 30 days. `node_exporter` and policy-approved DCGM exporter
installation belongs to versioned Swallow playbooks. A later central TSDB can be added
through `remote_write` without changing discovery labels.
