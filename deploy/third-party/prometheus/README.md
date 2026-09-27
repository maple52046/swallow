# Per-site Prometheus

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

Create one independent Compose project or native systemd installation per site. Replace
the Swallow URL, create `secrets/` with mode 0700, and install both
`secrets/machine-token` and the issuing CA certificate
`secrets/swallow-ca.crt` with mode 0444. Directory traversal remains root-only while
the non-root container can read the bind-mounted files. Set `PROMETHEUS_IMAGE` to the release
manifest's exact OCI digest.

The default retention is 30 days. `node_exporter` and policy-approved DCGM exporter
installation belongs to versioned Swallow playbooks. A later central TSDB can be added
through `remote_write` without changing discovery labels.
